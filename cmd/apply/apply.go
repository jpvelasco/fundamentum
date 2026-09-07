// Package apply implements the "fundamentum apply" command.
package apply

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/cmd/util"
	"github.com/jpvelasco/fundamentum/internal/github"
	"github.com/jpvelasco/fundamentum/internal/templates"
	"github.com/jpvelasco/fundamentum/internal/wizard"
)

// newClient creates the GitHub API client; a package-level var so tests can
// inject a client pointed at a mock server.
var newClient = github.NewClient

// renderTemplates renders the embedded templates; a package-level var so tests
// can inject a failing renderer to exercise the error branch.
var renderTemplates = templates.Render

// NewCmd returns the apply subcommand.
func NewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "apply OWNER/REPO",
		Short: "Harden an existing GitHub repo",
		Long: `Harden an existing GitHub repository with community health files,
branch protection, security features, and starter workflows.

Examples:
  fundamentum apply OWNER/REPO              # interactive harden
  fundamentum --dry-run apply OWNER/REPO    # preview without changes
  fundamentum --pr apply OWNER/REPO         # files via PR; settings still apply live
  fundamentum --preset oss apply OWNER/REPO # non-interactive public baseline
  fundamentum --from baseline.json apply OWNER/REPO
  fundamentum --ci generic apply OWNER/REPO # non-Go starter CI (no go.mod)
  fundamentum --strict apply OWNER/REPO     # fail if any core step fails
  fundamentum --require-checks Lint,gosec apply OWNER/REPO
  fundamentum --token $GITHUB_TOKEN apply OWNER/REPO`,
		Args: cobra.ExactArgs(1),
		RunE: run,
	}
}

func run(cmd *cobra.Command, args []string) error {
	owner, repo, err := util.ParseOwnerRepo(args[0])
	if err != nil {
		return err
	}

	token, err := util.RequireToken(globals.Token)
	if err != nil {
		return err
	}
	client := newClient(token, globals.Verbose)
	return runWithClient(client, owner, repo, cmd.InOrStdin(), cmd.OutOrStdout())
}

