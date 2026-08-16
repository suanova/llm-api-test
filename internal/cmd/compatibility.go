package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"llm-api-test/internal/chat"
	"llm-api-test/internal/messages"
	"llm-api-test/internal/registry"
	"llm-api-test/internal/responses"
	"llm-api-test/internal/runner"
)

var apiFormat string

// formats is the composition root: every API format the CLI knows about.
var formats = []registry.Format{chat.Format(), responses.Format(), messages.Format()}

func newCompatibilityCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compatibility [tests...]",
		Short: "Run compatibility tests",
		Long: `Run compatibility tests, or pass one or more tests to run a subset:
  llm-api-test compatibility            # all tests
  llm-api-test compatibility chat:seed  # one test
  llm-api-test compatibility seed       # tests named seed
  llm-api-test compatibility chat       # all chat tests
Use 'list' to see available tests.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			exitCode = runCompatibility(cmd, args)
			return nil
		},
	}
	cmd.Flags().StringVar(&apiFormat, "api-format", "all", "API format to test: all, chat, responses, messages")
	return cmd
}

// runCompatibility runs the selected tests and returns the exit code.
func runCompatibility(cmd *cobra.Command, tests []string) int {
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

	p := registry.Params{Config: cfg, Stream: !noStream, Debug: debugWriter()}
	all := map[string][]registry.CompatCase{}
	for _, f := range formats {
		all[f.Name] = f.Cases(p)
	}
	sel, err := selectTests(all, tests)
	if err != nil {
		fmt.Fprintln(errOut, "error:", err)
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	var jsonReports []runner.CompatJSONReport
	code := 0
	for _, f := range formats {
		cases, ok := sel[f.Name]
		if !ok {
			continue
		}
		for mi, m := range cfg.Models {
			if mi > 0 {
				fmt.Fprintln(out)
			}
			fmt.Fprintf(out, "base_url: %s  model: %s\n", cfg.BaseURL, m)
			fmt.Fprintf(out, "%s Compatibility\n\n", f.Desc)

			results := runner.RunCompat(ctx, cases, m)
			jsonReports = append(jsonReports, runner.BuildCompatJSON(m, cfg.BaseURL, f.Name, p.Stream, results))
			for _, r := range results {
				fmt.Fprintf(out, "  %-30s %s  %s\n", r.Case.ID(), status(r.Result), r.Result.Detail)
			}
			if verbose {
				for _, r := range results {
					if r.Result.Raw != "" {
						fmt.Fprintf(out, "\n--- %s raw ---\n%s\n", r.Case.ID(), r.Result.Raw)
					}
				}
			}
			if runner.AnyFailed(results) {
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

// selectTests applies the positional test arguments to the per-format case
// lists (refactor_design.md, "Test organization"):
//   - no arguments: everything
//   - "chat:seed":   that exact test
//   - "seed":        every test named seed
//   - "chat":        the whole format
func selectTests(all map[string][]registry.CompatCase, tests []string) (map[string][]registry.CompatCase, error) {
	if len(tests) == 0 {
		return all, nil
	}
	sel := map[string][]registry.CompatCase{}
	for _, arg := range tests {
		arg = strings.TrimSpace(arg)
		switch {
		case strings.Contains(arg, ":"):
			parts := strings.SplitN(arg, ":", 2)
			found := false
			for _, cs := range all[parts[0]] {
				if cs.ID() == arg {
					sel[parts[0]] = append(sel[parts[0]], cs)
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("test not found: %s", arg)
			}
		case isFormatName(arg, all):
			sel[arg] = append(sel[arg], all[arg]...)
		default:
			matches := 0
			for fname, cases := range all {
				for _, cs := range cases {
					if cs.Name() == arg {
						sel[fname] = append(sel[fname], cs)
						matches++
					}
				}
			}
			if matches == 0 {
				return nil, fmt.Errorf("test not found: %s (use 'list' to see available tests)", arg)
			}
		}
	}
	return dedupeCases(sel), nil
}

// isFormatName reports whether arg names one of the selected formats.
func isFormatName(arg string, all map[string][]registry.CompatCase) bool {
	_, ok := all[arg]
	return ok
}

// dedupeCases removes duplicate cases per format (same test given twice).
func dedupeCases(sel map[string][]registry.CompatCase) map[string][]registry.CompatCase {
	out := map[string][]registry.CompatCase{}
	for fname, cases := range sel {
		seen := map[string]bool{}
		for _, cs := range cases {
			if !seen[cs.ID()] {
				seen[cs.ID()] = true
				out[fname] = append(out[fname], cs)
			}
		}
	}
	return out
}

// resolveFormats resolves --api-format to the format list.
func resolveFormats(errOut io.Writer) ([]registry.Format, bool) {
	if apiFormat == "all" {
		return formats, true
	}
	for _, f := range formats {
		if f.Name == apiFormat {
			return []registry.Format{f}, true
		}
	}
	fmt.Fprintf(errOut, "unknown --api-format %q (available: all, %s)\n", apiFormat, formatNames())
	return nil, false
}

func formatNames() string {
	names := make([]string, len(formats))
	for i, f := range formats {
		names[i] = f.Name
	}
	return strings.Join(names, ", ")
}

func status(r *registry.CompatResult) string {
	if r.Pass {
		return "PASS"
	}
	return "FAIL"
}
