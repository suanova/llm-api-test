package runner

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"llm-api-test/internal/registry"
)

// httpClass extracts an HTTP failure class from a client error of the form
// "HTTP <code>: <body>" (every format client wraps non-2xx responses that
// way): "429" for rate limits, "5xx" for server errors, "other" otherwise.
func httpClass(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if !strings.HasPrefix(s, "HTTP ") {
		return "other"
	}
	fields := strings.Fields(strings.TrimPrefix(s, "HTTP "))
	if len(fields) == 0 {
		return "other"
	}
	code, cerr := strconv.Atoi(strings.TrimSuffix(fields[0], ":"))
	if cerr != nil {
		return "other"
	}
	switch {
	case code == 429:
		return "429"
	case code >= 500 && code < 600:
		return "5xx"
	default:
		return "other"
	}
}

// RunRate offers `rps` requests per second for `duration` in open loop: ticks
// keep firing on schedule regardless of completion, so the offered load stays
// flat. Ticks that find maxInFlight requests already running are counted as
// shed (offered load is preserved in the report rather than silently
// throttled downward). After the issue phase, outstanding requests drain for
// as long as ctx allows; requests still running at the ctx deadline are
// counted as Incomplete. When progress is non-nil, a live status line is
// written to it and cleared when done.
func RunRate(ctx context.Context, bc registry.BenchmarkCase, model, prompt string, rps float64, duration time.Duration, maxInFlight int, progress io.Writer) BenchmarkReport {
	start := time.Now()
	interval := time.Duration(float64(time.Second) / rps)
	interval = max(interval, time.Millisecond)
	deadline := start.Add(duration)

	var mu sync.Mutex
	metrics := make([]registry.Metrics, 0, int(rps*duration.Seconds())+1)
	inFlight := 0
	sent, shed, http429, http5xx := 0, 0, 0, 0
	var inFlightSamples []int
	var wg sync.WaitGroup

	done := make(chan struct{})
	defer close(done)
	go func() { // per-second in-flight sampler + live progress line
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case now := <-t.C:
				mu.Lock()
				inFlightSamples = append(inFlightSamples, inFlight)
				n, f := len(metrics), inFlight
				mu.Unlock()
				if progress != nil {
					fmt.Fprintf(progress, "\r[rate] elapsed %s, sent %d, completed %d, in-flight %d",
						now.Sub(start).Round(time.Second), sent, n, f)
				}
			}
		}
	}()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
issue:
	for {
		select {
		case <-ctx.Done():
			break issue
		case now := <-ticker.C:
			if now.After(deadline) {
				break issue
			}
			mu.Lock()
			if inFlight >= maxInFlight {
				shed++
				mu.Unlock()
				continue
			}
			sent++
			inFlight++
			mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				m := bc.Run(ctx, model, prompt)
				mu.Lock()
				metrics = append(metrics, *m)
				if m.Err != nil {
					switch httpClass(m.Err) {
					case "429":
						http429++
					case "5xx":
						http5xx++
					}
				}
				inFlight--
				mu.Unlock()
			}()
		}
	}
	if progress != nil {
		fmt.Fprint(progress, "\r\033[K") // clear the live status line
	}

	// drain: wait for outstanding requests until the ctx budget runs out
	drained := make(chan struct{})
	go func() { wg.Wait(); close(drained) }()
	select {
	case <-drained:
	case <-ctx.Done():
	}

	mu.Lock()
	incomplete := inFlight
	samples := append([]int(nil), inFlightSamples...)
	mu.Unlock()

	r := aggregate(metrics)
	r.CaseID = bc.ID()
	r.Prompt = prompt
	r.OfferedRPS = rps
	r.Duration = duration
	r.Sent = sent
	r.Shed = shed
	r.HTTP429 = http429
	r.HTTP5xx = http5xx
	r.Incomplete = incomplete
	r.InFlight = intSummary(samples)
	sum := 0
	for _, m := range metrics {
		if m.Err == nil {
			sum += m.CompletionTokens
		}
	}
	if elapsed := time.Since(start); elapsed > 0 {
		r.Elapsed = elapsed
		r.AchievedRPS = float64(len(metrics)) / elapsed.Seconds()
		r.RPS = r.AchievedRPS
		r.TokensPerSec = float64(sum) / elapsed.Seconds()
		r.InputTokensPerSec = float64(r.InputTokens) / elapsed.Seconds()
	}
	return r
}