// runWithClient runs the apply flow against client, reading prompts from stdin
// and writing output to stdout. Extracted from run so tests can inject a
// mock-server client and buffers.
func runWithClient(client *github.Client, owner, repo string, stdin io.Reader, stdout io.Writer) error {
	if err := loadFlags(); err != nil {
		return err
	}
	info, err := client.GetRepo(owner, repo)
	if err != nil {
		return fmt.Errorf("detect repo visibility: %w", err)
	}
	branch := info.DefaultBranch
	visibility := info.Visibility

	orgOwner := strings.EqualFold(info.OwnerType, "Organization")

	pack, err := resolveCIPack(client, owner, repo)
	if err != nil {
		return err
	}

	data := templates.RepoData{
		Owner:         owner,
		RepoName:      repo,
		DefaultBranch: branch,
		Visibility:    visibility,
		CodeOwnerLine: codeOwnerLine(owner, orgOwner),
		CIPack:        pack,
	}

	rendered, err := renderTemplates(data)
	if err != nil {
		return fmt.Errorf("render templates: %w", err)
	}

	// Pre-flight: check branch protection state before asking solo/team.
	var opts github.BranchProtectionOptions
	opts.SkipCodeOwners = orgOwner
	branchPlan, err := client.PlanBranchRuleset(owner, repo, requiredChecks(pack), opts)
	if err != nil {
		return fmt.Errorf("check branch ruleset: %w", err)
	}
	tagPlan, err := client.PlanTagRuleset(owner, repo)
	if err != nil {
		return fmt.Errorf("check tag ruleset: %w", err)
	}
	classicExists, err := client.ClassicProtectionExists(owner, repo, branch)
	if err != nil {
		return fmt.Errorf("check classic protection: %w", err)
	}

	// Only ask solo/team if branch protection will actually be created.
	// Existing rulesets keep their inferred solo/team settings on reconcile.
	_, _ = fmt.Fprintf(stdout, "fundamentum apply %s/%s\n\n", owner, repo)
	printPRModeNotice(stdout)
	switch {
	case branchPlan.Exists && !opts.SkipCodeOwners:
		opts.Solo = branchPlan.Solo
	case !globals.DryRun && !branchPlan.Exists && globals.Preset == "":
		opts.Solo = wizard.PromptProjectType(stdin, stdout)
		_, _ = fmt.Fprintln(stdout)
	case !branchPlan.Exists:
		// Named presets and dry-run take the advertised solo default so
		// a scripted run cannot enable CODEOWNERS review by accident.
		opts.Solo = true
	}

	paidSecurity := globals.AdvancedSecurity
	if !github.IsPublicVisibility(visibility) && !paidSecurity {
		switch {
		case globals.Preset != "":
			// Named presets skip the prompt. Only --preset strict
			// (or an explicit --advanced-security) enables GHAS.
			paidSecurity = globals.Preset == globals.PresetStrict
		case globals.DryRun:
			// Show the GHAS plan lines a live run would offer.
			paidSecurity = true
		default:
			paidSecurity = wizard.PromptAdvancedSecurity(stdin, stdout)
			_, _ = fmt.Fprintln(stdout)
		}
	}

	items, err := buildItems(client, owner, repo, branch, visibility, rendered, branchPlan, tagPlan, classicExists, &opts, paidSecurity, pack)
	if err != nil {
		return fmt.Errorf("plan %s/%s: %w (verify the token grants Contents read access and retry)", owner, repo, err)
	}

	if globals.DryRun {
		// Non-interactive dry run: plan table plus counts, then stop.
		wizard.PrintSummaryTable(stdout, items, false)
		_, _ = fmt.Fprintf(stdout, "\n  Dry run complete — %s — no changes made.\n", wizard.PlanSummary(items))
		return nil
	}

	wizard.PrintSummaryTable(stdout, items, true)

	if globals.Preset == "" && !wizard.ConfirmDefaults(stdin, stdout) {
		wizard.SelectInteractive(items, stdin, stdout)
	}
	if err := applyItems(client, owner, repo, branch, items, globals.ViaPR, &opts, stdout); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "\n  ✓ Done — https://github.com/%s/%s\n", owner, repo)
	return nil
}

// PlanNewRepo prints a dry-run harden plan for a repository that does not
// exist yet. Files, settings, and rulesets are modeled as absent — no GitHub
// GET is issued, so init --dry-run can preview create+harden.
func PlanNewRepo(owner, repo, visibility string, stdout io.Writer) error {
	if err := loadFlags(); err != nil {
		return err
	}
	pack, err := resolveCIPack(nil, owner, repo)
	if err != nil {
		return err
	}
	data := templates.RepoData{
		Owner:         owner,
		RepoName:      repo,
		DefaultBranch: "main",
		Visibility:    visibility,
		CodeOwnerLine: codeOwnerLine(owner, false),
		CIPack:        pack,
	}
	rendered, err := renderTemplates(data)
	if err != nil {
		return fmt.Errorf("render templates: %w", err)
	}
	// Named presets must match a live apply: only strict / --advanced-security
	// enable GHAS. Interactive dry-run still shows the offered plan line.
	paid := globals.AdvancedSecurity || github.IsPublicVisibility(visibility) || (globals.DryRun && globals.Preset == "")
	items, err := buildItems(nil, owner, repo, "main", visibility, rendered, github.RulesetPlan{}, github.RulesetPlan{}, false, &github.BranchProtectionOptions{}, paid, pack)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "fundamentum apply %s/%s\n\n", owner, repo)
	printPRModeNotice(stdout)
	wizard.PrintSummaryTable(stdout, items, false)
	_, _ = fmt.Fprintf(stdout, "\n  Dry run complete — %s — no changes made.\n", wizard.PlanSummary(items))
	return nil
}

