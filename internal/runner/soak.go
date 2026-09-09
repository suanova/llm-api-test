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

// SoakOpts configures one soak session.
type SoakOpts struct {
	Duration  time.Duration // wall-clock budget for the session
	Interval  time.Duration // nominal time between turn starts
	Stall     time.Duration // no-data watchdog: no stream event for this long = stalled
	LongEvery int           // every Nth turn uses the long generation budget; 0 disables
}

// SoakProbe is one planned idle window: no traffic from At to At+Gap
// (run-relative), so a proxy-side idle connection timeout has a chance to
// surface. The first turn after the window reports the probe's outcome.
type SoakProbe struct {
	At  time.Duration
	Gap time.Duration
}

// step is one planned action: sleep wait, then either run a turn or (when
// gap > 0) treat the sleep as an idle-probe window. gap equals wait: a probe
// window is exactly the idle stretch between the previous action and this
// step's end.
type step struct {
	wait time.Duration
	gap  time.Duration // > 0: this sleep is an idle probe window of this length
	turn bool
	long bool
}

// planSteps lays out the soak session: a turn at every interval from t=0,
// with probe windows cancelling any turn slot inside them. Turn slots on or
// after a window's end resume the normal cadence. A window running past the
// duration ends the session idle.
func planSteps(duration, interval time.Duration, longEvery int, probes []SoakProbe) []step {
	var steps []step
	t := time.Duration(0) // planned finish time of the previous step
	turn := 1             // 1-based turn identity (dropped slots keep their number)
	pi := 0
	for {
		slot := time.Duration(turn-1) * interval
		if slot >= duration {
			break
		}
		// Windows ending before this slot: pure idle gaps between turns.
		for pi < len(probes) {
			p := probes[pi]
			if p.At >= slot {
				break
			}
			if p.At+p.Gap <= t {
				pi++ // already behind (ordering invariant); skip
				continue
			}
			if p.At+p.Gap > slot {
				break // overlaps this slot; handled below
			}
			steps = append(steps, step{wait: p.At + p.Gap - t, gap: p.At + p.Gap - t})
			t = p.At + p.Gap
			pi++
		}
		// Window covering this slot: idle to its end, drop every slot inside.
		if pi < len(probes) {
			p := probes[pi]
			if p.At <= slot && slot < p.At+p.Gap {
				end := p.At + p.Gap
				if end >= duration {
					steps = append(steps, step{wait: duration - t, gap: duration - t})
					return steps
				}
				if end > t {
					steps = append(steps, step{wait: end - t, gap: end - t})
					t = end
				}
				pi++
				for slot < end {
					turn++
					slot = time.Duration(turn-1) * interval
				}
				continue
			}
		}
		// Plain turn at this slot (runs immediately when the run is behind).
		if slot > t {
			steps = append(steps, step{wait: slot - t})
			t = slot
		}
		steps = append(steps, step{turn: true, long: longEvery > 0 && turn%longEvery == 0})
		turn++
	}
	return steps
}

// SoakFailure is one failed turn, with its run-relative wall time.
type SoakFailure struct {
	Elapsed time.Duration
	Turn    int
	Long    bool
	Class   registry.SoakClass
	Err     error
	Total   time.Duration
}

// SoakProbeResult is the outcome of one idle probe: the first turn after the
// window. A nil result means the session ended inside the window.
type SoakProbeResult struct {
	At    time.Duration // run-relative idle start
	Gap   time.Duration // idle length
	Turn  int
	Class registry.SoakClass
	Total time.Duration
}

// SoakClassCount is one class's tally.
type SoakClassCount struct {
	Class registry.SoakClass
	Count int
}

// SoakBucket is the latency trend of ok turns in one time slice.
type SoakBucket struct {
	Start    time.Duration
	Turns    int
	Ok       int
	TotalP50 time.Duration // p50 of ok-turn totals
}

// SoakReport is the aggregated outcome of one soak session.
type SoakReport struct {
	CaseID      string
	Planned     time.Duration
	Interval    time.Duration
	Stall       time.Duration
	LongEvery   int
	Aborted     bool // the run ctx expired before the session finished
	Turns       int
	Ok          int
	Failed      int
	LongTurns   int
	ClassCounts []SoakClassCount
	Failures    []SoakFailure
	Probes      []SoakProbeResult
	Buckets     []SoakBucket
	OkTotalP50  time.Duration
	Elapsed     time.Duration
}

const soakBucketCount = 6

