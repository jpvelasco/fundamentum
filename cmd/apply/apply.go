// Package apply implements the "fundamentum apply" command.
package apply

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/internal/github"
	"github.com/jpvelasco/fundamentum/internal/templates"
	"github.com/jpvelasco/fundamentum/internal/wizard"
)

// NewCmd returns the apply subcommand.
func NewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "apply OWNER/REPO",
		Short: "Harden an existing GitHub repo",
		Args:  cobra.ExactArgs(1),
		RunE:  run,
	}
}

func run(cmd *cobra.Command, args []string) error {
	owner, repo, err := parseOwnerRepo(args[0])
	if err != nil {
		return err
	}

	client := github.NewClient(globals.Token, globals.Verbose)
	data := templates.RepoData{Owner: owner, RepoName: repo, DefaultBranch: "main"}

	rendered, err := templates.Render(data)
	if err != nil {
		return fmt.Errorf("render templates: %w", err)
	}

	items := buildItems(client, owner, repo, rendered)

	fmt.Printf("fundamentum apply %s/%s\n\n", owner, repo)
	wizard.PrintSummaryTable(os.Stdout, items, !globals.DryRun)

	if wizard.ConfirmDefaults(os.Stdin, os.Stdout) {
		return wizard.RunItems(items, globals.DryRun)
	}
	return wizard.RunInteractive(items, globals.DryRun)
}

func parseOwnerRepo(arg string) (string, string, error) {
	for i, c := range arg {
		if c == '/' {
			return arg[:i], arg[i+1:], nil
		}
	}
	return "", "", fmt.Errorf("invalid OWNER/REPO %q: expected a slash separator", arg)
}

func buildItems(c *github.Client, owner, repo string, rendered []templates.RenderedFile) []wizard.Item {
	var items []wizard.Item

	items = append(items, wizard.Item{
		Name:   "General settings (auto-delete branches)",
		Action: wizard.ActionCreate,
		Apply:  func() error { return c.ApplyGeneralSettings(owner, repo) },
	})
	items = append(items, wizard.Item{
		Name:   "Branch ruleset (protect-main)",
		Action: wizard.ActionCreate,
		Apply:  func() error { return c.CreateBranchRuleset(owner, repo, []string{}) },
	})
	items = append(items, wizard.Item{
		Name:   "Tag ruleset (protect-version-tags)",
		Action: wizard.ActionCreate,
		Apply:  func() error { return c.CreateTagRuleset(owner, repo) },
	})
	items = append(items, wizard.Item{
		Name:   "Security (secret scanning, CodeQL, Dependabot)",
		Action: wizard.ActionCreate,
		Apply:  func() error { return c.EnableSecurity(owner, repo) },
	})

	for _, f := range rendered {
		file := f
		items = append(items, wizard.Item{
			Name:   file.Path,
			Action: wizard.ActionCreate,
			Apply: func() error {
				_, err := c.UpsertFile(owner, repo, file.Path, []byte(file.Content))
				return err
			},
		})
	}
	return items
}
