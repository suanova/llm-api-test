package runner

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"llm-api-test/internal/registry"
)

// httpClass backs the rate-mode failure tally: 429 vs 5xx vs everything else.
func TestHTTPClass(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{nil, ""},
		{fmt.Errorf(`HTTP 429: {"error":"rate limited"}`), "429"},
		{fmt.Errorf("HTTP 503: overloaded"), "5xx"},
		{fmt.Errorf("HTTP 500: boom"), "5xx"},
		{fmt.Errorf(`HTTP 400: {"error":"bad"}`), "other"},
		{errors.New("http: connection reset by peer"), "other"},
		{context.DeadlineExceeded, "other"},
	}
	for _, c := range cases {
		if got := httpClass(c.err); got != c.want {
			t.Errorf("httpClass(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}

// rateCase is a fake BenchmarkCase with controllable latency and errors.
type rateCase struct {
	sleep time.Duration
	err   error
	run   atomic.Int64
}

func (f *rateCase) ID() string   { return "fake:rate" }
func (f *rateCase) Desc() string { return "fake" }
func (f *rateCase) Run(ctx context.Context, model, prompt string) *registry.Metrics {
	f.run.Add(1)
	time.Sleep(f.sleep)
	return &registry.Metrics{Total: f.sleep, Err: f.err, PromptTokens: 10, CompletionTokens: 5}
}

// RunRate offers rps requests/second for the duration: with a fast case the
// sent count matches the offered schedule and nothing is shed.
func TestRunRatePacing(t *testing.T) {
	bc := &rateCase{sleep: 5 * time.Millisecond}
	r := RunRate(context.Background(), bc, "m", "p", 20, 500*time.Millisecond, 100, nil)
	// 20 rps x 0.5s = 10 offered; pacing jitter makes this a range check.
	if r.Sent < 7 || r.Sent > 13 {
		t.Errorf("Sent = %d, want ~10", r.Sent)
	}
	if r.Shed != 0 || r.HTTP429 != 0 || r.Incomplete != 0 {
		t.Errorf("shed/429/incomplete = %d/%d/%d, want 0/0/0", r.Shed, r.HTTP429, r.Incomplete)
	}
	if r.OfferedRPS != 20 || r.Duration != 500*time.Millisecond {
		t.Errorf("offered/duration = %v/%v", r.OfferedRPS, r.Duration)
	}
	if r.AchievedRPS <= 0 {
		t.Errorf("AchievedRPS = %v, want > 0", r.AchievedRPS)
	}
	// Rate reports carry the usage-derived rates too (same as wave mode).
	if r.TokensPerSec <= 0 || r.InputTokensPerSec <= 0 {
		t.Errorf("TokensPerSec/InputTokensPerSec = %v/%v, want > 0", r.TokensPerSec, r.InputTokensPerSec)
	}
	if r.TotalRequests < 5 {
		t.Errorf("TotalRequests = %d, want ~10", r.TotalRequests)
	}
}

// A slow case with a low in-flight cap makes ticks shed instead of piling up
// requests: offered load is kept, concurrency is capped.
func TestRunRateShedsAtInFlightCap(t *testing.T) {
	bc := &rateCase{sleep: 300 * time.Millisecond}
	r := RunRate(context.Background(), bc, "m", "p", 50, 200*time.Millisecond, 1, nil)
	if r.Shed == 0 {
		t.Error("Shed = 0, want > 0 with cap 1 and 300ms requests")
	}
	if r.Sent > 3 {
		t.Errorf("Sent = %d, want <= 3 (cap 1)", r.Sent)
	}
}

// HTTP failures are tallied by class; they do not stop the run.
func TestRunRateCountsFailures(t *testing.T) {
	bc := &rateCase{sleep: 5 * time.Millisecond, err: fmt.Errorf(`HTTP 429: {"error":"slow down"}`)}
	r := RunRate(context.Background(), bc, "m", "p", 20, 300*time.Millisecond, 100, nil)
	if r.HTTP429 == 0 {
		t.Error("HTTP429 = 0, want > 0")
	}
	if r.Failed != r.HTTP429 {
		t.Errorf("Failed = %d, HTTP429 = %d, want equal", r.Failed, r.HTTP429)
	}
}

// Requests still running when the drain budget (ctx) expires are counted as
// incomplete rather than silently dropped.
func TestRunRateCountsIncompleteOnDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	bc := &rateCase{sleep: 400 * time.Millisecond}
	r := RunRate(ctx, bc, "m", "p", 20, 200*time.Millisecond, 10, nil)
	if r.Incomplete == 0 {
		t.Error("Incomplete = 0, want > 0 when requests outlive the ctx deadline")
	}
}
