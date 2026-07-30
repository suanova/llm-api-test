package runner

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// BenchmarkCase is a case that can be run in benchmark mode. It returns
// StreamMetrics for a single streaming request, which the benchmark runner
// aggregates into percentile summaries.
type BenchmarkCase interface {
	Name() string
	Desc() string
	RunBenchmark(ctx context.Context, model string) StreamMetrics
}

// BenchmarkRunner executes a BenchmarkCase repeatedly with concurrency,
// collecting per-request metrics.
type BenchmarkRunner struct {
	Case        BenchmarkCase
	Iterations  int
	Concurrency int
	Reporter    BenchmarkReporter
}

// Run executes the benchmark: iterations × concurrency requests, collecting
// StreamMetrics for each. It fans out concurrency goroutines per iteration
// (where each goroutine makes one request). Metrics are collected
// per-request and aggregated into percentiles.
func (r *BenchmarkRunner) Run(ctx context.Context, model string) *BenchmarkResult {
	totalRequests := r.Iterations * r.Concurrency
	if r.Reporter != nil {
		r.Reporter.BenchmarkStart(r.Case.Name(), r.Case.Desc(), r.Iterations, r.Concurrency)
	}

	start := time.Now()
	metrics := make([]StreamMetrics, totalRequests)

	var mu sync.Mutex
	var wg sync.WaitGroup

	idx := 0
	for iter := 0; iter < r.Iterations; iter++ {
		for g := 0; g < r.Concurrency; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				m := r.Case.RunBenchmark(ctx, model)
				mu.Lock()
				metrics[idx] = m
				idx++
				mu.Unlock()
			}()
		}
		// Wait for this batch to complete before starting the next iteration.
		// This groups requests into iteration batches for clearer semantics,
		// but metrics are still collected per-request.
		wg.Wait()

		if r.Reporter != nil {
			r.Reporter.BenchmarkIteration(iter+1, r.Iterations)
		}
	}

	elapsed := time.Since(start)
	agg := Aggregate(metrics)

	result := &BenchmarkResult{
		Name:        r.Case.Name(),
		Desc:        r.Case.Desc(),
		Aggregate:   agg,
		Elapsed:     elapsed,
		Iterations:  r.Iterations,
		Concurrency: r.Concurrency,
	}

	if r.Reporter != nil {
		r.Reporter.BenchmarkResult(result)
	}

	return result
}

// BenchmarkResult is the outcome of a benchmark run.
type BenchmarkResult struct {
	Name        string
	Desc        string
	Aggregate   AggregateMetrics
	Elapsed     time.Duration
	Iterations  int
	Concurrency int
	// Prompt is the benchmark prompt style ("pong" or "long"). When "pong",
	// throughput metrics (TPOT/TPS) are hidden from output since they are
	// not meaningful for single-token responses.
	Prompt string
}

// BenchmarkReporter consumes benchmark events as they complete.
type BenchmarkReporter interface {
	BenchmarkStart(name, desc string, iterations, concurrency int)
	BenchmarkIteration(current, total int)
	BenchmarkResult(r *BenchmarkResult)
}

