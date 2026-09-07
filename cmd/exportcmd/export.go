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
--advanced-security / --strict. Does not call GitHub.

Examples:
  fundamentum --preset oss export
  fundamentum --preset strict --ci generic export -o baseline.json
  fundamentum --from baseline.json apply OWNER/REPO`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
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