func buildItems(
	c *github.Client,
	owner, repo, branch, visibility string,
	rendered []templates.RenderedFile,
	branchPlan, tagPlan github.RulesetPlan,
	classicExists bool,
	opts *github.BranchProtectionOptions,
	paidSecurity bool,
	pack string,
) ([]wizard.Item, error) {
	var items []wizard.Item

	aliases := templates.FileAliases()

	// Files first — branch protection applied after, so direct commits are still allowed.
	for _, f := range rendered {
		file := f
		action := wizard.ActionCreate

		// Non-canonical aliases (root CODEOWNERS, octopus-review.yml, …) count
		// as already present so we do not write a second copy. The canonical
		// path still goes through FileStatus so content drift can update.
		if c != nil {
			if variants, ok := aliases[file.Path]; ok {
				exists, err := c.AnyFileExists(owner, repo, otherAliases(file.Path, variants))
				if err != nil {
					return nil, fmt.Errorf("check aliases of %s: %w", file.Path, err)
				}
				if exists {
					action = wizard.ActionSkip
				}
			}
			if action != wizard.ActionSkip {
				var err error
				action, err = fileItemAction(c, owner, repo, file.Path, file.Content)
				if err != nil {
					return nil, fmt.Errorf("check status of %s: %w", file.Path, err)
				}
			}
		}
		items = append(items, wizard.Item{
			Name:    file.Path,
			Action:  action,
			Content: []byte(file.Content),
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
	items = append(items, branchProtectionItem(c, owner, repo, branch, visibility, branchPlan, classicExists, opts, pack))
	items = append(items, tagRulesetItem(c, owner, repo, tagPlan))

	// Security features: CodeQL only for public repos (free-tier private needs GHAS).
	// Secret scanning and Dependabot work for all repos.
	securityName := "Security (Dependabot)"
	// When the advanced codeql.yml workflow is part of the render, default-setup
	// CodeQL must be skipped — GitHub rejects advanced SARIF uploads while
	// default setup is configured (it also disables the advanced workflow).
	advancedCodeQL := false
	for _, f := range rendered {
		if f.Path == ".github/workflows/codeql.yml" {
			advancedCodeQL = true
			break
		}
	}
	switch {
	case github.IsPublicVisibility(visibility):
		securityName = "Security (secret scanning, CodeQL, Dependabot)"
	case paidSecurity:
		securityName = "Security (secret scanning, Dependabot)"
	}
	secOpts := github.SecurityOptions{
		Visibility:     visibility,
		AdvancedCodeQL: advancedCodeQL,
		PaidFeatures:   paidSecurity,
	}
	items = append(items, wizard.Item{
		Name:     securityName,
		Action:   wizard.ActionCreate,
		Optional: true,
		Apply:    func() error { return c.EnableSecurity(owner, repo, secOpts) },
	})

	return items, nil
}

// branchProtectionItem returns the correct Item for branch protection based on current state:
//   - matching ruleset → skip
//   - drifted ruleset → update in place
//   - classic exists → upgrade (create ruleset + remove classic)
//   - neither exists → ruleset for public repos; try ruleset then fall back to classic for private
func branchProtectionItem(c *github.Client, owner, repo, branch, visibility string, plan github.RulesetPlan, classicExists bool, opts *github.BranchProtectionOptions, pack string) wizard.Item {
	if opts == nil {
		opts = &github.BranchProtectionOptions{}
	}
	switch {
	case plan.Exists && len(plan.Drift) == 0:
		return wizard.Item{
			Name:   "Branch protection (protect-main ruleset)",
			Action: wizard.ActionSkip,
		}
	case plan.Exists:
		return wizard.Item{
			Name:   "Branch protection (reconcile protect-main)",
			Action: wizard.ActionUpdate,
			Apply: func() error {
				return c.EnsureBranchRuleset(owner, repo, requiredChecks(pack), *opts)
			},
		}
	case classicExists:
		return wizard.Item{
			Name:   "Branch protection (upgrade classic → ruleset)",
			Action: wizard.ActionUpgrade,
			Apply: func() error {
				if err := c.EnsureBranchRuleset(owner, repo, requiredChecks(pack), *opts); err != nil {
					return err
				}
				return c.RemoveClassicBranchProtection(owner, repo, branch)
			},
		}
	default:
		return wizard.Item{
			Name:   "Branch protection (protect-main)",
			Action: wizard.ActionCreate,
			Apply: func() error {
				checks := requiredChecks(pack)
				err := c.EnsureBranchRuleset(owner, repo, checks, *opts)
				if err == nil {
					return nil
				}
				// Public repos must use rulesets — no fallback.
				if visibility == "public" {
					return err
				}
				// Only fall back to classic when rulesets are not offered on this
				// plan. Other 403s (token scope, SSO, IP allow lists) surface as-is.
				if !github.IsRulesetUnavailable(err) {
					return err
				}
				return c.ApplyClassicBranchProtection(owner, repo, branch, checks, *opts)
			},
		}
	}
}

func loadFlags() error {
	if globals.FromFile != "" {
		if err := globals.LoadBaselineFile(globals.FromFile); err != nil {
			return err
		}
	}
	return globals.ApplyPreset(globals.Preset)
}

const prModeNotice = "--pr: file changes go in a pull request. Settings, security, and branch protection still apply live via the API. Required status checks are deferred until you re-apply after that PR merges.\n\n"

func printPRModeNotice(stdout io.Writer) {
	if globals.ViaPR {
		_, _ = fmt.Fprint(stdout, prModeNotice)
	}
}

func resolveCIPack(c *github.Client, owner, repo string) (string, error) {
	goMod := false
	if c != nil {
		exists, err := c.AnyFileExists(owner, repo, []string{"go.mod"})
		if err != nil {
			return "", fmt.Errorf("detect go.mod: %w", err)
		}
		goMod = exists
	}
	pack, err := templates.ResolveCIPack(globals.CIPack, goMod)
	if err != nil {
		return "", err
	}
	return pack, nil
}

func requiredChecks(pack string) []string {
	return github.ResolveRequiredChecksForPack(globals.RequireChecks, pack)
}

func tagRulesetItem(c *github.Client, owner, repo string, plan github.RulesetPlan) wizard.Item {
	item := wizard.Item{
		Name:     "Tag ruleset (protect-version-tags)",
		Optional: true,
		Apply:    func() error { return c.EnsureTagRuleset(owner, repo) },
	}
	switch {
	case !plan.Exists:
		item.Action = wizard.ActionCreate
	case len(plan.Drift) > 0:
		item.Action = wizard.ActionUpdate
	default:
		item.Action = wizard.ActionSkip
	}
	return item
}

// otherAliases returns path variants that are not the canonical target.
func otherAliases(canonical string, variants []string) []string {
	out := make([]string, 0, len(variants))
	for _, v := range variants {
		if v != canonical {
			out = append(out, v)
		}
	}
	return out
}

// fileItemAction maps FileStatus to a wizard action, honoring --no-overwrite.
// Status-check errors are surfaced, not defaulted: pretending "create" on a
// failed check would misreport the plan and fail later with confusing PUTs.
func fileItemAction(c *github.Client, owner, repo, path, content string) (wizard.Action, error) {
	status, err := c.FileStatus(owner, repo, path, []byte(content))
	if err != nil {
		return wizard.ActionCreate, err
	}
	switch status {
	case "skip":
		return wizard.ActionSkip, nil
	case "update":
		if globals.NoOverwrite {
			return wizard.ActionSkip, nil
		}
		return wizard.ActionUpdate, nil
	default:
		return wizard.ActionCreate, nil
	}
}

func codeOwnerLine(owner string, org bool) string {
	if org {
		return "# Organizations need a team (@org/team), not @" + owner
	}
	return "* @" + owner
}

// applyItems runs the item list. When viaPR is true, file items are batched
// into a single PR instead of direct commits. If viaPR is false and a 409
// is detected, the tool automatically falls back to PR mode for remaining
// file items — no re-run needed.
func applyItems(c *github.Client, owner, repo, branch string, items []wizard.Item, viaPR bool, opts *github.BranchProtectionOptions, stdout io.Writer) error {
	var fileChanges []github.FileChange
	var nonFileItems []wizard.Item
	fallback := false // true after first 409 triggers auto-fallback to PR mode
	requiredFailed := false
	if viaPR {
		deferRequiredChecks(opts)
	}

	for _, item := range items {
		if wizard.ShouldSkip(item, stdout) {
			continue
		}

		// Check if this is a file item (has rendered content).
		if item.Content != nil {
			if viaPR || fallback {
				// Collect for PR batch (explicit --pr or auto-fallback).
				fileChanges = append(fileChanges, github.FileChange{
					Path:    item.Name,
					Content: item.Content,
				})
				continue
			}

			// Direct apply — detect 409 and auto-fallback to PR mode.
			_, _ = fmt.Fprintf(stdout, "  %-45s  applying...", item.Name)
			if err := item.Apply(); err != nil {
				if github.IsWorkflowLocked(err) {
					_, _ = fmt.Fprintf(stdout, "\r  %-45s  ⚠ workflow locked by GitHub Actions\n", item.Name)
					continue
				}
				if github.IsConflict409(err) {
					_, _ = fmt.Fprintf(stdout, "\r  %-45s  ⚠ branch protection requires PR — switching to PR mode\n", item.Name)
					// Retry this item via PR mode.
					fileChanges = append(fileChanges, github.FileChange{
						Path:    item.Name,
						Content: item.Content,
					})
					fallback = true
					deferRequiredChecks(opts)
					continue
				}
				_, _ = fmt.Fprint(stdout, "\r")
				wizard.PrintItemError(stdout, item, err)
				if itemFailedRequired(item) {
					requiredFailed = true
				}
			} else {
				_, _ = fmt.Fprintf(stdout, "\r  %-45s  ✓\n", item.Name)
			}
		} else {
			nonFileItems = append(nonFileItems, item)
		}
	}

	// If we collected file changes for PR mode, batch them.
	if len(fileChanges) > 0 {
		_, _ = fmt.Fprintf(stdout, "\n  Creating PR with %d file changes...\n", len(fileChanges))
		prNum, err := c.ApplyViaPR(owner, repo, branch, fileChanges, stdout)
		if err != nil {
			return fmt.Errorf("apply via PR: %w", err)
		}
		if prNum > 0 {
			_, _ = fmt.Fprintf(stdout, "  ✓ PR #%d created: https://github.com/%s/%s/pull/%d\n", prNum, owner, repo, prNum)
		} else {
			_, _ = fmt.Fprintf(stdout, "  no file changes to put in a PR\n")
		}
	}

	// Apply non-file items (settings, security) directly.
	for _, item := range nonFileItems {
		_, _ = fmt.Fprintf(stdout, "  %-45s  applying...", item.Name)
		if err := item.Apply(); err != nil {
			_, _ = fmt.Fprint(stdout, "\r")
			wizard.PrintItemError(stdout, item, err)
			if itemFailedRequired(item) {
				requiredFailed = true
			}
			continue
		}
		_, _ = fmt.Fprintf(stdout, "\r  %-45s  ✓\n", item.Name)
	}

	_, _ = fmt.Fprintf(stdout, "\n  Repo: https://github.com/%s/%s\n", owner, repo)
	if requiredFailed {
		return fmt.Errorf("one or more required items failed")
	}
	return nil
}

// deferRequiredChecks drops merge-blocking status checks so an open harden
// PR is not required to report Codacy/CI contexts that only exist after merge.
func deferRequiredChecks(opts *github.BranchProtectionOptions) {
	if opts != nil {
		opts.SkipStatusChecks = true
	}
}

// itemFailedRequired reports whether a failed item should fail the run.
// --strict treats optional core steps (tag ruleset, security) as required.
func itemFailedRequired(item wizard.Item) bool {
	return !item.Optional || globals.Strict
}
