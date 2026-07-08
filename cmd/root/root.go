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

// Version is set by build ldflags (e.g., -ldflags '-X github.com/jpvelasco/fundamentum/cmd/root.Version=v1.0.0').
var Version = "dev"

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fundamentum",
		Short: "Bootstrap and harden GitHub repos for OSS collaboration",
		Long: `fundamentum applies branch protection, security settings, and community
health files to a GitHub repository in one shot.

Examples:
  fundamentum apply OWNER/REPO              # harden existing repo
  fundamentum init OWNER/REPO               # create and harden new repo
  fundamentum --dry-run apply OWNER/REPO    # preview without changes
  fundamentum --version                     # show version`,
		Version: Version,
	}
	cmd.PersistentFlags().BoolVar(&globals.DryRun, "dry-run", false, "print actions without applying them")
	cmd.PersistentFlags().BoolVar(&globals.Verbose, "verbose", false, "print API calls")
	cmd.PersistentFlags().StringVar(&globals.Token, "token", "", "GitHub token (default: GITHUB_TOKEN env var)")
	cmd.PersistentFlags().BoolVar(&globals.NoOverwrite, "no-overwrite", false, "skip files that already exist, never update")
	cmd.PersistentFlags().BoolVar(&globals.ViaPR, "pr", false, "push file changes through a PR instead of direct commits")
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
