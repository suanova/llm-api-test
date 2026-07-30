package runner

import (
	"fmt"
	"sort"
	"time"
)

// StreamMetrics captures timing and token metrics from a single streaming
// API request.
type StreamMetrics struct {
	// TTFB is the time from request start to the first byte of the HTTP
	// response body (HTTP-level first-byte latency).
	TTFB time.Duration
	// TTFT is the time from request start to the first content token in the
	// SSE stream. This is the user-perceived "time to first token".
	TTFT time.Duration
	// TotalTime is the full end-to-end wall-clock time for the request.
	TotalTime time.Duration
	// CompletionTokens is the number of output tokens. Priority:
	//  1. usage field from the API (most accurate)
	//  2. SSE content chunk count (each delta ≈ 1 token)
	//  3. character heuristic (len/4)
	CompletionTokens int
	// PromptTokens is the number of input tokens (from usage, if available).
	PromptTokens int
	// ReasoningTokens is the number of reasoning/thinking tokens (from usage
	// or chunk counting). Used for TPS/TPOT calculations alongside
	// CompletionTokens so that total output = CompletionTokens + ReasoningTokens.
	ReasoningTokens int
	// ContentLen is the byte length of the accumulated content text. Useful
	// as a sanity check for token count accuracy.
	ContentLen int
	// ChunkCount is the number of SSE content delta events received.
	// Each chunk typically corresponds to ~1 token.
	ChunkCount int
	// NonStreaming is true when the request was made without streaming
	// (either because streaming was not requested, or as a fallback when the
	// server does not support streaming). TTFT = Total in this case.
	NonStreaming bool
	// Err is non-nil if this request failed.
	Err error
}

// TotalOutputTokens returns the total number of output tokens including both
// completion and reasoning tokens. This is what TPS/TPOT should be computed
// against, since reasoning tokens also consume generation time.
func (m StreamMetrics) TotalOutputTokens() int {
	return m.CompletionTokens + m.ReasoningTokens
}

// TPS returns tokens per second. For reasoning models, output time is measured
// from TTFB (when the server starts sending data) to TotalTime, since tokens
// (both reasoning and content) are generated throughout that window.
// Returns 0 if there are no output tokens or if the output time is zero.
func (m StreamMetrics) TPS() float64 {
	outputTime := m.TotalTime - m.TTFB
	total := m.TotalOutputTokens()
	if outputTime <= 0 || total <= 0 {
		return 0
	}
	return float64(total) / outputTime.Seconds()
}

// TPOT returns time per output token. For reasoning models, output time is
// measured from TTFB to TotalTime.
// Returns 0 if there are no output tokens.
func (m StreamMetrics) TPOT() time.Duration {
	outputTime := m.TotalTime - m.TTFB
	total := m.TotalOutputTokens()
	if total <= 0 {
		return 0
	}
	return outputTime / time.Duration(total)
}

// MetricsSummary holds computed percentile values for a set of metrics.
type MetricsSummary struct {
	P50 time.Duration
	P95 time.Duration
	P99 time.Duration
	Min time.Duration
	Max time.Duration
}