// FormatBenchmarkResult renders a BenchmarkResult as a multi-line string.
func FormatBenchmarkResult(r *BenchmarkResult) string {
	agg := r.Aggregate
	totalRequests := r.Iterations * r.Concurrency
	lines := []string{
		fmt.Sprintf("  %s  (%d iters x %d concurrency = %d requests)",
			r.Name, r.Iterations, r.Concurrency, totalRequests),
	}

	if agg.Failed > 0 {
		lines = append(lines, fmt.Sprintf("    FAILED: %d/%d requests", agg.Failed, totalRequests))
		// Show the first few distinct error messages so the user can diagnose.
		seen := map[string]bool{}
		count := 0
		for _, m := range agg.PerRequest {
			if m.Err == nil || count >= 3 {
				break
			}
			msg := m.Err.Error()
			if !seen[msg] {
				seen[msg] = true
				lines = append(lines, fmt.Sprintf("      %s", msg))
				count++
			}
		}
	}

	// Note when non-streaming fallback was used.
	nonStreaming := 0
	for _, m := range agg.PerRequest {
		if m.NonStreaming && m.Err == nil {
			nonStreaming++
		}
	}
	if nonStreaming > 0 {
		lines = append(lines, fmt.Sprintf("    Note: %d/%d requests used non-streaming fallback (server does not support streaming)", nonStreaming, agg.TotalRequests-agg.Failed))
	}

	lines = append(lines,
		fmt.Sprintf("    %s", FormatPercentile("TTFB", agg.TTFBSummary)),
	)

	if agg.TTFTSummary.P50 > 0 {
		lines = append(lines,
			fmt.Sprintf("    %s", FormatPercentile("TTFT", agg.TTFTSummary)),
		)
	}

	lines = append(lines,
		fmt.Sprintf("    %s", FormatPercentile("Total", agg.TotalSummary)),
	)

	// Throughput metrics are only meaningful with multi-token output.
	// Hide TPOT/TPS/Tokens/Output when using the "pong" prompt.
	if r.Prompt != PromptPong {
		lines = append(lines,
			fmt.Sprintf("    %s", FormatPercentileMicros("TPOT", agg.TPOTSummary)),
			fmt.Sprintf("    TPS:   p50=%.1f  p95=%.1f  p99=%.1f  min=%.1f  max=%.1f tok/s",
				agg.TPSSummary.P50, agg.TPSSummary.P95, agg.TPSSummary.P99,
				agg.TPSSummary.Min, agg.TPSSummary.Max),
			fmt.Sprintf("    Tokens: completion p50=%d p95=%d p99=%d  prompt p50=%d p95=%d p99=%d",
				agg.Tokens.CompletionP50, agg.Tokens.CompletionP95, agg.Tokens.CompletionP99,
				agg.Tokens.PromptP50, agg.Tokens.PromptP95, agg.Tokens.PromptP99),
		)

		// Show reasoning tokens if any were detected (thinking/reasoning models).
		if agg.Tokens.ReasoningP50 > 0 || agg.Tokens.ReasoningP95 > 0 {
			lines = append(lines,
				fmt.Sprintf("    Reasoning: p50=%d p95=%d p99=%d",
					agg.Tokens.ReasoningP50, agg.Tokens.ReasoningP95, agg.Tokens.ReasoningP99),
			)
		}

		// Add content size and chunk count stats for sanity-checking token counts.
		contentStats := computeContentStats(agg.PerRequest)
		if contentStats.AvgContentLen > 0 || contentStats.AvgChunkCount > 0 {
			lines = append(lines,
				fmt.Sprintf("    Output: avg_content=%d bytes  avg_chunks=%d  (content_len/chunks ≈ %.1f bytes/chunk)",
					contentStats.AvgContentLen, contentStats.AvgChunkCount,
					contentStats.BytesPerChunk),
			)
		}
	}

	lines = append(lines,
		fmt.Sprintf("    Elapsed: %s", r.Elapsed.Round(time.Millisecond)),
	)

	return joinLines(lines)
}

// contentStats holds aggregate content size information for sanity-checking.
type contentStats struct {
	AvgContentLen int
	AvgChunkCount int
	BytesPerChunk float64
}

func computeContentStats(metrics []StreamMetrics) contentStats {
	var totalLen, totalChunks, count int
	for _, m := range metrics {
		if m.Err != nil {
			continue
		}
		if m.ContentLen > 0 || m.ChunkCount > 0 {
			totalLen += m.ContentLen
			totalChunks += m.ChunkCount
			count++
		}
	}
	if count == 0 {
		return contentStats{}
	}
	avgLen := totalLen / count
	avgChunks := totalChunks / count
	var bpc float64
	if avgChunks > 0 {
		bpc = float64(avgLen) / float64(avgChunks)
	}
	return contentStats{
		AvgContentLen: avgLen,
		AvgChunkCount: avgChunks,
		BytesPerChunk: bpc,
	}
}

func joinLines(lines []string) string {
	var b []byte
	for i, l := range lines {
		if i > 0 {
			b = append(b, '\n')
		}
		b = append(b, l...)
	}
	return string(b)
}
