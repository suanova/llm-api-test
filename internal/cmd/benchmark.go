package cmd

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/registry"
	"llm-api-test/internal/runner"
)

var (
	benchConcurrency int
	benchIterations  int
	benchMode        string
)

func newBenchmarkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "benchmark",
		Short: "Run latency/throughput benchmarks",
		RunE: func(cmd *cobra.Command, args []string) error {
			exitCode = runBenchmark(cmd)
			return nil
		},
	}
	cmd.Flags().StringVar(&apiFormat, "api-format", "all", "API format to test: all, chat, responses, messages")
	cmd.Flags().IntVar(&benchConcurrency, "concurrency", 5, "concurrent requests per iteration")
	cmd.Flags().IntVar(&benchIterations, "iterations", 10, "iterations per benchmark case")
	cmd.Flags().StringVar(&benchMode, "mode", "latency", "latency (pong prompt) or throughput (long prompt)")
	return cmd
}

// runBenchmark runs the benchmarks and returns the exit code.
func runBenchmark(cmd *cobra.Command) int {
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
	prompt, ok := benchmarkPrompt(errOut)
	if !ok {
		return 2
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
			rep.Mode = benchMode
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

// benchmarkPrompt maps --mode to the benchmark prompt text.
func benchmarkPrompt(errOut io.Writer) (string, bool) {
	switch benchMode {
	case "latency":
		return cases.PongPrompt, true
	case "throughput":
		return cases.LongPrompt, true
	default:
		fmt.Fprintf(errOut, "unknown --mode %q (available: latency, throughput)\n", benchMode)
		return "", false
	}
}

// benchmarkTimeout gives each request up to 120s, plus a floor of 10 minutes.
func benchmarkTimeout() time.Duration {
	t := time.Duration(benchIterations*benchConcurrency) * 120 * time.Second
	if t < 10*time.Minute {
		t = 10 * time.Minute
	}
	return t
}
