package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"llm-api-test/internal/apis"
	"llm-api-test/internal/config"
	"llm-api-test/internal/runner"
)

var (
	cfgPath   string
	verbose   bool
	httpDebug bool
	outPath   string
	models    []string
	apiName   string
)

// NewRoot builds the command tree.
func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "llm-api-test",
		Short: "Test LLM API compatibility against OpenAI-compatible endpoints",
	}
	root.PersistentFlags().StringVarP(&cfgPath, "config", "c", "config.yaml", "path to config file")
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "print raw response body on each case")
	root.PersistentFlags().BoolVar(&httpDebug, "http-debug", false, "dump HTTP request/response to stderr (sensitive headers redacted)")
	root.PersistentFlags().StringVarP(&outPath, "out", "o", "", "write a clean report copy to this file (in addition to stdout)")
	root.PersistentFlags().StringArrayVarP(&models, "model", "m", nil, "model to test (repeatable; overrides config model/models)")
	root.PersistentFlags().StringVarP(&apiName, "api", "a", "all", "API surface to test: all, "+apiList())

	root.AddCommand(newRunCmd(), newListCmd())
	for _, s := range apis.All {
		for _, ci := range s.CaseMeta() {
			root.AddCommand(newCaseCmd(s.Name, ci.Name, ci.Desc))
		}
	}
	return root
}

// apiList returns a comma-separated list of available API surface names.
func apiList() string {
	names := make([]string, len(apis.All))
	for i, s := range apis.All {
		names[i] = s.Name
	}
	return strings.Join(names, ", ")
}

// loadConfig is shared by all subcommands.
func loadConfig() (*config.Config, error) {
	return config.Load(cfgPath)
}

// resolveSurfaces returns the surface(s) to test based on --api. "all" means
// every registered surface.
func resolveSurfaces() []apis.Surface {
	if apiName == "all" {
		return apis.All
	}
	s := apis.Find(apiName)
	if s == nil {
		fmt.Fprintf(os.Stderr, "unknown --api %q (available: all, %s)\n", apiName, apiList())
		os.Exit(2)
	}
	return []apis.Surface{*s}
}

// runCases runs the given case names (empty = all) and prints results, returning
// an exit code (0 = all pass, 1 = any fail, 2 on config/arg error).
func runCases(names []string) int {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 2
	}

	surfaces := resolveSurfaces()

	var debug io.Writer
	if httpDebug {
		debug = os.Stderr
	}

	// Resolve the model list: --model/-m flags override config. Dedupe while
	// preserving order.
	modelList := models
	if len(modelList) == 0 {
		modelList = cfg.Models
	}
	modelList = dedupe(modelList)

	// Build per-surface case lists, filtered by names.
	type surfaceCases struct {
		surface apis.Surface
		cases   []runner.Case
	}
	want := map[string]bool{}
	if len(names) > 0 {
		for _, n := range names {
			want[strings.TrimSpace(n)] = true
		}
	}
	var scList []surfaceCases
	for _, s := range surfaces {
		var filtered []runner.Case
		for _, cs := range s.Build(cfg.BaseURL, cfg.APIKey, debug) {
			if len(want) > 0 && !want[cs.Name()] {
				continue
			}
			filtered = append(filtered, cs)
		}
		if len(filtered) > 0 {
			scList = append(scList, surfaceCases{surface: s, cases: filtered})
		}
	}

	// Validate filtered names against the union of cases.
	if len(names) > 0 {
		seen := map[string]bool{}
		for _, sc := range scList {
			for _, cs := range sc.cases {
				seen[cs.Name()] = true
			}
		}
		var missing []string
		for _, n := range names {
			n = strings.TrimSpace(n)
			if !seen[n] {
				missing = append(missing, n)
			}
		}
		if len(missing) > 0 {
			fmt.Fprintf(os.Stderr, "error: case(s) not found: %s\n", strings.Join(missing, ", "))
			return 2
		}
	}

	rep := &runner.TextReporter{W: os.Stdout}
	if outPath != "" {
		f, err := os.Create(outPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "open --out %q: %v\n", outPath, err)
			return 2
		}
		defer f.Close()
		rep.Out = f
	}

	// Writer for headers (base_url, model, surface headings) — goes to both
	// stdout and the report file.
	headerW := io.MultiWriter(os.Stdout, io.Discard)
	if rep.Out != nil {
		headerW = io.MultiWriter(os.Stdout, rep.Out)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	exitCode := 0
	for mi, m := range modelList {
		// Print config header before the first model's output.
		if mi == 0 {
			fmt.Fprintf(headerW, "base_url: %s  model: %s", cfg.BaseURL, m)
			if len(modelList) > 1 {
				fmt.Fprint(headerW, " (1/", len(modelList), ")")
			}
			fmt.Fprintln(headerW)
		} else {
			fmt.Fprintf(headerW, "\nbase_url: %s  model: %s", cfg.BaseURL, m)
			if len(modelList) > 1 {
				fmt.Fprintf(headerW, " (%d/%d)", mi+1, len(modelList))
			}
			fmt.Fprintln(headerW)
		}

		for si, sc := range scList {
			// Surface section header.
			if si > 0 {
				fmt.Fprintln(headerW)
			}
			fmt.Fprintf(headerW, "%s Compatibility\n", sc.surface.Desc)

			r := &runner.Runner{Cases: sc.cases, Reporter: rep}
			results := r.Run(ctx, m, nil)
			passed, total, dur := runner.Summary(results)
			rep.Summary(os.Stdout, passed, total, dur)

			if verbose {
				for _, res := range results {
					if res.Raw != "" {
						fmt.Printf("\n--- %s raw ---\n%s\n", res.Name, res.Raw)
					}
				}
			}
			if passed != total {
				exitCode = 1
			}
		}
	}
	return exitCode
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

func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run [cases...]",
		Short: "Run all (or a subset of) compatibility cases",
		Long: `Run all compatibility cases, or pass one or more case names to run a subset.
Use 'list' to see available case names. Exit code is 0 if all pass, 1 if any fail, 2 on config error.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			os.Exit(runCases(args))
			return nil
		},
	}
}

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available test cases",
		Long:  "List available test cases for the selected API surface (--api). Default is all surfaces.",
		RunE: func(cmd *cobra.Command, args []string) error {
			surfaces := resolveSurfaces()
			for _, s := range surfaces {
				caseList := s.Build("", "", nil)
				runner.List(os.Stdout, caseList)
			}
			return nil
		},
	}
}

// newCaseCmd creates a subcommand that runs a single named case, pre-selecting
// the API surface so the user doesn't need --api.
func newCaseCmd(api, name, desc string) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: desc,
		RunE: func(cmd *cobra.Command, args []string) error {
			apiName = api
			os.Exit(runCases([]string{name}))
			return nil
		},
	}
}
