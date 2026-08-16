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

var (
	benchConcurrency int
	benchIterations  int
)

// newLatencyCmd builds the latency benchmark command (short pong prompt).
func newLatencyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "latency",
		Short: "Run latency benchmarks (short pong prompt)",
		RunE: func(cmd *cobra.Command, args []string) error {
			exitCode = runBenchmark(cmd, "latency")
			return nil
		},
	}
	addBenchmarkFlags(cmd, 5, 10)
	return cmd
}

// newThroughputCmd builds the throughput benchmark command (long prompt).
func newThroughputCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "throughput",
		Short: "Run throughput benchmarks (long prompt)",
		RunE: func(cmd *cobra.Command, args []string) error {
			exitCode = runBenchmark(cmd, "throughput")
			return nil
		},
	}
	addBenchmarkFlags(cmd, 3, 3)
	return cmd
}

// addBenchmarkFlags registers the flags shared by the benchmark commands.
// Throughput defaults to 3x3 because each request is expensive (long prompt);
// latency defaults to 5x10.
func addBenchmarkFlags(cmd *cobra.Command, concurrency, iterations int) {
	cmd.Flags().StringVar(&apiFormat, "api-format", "all", "API format to test: all, chat, responses, messages")
	cmd.Flags().IntVar(&benchConcurrency, "concurrency", concurrency, "concurrent requests per iteration")
	cmd.Flags().IntVar(&benchIterations, "iterations", iterations, "iterations per benchmark case")
}

// runBenchmark runs the benchmark for the given mode and returns the exit code.
func runBenchmark(cmd *cobra.Command, mode string) int {
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
	ctx, cancel := context.WithTimeout(context.Background(), benchmarkTimeout())
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
				benchIterations, benchConcurrency, prompt)

			rep := runner.RunBenchmark(ctx, bc, m, prompt, benchIterations, benchConcurrency, errOut)
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
func benchmarkTimeout() time.Duration {
	t := time.Duration(benchIterations*benchConcurrency) * 120 * time.Second
	if t < 10*time.Minute {
		t = 10 * time.Minute
	}
	return t
}
