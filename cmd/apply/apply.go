// Package apply implements the "fundamentum apply" command.
package apply

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/cmd/util"
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
	owner, repo, err := util.ParseOwnerRepo(args[0])
	if err != nil {
		return err
	}

	client := github.NewClient(globals.Token, globals.Verbose)
	branch := "main"

	// Detect repo visibility to determine which tooling to apply.
	visibility, err := client.GetRepoVisibility(owner, repo)
	if err != nil {
		return fmt.Errorf("detect repo visibility: %w", err)
	}

	data := templates.RepoData{Owner: owner, RepoName: repo, DefaultBranch: branch, Visibility: visibility}

	rendered, err := templates.Render(data)
	if err != nil {
		return fmt.Errorf("render templates: %w", err)
	}

	// Pre-flight: check branch protection state before asking solo/team.
	rulesetExists, err := client.RulesetExists(owner, repo, "protect-main")
	if err != nil {
		return fmt.Errorf("check branch ruleset: %w", err)
	}
	tagExists, err := client.RulesetExists(owner, repo, "protect-version-tags")
	if err != nil {
		return fmt.Errorf("check tag ruleset: %w", err)
	}
	classicExists, err := client.ClassicProtectionExists(owner, repo, branch)
	if err != nil {
		return fmt.Errorf("check classic protection: %w", err)
	}

	// Only ask solo/team if branch protection will actually be applied.
	// If the ruleset already exists, the question has no effect.
	var opts github.BranchProtectionOptions
	needsBranchProtection := !rulesetExists
	if needsBranchProtection {
		fmt.Printf("fundamentum apply %s/%s\n\n", owner, repo)
		opts.Solo = wizard.PromptProjectType(os.Stdin, os.Stdout)
		fmt.Println()
	} else {
		fmt.Printf("fundamentum apply %s/%s\n\n", owner, repo)
	}

	items := buildItems(client, owner, repo, branch, visibility, rendered, rulesetExists, tagExists, classicExists, opts)

	wizard.PrintSummaryTable(os.Stdout, items, !globals.DryRun)

	if wizard.ConfirmDefaults(os.Stdin, os.Stdout) {
		return wizard.RunItems(items, globals.DryRun)
	}
	return wizard.RunInteractive(items, globals.DryRun, os.Stdin)
}

