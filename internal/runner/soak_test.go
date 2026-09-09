package runner

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"llm-api-test/internal/registry"
)

func turnStep(long bool) step { return step{turn: true, long: long} }
func wait(d time.Duration) step {
	return step{wait: d}
}
func gap(d time.Duration) step { return step{wait: d, gap: d} }

func assertSteps(t *testing.T, got, want []step) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d steps, want %d\n got: %+v\nwant: %+v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("step %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestPlanStepsCadence(t *testing.T) {
	// 100s run, 30s interval: turns at 0, 30, 60, 90.
	steps := planSteps(100*time.Second, 30*time.Second, 0, nil)
	assertSteps(t, steps, []step{
		turnStep(false),
		wait(30 * time.Second), turnStep(false),
		wait(30 * time.Second), turnStep(false),
		wait(30 * time.Second), turnStep(false),
	})
}

func TestPlanStepsLongEvery(t *testing.T) {
	// Every 3rd executed turn is long: turns 3 (and none later in a 4-turn run).
	steps := planSteps(100*time.Second, 30*time.Second, 3, nil)
	assertSteps(t, steps, []step{
		turnStep(false),
		wait(30 * time.Second), turnStep(false),
		wait(30 * time.Second), turnStep(true),
		wait(30 * time.Second), turnStep(false),
	})
}

func TestPlanStepsProbeBetweenTurns(t *testing.T) {
	// Probe window [45s, 75s): the run is idle from the turn at 30s to the
	// window end (45s of no traffic), slot 60 is dropped, turns resume at 90s.
	steps := planSteps(100*time.Second, 30*time.Second, 0, []SoakProbe{{At: 45 * time.Second, Gap: 30 * time.Second}})
	assertSteps(t, steps, []step{
		turnStep(false),                         // 0
		wait(30 * time.Second), turnStep(false), // 30
		gap(45 * time.Second),                   // idle 30..75 (slot 60 dropped)
		wait(15 * time.Second), turnStep(false), // 90
	})
}

func TestPlanStepsProbeSwallowsSlot(t *testing.T) {
	// Probe window [30s, 60s) starts exactly on a turn slot: no traffic from
	// 0..60 (slots 30 dropped), turns resume at 60.
	steps := planSteps(100*time.Second, 30*time.Second, 0, []SoakProbe{{At: 30 * time.Second, Gap: 30 * time.Second}})
	assertSteps(t, steps, []step{
		turnStep(false),                         // 0
		gap(60 * time.Second),                   // idle 0..60
		turnStep(false),                         // 60
		wait(30 * time.Second), turnStep(false), // 90
	})
}

func TestPlanStepsProbeToEndOfRun(t *testing.T) {
	// Probe window [90s, 110s) runs past the 100s deadline: the session idles
	// out after the turn at 60s and no further turns run.
	steps := planSteps(100*time.Second, 30*time.Second, 0, []SoakProbe{{At: 90 * time.Second, Gap: 20 * time.Second}})
	assertSteps(t, steps, []step{
		turnStep(false),
		wait(30 * time.Second), turnStep(false),
		wait(30 * time.Second), turnStep(false),
		gap(40 * time.Second), // idle 60..100
	})
}

func TestPlanStepsProbeBeforeSecondSlot(t *testing.T) {
	// Probe window [20s, 30s) between the turn at 0 and the slot at 60: idle
	// from 0 to 30, turn at 60 unaffected.
	steps := planSteps(150*time.Second, 60*time.Second, 0, []SoakProbe{{At: 20 * time.Second, Gap: 10 * time.Second}})
	assertSteps(t, steps, []step{
		turnStep(false),
		gap(30 * time.Second),                   // idle 0..30
		wait(30 * time.Second), turnStep(false), // 60
		wait(60 * time.Second), turnStep(false), // 120
	})
}

func TestPlanStepsProbeEndsOnSlot(t *testing.T) {
	// Window [20s, 30s) ends exactly on the slot at 30: the turn at 30 is the
	// probe's resume turn and runs on schedule.
	steps := planSteps(100*time.Second, 30*time.Second, 0, []SoakProbe{{At: 20 * time.Second, Gap: 10 * time.Second}})
	assertSteps(t, steps, []step{
		turnStep(false),
		gap(30 * time.Second),                   // idle 0..30
		turnStep(false),                         // 30
		wait(30 * time.Second), turnStep(false), // 60
		wait(30 * time.Second), turnStep(false), // 90
	})
}