// Percentile computes p50/p95/p99/min/max for a slice of durations.
// Returns a zero summary if the slice is empty.
func Percentile(durs []time.Duration) MetricsSummary {
	if len(durs) == 0 {
		return MetricsSummary{}
	}
	sorted := make([]time.Duration, len(durs))
	copy(sorted, durs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	return MetricsSummary{
		Min: sorted[0],
		Max: sorted[len(sorted)-1],
		P50: sorted[percentileIndex(len(sorted), 50)],
		P95: sorted[percentileIndex(len(sorted), 95)],
		P99: sorted[percentileIndex(len(sorted), 99)],
	}
}

// percentileIndex returns the index into a sorted slice for the given
// percentile (0-100). Uses the "nearest rank" method.
func percentileIndex(n, pct int) int {
	if n <= 1 {
		return 0
	}
	idx := (pct * n) / 100
	if idx >= n {
		idx = n - 1
	}
	return idx
}

// FormatPercentile renders a MetricsSummary as a one-line string.
// If label is non-empty, it's prepended with a colon separator.
// TPOT values are formatted in microseconds (µs) since they are typically
// small; other metrics use milliseconds (ms).
func FormatPercentile(label string, s MetricsSummary) string {
	prefix := ""
	if label != "" {
		prefix = label + ": "
	}
	return fmt.Sprintf("%sp50=%s p95=%s p99=%s min=%s max=%s",
		prefix, s.P50.Round(time.Millisecond), s.P95.Round(time.Millisecond),
		s.P99.Round(time.Millisecond), s.Min.Round(time.Millisecond),
		s.Max.Round(time.Millisecond))
}

// FormatPercentileMicros renders a MetricsSummary using microsecond precision.
// Used for TPOT which is typically in the millisecond/sub-millisecond range.
func FormatPercentileMicros(label string, s MetricsSummary) string {
	prefix := ""
	if label != "" {
		prefix = label + ": "
	}
	toMicros := func(d time.Duration) string {
		us := d.Microseconds()
		if us >= 1000 {
			return fmt.Sprintf("%.1fms", float64(us)/1000.0)
		}
		return fmt.Sprintf("%dµs", us)
	}
	return fmt.Sprintf("%sp50=%s p95=%s p99=%s min=%s max=%s",
		prefix, toMicros(s.P50), toMicros(s.P95),
		toMicros(s.P99), toMicros(s.Min), toMicros(s.Max))
}

// AggregateMetrics collects all per-request StreamMetrics from a benchmark
// run and computes summary percentiles.
type AggregateMetrics struct {
	// PerRequest is the raw per-request metrics, in order of completion.
	PerRequest []StreamMetrics

	TTFBSummary  MetricsSummary
	TTFTSummary  MetricsSummary
	TotalSummary MetricsSummary
	TPOTSummary  MetricsSummary
	TPSSummary   FloatMetricsSummary
	Tokens       TokenSummary

	TotalRequests int
	Failed        int
}

// TokenSummary holds token count percentiles.
type TokenSummary struct {
	CompletionP50 int
	CompletionP95 int
	CompletionP99 int
	PromptP50     int
	PromptP95     int
	PromptP99     int
	ReasoningP50  int
	ReasoningP95  int
	ReasoningP99  int
}

// FloatMetricsSummary holds percentile values for a float64 metric (TPS).
type FloatMetricsSummary struct {
	P50 float64
	P95 float64
	P99 float64
	Min float64
	Max float64
}

// Aggregate computes percentile summaries from a slice of StreamMetrics.
// It separates successful and failed requests.
func Aggregate(metrics []StreamMetrics) AggregateMetrics {
	var agg AggregateMetrics
	agg.PerRequest = metrics
	agg.TotalRequests = len(metrics)

	var ttfb, ttft, total, tpot []time.Duration
	var tps []float64
	var completionTokens, promptTokens, reasoningTokens []int

	for _, m := range metrics {
		if m.Err != nil {
			agg.Failed++
			continue
		}
		if m.TTFB > 0 {
			ttfb = append(ttfb, m.TTFB)
		}
		if m.TTFT > 0 {
			ttft = append(ttft, m.TTFT)
		}
		if m.TotalTime > 0 {
			total = append(total, m.TotalTime)
		}
		tpot = append(tpot, m.TPOT())
		if tpsVal := m.TPS(); tpsVal > 0 {
			tps = append(tps, tpsVal)
		}
		if m.CompletionTokens > 0 {
			completionTokens = append(completionTokens, m.CompletionTokens)
		}
		if m.PromptTokens > 0 {
			promptTokens = append(promptTokens, m.PromptTokens)
		}
		if m.ReasoningTokens > 0 {
			reasoningTokens = append(reasoningTokens, m.ReasoningTokens)
		}
	}

	agg.TTFBSummary = Percentile(ttfb)
	agg.TTFTSummary = Percentile(ttft)
	agg.TotalSummary = Percentile(total)
	agg.TPOTSummary = Percentile(tpot)
	agg.TPSSummary = floatPercentile(tps)

	if len(completionTokens) > 0 {
		sort.Ints(completionTokens)
		agg.Tokens.CompletionP50 = completionTokens[percentileIndex(len(completionTokens), 50)]
		agg.Tokens.CompletionP95 = completionTokens[percentileIndex(len(completionTokens), 95)]
		agg.Tokens.CompletionP99 = completionTokens[percentileIndex(len(completionTokens), 99)]
	}
	if len(promptTokens) > 0 {
		sort.Ints(promptTokens)
		agg.Tokens.PromptP50 = promptTokens[percentileIndex(len(promptTokens), 50)]
		agg.Tokens.PromptP95 = promptTokens[percentileIndex(len(promptTokens), 95)]
		agg.Tokens.PromptP99 = promptTokens[percentileIndex(len(promptTokens), 99)]
	}
	if len(reasoningTokens) > 0 {
		sort.Ints(reasoningTokens)
		agg.Tokens.ReasoningP50 = reasoningTokens[percentileIndex(len(reasoningTokens), 50)]
		agg.Tokens.ReasoningP95 = reasoningTokens[percentileIndex(len(reasoningTokens), 95)]
		agg.Tokens.ReasoningP99 = reasoningTokens[percentileIndex(len(reasoningTokens), 99)]
	}

	return agg
}

// floatPercentile computes p50/p95/p99/min/max for a slice of float64.
func floatPercentile(vals []float64) FloatMetricsSummary {
	if len(vals) == 0 {
		return FloatMetricsSummary{}
	}
	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	sort.Float64s(sorted)

	return FloatMetricsSummary{
		Min: sorted[0],
		Max: sorted[len(sorted)-1],
		P50: sorted[floatPercentileIndex(len(sorted), 50)],
		P95: sorted[floatPercentileIndex(len(sorted), 95)],
		P99: sorted[floatPercentileIndex(len(sorted), 99)],
	}
}

func floatPercentileIndex(n, pct int) int {
	if n <= 1 {
		return 0
	}
	idx := (pct * n) / 100
	if idx >= n {
		idx = n - 1
	}
	return idx
}
