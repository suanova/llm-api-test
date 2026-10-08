package runner

import (
	"math"
	"testing"
	"time"

	"llm-api-test/internal/registry"
)

// StatsJSON must carry p90 alongside p50/p95/p99 (the vendor benchmark's
// percentile set).
func TestStatsJSONIncludesP90(t *testing.T) {
	s := Summary{P50: time.Second, P90: 2 * time.Second, P95: 3 * time.Second, P99: 4 * time.Second, Min: time.Second, Max: 4 * time.Second}
	if got := statsJSON(s).P90; got != 2000 {
		t.Errorf("statsJSON P90 = %d ms, want 2000", got)
	}
}

func TestFloatStatsJSONIncludesP90(t *testing.T) {
	f := FloatSummary{P50: 1, P90: 2, P95: 3, P99: 4, Min: 1, Max: 4}
	if got := floatStatsJSON(f).P90; got != 2 {
		t.Errorf("floatStatsJSON P90 = %v, want 2", got)
	}
}

// The JSON report carries one entry per request so the vendor benchmark's
// per-request statistics can be recomputed: tpot_ms is the decode time per
// output token (total-ttft)/completion, otps is its reciprocal, and failed
// requests keep their error instead of timings.
func TestBenchmarkJSONRequests(t *testing.T) {
	rep := BenchmarkReport{
		Mode: "throughput", Stream: true, TotalRequests: 2,
		Total:             Summary{P90: 90 * time.Millisecond},
		InputTokens:       5030,
		CachedTokens:      4970,
		CacheHitRate:      4970.0 / 5030,
		InputTokensPerSec: 1234.5,
		Samples: []registry.Metrics{
			{TTFB: 10 * time.Millisecond, TTFT: 20 * time.Millisecond, Total: 120 * time.Millisecond,
				CompletionTokens: 10, PromptTokens: 5030, CachedTokens: 4970, ReasoningTokens: 4, Chunks: 10},
			{Err: errBoom},
		},
	}
	j := rep.JSON("m", "http://x", "chat")

	if len(j.Requests) != 2 {
		t.Fatalf("Requests = %d, want 2", len(j.Requests))
	}
	r0 := j.Requests[0]
	// (120-20)ms / 10 output tokens = 10ms per token → 100 tok/s.
	if r0.TPOTMS != 10 {
		t.Errorf("TPOTMS = %v, want 10", r0.TPOTMS)
	}
	if math.Abs(r0.OTPS-100) > 0.001 {
		t.Errorf("OTPS = %v, want 100", r0.OTPS)
	}
	if r0.TTFBMS != 10 || r0.TTFTMS != 20 || r0.TotalMS != 120 {
		t.Errorf("request timings wrong: %+v", r0)
	}
	if r0.PromptTokens != 5030 || r0.CachedTokens != 4970 || r0.Chunks != 10 {
		t.Errorf("request tokens/chunks wrong: %+v", r0)
	}
	if r0.ReasoningTokens != 4 {
		t.Errorf("ReasoningTokens = %d, want 4", r0.ReasoningTokens)
	}
	if j.Requests[1].Error == "" {
		t.Error("failed request lost its error")
	}
	if j.Total.P90 != 90 {
		t.Errorf("Total.P90 = %d ms, want 90", j.Total.P90)
	}
	if j.InputTokens != 5030 || j.InputTokensPerSec != 1234.5 {
		t.Errorf("input rate fields wrong: %+v", j)
	}
	if j.CachedTokens != 4970 || math.Abs(j.CacheHitRate-4970.0/5030) > 1e-9 {
		t.Errorf("cache fields wrong: %+v", j)
	}
}