// stubSoakCase returns canned turns; a class of ok unless the slice says
// otherwise, plus a generic transport failure on failN (1-based).
type stubSoakCase struct {
	classes []registry.SoakClass
	failN   int
	n       int
}

func (s *stubSoakCase) ID() string   { return "stub:soak" }
func (s *stubSoakCase) Desc() string { return "stub" }
func (s *stubSoakCase) RunTurn(ctx context.Context, model string, long bool, stall time.Duration) *registry.SoakTurn {
	s.n++
	if s.failN == s.n {
		return &registry.SoakTurn{Long: long, Class: registry.SoakConn, Err: errors.New("connection reset"), Total: 10 * time.Millisecond}
	}
	cls := registry.SoakOK
	if i := s.n - 1; i < len(s.classes) {
		cls = s.classes[i]
	}
	tr := &registry.SoakTurn{Long: long, Class: cls, Total: 20 * time.Millisecond}
	if cls != registry.SoakOK {
		tr.Err = errors.New("boom")
	}
	return tr
}

func TestRunSoakAggregation(t *testing.T) {
	sc := &stubSoakCase{}
	rep := RunSoak(context.Background(), sc, "m", SoakOpts{
		Duration:  60 * time.Millisecond,
		Interval:  10 * time.Millisecond,
		Stall:     50 * time.Millisecond,
		LongEvery: 3,
	}, nil, nil)
	if rep.Turns != 6 { // slots 0..50
		t.Errorf("turns = %d, want 6", rep.Turns)
	}
	if rep.Ok != 6 || rep.Failed != 0 || rep.LongTurns != 2 { // long every 3: turns 3, 6
		t.Errorf("ok/failed/long = %d/%d/%d, want 6/0/2", rep.Ok, rep.Failed, rep.LongTurns)
	}
	if rep.OkTotalP50 <= 0 {
		t.Errorf("ok p50 = %v, want > 0", rep.OkTotalP50)
	}
	if len(rep.ClassCounts) != 1 || rep.ClassCounts[0].Class != registry.SoakOK || rep.ClassCounts[0].Count != 6 {
		t.Errorf("class counts = %+v, want ok=6", rep.ClassCounts)
	}
	if rep.Planned != 60*time.Millisecond {
		t.Errorf("planned = %v, want 60ms", rep.Planned)
	}
	var bTurns, bOk int
	for _, b := range rep.Buckets {
		bTurns += b.Turns
		bOk += b.Ok
	}
	if bTurns != rep.Turns || bOk != rep.Ok {
		t.Errorf("buckets sum to %d/%d, want %d/%d", bTurns, bOk, rep.Turns, rep.Ok)
	}
}

func TestRunSoakFailuresAndClasses(t *testing.T) {
	sc := &stubSoakCase{classes: []registry.SoakClass{
		registry.SoakDropped, registry.SoakHTTP429, registry.SoakOK,
	}}
	rep := RunSoak(context.Background(), sc, "m", SoakOpts{
		Duration: 60 * time.Millisecond, Interval: 10 * time.Millisecond, Stall: 50 * time.Millisecond,
	}, nil, nil)
	if rep.Failed != 2 || rep.Ok != 4 {
		t.Errorf("failed/ok = %d/%d, want 2/4", rep.Failed, rep.Ok)
	}
	counts := map[registry.SoakClass]int{}
	for _, c := range rep.ClassCounts {
		counts[c.Class] = c.Count
	}
	if counts[registry.SoakDropped] != 1 || counts[registry.SoakHTTP429] != 1 || counts[registry.SoakOK] != 4 {
		t.Errorf("class counts = %v, want dropped=1 http-429=1 ok=4", counts)
	}
	if len(rep.Failures) != 2 {
		t.Fatalf("failures = %d, want 2", len(rep.Failures))
	}
	if rep.Failures[0].Turn != 1 || rep.Failures[0].Class != registry.SoakDropped {
		t.Errorf("failure 0 = turn %d class %q, want turn 1 dropped", rep.Failures[0].Turn, rep.Failures[0].Class)
	}
	if rep.Failures[0].Elapsed <= 0 || rep.Failures[0].Total <= 0 {
		t.Errorf("failure elapsed/total = %v/%v, want > 0", rep.Failures[0].Elapsed, rep.Failures[0].Total)
	}
}

