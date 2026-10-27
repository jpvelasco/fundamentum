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
	"github.com/jpvelasco/fundamentum/internal/steps"
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

Files land as ONE commit (git data API); --pr batches them into a single
pull request instead. A direct commit rejected by branch protection (409)
switches to PR mode automatically.

Reconcile MERGES with the live ruleset: required checks already present keep
their integration_id, extra checks and unowned rules survive, and stricter
live review settings are never weakened.

Steps can be excluded or selected with --skip / --only: settings,
protect-main, protect-version-tags, security, files, or an exact file path.

Examples:
  fundamentum apply OWNER/REPO              # interactive harden
  fundamentum --dry-run apply OWNER/REPO    # preview without changes
  fundamentum --pr apply OWNER/REPO         # files via PR; settings still apply live
  fundamentum --preset oss apply OWNER/REPO # non-interactive public baseline
  fundamentum --from baseline.json apply OWNER/REPO
  fundamentum --ci node apply OWNER/REPO    # Node starter CI
  fundamentum --ci generic apply OWNER/REPO # generic starter CI (no language)
  fundamentum --strict apply OWNER/REPO     # fail if any core step fails
  fundamentum --skip protect-main,security apply OWNER/REPO # leave steps alone
  fundamentum --octopus apply OWNER/REPO    # opt in to Octopus Review
  fundamentum --require-checks Lint,gosec apply OWNER/REPO
  fundamentum --token $GITHUB_TOKEN apply OWNER/REPO`,
		Args: cobra.ExactArgs(1),
		RunE: run,
	}
}

func run(cmd *cobra.Command, args []string) error {
	// Runtime errors must not dump the usage block after the plan output.
	cmd.SilenceUsage = true

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
	if err := validateBotManaged(client); err != nil {
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

	data, err := RenderData(client, info, pack, orgOwner)
	if err != nil {
		return fmt.Errorf("plan %s/%s: %w (verify the token grants Contents read access and retry)", owner, repo, err)
	}
	rendered, err := renderTemplates(data)
	if err != nil {
		return fmt.Errorf("render templates: %w", err)
	}

	// Free-tier guard: the shipped workflow reverts direct pushes to the
	// default branch, so file changes must go through a PR — including the
	// bootstrap commit that ships the guard itself (a direct commit would be
	// reverted seconds after landing).
	guardPR := guardShipped(client, owner, repo, rendered)
	if guardPR && !globals.ViaPR {
		_, _ = fmt.Fprint(stdout, prGuardNotice)
	}

	// Validate --skip/--only tokens against the real steps and rendered
	// paths: a typo must be an error, not a silent no-op that lets apply
	// touch exactly what the operator meant to leave alone.
	if unknown := steps.New(globals.SkipTokens(), globals.OnlySteps).Unknown(renderedPaths(rendered)); len(unknown) > 0 {
		return fmt.Errorf("unknown --skip/--only key(s): %s — use settings, protect-main, protect-version-tags, security, files, or an exact shipped file path", strings.Join(unknown, ", "))
	}

	// Pre-flight: check branch protection state before asking solo/team.
	// On a free-tier private repo GitHub returns "Upgrade to GitHub Pro" 403s
	// for both the rulesets and classic branch-protection APIs, so plan all
	// three probes as absent instead of aborting; other 403s (token scope,
	// SSO, IP allow list) still abort. A plan-unavailable 403 is remembered
	// so dry-run does not claim it would create protection that cannot land.
	var opts github.BranchProtectionOptions
	opts.SkipCodeOwners = orgOwner
	opts.StrictChecks = globals.BranchUpToDate
	opts.StrictChecksSet = globals.BranchUpToDateSet
	opts.BotManaged = globals.BotManaged
	opts.PolicyChecks = globals.PolicyChecks
	protectionUnavailable := false
	branchPlan, err := client.PlanBranchRuleset(owner, repo, requiredChecks(pack), opts)
	protectionUnavailable = noteProtectionUnavailable(protectionUnavailable, err)
	if err != nil && !github.IsBranchProtectionUnavailable(err) {
		return fmt.Errorf("check branch ruleset: %w", err)
	}
	tagPlan, err := client.PlanTagRuleset(owner, repo)
	protectionUnavailable = noteProtectionUnavailable(protectionUnavailable, err)
	if err != nil && !github.IsBranchProtectionUnavailable(err) {
		return fmt.Errorf("check tag ruleset: %w", err)
	}
	classicExists, err := client.ClassicProtectionExists(owner, repo, branch)
	protectionUnavailable = noteProtectionUnavailable(protectionUnavailable, err)
	if err != nil && !github.IsBranchProtectionUnavailable(err) {
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

	items, err := buildItems(client, &info, owner, repo, branch, visibility, rendered, branchPlan, tagPlan, classicExists, protectionUnavailable, &opts, paidSecurity, pack, stdout)
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
	if err := applyItems(client, owner, repo, branch, items, globals.ViaPR || guardPR, &opts, stdout); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "\n  ✓ Done — https://github.com/%s/%s\n", owner, repo)
	return nil
}

// RenderData assembles the template substitution data shared by apply and
// audit: the fixed fields from flags + repo info, plus live detection —
// README/agent-guide filenames and, for the node pack, the lint/test
// commands from package.json scripts. A nil client (init --dry-run) skips
// detection and takes the defaults. The same function backs audit so its
// rendered baseline matches what apply would ship.
func RenderData(client *github.Client, info github.Repo, pack string, orgOwner bool) (templates.RepoData, error) {
	data := templates.RepoData{
		Owner:              info.Owner,
		RepoName:           info.RepoName,
		DefaultBranch:      info.DefaultBranch,
		Visibility:         info.Visibility,
		CodeOwnerLine:      codeOwnerLine(info.Owner, orgOwner),
		CIPack:             pack,
		Octopus:            globals.Octopus,
		CodacyInstructions: globals.CodacyInstructions,
		BotManaged:         globals.BotManaged,
	}
	if client == nil {
		return data, nil
	}
	readme, agentGuide, nodeLint, nodeTest, err := templates.DetectDocData(
		func(path string) (bool, error) {
			return client.AnyFileExists(info.Owner, info.RepoName, []string{path})
		},
		func() ([]byte, bool, error) {
			body, ok, err := client.ReadFileContents(info.Owner, info.RepoName, "package.json")
			if err != nil {
				err = fmt.Errorf("read package.json: %w", err)
			}
			return body, ok, err
		},
		pack,
	)
	if err != nil {
		return data, err
	}
	data.ReadmePath = readme
	data.AgentGuideLine = agentGuide
	data.NodeLintCommand = nodeLint
	data.NodeTestCommand = nodeTest
	return data, nil
}

// PlanNewRepo prints a dry-run harden plan for a repository that does not
// exist yet. Files, settings, and rulesets are modeled as absent — no GitHub
// GET is issued, so init --dry-run can preview create+harden.
func PlanNewRepo(owner, repo, visibility string, stdout io.Writer) error {
	if err := loadFlags(); err != nil {
		return err
	}
	// init --dry-run has no client for the token guardrail, but the flag
	// guardrails (--octopus, unpinned policy checks) still apply.
	if err := validateBotManagedFlags(); err != nil {
		return err
	}
	pack, err := resolveCIPack(nil, owner, repo)
	if err != nil {
		return err
	}
	// A repo that does not exist yet has nothing to detect; the nil client
	// takes the documentation defaults (README.md, AGENTS.md).
	info := github.Repo{Owner: owner, RepoName: repo, DefaultBranch: "main", Visibility: visibility}
	data, err := RenderData(nil, info, pack, false)
	if err != nil {
		return err
	}
	rendered, err := renderTemplates(data)
	if err != nil {
		return fmt.Errorf("render templates: %w", err)
	}
	if unknown := steps.New(globals.SkipTokens(), globals.OnlySteps).Unknown(renderedPaths(rendered)); len(unknown) > 0 {
		return fmt.Errorf("unknown --skip/--only key(s): %s — use settings, protect-main, protect-version-tags, security, files, or an exact shipped file path", strings.Join(unknown, ", "))
	}
	// Named presets must match a live apply: only strict / --advanced-security
	// enable GHAS. Interactive dry-run still shows the offered plan line.
	paid := globals.AdvancedSecurity || github.IsPublicVisibility(visibility) || (globals.DryRun && globals.Preset == "")
	items, err := buildItems(nil, nil, owner, repo, "main", visibility, rendered, github.RulesetPlan{}, github.RulesetPlan{}, false, false, &github.BranchProtectionOptions{}, paid, pack, stdout)
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
	info *github.Repo,
	owner, repo, branch, visibility string,
	rendered []templates.RenderedFile,
	branchPlan, tagPlan github.RulesetPlan,
	classicExists, protectionUnavailable bool,
	opts *github.BranchProtectionOptions,
	paidSecurity bool,
	pack string,
	stdout io.Writer,
) ([]wizard.Item, error) {
	var items []wizard.Item

	selector := steps.New(globals.SkipTokens(), globals.OnlySteps)
	aliases := templates.FileAliases()

	// Files first — branch protection applied after, so direct commits are still allowed.
	for _, f := range rendered {
		file := f
		action := wizard.ActionCreate
		detail := ""

		if !selector.IncludeFile(file.Path) {
			items = append(items, optedOutFileItem(file.Path, selector))
			continue
		}

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
				action, detail, err = fileItemAction(c, owner, repo, file.Path, file.Content)
				if err != nil {
					return nil, fmt.Errorf("check status of %s: %w", file.Path, err)
				}
			}
		}
		items = append(items, wizard.Item{
			Name:   file.Path,
			Action: action,
			Detail: detail,
			// No Apply closure: file items are applied exclusively through the
			// client (single-commit batch, per-file fallback, or PR mode).
			Content: []byte(file.Content),
		})
	}

	settingsItem := generalSettingsItem(c, info, owner, repo)
	if !selector.IncludeStep(steps.Settings) {
		settingsItem = optedOutItem(settingsItem.Name, steps.Settings, selector)
	}
	items = append(items, settingsItem)

	protectionItem := branchProtectionItem(c, owner, repo, branch, visibility, branchPlan, classicExists, protectionUnavailable, opts, pack, guardShipped(c, owner, repo, rendered), stdout)
	if !selector.IncludeStep(steps.ProtectMain) {
		protectionItem = optedOutItem(protectionItem.Name, steps.ProtectMain, selector)
	}
	items = append(items, protectionItem)

	tagItem := tagRulesetItem(c, owner, repo, tagPlan, protectionUnavailable)
	if !selector.IncludeStep(steps.ProtectVersionTags) {
		tagItem = optedOutItem(tagItem.Name, steps.ProtectVersionTags, selector)
	}
	items = append(items, tagItem)

	secItem, err := securityItem(c, info, owner, repo, visibility, rendered, paidSecurity)
	if err != nil {
		return nil, err
	}
	if !selector.IncludeStep(steps.Security) {
		secItem = optedOutItem(secItem.Name, steps.Security, selector)
	}
	items = append(items, secItem)

	// The Actions default-token step is bot-managed only: with the profile off
	// it never appears in the plan.
	if globals.BotManaged {
		actItem := actionsPermissionsItem(c, owner, repo, selector, stdout)
		items = append(items, actItem)
	}

	return items, nil
}

// printRemovedBypassActors prints the bypass actors a bot-managed reconcile
// drops from protect-main (the profile sends an explicit empty list). A
// bot-managed repo grants no one a standing bypass around the ruleset.
func printRemovedBypassActors(stdout io.Writer, removed []github.BypassActor) {
	if len(removed) == 0 {
		return
	}
	names := make([]string, 0, len(removed))
	for _, a := range removed {
		names = append(names, github.DescribeBypassActor(a))
	}
	_, _ = fmt.Fprintf(stdout, "\n    bot-managed: removing %d ruleset bypass actor(s): %s\n", len(removed), strings.Join(names, ", "))
}

// actionsPermissionsItem is the bot-managed "actions" step: lock the default
// GITHUB_TOKEN down to read-only and forbid PR approval. The Apply closure
// always prints the before/after to the injected writer so the operator sees
// the exact change. A nil client (init --dry-run) shows a would-update row
// with no Apply closure.
func actionsPermissionsItem(c *github.Client, owner, repo string, selector steps.Selector, stdout io.Writer) wizard.Item {
	name := "Actions default token (read-only, no PR approval)"
	if !selector.IncludeStep(steps.Actions) {
		return optedOutItem(name, steps.Actions, selector)
	}
	if c == nil {
		return wizard.Item{Name: name, Action: wizard.ActionUpdate}
	}
	return wizard.Item{
		Name:   name,
		Action: wizard.ActionUpdate,
		Apply: func() error {
			before, err := c.GetActionsWorkflowPermissions(owner, repo)
			if err != nil {
				return err
			}
			// Lead with a newline: the apply loop printed "applying..." without
			// one, so the before/after block starts on its own lines.
			_, _ = fmt.Fprintf(stdout, "\n    before: default_workflow_permissions=%s can_approve_pull_request_reviews=%t\n",
				before.DefaultWorkflowPermissions, before.CanApprovePullRequestReviews)
			if err := c.SetActionsWorkflowPermissions(owner, repo); err != nil {
				return err
			}
			after, err := c.GetActionsWorkflowPermissions(owner, repo)
			if err != nil {
				// The PUT succeeded; report the intended end state if the
				// confirm read fails rather than failing the whole step.
				_, _ = fmt.Fprintf(stdout, "    after:  default_workflow_permissions=read can_approve_pull_request_reviews=false (confirm read failed: %v)\n", err)
				return nil
			}
			_, _ = fmt.Fprintf(stdout, "    after:  default_workflow_permissions=%s can_approve_pull_request_reviews=%t\n",
				after.DefaultWorkflowPermissions, after.CanApprovePullRequestReviews)
			return nil
		},
	}
}

// optedOutItem returns a skip row for a step or file excluded by --skip /
// --only, naming the mechanism so the plan table shows the exclusion.
// stepKey is the canonical step key or the file's target path.
func optedOutItem(name, stepKey string, selector steps.Selector) wizard.Item {
	return wizard.Item{Name: name, Action: wizard.ActionSkip, OptOutReason: selector.ExcludeReason(stepKey)}
}

// optedOutFileItem is optedOutItem for file paths (the "files" key and exact
// path entries have file-specific reason semantics).
func optedOutFileItem(path string, selector steps.Selector) wizard.Item {
	return wizard.Item{Name: path, Action: wizard.ActionSkip, OptOutReason: selector.ExcludeFileReason(path)}
}

func generalSettingsItem(c *github.Client, info *github.Repo, owner, repo string) wizard.Item {
	action := wizard.ActionCreate
	detail := ""
	if info != nil {
		action = wizard.ActionUpdate
		detail = "delete_branch_on_merge=false"
		if info.DeleteBranchOnMerge {
			action = wizard.ActionSkip
			detail = ""
		}
	}
	return wizard.Item{
		Name:   generalSettingsName(),
		Action: action,
		Detail: detail,
		Apply:  func() error { return c.ApplyGeneralSettings(owner, repo, globals.BotManaged) },
	}
}

// generalSettingsName names the settings item so a dry-run shows the
// bot-managed merge lockdown when the profile is on.
func generalSettingsName() string {
	if globals.BotManaged {
		return "General settings (squash-only merges, auto-delete branches)"
	}
	return "General settings (auto-delete branches)"
}

func securityItem(c *github.Client, info *github.Repo, owner, repo, visibility string, rendered []templates.RenderedFile, paidSecurity bool) (wizard.Item, error) {
	securityName := "Security (Dependabot)"
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

	action := wizard.ActionCreate
	detail := ""
	if info != nil {
		// Fail closed: a status-check error aborts the plan instead of
		// silently planning an update (a false "would update" once made
		// this repo's dry run lie about already-enabled security).
		missing, err := securityMissing(c, *info, owner, repo, secOpts)
		if err != nil {
			return wizard.Item{}, err
		}
		if len(missing) == 0 {
			action = wizard.ActionSkip
		} else {
			action = wizard.ActionUpdate
			detail = "off: " + strings.Join(missing, ", ")
		}
	}
	return wizard.Item{
		Name:     securityName,
		Action:   action,
		Detail:   detail,
		Optional: true,
		Apply:    func() error { return c.EnableSecurity(owner, repo, secOpts) },
	}, nil
}

// securityMissing reports which security features are still off. An empty
// slice means the repo is fully configured for the intended visibility.
// The CodeQL check accepts EITHER default setup or a codeql workflow in
// .github/workflows — an advanced-workflow repo must not be flagged.
func securityMissing(c *github.Client, info github.Repo, owner, repo string, opts github.SecurityOptions) ([]string, error) {
	var missing []string

	alertsEnabled, err := c.DependabotAlertsEnabled(owner, repo)
	if err != nil {
		return nil, err
	}
	if !alertsEnabled {
		missing = append(missing, "dependabot alerts")
	}
	fixesEnabled, err := c.AutomatedSecurityFixesEnabled(owner, repo)
	if err != nil {
		return nil, err
	}
	if !fixesEnabled {
		missing = append(missing, "automated security fixes")
	}
	if github.IsPublicVisibility(opts.Visibility) || opts.PaidFeatures {
		if !info.SecretScanning {
			missing = append(missing, "secret scanning")
		}
		if !info.SecretPushProtection {
			missing = append(missing, "push protection")
		}
	}
	if github.IsPublicVisibility(opts.Visibility) && !opts.AdvancedCodeQL {
		defaultSetup, err := c.DefaultCodeQLSetupEnabled(owner, repo)
		if err != nil {
			return nil, err
		}
		if !defaultSetup {
			// An advanced CodeQL workflow also counts — CodeQL analysis
			// exists even though default setup is not configured.
			codeqlWorkflow, err := c.AnyFileExists(owner, repo, github.CodeQLWorkflowVariants)
			if err != nil {
				return nil, err
			}
			if !codeqlWorkflow {
				missing = append(missing, "codeql")
			}
		}
	}
	return missing, nil
}

// branchProtectionItem returns the correct Item for branch protection based on current state:
//   - matching ruleset → skip
//   - drifted ruleset → update in place
//   - classic exists → upgrade (create ruleset + remove classic)
//   - neither exists → ruleset for public repos; try ruleset then fall back to classic for private
func branchProtectionItem(c *github.Client, owner, repo, branch, visibility string, plan github.RulesetPlan, classicExists, protectionUnavailable bool, opts *github.BranchProtectionOptions, pack string, guardPresent bool, stdout io.Writer) wizard.Item {
	if opts == nil {
		opts = &github.BranchProtectionOptions{}
	}
	// Free-tier guard shipped or already present: the Actions workflow is
	// the enforcement (it reverts non-PR pushes), so this item points at
	// the file batch instead of trying a ruleset the plan refuses.
	if protectionUnavailable && guardPresent {
		return wizard.Item{
			Name:   "Branch protection (free-tier Actions guard)",
			Action: wizard.ActionSkip,
			Detail: "enforced via " + github.FreeTierGuardPath + " — the guard reverts direct pushes",
		}
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
			Detail: "drift: " + strings.Join(plan.Drift, ", "),
			Apply: func() error {
				printRemovedBypassActors(stdout, github.RemovedBypassActors(plan.Live, *opts))
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
		if item, ok := unavailableProtectionItem("Branch protection (protect-main)", false, protectionUnavailable, false); ok {
			return item
		}
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
				if !github.IsBranchProtectionUnavailable(err) {
					return err
				}
				if err := c.ApplyClassicBranchProtection(owner, repo, branch, checks, *opts); err != nil {
					// Free-tier private: the classic API is plan-gated too. Both
					// protection paths are unavailable — point at the manual page
					// while preserving the 403 cause for re-inspection (--verbose).
					if github.IsBranchProtectionUnavailable(err) {
						return fmt.Errorf("branch protection requires GitHub Pro for this private repo: rulesets and classic protection are both unavailable via API — set it manually in repo Settings → Branches: %w", err)
					}
					return err
				}
				return nil
			},
		}
	}
}

// renderedPaths lists the target paths of the rendered files (for --skip /
// --only token validation).
func renderedPaths(rendered []templates.RenderedFile) []string {
	paths := make([]string, 0, len(rendered))
	for _, f := range rendered {
		paths = append(paths, f.Path)
	}
	return paths
}

func loadFlags() error {
	if globals.FromFile != "" {
		if err := globals.LoadBaselineFile(globals.FromFile); err != nil {
			return err
		}
	}
	return globals.ApplyPreset(globals.Preset)
}

// validateBotManagedFlags enforces the profile's flag-only guardrails (no
// client needed): --octopus is refused outright, and every policy check must
// pin its app. init --dry-run runs this too (it has no client for the token
// check).
func validateBotManagedFlags() error {
	if !globals.BotManaged {
		return nil
	}
	if globals.Octopus {
		return fmt.Errorf("--octopus cannot be combined with --bot-managed: the bot-managed profile ships no third-party review workflow")
	}
	return github.ValidatePolicyChecks(globals.PolicyChecks)
}

// validateBotManaged enforces the bot-managed profile's flag and token
// guardrails before any planning or mutation: the flag rules above, plus the
// token must belong to a human, not a bot or app — the profile's guardrails
// assume a human operator is hardening the automation's repo.
func validateBotManaged(client *github.Client) error {
	if err := validateBotManagedFlags(); err != nil {
		return err
	}
	if !globals.BotManaged {
		return nil
	}
	isBot, why, err := client.IsBotOrAppToken()
	if err != nil {
		return fmt.Errorf("verify token owner for --bot-managed: %w", err)
	}
	if isBot {
		return fmt.Errorf("--bot-managed refuses to apply under a bot or app token (%s): run it as a human operator", why)
	}
	return nil
}

const prModeNotice = "--pr: file changes go in a pull request. Settings, security, and branch protection still apply live via the API. Required status checks are deferred until you re-apply after that PR merges.\n\n"

func printPRModeNotice(stdout io.Writer) {
	if globals.ViaPR {
		_, _ = fmt.Fprint(stdout, prModeNotice)
	}
}

func resolveCIPack(c *github.Client, owner, repo string) (string, error) {
	m := templates.Manifests{}
	if c != nil {
		var err error
		m, err = templates.DetectManifests(func(path string) (bool, error) {
			return c.AnyFileExists(owner, repo, []string{path})
		})
		if err != nil {
			return "", err
		}
	}
	return templates.ResolveCIPackFrom(globals.CIPack, m)
}

func requiredChecks(pack string) []string {
	return github.ResolveRequiredChecksForPack(globals.RequireChecks, pack)
}

// protectionUnavailableDetail is the dry-run row for a plan that 403s both
// ruleset and classic protection. ActionSkip so applyItems will not call
// Apply; Detail so the table does not say "already exists" or "would create".
// Live apply is unchanged: it still tries ruleset, then classic, then the
// Settings → Branches error.
const protectionUnavailableDetail = "unavailable on this plan — set it in Settings → Branches"

// unavailableProtectionItem is the dry-run row when protection APIs are
// plan-unavailable and the item does not already exist. Live runs and
// existing plans return ok=false so the caller keeps ActionCreate.
func unavailableProtectionItem(name string, optional, protectionUnavailable, exists bool) (wizard.Item, bool) {
	if !globals.DryRun || !protectionUnavailable || exists {
		return wizard.Item{}, false
	}
	return wizard.Item{
		Name:     name,
		Action:   wizard.ActionSkip,
		Optional: optional,
		Detail:   protectionUnavailableDetail,
	}, true
}

// noteProtectionUnavailable remembers a plan-limit 403. The probe is still
// planned as absent (err is left for the caller to ignore or abort).
func noteProtectionUnavailable(already bool, err error) bool {
	return already || github.IsBranchProtectionUnavailable(err)
}

func tagRulesetItem(c *github.Client, owner, repo string, plan github.RulesetPlan, protectionUnavailable bool) wizard.Item {
	if item, ok := unavailableProtectionItem("Tag ruleset (protect-version-tags)", true, protectionUnavailable, plan.Exists); ok {
		return item
	}
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
		item.Detail = "drift: " + strings.Join(plan.Drift, ", ")
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

// fileItemAction maps FileStatus to a wizard action plus a drift summary,
// honoring --no-overwrite. Status-check errors are surfaced, not defaulted:
// pretending "create" on a failed check would misreport the plan and fail
// later with confusing PUTs.
func fileItemAction(c *github.Client, owner, repo, path, content string) (wizard.Action, string, error) {
	status, detail, err := c.FileStatusDetail(owner, repo, path, []byte(content))
	if err != nil {
		return wizard.ActionCreate, "", err
	}
	switch status {
	case "skip":
		return wizard.ActionSkip, "", nil
	case "update":
		if globals.NoOverwrite {
			return wizard.ActionSkip, "", nil
		}
		return wizard.ActionUpdate, "content differs (" + detail + ")", nil
	default:
		return wizard.ActionCreate, "", nil
	}
}

func codeOwnerLine(owner string, org bool) string {
	if org {
		return "# Organizations need a team (@org/team), not @" + owner
	}
	return "* @" + owner
}

// applyItems runs the item list. File changes land as ONE commit on the
// default branch (git data API) instead of one commit per file; --pr batches
// them into a PR instead. When a direct batch is rejected by branch
// protection (409), the remaining file changes switch to PR mode — no
// re-run needed. If the single-commit path fails for another reason (e.g.
// the batch contains a workflow path the git data API refuses without the
// workflow token scope), apply falls back to the per-file Contents API,
// which skips scope-refused workflow files with a warning.
func applyItems(c *github.Client, owner, repo, branch string, items []wizard.Item, viaPR bool, opts *github.BranchProtectionOptions, stdout io.Writer) error {
	var fileChanges []github.FileChange
	var nonFileItems []wizard.Item
	requiredFailed := false
	if viaPR {
		deferRequiredChecks(opts)
	}

	for _, item := range items {
		if wizard.ShouldSkip(item, stdout) {
			continue
		}
		if item.Content != nil {
			fileChanges = append(fileChanges, github.FileChange{
				Path:    item.Name,
				Content: item.Content,
			})
			continue
		}
		nonFileItems = append(nonFileItems, item)
	}

	switch {
	case len(fileChanges) > 0 && viaPR:
		if err := applyFilesViaPR(c, owner, repo, branch, fileChanges, stdout); err != nil {
			return err
		}
	case len(fileChanges) > 0:
		err := c.CommitFiles(owner, repo, branch, fileChanges)
		switch {
		case err == nil:
			_, _ = fmt.Fprintf(stdout, "  %-45s  ✓ committed as one commit (%d files)\n", "files", len(fileChanges))
		case github.IsConflict409(err):
			_, _ = fmt.Fprintf(stdout, "  %-45s  ⚠ branch protection requires PR — switching to PR mode\n", "files")
			deferRequiredChecks(opts)
			if err := applyFilesViaPR(c, owner, repo, branch, fileChanges, stdout); err != nil {
				return err
			}
		default:
			_, _ = fmt.Fprintf(stdout, "  %-45s  ⚠ single commit failed (%v) — applying file by file\n", "files", err)
			requiredFailed = requiredFailed || applyFilesPerItem(c, owner, repo, branch, fileChanges, opts, stdout)
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

// applyFilesViaPR batches fileChanges into a single pull request.
func applyFilesViaPR(c *github.Client, owner, repo, branch string, changes []github.FileChange, stdout io.Writer) error {
	_, _ = fmt.Fprintf(stdout, "\n  Creating PR with %d file changes...\n", len(changes))
	prNum, err := c.ApplyViaPR(owner, repo, branch, changes, stdout)
	if err != nil {
		return fmt.Errorf("apply via PR: %w", err)
	}
	if prNum > 0 {
		_, _ = fmt.Fprintf(stdout, "  ✓ PR #%d created: https://github.com/%s/%s/pull/%d\n", prNum, owner, repo, prNum)
	} else {
		_, _ = fmt.Fprintf(stdout, "  no file changes to put in a PR\n")
	}
	return nil
}

// applyFilesPerItem is the fallback direct path: one Contents-API PUT per
// file. It is only used when the single-commit batch failed for a non-409
// reason (e.g. the batch contains a workflow path, which the git data API
// refuses for a token without the workflow scope). Workflow writes refused
// by the scope gate are skipped with a warning — creates as well as updates.
// Returns true when a required item failed.
func applyFilesPerItem(c *github.Client, owner, repo, branch string, changes []github.FileChange, opts *github.BranchProtectionOptions, stdout io.Writer) bool {
	requiredFailed := false
	for _, ch := range changes {
		_, _ = fmt.Fprintf(stdout, "  %-45s  applying...", ch.Path)
		action, err := c.UpsertFile(owner, repo, ch.Path, ch.Content)
		switch {
		case err != nil && github.IsWorkflowLocked(err):
			_, _ = fmt.Fprintf(stdout, "\r  %-45s  ⚠ workflow write refused — token lacks workflow scope; skipped (add the scope and re-run to ship it)\n", ch.Path)
		case err != nil && github.IsConflict409(err):
			// Protection kicked in mid-fallback — the rest goes to a PR.
			_, _ = fmt.Fprintf(stdout, "\r  %-45s  ⚠ branch protection requires PR — switching to PR mode\n", ch.Path)
			deferRequiredChecks(opts)
			remaining := []github.FileChange{ch}
			// collect the not-yet-applied remainder
			rest := changesAfter(changes, ch.Path)
			remaining = append(remaining, rest...)
			if err := applyFilesViaPR(c, owner, repo, branch, remaining, stdout); err != nil {
				_, _ = fmt.Fprintf(stdout, "  %-45s  ✗ %v\n", "files (PR fallback)", err)
				requiredFailed = true
			}
			return requiredFailed
		case err != nil:
			_, _ = fmt.Fprint(stdout, "\r")
			wizard.PrintItemError(stdout, wizard.Item{Name: ch.Path}, err)
			requiredFailed = true
		default:
			_ = action
			_, _ = fmt.Fprintf(stdout, "\r  %-45s  ✓\n", ch.Path)
		}
	}
	return requiredFailed
}

// changesAfter returns the changes after the first entry whose Path matches.
func changesAfter(changes []github.FileChange, path string) []github.FileChange {
	for i, ch := range changes {
		if ch.Path == path {
			return changes[i+1:]
		}
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

// prGuardNotice explains why file changes go via a PR without --pr.
const prGuardNotice = "--free-tier-guard: the protect-main Actions guard reverts direct pushes to the default branch, so file changes go in a pull request (settings and security still apply live).\n"

// guardShipped reports whether the free-tier Actions guard is being shipped
// in this apply (rendered) or already exists on the repo. Either way the
// repo's direct-commit path is guarded: any push to the default branch that
// is not a merged pull request gets force-reverted.
func guardShipped(c *github.Client, owner, repo string, rendered []templates.RenderedFile) bool {
	if containsPath(rendered, github.FreeTierGuardPath) {
		return true
	}
	if c == nil {
		return false
	}
	exists, err := c.AnyFileExists(owner, repo, []string{github.FreeTierGuardPath})
	if err != nil {
		// A probe failure must not silently pick direct mode; the apply's
		// fail-closed pre-flight will surface the same error on the file
		// status checks anyway.
		return false
	}
	return exists
}

// containsPath reports whether the rendered set includes the target path.
func containsPath(rendered []templates.RenderedFile, path string) bool {
	for _, f := range rendered {
		if f.Path == path {
			return true
		}
	}
	return false
}
