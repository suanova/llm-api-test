package runner

import (
	"fmt"
	"sort"
	"time"

	"llm-api-test/internal/registry"
)

// Summary holds p50/p95/p99/min/max for a duration metric.
type Summary struct {
	P50, P95, P99, Min, Max time.Duration
}

// FloatSummary is Summary for float metrics (TPS).
type FloatSummary struct {
	P50, P95, P99, Min, Max float64
}

// IntSummary is Summary for token counts.
type IntSummary struct {
	P50, P95, P99, Min, Max int
}

// percentileIndex is the "nearest rank" index into a sorted slice.
func percentileIndex(n, pct int) int {
	if n <= 1 {
		return 0
	}
	if idx := (pct * n) / 100; idx < n {
		return idx
	}
	return n - 1
}

func durationSummary(durs []time.Duration) Summary {
	if len(durs) == 0 {
		return Summary{}
	}
	sorted := make([]time.Duration, len(durs))
	copy(sorted, durs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return Summary{
		Min: sorted[0],
		Max: sorted[len(sorted)-1],
		P50: sorted[percentileIndex(len(sorted), 50)],
		P95: sorted[percentileIndex(len(sorted), 95)],
		P99: sorted[percentileIndex(len(sorted), 99)],
	}
}

func floatSummary(vals []float64) FloatSummary {
	if len(vals) == 0 {
		return FloatSummary{}
	}
	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	sort.Float64s(sorted)
	return FloatSummary{
		Min: sorted[0],
		Max: sorted[len(sorted)-1],
		P50: sorted[percentileIndex(len(sorted), 50)],
		P95: sorted[percentileIndex(len(sorted), 95)],
		P99: sorted[percentileIndex(len(sorted), 99)],
	}
}

func intSummary(vals []int) IntSummary {
	if len(vals) == 0 {
		return IntSummary{}
	}
	sorted := make([]int, len(vals))
	copy(sorted, vals)
	sort.Ints(sorted)
	return IntSummary{
		Min: sorted[0],
		Max: sorted[len(sorted)-1],
		P50: sorted[percentileIndex(len(sorted), 50)],
		P95: sorted[percentileIndex(len(sorted), 95)],
		P99: sorted[percentileIndex(len(sorted), 99)],
	}
}

// FormatSummary renders a duration Summary in milliseconds.
func FormatSummary(s Summary) string {
	ms := func(d time.Duration) string { return d.Round(time.Millisecond).String() }
	return fmt.Sprintf("p50=%s p95=%s p99=%s min=%s max=%s",
		ms(s.P50), ms(s.P95), ms(s.P99), ms(s.Min), ms(s.Max))
}

// FormatSummaryMs1 renders a duration Summary with one decimal millisecond
// precision (used for TPOT, which is typically sub-10ms).
func FormatSummaryMs1(s Summary) string {
	ms := func(d time.Duration) string { return fmt.Sprintf("%.1fms", float64(d)/float64(time.Millisecond)) }
	return fmt.Sprintf("p50=%s p95=%s p99=%s min=%s max=%s",
		ms(s.P50), ms(s.P95), ms(s.P99), ms(s.Min), ms(s.Max))
}

// FormatFloatSummary renders a FloatSummary.
func FormatFloatSummary(s FloatSummary) string {
	f := func(v float64) string { return fmt.Sprintf("%.1f", v) }
	return fmt.Sprintf("p50=%s p95=%s p99=%s min=%s max=%s",
		f(s.P50), f(s.P95), f(s.P99), f(s.Min), f(s.Max))
}

// FormatIntSummary renders the p50/p95/p99 of an IntSummary (min/max are
// omitted for token counts).
func FormatIntSummary(s IntSummary) string {
	return fmt.Sprintf("p50=%d p95=%d p99=%d", s.P50, s.P95, s.P99)
}

// aggregate computes summaries from per-request metrics. Failed requests
// count toward Failed and are excluded from the summaries.
func aggregate(metrics []registry.Metrics) BenchmarkReport {
	r := BenchmarkReport{TotalRequests: len(metrics)}
	var ttfb, ttft, total, tpot []time.Duration
	var tps []float64
	var completion, prompt []int
	successes := 0
	for _, m := range metrics {
		if m.Err != nil {
			r.Failed++
			r.Errors = append(r.Errors, m.Err.Error())
			continue
		}
		successes++
		if m.TTFB > 0 {
			ttfb = append(ttfb, m.TTFB)
		}
		if m.TTFT > 0 {
			ttft = append(ttft, m.TTFT)
		}
		if m.Total > 0 {
			total = append(total, m.Total)
		}
		tpot = append(tpot, m.TPOTs...)
		if t := m.Total - m.TTFB; t > 0 && m.CompletionTokens > 0 {
			tps = append(tps, float64(m.CompletionTokens)/t.Seconds())
		}
		if m.CompletionTokens > 0 {
			completion = append(completion, m.CompletionTokens)
		}
		if m.PromptTokens > 0 {
			prompt = append(prompt, m.PromptTokens)
		}
		r.AvgContentBytes += int64(m.ContentBytes)
		r.AvgChunks += int64(m.Chunks)
	}
	r.TTFB = durationSummary(ttfb)
	r.TTFT = durationSummary(ttft)
	r.Total = durationSummary(total)
	r.TPOT = durationSummary(tpot)
	r.TPS = floatSummary(tps)
	r.Tokens.Completion = intSummary(completion)
	r.Tokens.Prompt = intSummary(prompt)
	if successes > 0 {
		r.AvgContentBytes /= int64(successes)
		r.AvgChunks /= int64(successes)
	}
	return r
}
