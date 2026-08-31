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

// newLatencyCmd builds the latency benchmark command (short pong prompt).
func newLatencyCmd() *cobra.Command {
	iterations, concurrency := 10, 5
	cmd := &cobra.Command{
		Use:   "latency",
		Short: "Run latency benchmarks (short pong prompt)",
		RunE: func(cmd *cobra.Command, args []string) error {
			exitCode = runBenchmark(cmd, "latency", iterations, concurrency)
			return nil
		},
	}
	addBenchmarkFlags(cmd, &concurrency, &iterations)
	return cmd
}

// newThroughputCmd builds the throughput benchmark command (long prompt).
func newThroughputCmd() *cobra.Command {
	iterations, concurrency := 3, 3
	cmd := &cobra.Command{
		Use:   "throughput",
		Short: "Run throughput benchmarks (long prompt)",
		RunE: func(cmd *cobra.Command, args []string) error {
			exitCode = runBenchmark(cmd, "throughput", iterations, concurrency)
			return nil
		},
	}
	addBenchmarkFlags(cmd, &concurrency, &iterations)
	return cmd
}

// addBenchmarkFlags registers the flags shared by the benchmark commands.
// Throughput defaults to 3x3 because each request is expensive (long prompt);
// latency defaults to 5x10. Each command binds its own variables so the
// defaults cannot clobber each other (pflag writes through the bound
// pointer, so shared variables would keep the last registered default).
func addBenchmarkFlags(cmd *cobra.Command, concurrency, iterations *int) {
	cmd.Flags().StringVar(&apiFormat, "api-format", "all", "API format to test: all, chat, responses, messages")
	cmd.Flags().IntVar(concurrency, "concurrency", *concurrency, "concurrent requests per iteration")
	cmd.Flags().IntVar(iterations, "iterations", *iterations, "iterations per benchmark case")
}

// runBenchmark runs the benchmark for the given mode and returns the exit code.
func runBenchmark(cmd *cobra.Command, mode string, iterations, concurrency int) int {
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()

	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(errOut, "config: %v\n", err)
		return 2
	}
	formats, ok := resolveFormats(errOut)
	if !ok {
		return 2
	}
	prompt := cases.PongPrompt
	if mode == "throughput" {
		prompt = cases.LongPrompt
	}

	p := registry.Params{Config: cfg, Stream: !noStream, Debug: debugWriter()}
	ctx, cancel := context.WithTimeout(context.Background(), benchmarkTimeout(iterations, concurrency))
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
			fmt.Fprintf(out, "iterations=%d  concurrency=%d  prompt=%s\n\n",
				iterations, concurrency, prompt)

			rep := runner.RunBenchmark(ctx, bc, m, prompt, iterations, concurrency, errOut)
			rep.Stream = p.Stream
			rep.Mode = mode
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

// benchmarkTimeout gives each request up to 120s, plus a floor of 10 minutes.
func benchmarkTimeout(iterations, concurrency int) time.Duration {
	t := time.Duration(iterations*concurrency) * 120 * time.Second
	if t < 10*time.Minute {
		t = 10 * time.Minute
	}
	return t
}
