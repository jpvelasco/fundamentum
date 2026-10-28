// Package exportcmd implements the "fundamentum export" command.
package exportcmd

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/jpvelasco/fundamentum/cmd/globals"
)

// NewCmd returns the export subcommand.
func NewCmd() *cobra.Command {
	var outPath string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Write a portable harden baseline",
		Long: `Write the current flag baseline as JSON so apply --from can
reproduce it. Combine with --preset / --ci / --require-checks /
--skip / --only / --advanced-security / --strict / --octopus /
--codacy-instructions / --bot-managed / --branch-up-to-date. The baseline
also carries bot_policy_checks and keep (no CLI flag). An explicit flag on
the later apply or audit wins over the file, including --flag=false.
Does not call GitHub.

Examples:
  fundamentum --preset oss export
  fundamentum --preset strict --ci generic export -o baseline.json
  fundamentum --bot-managed export -o bot-baseline.json
  fundamentum --from baseline.json apply OWNER/REPO`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			return run(outPath, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVarP(&outPath, "output", "o", "", "write baseline to this file (default: stdout)")
	return cmd
}

func run(outPath string, stdout io.Writer) error {
	if err := globals.ApplyPreset(globals.Preset); err != nil {
		return err
	}
	b := globals.CurrentBaseline()
	if outPath == "" {
		return globals.WriteBaseline(stdout, b)
	}
	f, err := globals.CreateBaselineFile(outPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", outPath, err)
	}
	defer func() { _ = f.Close() }()
	if err := globals.WriteBaseline(f, b); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "wrote baseline %s\n", outPath)
	return nil
}
