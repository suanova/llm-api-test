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

var cacheTurns int

// newCacheCmd builds the cache hit-rate test command (session-shaped).
func newCacheCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Run cache hit-rate tests (session-shaped)",
		RunE: func(cmd *cobra.Command, args []string) error {
			exitCode = runCache(cmd)
			return nil
		},
	}
	cmd.Flags().StringVar(&apiFormat, "api-format", "all", "API format to test: all, chat, messages")
	cmd.Flags().IntVar(&cacheTurns, "turns", 8, "session turns (1-8)")
	return cmd
}

// runCache runs the cache session for the selected formats and models.
func runCache(cmd *cobra.Command) int {
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()

	if noStream {
		fmt.Fprintln(errOut, "--no-stream does not apply to cache: sessions are always non-streamed")
		return 2
	}
	if cacheTurns < 1 || cacheTurns > len(cases.CacheQuestions) {
		fmt.Fprintf(errOut, "turns must be between 1 and %d\n", len(cases.CacheQuestions))
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

	p := registry.Params{Config: cfg, Debug: debugWriter()} // Stream ignored: cache is always non-streamed
	ctx, cancel := context.WithTimeout(context.Background(), cacheTimeout())
	defer cancel()

	var jsonReports []runner.CacheJSONReport
	code := 0
	for _, f := range formats {
		if f.Cache == nil {
			continue // format has no cache test (responses)
		}
		cc := f.Cache(p)
		for mi, m := range cfg.Models {
			if mi > 0 {
				fmt.Fprintln(out)
			}
			fmt.Fprintf(out, "base_url: %s  model: %s\n", cfg.BaseURL, m)
			fmt.Fprintf(out, "turns=%d\n\n", cacheTurns)

			rep := runner.RunCache(ctx, cc, m, cacheTurns)
			jsonReports = append(jsonReports, rep.CacheJSON(m, cfg.BaseURL, f.Name))
			fmt.Fprintln(out, runner.FormatCacheReport(rep))
			if rep.FailedTurns > 0 {
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

// cacheTimeout gives each turn up to 120s, plus a floor of 10 minutes.
func cacheTimeout() time.Duration {
	t := time.Duration(cacheTurns) * 120 * time.Second
	if t < 10*time.Minute {
		t = 10 * time.Minute
	}
	return t
}