func TestRunSoakProbeResults(t *testing.T) {
	// Window [25ms, 40ms) drops the slot at 30ms; the turn at 40ms (executed
	// turn 4) is the probe result.
	sc := &stubSoakCase{}
	rep := RunSoak(context.Background(), sc, "m", SoakOpts{
		Duration: 60 * time.Millisecond, Interval: 10 * time.Millisecond, Stall: 50 * time.Millisecond,
	}, []SoakProbe{{At: 25 * time.Millisecond, Gap: 15 * time.Millisecond}}, nil)
	if rep.Turns != 5 {
		t.Errorf("turns = %d, want 5 (slots 0,10,20,40,50)", rep.Turns)
	}
	if len(rep.Probes) != 1 {
		t.Fatalf("probes = %d, want 1", len(rep.Probes))
	}
	p := rep.Probes[0]
	if p.Turn != 4 || p.Class != registry.SoakOK {
		t.Errorf("probe = turn %d class %q, want turn 4 ok", p.Turn, p.Class)
	}
	if p.Gap <= 0 || p.Total <= 0 {
		t.Errorf("probe gap/total = %v/%v, want > 0", p.Gap, p.Total)
	}
}

func TestRunSoakContextCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	sc := &stubSoakCase{}
	rep := RunSoak(ctx, sc, "m", SoakOpts{
		Duration: time.Second, Interval: 10 * time.Millisecond, Stall: 50 * time.Millisecond,
	}, nil, nil)
	if !rep.Aborted {
		t.Error("aborted = false, want true (ctx expired mid-run)")
	}
	if rep.Turns == 0 || rep.Elapsed >= time.Second {
		t.Errorf("run stopped early: turns=%d elapsed=%v", rep.Turns, rep.Elapsed)
	}
}

func TestSoakJSON(t *testing.T) {
	sc := &stubSoakCase{classes: []registry.SoakClass{registry.SoakDropped}}
	rep := RunSoak(context.Background(), sc, "m", SoakOpts{
		Duration: 60 * time.Millisecond, Interval: 10 * time.Millisecond, Stall: 50 * time.Millisecond, LongEvery: 2,
	}, nil, nil)
	j := rep.SoakJSON("m", "http://x", "chat")
	if j.Model != "m" || j.BaseURL != "http://x" || j.APIFormat != "chat" {
		t.Errorf("header = %+v, want model/base/format filled", j)
	}
	if j.DurationMS != 60 || j.IntervalMS != 10 || j.StallMS != 50 || j.LongEvery != 2 {
		t.Errorf("opts = %+v, want duration=60 interval=10 stall=50 longEvery=2", j)
	}
	if j.Turns != rep.Turns || j.Ok != rep.Ok || j.Failed != rep.Failed {
		t.Errorf("counts = %d/%d/%d, want %d/%d/%d", j.Turns, j.Ok, j.Failed, rep.Turns, rep.Ok, rep.Failed)
	}
	if len(j.Classes) != 2 { // ok + dropped
		t.Errorf("classes = %+v, want 2 entries", j.Classes)
	}
	if len(j.Failures) != 1 || j.Failures[0].Class != "dropped" || j.Failures[0].Detail == "" {
		t.Errorf("failures = %+v, want one dropped failure with detail", j.Failures)
	}
	if len(j.Buckets) != len(rep.Buckets) {
		t.Errorf("buckets = %d, want %d", len(j.Buckets), len(rep.Buckets))
	}
}

func TestFormatSoakReport(t *testing.T) {
	sc := &stubSoakCase{classes: []registry.SoakClass{registry.SoakDropped}}
	rep := RunSoak(context.Background(), sc, "m", SoakOpts{
		Duration: 60 * time.Millisecond, Interval: 10 * time.Millisecond, Stall: 50 * time.Millisecond, LongEvery: 2,
	}, nil, nil)
	text := FormatSoakReport(rep)
	for _, want := range []string{"stub:soak", "failed: 1/6", "dropped"} {
		if !strings.Contains(text, want) {
			t.Errorf("report missing %q:\n%s", want, text)
		}
	}
}
