package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/registry"
	"llm-api-test/internal/runner"
)

// benchOpts carries the shared benchmark-command flags into the runner. Each
// command owns its own instance so per-command defaults cannot clobber each
// other (pflag writes through the bound pointer).
type benchOpts struct {
	Mode            string
	APIFormat       string
	Iterations      int
	Concurrency     int
	InputTokens     int
	MaxOutputTokens int
	ReasoningEffort string
	RPS             float64 // rate mode: offered requests/second; 0 = wave mode
	Duration        time.Duration
	MaxInFlight     int
}

// newLatencyCmd builds the latency benchmark command (short pong prompt).
func newLatencyCmd() *cobra.Command {
	o := benchOpts{Mode: "latency", Iterations: 10, Concurrency: 5, MaxOutputTokens: 4096, MaxInFlight: 1024}
	cmd := &cobra.Command{
		Use:   "latency",
		Short: "Run latency benchmarks (short pong prompt)",
		RunE: func(cmd *cobra.Command, args []string) error {
			exitCode = runBenchmark(cmd, o)
			return nil
		},
	}
	addBenchmarkFlags(cmd, &o)
	return cmd
}

// newThroughputCmd builds the throughput benchmark command (long prompt).
func newThroughputCmd() *cobra.Command {
	o := benchOpts{Mode: "throughput", Iterations: 3, Concurrency: 3, MaxOutputTokens: 4096, MaxInFlight: 1024}
	cmd := &cobra.Command{
		Use:   "throughput",
		Short: "Run throughput benchmarks (long prompt)",
		RunE: func(cmd *cobra.Command, args []string) error {
			exitCode = runBenchmark(cmd, o)
			return nil
		},
	}
	addBenchmarkFlags(cmd, &o)
	return cmd
}

// addBenchmarkFlags registers the flags shared by the benchmark commands.
// Throughput defaults to 3x3 because each request is expensive (long prompt);
// latency defaults to 5x10.
func addBenchmarkFlags(cmd *cobra.Command, o *benchOpts) {
	cmd.Flags().StringVar(&o.APIFormat, "api-format", "chat", "API format to test: all, chat, responses, messages")
	cmd.Flags().IntVar(&o.Concurrency, "concurrency", o.Concurrency, "concurrent requests per iteration")
	cmd.Flags().IntVar(&o.Iterations, "iterations", o.Iterations, "iterations per benchmark case")
	cmd.Flags().IntVar(&o.InputTokens, "input-tokens", o.InputTokens, "replace the prompt with a generated filler prompt of about N tokens (0 = built-in prompt)")
	cmd.Flags().IntVar(&o.MaxOutputTokens, "max-output-tokens", o.MaxOutputTokens, "generation cap per request, in tokens")
	cmd.Flags().StringVar(&o.ReasoningEffort, "reasoning-effort", o.ReasoningEffort, "reasoning effort sent to reasoning-capable formats (chat/responses), e.g. none disables thinking")
	cmd.Flags().Float64Var(&o.RPS, "rps", o.RPS, "rate mode: offer this many requests per second (needs --duration)")
	cmd.Flags().DurationVar(&o.Duration, "duration", o.Duration, "rate mode: how long to offer the rate (e.g. 3m)")
	cmd.Flags().IntVar(&o.MaxInFlight, "max-in-flight", o.MaxInFlight, "rate mode: in-flight cap; over-cap ticks are counted as shed")
}

// runBenchmark runs the benchmark for the given mode and returns the exit code.
func runBenchmark(cmd *cobra.Command, o benchOpts) int {
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()

	if o.RPS > 0 && o.Duration <= 0 {
		fmt.Fprintln(errOut, "--rps requires --duration (e.g. --duration 3m)")
		return 2
	}
	if o.RPS <= 0 && o.Duration > 0 {
		fmt.Fprintln(errOut, "--duration requires --rps")
		return 2
	}
	if o.RPS > 0 && o.MaxInFlight <= 0 {
		fmt.Fprintln(errOut, "--max-in-flight must be > 0 in rate mode")
		return 2
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(errOut, "config: %v\n", err)
		return 2
	}
	formats, ok := resolveFormats(errOut, o.APIFormat)
	if !ok {
		return 2
	}
	prompt, promptLabel := cases.PongPrompt, "pong"
	if o.Mode == "throughput" {
		prompt, promptLabel = cases.LongPrompt, "long"
	}
	if o.InputTokens > 0 {
		prompt = cases.FillerPrompt(o.InputTokens)
		promptLabel = fmt.Sprintf("filler:%d", o.InputTokens)
	}

	p := registry.Params{
		Config:          cfg,
		Stream:          !noStream,
		Debug:           debugWriter(),
		MaxOutputTokens: o.MaxOutputTokens,
		ReasoningEffort: o.ReasoningEffort,
	}
	timeout := benchmarkTimeout(o.Iterations, o.Concurrency)
	if o.RPS > 0 {
		timeout = o.Duration + rateDrainTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var jsonReports []runner.BenchmarkJSONReport
	code := 0
	for _, f := range formats {
		bc := f.Benchmark(p)
		for mi, m := range cfg.Models {
			if mi > 0 {
				fmt.Fprintln(out)
			}
			fmt.Fprintf(out, "base_url: %s  model: %s\n", cfg.BaseURL, m)
			if o.RPS > 0 {
				fmt.Fprintf(out, "rps=%.1f  duration=%s  prompt=%s\n\n", o.RPS, o.Duration, promptLabel)
			} else {
				fmt.Fprintf(out, "iterations=%d  concurrency=%d  prompt=%s\n\n",
					o.Iterations, o.Concurrency, promptLabel)
			}

			var rep runner.BenchmarkReport
			if o.RPS > 0 {
				rep = runner.RunRate(ctx, bc, m, prompt, o.RPS, o.Duration, o.MaxInFlight, errOut)
			} else {
				rep = runner.RunBenchmark(ctx, bc, m, prompt, o.Iterations, o.Concurrency, errOut)
			}
			rep.Stream = p.Stream
			rep.Mode = o.Mode
			// The report stores the label: a filler prompt is far too large
			// to embed, and the built-in prompts are identified by their label.
			rep.Prompt = promptLabel
			jsonReports = append(jsonReports, rep.JSON(m, cfg.BaseURL, f.Name))
			fmt.Fprintln(out, runner.FormatBenchmarkReport(rep))
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

// rateDrainTimeout is how long rate-mode runs may keep draining outstanding
// requests after the offering window closes, on top of --duration.
const rateDrainTimeout = 5 * time.Minute

// benchmarkTimeout gives each request up to 120s, plus a floor of 10 minutes.
func benchmarkTimeout(iterations, concurrency int) time.Duration {
	t := time.Duration(iterations*concurrency) * 120 * time.Second
	if t < 10*time.Minute {
		t = 10 * time.Minute
	}
	return t
}
