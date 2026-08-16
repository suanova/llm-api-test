// Package runner executes compatibility cases and benchmarks and formats
// their results. It is format-agnostic: everything format-specific lives in
// the format packages (chat, responses, messages).
package runner

import (
	"context"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"llm-api-test/internal/registry"
)

// CaseResult pairs a case with its outcome.
type CaseResult struct {
	Case   registry.CompatCase
	Result *registry.CompatResult
}

// RunCompat runs cases in order. The first case is the format's basic test:
// when it fails, the remaining cases are skipped. When progress is non-nil,
// a live status line (see compatProgressLine) is written to it and cleared
// when done.
func RunCompat(ctx context.Context, cases []registry.CompatCase, model string, progress io.Writer) []CaseResult {
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
					fmt.Fprintf(progress, "\r%s", compatProgressLine(now.Sub(start), int(done.Load()), len(cases)))
				}
			}
		}()
	}
	results := make([]CaseResult, 0, len(cases))
	basicFailed := false
	for i, cs := range cases {
		if basicFailed {
			results = append(results, CaseResult{
				Case:   cs,
				Result: &registry.CompatResult{Detail: "skipped: basic failed"},
			})
			done.Add(1)
			continue
		}
		res := cs.Run(ctx, model)
		done.Add(1)
		results = append(results, CaseResult{Case: cs, Result: res})
		if i == 0 && !res.Pass {
			basicFailed = true
		}
	}
	if progress != nil {
		fmt.Fprint(progress, "\r\033[K") // clear the live status line
	}
	return results
}

// compatProgressLine is the live status line printed while compatibility tests run.
func compatProgressLine(elapsed time.Duration, done, total int) string {
	return fmt.Sprintf("[compat] elapsed %s, %d/%d cases completed",
		elapsed.Round(time.Second), done, total)
}

// AnyFailed reports whether any result is not passing.
func AnyFailed(results []CaseResult) bool {
	for _, r := range results {
		if !r.Result.Pass {
			return true
		}
	}
	return false
}
