// Package root wires the fundamentum CLI root command.
package root

import (
	"github.com/spf13/cobra"

	"github.com/jpvelasco/fundamentum/cmd/apply"
	"github.com/jpvelasco/fundamentum/cmd/audit"
	"github.com/jpvelasco/fundamentum/cmd/exportcmd"
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
  fundamentum --preset oss apply OWNER/REPO # non-interactive public baseline
  fundamentum --preset oss export -o baseline.json
  fundamentum --from baseline.json apply OWNER/REPO
  fundamentum --skip protect-main,security apply OWNER/REPO
  fundamentum --dry-run apply OWNER/REPO    # preview without changes
  fundamentum --version                     # show version`,
		Version:           Version,
		PersistentPreRunE: noteExplicitBaselineFlags,
	}
	cmd.PersistentFlags().BoolVar(&globals.DryRun, "dry-run", false, "print actions without applying them")
	cmd.PersistentFlags().BoolVar(&globals.Verbose, "verbose", false, "print API calls")
	cmd.PersistentFlags().StringVar(&globals.Token, "token", "", "GitHub token (default: GITHUB_TOKEN env var)")
	cmd.PersistentFlags().BoolVar(&globals.NoOverwrite, "no-overwrite", false, "skip files that already exist, never update")
	cmd.PersistentFlags().BoolVar(&globals.ViaPR, "pr", false, "batch file changes into a PR; settings, security, and branch protection still apply live")
	cmd.PersistentFlags().BoolVar(&globals.AdvancedSecurity, "advanced-security", false, "enable GitHub Advanced Security (secret scanning, push protection) on private/internal repos")
	cmd.PersistentFlags().BoolVar(&globals.Strict, "strict", false, "fail the run when any core harden step fails, including optional tag/security items")
	cmd.PersistentFlags().StringSliceVar(&globals.RequireChecks, "require-checks", nil, "required status-check contexts for protect-main (comma-separated; default: jobs for the resolved --ci pack)")
	cmd.PersistentFlags().StringVar(&globals.CIPack, "ci", "", "CI pack: auto (default; detect go/node/python/rust, else generic), go, node, python, rust, generic, or none")
	cmd.PersistentFlags().StringSliceVar(&globals.SkipSteps, "skip", nil, "steps/paths to leave alone: settings, protect-main, protect-version-tags, security, files, or an exact file path (comma-separated)")
	cmd.PersistentFlags().StringSliceVar(&globals.OnlySteps, "only", nil, "when set, apply only these steps/paths (same keys as --skip)")
	cmd.PersistentFlags().BoolVar(&globals.Octopus, "octopus", false, "opt in to the Octopus Review workflow (public repos; shares PR diffs with octopus-review.ai)")
	cmd.PersistentFlags().BoolVar(&globals.CodacyInstructions, "codacy-instructions", false, "opt in to shipping .github/instructions/codacy.instructions.md (Codacy MCP rules for AI agents)")
	cmd.PersistentFlags().BoolVar(&globals.BotManaged, "bot-managed", false, "opt in to the bot-managed profile: harden repos whose PRs are opened by automation (squash-only merges, no privileged workflow triggers, read-only default Actions token, pinned required checks)")
	cmd.PersistentFlags().BoolVar(&globals.BranchUpToDate, "branch-up-to-date", true, "protect-main: require branches up to date before merging (default true; unset keeps the live policy on reconcile)")
	cmd.PersistentFlags().StringVar(&globals.Preset, "preset", "", "named baseline: oss (public defaults, no prompts), private (solo, no GHAS prompt), strict (fail optional steps + enable GHAS)")
	cmd.PersistentFlags().StringVar(&globals.FromFile, "from", "", "load a portable JSON baseline (export writes this; explicit flags win, including --flag=false)")
	cmd.AddCommand(apply.NewCmd())
	cmd.AddCommand(repoinit.NewCmd())
	cmd.AddCommand(audit.NewCmd())
	cmd.AddCommand(exportcmd.NewCmd())
	return cmd
}

// Execute runs the root command, returning any error so main can set the
// process exit code. Split from main to keep the exit path testable.
func Execute() error {
	cmd := newRootCmd()
	return cmd.Execute()
}

// noteExplicitBaselineFlags records bool flags the operator actually passed
// before apply/audit load a baseline. Cobra parses flags first; a false
// value is otherwise indistinguishable from "never set".
func noteExplicitBaselineFlags(cmd *cobra.Command, _ []string) error {
	globals.NoteExplicitBools(cmd.Flags().Changed)
	return nil
}