// turnBudget is the per-turn context timeout: the stall watchdog must always
// fire first, and long turns need headroom beyond the stall window.
func turnBudget(stall time.Duration) time.Duration {
	return max(stall+60*time.Second, 2*time.Minute)
}

// sleepCtx sleeps d or until the ctx is canceled. It reports whether the
// sleep ran to completion.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// soakClassOrder is the report's fixed class ordering.
var soakClassOrder = []registry.SoakClass{
	registry.SoakOK, registry.SoakConn, registry.SoakTimeout, registry.SoakStall,
	registry.SoakDropped, registry.SoakHTTP429, registry.SoakHTTP5xx,
	registry.SoakHTTP4xx, registry.SoakOther,
}

type bucketAgg struct {
	turns  int
	ok     int
	totals []time.Duration
}

// RunSoak executes one soak session: turns on the planned cadence, idle
// probes between them, and per-turn failure classification. Failed turns do
// not abort the session (turns are independent); the run only stops when the
// duration elapses or the ctx is canceled. When progress is non-nil, a live
// status line is written to it and cleared when done.
func RunSoak(ctx context.Context, sc registry.SoakCase, model string, opts SoakOpts, probes []SoakProbe, progress io.Writer) SoakReport {
	steps := planSteps(opts.Duration, opts.Interval, opts.LongEvery, probes)
	start := time.Now()

	var done, failed atomic.Int32
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
					fmt.Fprintf(progress, "\r%s", soakProgressLine(now.Sub(start), int(done.Load()), int(failed.Load())))
				}
			}
		}()
	}

	rep := SoakReport{CaseID: sc.ID(), Planned: opts.Duration, Interval: opts.Interval, Stall: opts.Stall, LongEvery: opts.LongEvery}
	buckets := make([]bucketAgg, soakBucketCount)
	okTotals := make([]time.Duration, 0, len(steps))
	turnNo := 0
	var pending *SoakProbeResult

	for _, st := range steps {
		if time.Since(start) >= opts.Duration {
			break
		}
		if st.gap > 0 {
			pending = &SoakProbeResult{At: time.Since(start), Gap: st.gap}
		}
		if st.wait > 0 {
			remaining := opts.Duration - time.Since(start)
			d := min(st.wait, remaining)
			if !sleepCtx(ctx, d) {
				break // ctx canceled: report as aborted below
			}
		}
		if !st.turn {
			if time.Since(start) >= opts.Duration {
				break // the session ended inside the idle window: no resume turn
			}
			continue
		}
		turnNo++
		obs := runSoakTurn(ctx, sc, model, st.long, opts.Stall)
		obs.Turn = turnNo
		done.Store(int32(turnNo))
		elapsed := time.Since(start)

		rep.Turns++
		if obs.Long {
			rep.LongTurns++
		}
		bi := int(float64(elapsed) / float64(opts.Duration) * soakBucketCount)
		if bi >= soakBucketCount {
			bi = soakBucketCount - 1
		}
		buckets[bi].turns++
		if obs.Class == registry.SoakOK {
			rep.Ok++
			okTotals = append(okTotals, obs.Total)
			buckets[bi].ok++
			buckets[bi].totals = append(buckets[bi].totals, obs.Total)
		} else {
			rep.Failed++
			failed.Store(int32(rep.Failed))
			rep.Failures = append(rep.Failures, SoakFailure{
				Elapsed: elapsed, Turn: turnNo, Long: obs.Long, Class: obs.Class, Err: obs.Err, Total: obs.Total,
			})
		}
		if pending != nil {
			pending.Turn = turnNo
			pending.Class = obs.Class
			pending.Total = obs.Total
			rep.Probes = append(rep.Probes, *pending)
			pending = nil
		}
	}
	if progress != nil {
		fmt.Fprint(progress, "\r\033[K") // clear the live status line
	}

	rep.Elapsed = time.Since(start)
	rep.Aborted = ctx.Err() != nil
	for _, c := range soakClassOrder {
		n := 0
		for _, f := range rep.Failures {
			if f.Class == c {
				n++
			}
		}
		if c == registry.SoakOK {
			n = rep.Ok
		}
		if n > 0 {
			rep.ClassCounts = append(rep.ClassCounts, SoakClassCount{Class: c, Count: n})
		}
	}
	if len(okTotals) > 0 {
		rep.OkTotalP50 = durationSummary(okTotals).P50
	}
	bucketSize := opts.Duration / soakBucketCount
	for i, b := range buckets {
		rep.Buckets = append(rep.Buckets, SoakBucket{
			Start:    time.Duration(i) * bucketSize,
			Turns:    b.turns,
			Ok:       b.ok,
			TotalP50: durationSummary(b.totals).P50, // zero when the bucket has no ok turns
		})
	}
	return rep
}

