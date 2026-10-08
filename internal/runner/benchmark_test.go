package runner

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestProgressLine(t *testing.T) {
	got := progressLine(3*time.Second, 1, 4)
	want := "[benchmark] elapsed 3s, 1/4 requests completed"
	if got != want {
		t.Errorf("progressLine = %q, want %q", got, want)
	}
}

// RunBenchmark derives the input-token rate from the run's wall clock and
// keeps one sample per request for the JSON report.
func TestRunBenchmarkInputRateAndSamples(t *testing.T) {
	bc := fakeBenchmarkCase{}
	r := RunBenchmark(context.Background(), bc, "m", "pong", 2, 3, nil)
	if r.InputTokens != 60 { // 6 requests x 10 prompt tokens
		t.Errorf("InputTokens = %d, want 60", r.InputTokens)
	}
	if r.InputTokensPerSec <= 0 {
		t.Errorf("InputTokensPerSec = %v, want > 0", r.InputTokensPerSec)
	}
	if len(r.Samples) != 6 {
		t.Errorf("Samples = %d, want 6", len(r.Samples))
	}
}

// The throughput text report must surface the usage-derived figures the
// vendor comparison needs: input tok/s and observed cache reads.
func TestFormatBenchmarkReportRates(t *testing.T) {
	r := BenchmarkReport{
		CaseID: "chat:benchmark", Mode: "throughput", Stream: true, TotalRequests: 9,
		InputTokens: 9000, CachedTokens: 7200, CacheHitRate: 0.8, InputTokensPerSec: 1234.5,
	}
	out := FormatBenchmarkReport(r)
	if !strings.Contains(out, "Input:") || !strings.Contains(out, "1234.5 tok/s") {
		t.Errorf("report missing input rate:\n%s", out)
	}
	if !strings.Contains(out, "Cache:") || !strings.Contains(out, "7200/9000") {
		t.Errorf("report missing cache line:\n%s", out)
	}
}
