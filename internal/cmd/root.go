// Package cmd wires the CLI: the root command with common flags and the
// compatibility, latency, throughput, and list subcommands.
package cmd

import (
	"io"
	"os"

	"github.com/spf13/cobra"

	"llm-api-test/internal/config"
)

var (
	cfgPath   string
	baseURL   string
	apiKey    string
	models    []string
	noStream  bool
	httpDebug bool
	verbose   bool
	outPath   string
)

// NewRoot builds the command tree.
func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "llm-api-test",
		Short:         "Test LLM API compatibility and benchmark latency/throughput",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.PersistentFlags().StringVarP(&cfgPath, "config", "c", "config.yaml", "path to config file")
	root.PersistentFlags().StringVar(&baseURL, "base-url", "", "base URL, overrides config base_url")
	root.PersistentFlags().StringVar(&apiKey, "api-key", "", "API key, overrides config api_key")
	root.PersistentFlags().StringArrayVarP(&models, "models", "m", nil, "models to test, overrides config models")
	root.PersistentFlags().BoolVar(&noStream, "no-stream", false, "disable streaming (default: requests are streamed)")
	root.PersistentFlags().BoolVar(&httpDebug, "http-debug", false, "dump HTTP request/response to stderr (sensitive headers redacted)")
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "print raw response body on each case")
	root.PersistentFlags().StringVarP(&outPath, "out", "o", "", "write the JSON report to this file; text report still goes to stdout")

	root.AddCommand(newCompatibilityCmd(), newLatencyCmd(), newThroughputCmd(), newCacheCmd(), newListCmd())
	return root
}

// exitCode is set by the subcommand handlers: 0 all pass, 1 any failure,
// 2 config or argument error.
var exitCode int

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	if err := NewRoot().Execute(); err != nil {
		return 2
	}
	return exitCode
}

// loadConfig loads the config file and applies the CLI overrides.
func loadConfig() (*config.Config, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, err
	}
	if baseURL != "" {
		cfg.BaseURL = baseURL
	}
	if apiKey != "" {
		cfg.APIKey = apiKey
	}
	if len(models) > 0 {
		cfg.Models = dedupe(models)
	}
	return cfg, nil
}

// dedupe removes duplicate strings, preserving first-seen order.
func dedupe(ss []string) []string {
	seen := make(map[string]bool, len(ss))
	out := ss[:0]
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// debugWriter returns the --http-debug dump target, or nil.
func debugWriter() io.Writer {
	if httpDebug {
		return os.Stderr
	}
	return nil
}
