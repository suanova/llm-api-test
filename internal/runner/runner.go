// Package runner executes compatibility cases and benchmarks and formats
// their results. It is format-agnostic: everything format-specific lives in
// the format packages (chat, responses, messages).
package runner

import (
	"context"

	"llm-api-test/internal/registry"
)

// CaseResult pairs a case with its outcome.
type CaseResult struct {
	Case   registry.CompatCase
	Result *registry.CompatResult
}

// RunCompat runs cases in order. The first case is the format's basic test:
// when it fails, the remaining cases are skipped.
func RunCompat(ctx context.Context, cases []registry.CompatCase, model string) []CaseResult {
	results := make([]CaseResult, 0, len(cases))
	basicFailed := false
	for i, cs := range cases {
		if basicFailed {
			results = append(results, CaseResult{
				Case:   cs,
				Result: &registry.CompatResult{Detail: "skipped: basic failed"},
			})
			continue
		}
		res := cs.Run(ctx, model)
		results = append(results, CaseResult{Case: cs, Result: res})
		if i == 0 && !res.Pass {
			basicFailed = true
		}
	}
	return results
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
