// Package root wires the fundamentum CLI root command.
package root

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/jpvelasco/fundamentum/cmd/apply"
	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/cmd/repoinit"
)

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fundamentum",
		Short: "Bootstrap and harden GitHub repos for OSS collaboration",
		Long: `fundamentum applies branch protection, security settings, and community
health files to a GitHub repository in one shot.`,
	}
	cmd.PersistentFlags().BoolVar(&globals.DryRun, "dry-run", false, "print actions without applying them")
	cmd.PersistentFlags().BoolVar(&globals.Verbose, "verbose", false, "print API calls")
	cmd.PersistentFlags().StringVar(&globals.Token, "token", "", "GitHub token (default: GITHUB_TOKEN env var)")
	cmd.AddCommand(apply.NewCmd())
	cmd.AddCommand(repoinit.NewCmd())
	return cmd
}

func Execute() {
	cmd := newRootCmd()
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
