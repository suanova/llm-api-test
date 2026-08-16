package runner

import (
	"context"
	"fmt"
	"strings"
	"time"

	"llm-api-test/internal/registry"
)

// CacheReport is the aggregated outcome of one cache session.
type CacheReport struct {
	CaseID string
	Turns  []registry.CacheTurn

	SessionHitRate  float64 // ΣCached / ΣPrompt over all turns
	WarmHitRate     float64 // turns 2..N only
	RequestLevelHit float64 // warm turns with Cached > 0 / warm turns executed
	CachedTokens    int64
	PromptTokens    int64
	WarmTurns       int
	ColdTotal       time.Duration
	WarmTotalP50    time.Duration
	FailedTurns     int
	Verdict         string // cache observed | no cache observed | inconclusive
}

// RunCache runs one cache session and aggregates the per-turn observations.
func RunCache(ctx context.Context, cc registry.CacheCase, model string, turns int) CacheReport {
	obs := cc.RunSession(ctx, model, turns)
	r := CacheReport{CaseID: cc.ID(), Turns: obs}
	var warmTotals []time.Duration
	var allCached, allPrompt, warmCached, warmPrompt int64
	warmHits := 0
	for _, t := range obs {
		if t.Err != nil {
			r.FailedTurns++
			continue
		}
		allCached += int64(t.Cached)
		allPrompt += int64(t.PromptTokens)
		if t.Turn == 1 {
			r.ColdTotal = t.Total
			continue
		}
		warmCached += int64(t.Cached)
		warmPrompt += int64(t.PromptTokens)
		warmTotals = append(warmTotals, t.Total)
		r.WarmTurns++
		if t.Cached > 0 {
			warmHits++
		}
	}
	r.CachedTokens = allCached
	r.PromptTokens = allPrompt
	if allPrompt > 0 {
		r.SessionHitRate = float64(allCached) / float64(allPrompt)
	}
	if warmPrompt > 0 {
		r.WarmHitRate = float64(warmCached) / float64(warmPrompt)
	}
	if r.WarmTurns > 0 {
		r.RequestLevelHit = float64(warmHits) / float64(r.WarmTurns)
		r.WarmTotalP50 = durationSummary(warmTotals).P50
	}
	r.Verdict = cacheVerdict(r, warmHits)
	return r
}

// cacheVerdict classifies the session outcome (design.md "Verdicts").
func cacheVerdict(r CacheReport, warmHits int) string {
	if warmHits > 0 {
		return "cache observed"
	}
	if r.FailedTurns == 0 {
		return "no cache observed"
	}
	return "inconclusive"
}

// FormatCacheReport renders the text report (design.md "cache").
func FormatCacheReport(r CacheReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %s  (%d turns, non-streamed)\n", r.CaseID, len(r.Turns))
	b.WriteString("    Turn  prompt  cached  written  miss  read%   total\n")
	for _, t := range r.Turns {
		if t.Err != nil {
			fmt.Fprintf(&b, "    %d    error: %v\n", t.Turn, t.Err)
			continue
		}
		miss := t.PromptTokens - t.Cached - t.CacheWrite
		readPct := 0.0
		if t.PromptTokens > 0 {
			readPct = 100 * float64(t.Cached) / float64(t.PromptTokens)
		}
		fmt.Fprintf(&b, "    %-4d %-7d %-7d %-8d %-5d %6.1f%% %8s\n",
			t.Turn, t.PromptTokens, t.Cached, t.CacheWrite, miss, readPct, t.Total.Round(time.Millisecond))
	}
	fmt.Fprintf(&b, "    Session hit rate: %.1f%% (cached %d / prompt %d)\n",
		100*r.SessionHitRate, r.CachedTokens, r.PromptTokens)
	fmt.Fprintf(&b, "    Warm turns: %.1f%% token-weighted, %.0f/%d request-level\n",
		100*r.WarmHitRate, r.RequestLevelHit*float64(r.WarmTurns), r.WarmTurns)
	fmt.Fprintf(&b, "    Cold %s vs warm p50 %s\n",
		r.ColdTotal.Round(time.Millisecond), r.WarmTotalP50.Round(time.Millisecond))
	fmt.Fprintf(&b, "    Verdict: %s\n", r.Verdict)
	fmt.Fprintf(&b, "    Failed: %d/%d\n", r.FailedTurns, len(r.Turns))
	return b.String()
}
