package runner

import (
	"context"
	"testing"
	"time"

	"llm-api-test/internal/registry"
)

func TestDurationSummary(t *testing.T) {
	// 1s..10s: nearest-rank p50 = 6s, p95 = 10s, p99 = 10s.
	durs := []time.Duration{
		1e9, 2e9, 3e9, 4e9, 5e9, 6e9, 7e9, 8e9, 9e9, 10e9,
	}
	s := durationSummary(durs)
	if s.Min != 1e9 || s.Max != 10e9 {
		t.Errorf("min/max = %v/%v, want 1s/10s", s.Min, s.Max)
	}
	if s.P50 != 6e9 {
		t.Errorf("p50 = %v, want 6s", s.P50)
	}
	if s.P95 != 10e9 || s.P99 != 10e9 {
		t.Errorf("p95/p99 = %v/%v, want 10s/10s", s.P95, s.P99)
	}
}

func TestAggregateExcludesFailed(t *testing.T) {
	ms := []registry.Metrics{
		{Total: 2 * time.Second, TTFB: 100 * time.Millisecond, TTFT: 200 * time.Millisecond,
			CompletionTokens: 10, ContentBytes: 20, Chunks: 4},
		{Err: errBoom},
	}
	r := aggregate(ms)
	if r.TotalRequests != 2 || r.Failed != 1 {
		t.Errorf("TotalRequests/Failed = %d/%d, want 2/1", r.TotalRequests, r.Failed)
	}
	if r.Total.P50 != 2*time.Second {
		t.Errorf("Total.P50 = %v, want 2s", r.Total.P50)
	}
	if r.AvgContentBytes != 20 || r.AvgChunks != 4 {
		t.Errorf("AvgContentBytes/AvgChunks = %d/%d, want 20/4", r.AvgContentBytes, r.AvgChunks)
	}
	if r.Tokens.Completion.P50 != 10 {
		t.Errorf("Completion.P50 = %d, want 10", r.Tokens.Completion.P50)
	}
}

var errBoom = context.Canceled

func TestRunCompatGating(t *testing.T) {
	run := 0
	cases := []registry.CompatCase{
		&fakeCase{id: "f:basic", name: "basic", res: &registry.CompatResult{Pass: false, Detail: "boom"}, run: &run},
		&fakeCase{id: "f:seed", name: "seed", res: &registry.CompatResult{Pass: true}, run: &run},
	}
	results := RunCompat(context.Background(), cases, "m")
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if run != 1 {
		t.Errorf("cases executed %d times, want 1 (seed must be skipped)", run)
	}
	if results[1].Result.Pass || results[1].Result.Detail != "skipped: basic failed" {
		t.Errorf("skipped result = %+v, want fail with 'skipped: basic failed'", results[1].Result)
	}
	if !AnyFailed(results) {
		t.Error("AnyFailed = false, want true")
	}
}

func TestRunCompatAllPass(t *testing.T) {
	run := 0
	cases := []registry.CompatCase{
		&fakeCase{id: "f:basic", name: "basic", res: &registry.CompatResult{Pass: true}, run: &run},
		&fakeCase{id: "f:seed", name: "seed", res: &registry.CompatResult{Pass: true}, run: &run},
	}
	results := RunCompat(context.Background(), cases, "m")
	if run != 2 {
		t.Errorf("cases executed %d times, want 2", run)
	}
	if AnyFailed(results) {
		t.Error("AnyFailed = true, want false")
	}
}

func TestRunBenchmark(t *testing.T) {
	bc := fakeBenchmarkCase{}
	r := RunBenchmark(context.Background(), bc, "m", "pong", 2, 3, nil)
	if r.TotalRequests != 6 {
		t.Errorf("TotalRequests = %d, want 6", r.TotalRequests)
	}
	if r.Failed != 0 {
		t.Errorf("Failed = %d, want 0", r.Failed)
	}
	if r.RPS <= 0 {
		t.Error("RPS = 0, want > 0")
	}
	if r.Total.P50 != 100*time.Millisecond {
		t.Errorf("Total.P50 = %v, want 100ms", r.Total.P50)
	}
	if got := r.TPS.P50; got < 55 || got > 56 { // 5 tokens / (100-10)ms
		t.Errorf("TPS.P50 = %v, want ~55.6", got)
	}
	if r.AvgContentBytes != 5 || r.AvgChunks != 5 {
		t.Errorf("AvgContentBytes/AvgChunks = %d/%d, want 5/5", r.AvgContentBytes, r.AvgChunks)
	}
}

type fakeCase struct {
	id   string
	name string
	res  *registry.CompatResult
	run  *int
}

func (f *fakeCase) ID() string   { return f.id }
func (f *fakeCase) Name() string { return f.name }
func (f *fakeCase) Desc() string { return "fake" }

func (f *fakeCase) Run(ctx context.Context, model string) *registry.CompatResult {
	*f.run++
	return f.res
}

type fakeBenchmarkCase struct{}

func (fakeBenchmarkCase) ID() string   { return "fake:benchmark" }
func (fakeBenchmarkCase) Desc() string { return "fake" }

func (fakeBenchmarkCase) Run(ctx context.Context, model, prompt string) *registry.Metrics {
	return &registry.Metrics{
		Total:            100 * time.Millisecond,
		TTFB:             10 * time.Millisecond,
		TTFT:             20 * time.Millisecond,
		CompletionTokens: 5,
		PromptTokens:     10,
		ContentBytes:     5,
		Chunks:           5,
	}
}
