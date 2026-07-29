package runner

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// Case is a single compatibility test. Each case holds its own API client (set
// at construction), so Run only needs the model name. This keeps the runner
// agnostic of which API surface (Responses, Chat, ...) a case targets.
type Case interface {
	Name() string
	Desc() string
	Run(ctx context.Context, model string) *Result
}

// Result is the outcome of running a Case.
type Result struct {
	Name       string
	Desc       string
	Pass       bool
	Skipped    bool
	SkipReason string
	Duration   time.Duration
	Err        error
	// Detail is a short human-readable summary of what was checked.
	Detail string
	// Raw is the raw response body (captured for failures / debugging).
	Raw string
}

// Reporter consumes results as they complete.
type Reporter interface {
	// Start is called just before a case begins running, so the user can see
	// what is currently in flight (cases may take seconds over the network).
	Start(name, desc string)
	// Result is called when a case finishes, with its outcome.
	Result(r *Result)
	Summary(w io.Writer, passed, total int, duration time.Duration)
}

// Runner executes a set of Cases in order.
type Runner struct {
	Cases    []Case
	Reporter Reporter
}

func (r *Runner) Run(ctx context.Context, model string, names []string) []*Result {
	want := map[string]bool{}
	for _, n := range names {
		want[strings.TrimSpace(n)] = true
	}

	var results []*Result
	for _, cs := range r.Cases {
		if len(want) > 0 && !want[cs.Name()] {
			continue
		}
		if r.Reporter != nil {
			r.Reporter.Start(cs.Name(), cs.Desc())
		}
		start := time.Now()
		res := cs.Run(ctx, model)
		res.Name = cs.Name()
		res.Desc = cs.Desc()
		res.Duration = time.Since(start)
		results = append(results, res)
		if r.Reporter != nil {
			r.Reporter.Result(res)
		}
	}
	return results
}

// Summary computes totals and writes a summary via the reporter.
func Summary(results []*Result) (passed, total int, duration time.Duration) {
	for _, r := range results {
		total++
		duration += r.Duration
		if r.Pass {
			passed++
		}
	}
	return passed, total, duration
}

// fmtDur formats a duration at millisecond precision with a label, e.g.
// "runtime=1.122s" or "total=3.21s". Millisecond precision is enough for
// network-bound cases and avoids noisy nanosecond tails like "21.503229937s".
func fmtDur(d time.Duration, label string) string {
	return fmt.Sprintf("%s=%s", label, d.Round(time.Millisecond))
}

// FormatVerdict turns a Result into a one-line PASS/FAIL/SKIP verdict.
func FormatVerdict(r *Result) string {
	switch {
	case r.Skipped:
		return fmt.Sprintf("SKIP  %s  (%s) %s", r.Name, fmtDur(r.Duration, "runtime"), r.SkipReason)
	case r.Pass:
		return fmt.Sprintf("PASS  %s  (%s) %s", r.Name, fmtDur(r.Duration, "runtime"), r.Detail)
	default:
		msg := r.Detail
		if r.Err != nil {
			msg = r.Err.Error()
		}
		return fmt.Sprintf("FAIL  %s  (%s) %s", r.Name, fmtDur(r.Duration, "runtime"), msg)
	}
}