// runSoakTurn runs one turn under its own budget and classifies the outcome.
func runSoakTurn(ctx context.Context, sc registry.SoakCase, model string, long bool, stall time.Duration) *registry.SoakTurn {
	turnCtx, cancel := context.WithTimeout(ctx, turnBudget(stall))
	defer cancel()
	return sc.RunTurn(turnCtx, model, long, stall)
}

// soakProgressLine is the live status line printed while a soak runs.
func soakProgressLine(elapsed time.Duration, done, failed int) string {
	return fmt.Sprintf("[soak] elapsed %s, %d turns completed, %d failed",
		FormatDur(elapsed), done, failed)
}

// FormatDur renders a duration for reports: millisecond precision below 10s
// (sub-second runs keep their shape), compact whole units above
// ("1h", "5m", "1m30s").
func FormatDur(d time.Duration) string {
	if d < 10*time.Second {
		return d.Truncate(time.Millisecond).String()
	}
	d = d.Round(time.Second)
	h := d / time.Hour
	m := (d % time.Hour) / time.Minute
	s := (d % time.Minute) / time.Second
	switch {
	case h > 0 && m > 0 && s > 0:
		return fmt.Sprintf("%dh%dm%ds", h, m, s)
	case h > 0 && m > 0:
		return fmt.Sprintf("%dh%dm", h, m)
	case h > 0:
		return fmt.Sprintf("%dh", h)
	case m > 0 && s > 0:
		return fmt.Sprintf("%dm%ds", m, s)
	case m > 0:
		return fmt.Sprintf("%dm", m)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

// FormatSoakReport renders the text report.
func FormatSoakReport(r SoakReport) string {
	var b strings.Builder
	extra := ""
	if r.Aborted {
		extra = ", aborted"
	}
	fmt.Fprintf(&b, "  %s  (%s planned, %d turns%s)\n", r.CaseID, FormatDur(r.Planned), r.Turns, extra)
	fmt.Fprintf(&b, "    turns: %d ok, %d failed", r.Ok, r.Failed)
	if r.LongTurns > 0 {
		fmt.Fprintf(&b, " (%d long)", r.LongTurns)
	}
	b.WriteString("\n")
	if len(r.ClassCounts) > 0 {
		parts := make([]string, 0, len(r.ClassCounts))
		for _, c := range r.ClassCounts {
			parts = append(parts, fmt.Sprintf("%s=%d", c.Class, c.Count))
		}
		fmt.Fprintf(&b, "    by class: %s\n", strings.Join(parts, " "))
	}
	if r.Ok > 0 {
		fmt.Fprintf(&b, "    ok total: p50=%s\n", r.OkTotalP50.Round(time.Millisecond))
	}
	if len(r.Failures) > 0 {
		b.WriteString("    failures:\n")
		for _, f := range r.Failures {
			long := ""
			if f.Long {
				long = " (long)"
			}
			fmt.Fprintf(&b, "      t+%-8s turn %-4d %s%s  %v\n",
				FormatDur(f.Elapsed), f.Turn, f.Class, long, f.Err)
		}
	}
	if len(r.Probes) > 0 {
		b.WriteString("    idle probes:\n")
		for _, p := range r.Probes {
			fmt.Fprintf(&b, "      %s idle at t+%s -> turn %d: %s (%s)\n",
				FormatDur(p.Gap), FormatDur(p.At), p.Turn, p.Class, p.Total.Round(time.Millisecond))
		}
	}
	if r.Ok > 0 {
		b.WriteString("    ok latency by bucket:\n")
		size := r.Planned / soakBucketCount
		for _, bk := range r.Buckets {
			if bk.Turns == 0 {
				continue
			}
			end := min(bk.Start+size, r.Planned)
			line := "        "
			if bk.Ok > 0 {
				line += fmt.Sprintf("p50=%s ", bk.TotalP50.Round(time.Millisecond))
			} else {
				line += "all failed "
			}
			fmt.Fprintf(&b, "%s%s-%s (%d turns)\n", line, FormatDur(bk.Start), FormatDur(end), bk.Turns)
		}
	}
	fmt.Fprintf(&b, "    failed: %d/%d\n", r.Failed, r.Turns)
	return b.String()
}