func buildItems(
	c *github.Client,
	owner, repo, branch, visibility string,
	rendered []templates.RenderedFile,
	rulesetExists, tagExists, classicExists bool,
	opts github.BranchProtectionOptions,
) []wizard.Item {
	var items []wizard.Item

	// aliases maps template output paths to known case/path variants that count as "already exists".
	// Covers legacy root placements, case variants, and format variants (.yml vs .md).
	aliases := map[string][]string{
		".github/CODEOWNERS": {
			".github/CODEOWNERS",
			"CODEOWNERS",
		},
		".github/CONTRIBUTING.md": {
			".github/CONTRIBUTING.md",
			"CONTRIBUTING.md",
		},
		".github/CODE_OF_CONDUCT.md": {
			".github/CODE_OF_CONDUCT.md",
			"CODE_OF_CONDUCT.md",
		},
		".github/SECURITY.md": {
			".github/SECURITY.md",
			"SECURITY.md",
		},
		".github/PULL_REQUEST_TEMPLATE.md": {
			".github/PULL_REQUEST_TEMPLATE.md",
			".github/pull_request_template.md",
		},
		".codacy.yml": {
			".codacy.yml",
			".codacy.yaml",
			".codacy/codacy.yaml",
			".codacy/codacy.yml",
		},
		".github/ISSUE_TEMPLATE/bug_report.yml": {
			".github/ISSUE_TEMPLATE/bug_report.yml",
			".github/ISSUE_TEMPLATE/bug_report.md",
		},
		".github/ISSUE_TEMPLATE/feature_request.yml": {
			".github/ISSUE_TEMPLATE/feature_request.yml",
			".github/ISSUE_TEMPLATE/feature_request.md",
		},
	}

	// Files first — branch protection applied after, so direct commits are still allowed.
	for _, f := range rendered {
		file := f
		action := wizard.ActionCreate

		// Check alias paths before the exact path to avoid false "missing" on case/path variants.
		if variants, ok := aliases[file.Path]; ok {
			if exists, err := c.AnyFileExists(owner, repo, variants); err == nil && exists {
				action = wizard.ActionSkip
			}
		} else if status, err := c.FileStatus(owner, repo, file.Path, []byte(file.Content)); err == nil {
			switch status {
			case "skip":
				action = wizard.ActionSkip
			case "update":
				if globals.NoOverwrite {
					action = wizard.ActionSkip
				} else {
					action = wizard.ActionUpdate
				}
			}
		}
		items = append(items, wizard.Item{
			Name:   file.Path,
			Action: action,
			Apply: func() error {
				_, err := c.UpsertFile(owner, repo, file.Path, []byte(file.Content))
				return err
			},
		})
	}

	items = append(items, wizard.Item{
		Name:   "General settings (auto-delete branches)",
		Action: wizard.ActionCreate,
		Apply:  func() error { return c.ApplyGeneralSettings(owner, repo) },
	})
	items = append(items, branchProtectionItem(c, owner, repo, branch, visibility, rulesetExists, classicExists, opts))
	items = append(items, wizard.Item{
		Name:     "Tag ruleset (protect-version-tags)",
		Action:   actionFromExists(tagExists),
		Optional: true,
		Apply:    func() error { return c.EnsureTagRuleset(owner, repo) },
	})

	// Security features: CodeQL only for public repos (free-tier private needs GHAS).
	// Secret scanning and Dependabot work for all repos.
	if visibility == "public" {
		items = append(items, wizard.Item{
			Name:     "Security (secret scanning, CodeQL, Dependabot)",
			Action:   wizard.ActionCreate,
			Optional: true,
			Apply:    func() error { return c.EnableSecurity(owner, repo, visibility) },
		})
	} else {
		items = append(items, wizard.Item{
			Name:     "Security (secret scanning, Dependabot)",
			Action:   wizard.ActionCreate,
			Optional: true,
			Apply:    func() error { return c.EnableSecurity(owner, repo, visibility) },
		})
	}

	return items
}

// branchProtectionItem returns the correct Item for branch protection based on current state:
//   - ruleset exists → skip
//   - classic exists → upgrade (create ruleset + remove classic)
//   - neither exists → ruleset for public repos; try ruleset then fall back to classic for private
func branchProtectionItem(c *github.Client, owner, repo, branch, visibility string, rulesetExists, classicExists bool, opts github.BranchProtectionOptions) wizard.Item {
	switch {
	case rulesetExists:
		return wizard.Item{
			Name:   "Branch protection (protect-main ruleset)",
			Action: wizard.ActionSkip,
		}
	case classicExists:
		return wizard.Item{
			Name:   "Branch protection (upgrade classic → ruleset)",
			Action: wizard.ActionUpgrade,
			Apply: func() error {
				if err := c.EnsureBranchRuleset(owner, repo, nil, opts); err != nil {
					return err
				}
				return c.RemoveClassicBranchProtection(owner, repo, branch)
			},
		}
	default:
		return wizard.Item{
			Name:     "Branch protection (protect-main)",
			Action:   wizard.ActionCreate,
			Optional: true,
			Apply: func() error {
				err := c.EnsureBranchRuleset(owner, repo, nil, opts)
				if err == nil {
					return nil
				}
				// Public repos must use rulesets — no fallback.
				if visibility == "public" {
					return err
				}
				// Private free-tier: rulesets unavailable — fall back to classic.
				return c.ApplyClassicBranchProtection(owner, repo, branch, github.DefaultStatusChecks, opts)
			},
		}
	}
}

func actionFromExists(exists bool) wizard.Action {
	if exists {
		return wizard.ActionSkip
	}
	return wizard.ActionCreate
}
