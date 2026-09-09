package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"llm-api-test/internal/registry"
	"llm-api-test/internal/runner"
)

var (
	soakDuration  time.Duration
	soakInterval  time.Duration
	soakStall     time.Duration
	soakLongEvery int
	soakIdleGaps  string
)

// newSoakCmd builds the long-duration stream stability test command.
func newSoakCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "soak",
		Short: "Run long-duration stream stability tests",
		Long: `Run a stream stability session over a long stretch of real time
(default 1h): one short streamed request per turn, idle probe windows
between turns (to expose proxy-side idle connection timeouts), and per-turn
failure classification (stall, mid-stream cut, 5xx, 429, ...).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			exitCode = runSoak(cmd)
			return nil
		},
	}
	cmd.Flags().StringVar(&apiFormat, "api-format", "all", "API format to test: all, chat, messages")
	cmd.Flags().DurationVar(&soakDuration, "duration", time.Hour, "total run duration")
	cmd.Flags().DurationVar(&soakInterval, "interval", 30*time.Second, "time between turn starts")
	cmd.Flags().DurationVar(&soakStall, "stall", time.Minute, "no-data window that marks a stream stalled")
	cmd.Flags().IntVar(&soakLongEvery, "long-every", 10, "every Nth turn uses a larger generation budget (0 disables)")
	cmd.Flags().StringVar(&soakIdleGaps, "idle-gaps", "1m,5m,10m", "idle probe windows (comma-separated), spread evenly across the run; empty disables")
	return cmd
}

// runSoak runs the soak session for the selected formats and models.
func runSoak(cmd *cobra.Command) int {
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()

	if noStream {
		fmt.Fprintln(errOut, "--no-stream does not apply to soak: sessions are always streamed")
		return 2
	}
	if soakDuration <= 0 {
		fmt.Fprintln(errOut, "--duration must be positive")
		return 2
	}
	if soakInterval <= 0 {
		fmt.Fprintln(errOut, "--interval must be positive")
		return 2
	}
	if soakStall <= 0 {
		fmt.Fprintln(errOut, "--stall must be positive")
		return 2
	}
	if soakLongEvery < 0 {
		fmt.Fprintln(errOut, "--long-every must be >= 0")
		return 2
	}
	probes, err := parseSoakProbes(soakDuration, soakIdleGaps)
	if err != nil {
		fmt.Fprintf(errOut, "soak: %v\n", err)
		return 2
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(errOut, "config: %v\n", err)
		return 2
	}
	formats, ok := resolveFormats(errOut)
	if !ok {
		return 2
	}
	if apiFormat != "all" && formats[0].Soak == nil {
		fmt.Fprintf(errOut, "api format %q has no soak test (v1: chat, messages)\n", apiFormat)
		return 2
	}

	p := registry.Params{Config: cfg, Debug: debugWriter()} // Stream ignored: soak is always streamed
	ctx, cancel := context.WithTimeout(context.Background(), soakTimeout())
	defer cancel()

	opts := runner.SoakOpts{
		Duration:  soakDuration,
		Interval:  soakInterval,
		Stall:     soakStall,
		LongEvery: soakLongEvery,
	}
	gapSpec := "(none)"
	if len(probes) > 0 {
		parts := make([]string, len(probes))
		for i, pr := range probes {
			parts[i] = fmt.Sprintf("%s@%s", runner.FormatDur(pr.Gap), runner.FormatDur(pr.At))
		}
		gapSpec = strings.Join(parts, ", ")
	}

	var jsonReports []runner.SoakJSONReport
	code := 0
	for _, f := range formats {
		if f.Soak == nil {
			continue // format has no soak test (responses); skip in "all" runs
		}
		sc := f.Soak(p)
		for mi, m := range cfg.Models {
			if mi > 0 {
				fmt.Fprintln(out)
			}
			fmt.Fprintf(out, "base_url: %s  model: %s\n", cfg.BaseURL, m)
			fmt.Fprintf(out, "duration=%s interval=%s stall=%s long-every=%d idle-gaps=%s\n\n",
				runner.FormatDur(soakDuration), runner.FormatDur(soakInterval),
				runner.FormatDur(soakStall), soakLongEvery, gapSpec)

			rep := runner.RunSoak(ctx, sc, m, opts, probes, errOut)
			jsonReports = append(jsonReports, rep.SoakJSON(m, cfg.BaseURL, f.Name))
			fmt.Fprintln(out, runner.FormatSoakReport(rep))
			if rep.Failed > 0 {
				code = 1
			}
		}
	}
	if outPath != "" {
		if err := runner.WriteJSON(outPath, jsonReports); err != nil {
			fmt.Fprintf(errOut, "write --out %q: %v\n", outPath, err)
			return 2
		}
	}
	return code
}

// soakTimeout bounds the whole run: the wall clock plus margin for the last
// in-flight turn.
func soakTimeout() time.Duration {
	return soakDuration + 10*time.Minute
}

// parseSoakProbes parses the --idle-gaps spec into probe windows spread
// evenly across the run: with n gaps, the i-th window starts at
// duration*(i+1)/(n+1). Windows must fit inside the run and not overlap.
func parseSoakProbes(duration time.Duration, spec string) ([]runner.SoakProbe, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, nil
	}
	var gaps []time.Duration
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		g, err := time.ParseDuration(part)
		if err != nil {
			return nil, fmt.Errorf("idle gap %q: %v", part, err)
		}
		if g <= 0 {
			return nil, fmt.Errorf("idle gap %q must be positive", part)
		}
		gaps = append(gaps, g)
	}
	if len(gaps) == 0 {
		return nil, nil
	}
	probes := make([]runner.SoakProbe, len(gaps))
	prevEnd := time.Duration(0)
	for i, g := range gaps {
		at := duration * time.Duration(i+1) / time.Duration(len(gaps)+1)
		if at < prevEnd {
			return nil, fmt.Errorf("idle gaps overlap: window %d starts at %s before the previous ends", i+1, at)
		}
		if at+g > duration {
			return nil, fmt.Errorf("idle gap %d (%s) does not fit in --duration %s", i+1, g, duration)
		}
		probes[i] = runner.SoakProbe{At: at, Gap: g}
		prevEnd = at + g
	}
	return probes, nil
}
