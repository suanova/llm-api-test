package runner

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"llm-api-test/internal/registry"
)

// BenchmarkReport is the aggregated outcome of one benchmark case.
type BenchmarkReport struct {
	CaseID                  string
	Mode                    string // latency | throughput
	Prompt                  string // pong | long | filler
	Iterations              int
	Concurrency             int
	TotalRequests           int
	Failed                  int
	Errors                  []string // per-request error text of failed requests
	Stream                  bool
	TTFB, TTFT, Total, TPOT Summary
	TPS                     FloatSummary
	Tokens                  struct {
		Completion IntSummary
		Prompt     IntSummary
	}
	InputTokens       int     // Σ prompt tokens over successful requests
	CachedTokens      int     // Σ cache-read prompt tokens over successful requests
	CacheHitRate      float64 // CachedTokens / InputTokens; 0 when no prompt tokens reported
	InputTokensPerSec float64 // Σ prompt tokens / wall clock
	AvgContentBytes   int64
	AvgChunks         int64
	RPS               float64
	TokensPerSec      float64            // usage-based tokens/s (non-streamed runs)
	Samples           []registry.Metrics // per-request observations for the JSON report
	Elapsed           time.Duration
	// Rate mode (--rps) offering stats; zero for wave-based runs.
	OfferedRPS  float64
	Duration    time.Duration
	Sent        int
	Shed        int
	HTTP429     int
	HTTP5xx     int
	Incomplete  int
	InFlight    IntSummary
	AchievedRPS float64
}

// RunBenchmark runs the benchmark case `iterations` times, each time with
// `concurrency` parallel requests (iterations × concurrency requests total),
// and aggregates the per-request metrics. When progress is non-nil, a live
// status line (see progressLine) is written to it and cleared when done.
func RunBenchmark(ctx context.Context, bc registry.BenchmarkCase, model, prompt string, iterations, concurrency int, progress io.Writer) BenchmarkReport {
	start := time.Now()
	metrics := runWaves(ctx, bc, model, prompt, iterations, concurrency, progress)
	if progress != nil {
		fmt.Fprint(progress, "\r\033[K") // clear the live status line
	}

	r := aggregate(metrics)
	r.CaseID = bc.ID()
	r.Prompt = prompt
	r.Iterations = iterations
	r.Concurrency = concurrency

	sum := 0
	for _, m := range metrics {
		if m.Err == nil {
			sum += m.CompletionTokens
		}
	}
	if elapsed := time.Since(start); elapsed > 0 {
		r.RPS = float64(r.TotalRequests) / elapsed.Seconds()
		r.TokensPerSec = float64(sum) / elapsed.Seconds()
		r.InputTokensPerSec = float64(r.InputTokens) / elapsed.Seconds()
		r.Elapsed = elapsed
	}
	return r
}

// progressLine is the live status line printed while a benchmark runs.
func progressLine(elapsed time.Duration, done, total int) string {
	return fmt.Sprintf("[benchmark] elapsed %s, %d/%d requests completed",
		elapsed.Round(time.Second), done, total)
}

// runWaves runs `iterations` waves of `concurrency` parallel requests.
func runWaves(ctx context.Context, bc registry.BenchmarkCase, model, prompt string, iterations, concurrency int, progress io.Writer) []registry.Metrics {
	var mu sync.Mutex
	metrics := make([]registry.Metrics, 0, iterations*concurrency)
	total := iterations * concurrency
	start := time.Now()
	done := make(chan struct{})
	defer close(done)
	if progress != nil {
		go func() {
			t := time.NewTicker(time.Second)
			defer t.Stop()
			for {
				select {
				case <-done:
					return
				case now := <-t.C:
					mu.Lock()
					n := len(metrics)
					mu.Unlock()
					fmt.Fprintf(progress, "\r%s", progressLine(now.Sub(start), n, total))
				}
			}
		}()
	}
	for it := 0; it < iterations; it++ {
		var wg sync.WaitGroup
		for g := 0; g < concurrency; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				m := bc.Run(ctx, model, prompt)
				mu.Lock()
				metrics = append(metrics, *m)
				mu.Unlock()
			}()
		}
		wg.Wait()
	}
	return metrics
}

// FormatBenchmarkReport renders the text report (refactor_design.md). TTFB,
// TTFT, and TPOT require streaming and are omitted for non-streamed runs;
// TPOT/TPS/Tokens/Output are only reported in throughput mode.
func FormatBenchmarkReport(r BenchmarkReport) string {
	var b strings.Builder
	if r.OfferedRPS > 0 {
		fmt.Fprintf(&b, "  %s  rate mode: offered %.1f req/s for %s\n", r.CaseID, r.OfferedRPS, r.Duration)
		fmt.Fprintf(&b, "    Sent: %d  Shed: %d  Completed: %d  Incomplete: %d  HTTP 429: %d  HTTP 5xx: %d\n",
			r.Sent, r.Shed, r.TotalRequests, r.Incomplete, r.HTTP429, r.HTTP5xx)
		fmt.Fprintf(&b, "    Achieved: %.1f req/s   In-flight p50/p90/p99/max: %d/%d/%d/%d\n",
			r.AchievedRPS, r.InFlight.P50, r.InFlight.P90, r.InFlight.P99, r.InFlight.Max)
	} else {
		fmt.Fprintf(&b, "  %s  (%d iters x %d concurrency = %d requests)\n",
			r.CaseID, r.Iterations, r.Concurrency, r.TotalRequests)
	}
	if r.Stream {
		fmt.Fprintf(&b, "    TTFB:  %s\n", FormatSummary(r.TTFB))
		fmt.Fprintf(&b, "    TTFT:  %s\n", FormatSummary(r.TTFT))
	}
	fmt.Fprintf(&b, "    Total: %s\n", FormatSummary(r.Total))
	if r.Mode == "throughput" {
		if r.Stream {
			fmt.Fprintf(&b, "    TPOT:  %s\n", FormatSummaryMs1(r.TPOT))
			fmt.Fprintf(&b, "    TPS:   %s tok/s\n", FormatFloatSummary(r.TPS))
			fmt.Fprintf(&b, "    Tokens: completion %s  prompt %s\n",
				FormatIntSummary(r.Tokens.Completion), FormatIntSummary(r.Tokens.Prompt))
			fmt.Fprintf(&b, "    Output: avg_content=%d bytes  avg_chunks=%d\n",
				r.AvgContentBytes, r.AvgChunks)
		} else {
			fmt.Fprintf(&b, "    Tokens: %.1f tok/s (from usage)\n", r.TokensPerSec)
		}
		if r.InputTokensPerSec > 0 {
			fmt.Fprintf(&b, "    Input:  %.1f tok/s (from usage)\n", r.InputTokensPerSec)
		}
		if r.CachedTokens > 0 {
			fmt.Fprintf(&b, "    Cache:  %d/%d prompt tokens (%.1f%%) read from cache\n",
				r.CachedTokens, r.InputTokens, 100*r.CacheHitRate)
		}
	}
	fmt.Fprintf(&b, "    RPS:    %.1f req/s\n", r.RPS)
	fmt.Fprintf(&b, "    Failed: %d/%d\n", r.Failed, r.TotalRequests)
	for _, e := range r.Errors {
		fmt.Fprintf(&b, "      error: %s\n", e)
	}
	fmt.Fprintf(&b, "    Elapsed: %s\n", r.Elapsed.Round(100*time.Millisecond))
	return b.String()
}
