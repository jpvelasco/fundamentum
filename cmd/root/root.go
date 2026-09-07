// Package root wires the fundamentum CLI root command.
package root

import (
	"github.com/spf13/cobra"

	"github.com/jpvelasco/fundamentum/cmd/apply"
	"github.com/jpvelasco/fundamentum/cmd/audit"
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
  fundamentum audit OWNER/REPO              # verify the harden baseline
  fundamentum --dry-run apply OWNER/REPO    # preview without changes
  fundamentum --version                     # show version`,
		Version: Version,
	}
	cmd.PersistentFlags().BoolVar(&globals.DryRun, "dry-run", false, "print actions without applying them")
	cmd.PersistentFlags().BoolVar(&globals.Verbose, "verbose", false, "print API calls")
	cmd.PersistentFlags().StringVar(&globals.Token, "token", "", "GitHub token (default: GITHUB_TOKEN env var)")
	cmd.PersistentFlags().BoolVar(&globals.NoOverwrite, "no-overwrite", false, "skip files that already exist, never update")
	cmd.PersistentFlags().BoolVar(&globals.ViaPR, "pr", false, "batch file changes into a PR; settings, security, and branch protection still apply live")
	cmd.PersistentFlags().BoolVar(&globals.AdvancedSecurity, "advanced-security", false, "enable GitHub Advanced Security (secret scanning, push protection) on private/internal repos")
	cmd.PersistentFlags().BoolVar(&globals.Strict, "strict", false, "fail the run when any core harden step fails, including optional tag/security items")
	cmd.PersistentFlags().StringSliceVar(&globals.RequireChecks, "require-checks", nil, "required status-check contexts for protect-main (comma-separated; default: jobs for the resolved --ci pack)")
	cmd.PersistentFlags().StringVar(&globals.CIPack, "ci", "", "CI pack: auto (default; go if go.mod exists, else generic), go, generic, or none")
	cmd.AddCommand(apply.NewCmd())
	cmd.AddCommand(repoinit.NewCmd())
	cmd.AddCommand(audit.NewCmd())
	return cmd
}

// Execute runs the root command, returning any error so main can set the
// process exit code. Split from main to keep the exit path testable.
func Execute() error {
	cmd := newRootCmd()
	return cmd.Execute()
}
