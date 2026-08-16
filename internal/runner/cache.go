package runner

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
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
	WarmHits        int // warm turns where the request observed a cache hit
	ColdTotal       time.Duration
	WarmTotalP50    time.Duration
	FailedTurns     int
	Verdict         string // cache observed | no cache observed | inconclusive
}

// RunCache runs one cache session and aggregates the per-turn observations.
// When progress is non-nil, a live status line (see cacheProgressLine) is
// written to it and cleared when done.
func RunCache(ctx context.Context, cc registry.CacheCase, model string, turns int, progress io.Writer) CacheReport {
	var done atomic.Int32
	start := time.Now()
	doneCh := make(chan struct{})
	defer close(doneCh)
	if progress != nil {
		go func() {
			t := time.NewTicker(time.Second)
			defer t.Stop()
			for {
				select {
				case <-doneCh:
					return
				case now := <-t.C:
					fmt.Fprintf(progress, "\r%s", cacheProgressLine(now.Sub(start), int(done.Load()), turns))
				}
			}
		}()
	}
	obs := cc.RunSession(ctx, model, turns, func(n int) { done.Store(int32(n)) })
	if progress != nil {
		fmt.Fprint(progress, "\r\033[K") // clear the live status line
	}
	r := CacheReport{CaseID: cc.ID(), Turns: obs}
	var warmTotals []time.Duration
	var allCached, allPrompt, warmCached, warmPrompt int64
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
			r.WarmHits++
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
		r.RequestLevelHit = float64(r.WarmHits) / float64(r.WarmTurns)
		r.WarmTotalP50 = durationSummary(warmTotals).P50
	}
	r.Verdict = cacheVerdict(r, r.WarmHits)
	return r
}

// cacheProgressLine is the live status line printed while a cache session runs.
func cacheProgressLine(elapsed time.Duration, done, total int) string {
	return fmt.Sprintf("[cache] elapsed %s, %d/%d turns completed",
		elapsed.Round(time.Second), done, total)
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
	fmt.Fprintf(&b, "    Warm turns: %.1f%% token-weighted, %d/%d request-level\n",
		100*r.WarmHitRate, r.WarmHits, r.WarmTurns)
	if r.ColdTotal > 0 {
		fmt.Fprintf(&b, "    Cold %s vs warm p50 %s\n",
			r.ColdTotal.Round(time.Millisecond), r.WarmTotalP50.Round(time.Millisecond))
	}
	fmt.Fprintf(&b, "    Verdict: %s\n", r.Verdict)
	fmt.Fprintf(&b, "    Failed: %d/%d\n", r.FailedTurns, len(r.Turns))
	return b.String()
}
