// Package root provides the root Cobra command.
package root

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	DryRun  bool
	Verbose bool
	Token   string
)

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fundamentum",
		Short: "Bootstrap and harden GitHub repos for OSS collaboration",
		Long: `fundamentum applies branch protection, security settings, and community
health files to a GitHub repository in one shot.`,
	}
	cmd.PersistentFlags().BoolVar(&DryRun, "dry-run", false, "print actions without applying them")
	cmd.PersistentFlags().BoolVar(&Verbose, "verbose", false, "print API calls")
	cmd.PersistentFlags().StringVar(&Token, "token", "", "GitHub token (default: GITHUB_TOKEN env var)")
	return cmd
}

func Execute() {
	cmd := newRootCmd()
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
