package runner

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"llm-api-test/internal/registry"
)

type stubCacheCase struct {
	turns []registry.CacheTurn
}

func (s *stubCacheCase) ID() string   { return "stub:cache" }
func (s *stubCacheCase) Desc() string { return "stub" }
func (s *stubCacheCase) RunSession(context.Context, string, int) []registry.CacheTurn {
	return s.turns
}

func turn(n, prompt, cached, written int, total time.Duration) registry.CacheTurn {
	return registry.CacheTurn{Turn: n, PromptTokens: prompt, Cached: cached, CacheWrite: written, Total: total}
}

func TestRunCacheAggregation(t *testing.T) {
	cc := &stubCacheCase{turns: []registry.CacheTurn{
		turn(1, 5000, 0, 5000, 1200*time.Millisecond),
		turn(2, 5030, 4970, 0, 340*time.Millisecond),
		turn(3, 5060, 5000, 0, 320*time.Millisecond),
	}}
	r := RunCache(context.Background(), cc, "m", 3)
	if r.FailedTurns != 0 {
		t.Errorf("failed = %d, want 0", r.FailedTurns)
	}
	if want := 0.6607; r.SessionHitRate < want-0.001 || r.SessionHitRate > want+0.001 {
		t.Errorf("session hit rate = %.4f, want ~%.4f (9970/15090)", r.SessionHitRate, want)
	}
	if want := 0.9881; r.WarmHitRate < want-0.001 || r.WarmHitRate > want+0.001 {
		t.Errorf("warm hit rate = %.4f, want ~%.4f (9970/10090)", r.WarmHitRate, want)
	}
	if r.RequestLevelHit != 1.0 {
		t.Errorf("request-level hit = %v, want 1.0", r.RequestLevelHit)
	}
	if r.CachedTokens != 9970 || r.PromptTokens != 15090 {
		t.Errorf("totals = %d/%d, want 9970/15090", r.CachedTokens, r.PromptTokens)
	}
	if r.ColdTotal != 1200*time.Millisecond || r.WarmTotalP50 != 340*time.Millisecond {
		t.Errorf("cold/warm = %v/%v, want 1.2s/340ms (p50 of {340,320} by nearest-rank)", r.ColdTotal, r.WarmTotalP50)
	}
	if r.WarmTurns != 2 {
		t.Errorf("warm turns = %d, want 2", r.WarmTurns)
	}
	if r.WarmHits != 2 {
		t.Errorf("warm hits = %d, want 2", r.WarmHits)
	}
	if r.Verdict != "cache observed" {
		t.Errorf("verdict = %q, want cache observed", r.Verdict)
	}
}

func TestRunCacheNoCacheObserved(t *testing.T) {
	cc := &stubCacheCase{turns: []registry.CacheTurn{
		turn(1, 5000, 0, 0, time.Second),
		turn(2, 5030, 0, 0, 900*time.Millisecond),
	}}
	r := RunCache(context.Background(), cc, "m", 2)
	if r.Verdict != "no cache observed" {
		t.Errorf("verdict = %q, want no cache observed", r.Verdict)
	}
	if r.RequestLevelHit != 0 || r.WarmHitRate != 0 {
		t.Errorf("rates = %v/%v, want 0/0", r.RequestLevelHit, r.WarmHitRate)
	}
}

func TestRunCacheInconclusiveOnFirstTurnFailure(t *testing.T) {
	cc := &stubCacheCase{turns: []registry.CacheTurn{
		{Turn: 1, Err: errors.New("boom")},
	}}
	r := RunCache(context.Background(), cc, "m", 3)
	if r.Verdict != "inconclusive" {
		t.Errorf("verdict = %q, want inconclusive", r.Verdict)
	}
	if r.FailedTurns != 1 {
		t.Errorf("failed = %d, want 1", r.FailedTurns)
	}
}

func TestRunCacheObservedDespiteLateFailure(t *testing.T) {
	cc := &stubCacheCase{turns: []registry.CacheTurn{
		turn(1, 5000, 0, 5000, time.Second),
		turn(2, 5030, 4970, 0, 340*time.Millisecond),
		{Turn: 3, Err: errors.New("boom")},
	}}
	r := RunCache(context.Background(), cc, "m", 3)
	if r.Verdict != "cache observed" {
		t.Errorf("verdict = %q, want cache observed", r.Verdict)
	}
	if r.FailedTurns != 1 || r.WarmTurns != 1 {
		t.Errorf("failed/warm = %d/%d, want 1/1", r.FailedTurns, r.WarmTurns)
	}
}

func TestFormatCacheReport(t *testing.T) {
	cc := &stubCacheCase{turns: []registry.CacheTurn{
		turn(1, 5000, 0, 5000, 1200*time.Millisecond),
		turn(2, 5030, 4970, 0, 340*time.Millisecond),
		turn(3, 5060, 5000, 0, 320*time.Millisecond),
	}}
	r := RunCache(context.Background(), cc, "m", 3)
	out := FormatCacheReport(r)
	for _, want := range []string{
		"stub:cache", "3 turns", "Turn", "read%",
		"Session hit rate: 66.1%", "Warm turns: 98.8%",
		"Cold 1.2s vs warm p50 340ms", "Verdict: cache observed", "Failed: 0/3",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q\n%s", want, out)
		}
	}
}

func TestCacheJSON(t *testing.T) {
	cc := &stubCacheCase{turns: []registry.CacheTurn{
		turn(1, 5000, 0, 5000, 1200*time.Millisecond),
		turn(2, 5030, 4970, 0, 340*time.Millisecond),
		{Turn: 3, Err: errors.New("boom")},
	}}
	r := RunCache(context.Background(), cc, "m", 3)
	j := r.CacheJSON("m", "http://x", "chat")
	if j.APIFormat != "chat" || j.Model != "m" || j.Verdict != "cache observed" {
		t.Errorf("metadata wrong: %+v", j)
	}
	if j.SessionHitRate <= 0 || j.WarmHitRate <= 0 || j.ColdTotalMS != 1200 || j.WarmTotalP50MS != 340 {
		t.Errorf("rates/ms wrong: %+v", j)
	}
	if len(j.Turns) != 3 {
		t.Fatalf("got %d JSON turns, want 3", len(j.Turns))
	}
	if j.Turns[1].Miss != 60 || j.Turns[1].Cached != 4970 {
		t.Errorf("turn 2 JSON = %+v, want miss=60 cached=4970", j.Turns[1])
	}
	if j.Turns[2].Error == "" || j.FailedTurns != 1 {
		t.Errorf("failed turn not surfaced: %+v", j)
	}
}
