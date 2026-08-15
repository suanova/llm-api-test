package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"llm-api-test/internal/config"
	"llm-api-test/internal/registry"
)

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available compatibility tests, grouped by API format",
		RunE: func(cmd *cobra.Command, args []string) error {
			exitCode = runList(cmd)
			return nil
		},
	}
	cmd.Flags().StringVar(&apiFormat, "api-format", "all", "API format to test: all, chat, responses, messages")
	return cmd
}

// runList prints the available tests and returns the exit code.
func runList(cmd *cobra.Command) int {
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()

	formats, ok := resolveFormats(errOut)
	if !ok {
		return 2
	}
	// Cases only construct their clients lazily; an empty config is enough to
	// enumerate them.
	p := registry.Params{Config: &config.Config{}}
	for _, f := range formats {
		fmt.Fprintf(out, "%s (%s)\n", f.Name, f.Desc)
		for _, cs := range f.Cases(p) {
			fmt.Fprintf(out, "  %-30s %s\n", cs.ID(), cs.Desc())
		}
		fmt.Fprintln(out)
	}
	return 0
}
