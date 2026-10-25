// Package audit implements the "fundamentum audit" command.
package audit

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jpvelasco/fundamentum/cmd/apply"
	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/cmd/util"
	"github.com/jpvelasco/fundamentum/internal/github"
	"github.com/jpvelasco/fundamentum/internal/steps"
	"github.com/jpvelasco/fundamentum/internal/templates"
)

// newClient creates the GitHub API client; a package-level var so tests can
// inject a client pointed at a mock server.
var newClient = github.NewClient

// NewCmd returns the audit subcommand.
func NewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "audit OWNER/REPO",
		Short: "Verify a repo matches the Fundamentum harden baseline",
		Long: `Compare a GitHub repository against the intended Fundamentum baseline
without changing anything. Prints a compact PASS/FAIL/WARN/SKIP report.

FAIL — required drift (missing baseline files, weakened ruleset, disabled
       core settings). Fails the run.
WARN — optional drift (content drift on a shipped file, optional tag
       ruleset or secret-scanning drift). Fails only with --strict.
SKIP — intentionally not checked (a step or file excluded via --skip /
       --only, secret scanning on a private repo without GHAS, plan-gated
       protection). Never fails.

A repo whose settings are STRICTER than the baseline passes: required
status checks are judged as "at least the intended ones", and stricter
review settings never count as drift.

Examples:
  fundamentum audit OWNER/REPO
  fundamentum --preset oss audit OWNER/REPO
  fundamentum --ci generic audit OWNER/REPO
  fundamentum --skip octopus.yml audit OWNER/REPO
  fundamentum --strict audit OWNER/REPO`,
		Args: cobra.ExactArgs(1),
		RunE: run,
	}
}

func run(cmd *cobra.Command, args []string) error {
	// Runtime errors must not dump the usage block after the report.
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
	return runWithClient(client, owner, repo, cmd.OutOrStdout())
}

// status is the audit verdict for one check.
type status int

const (
	statusPass status = iota
	statusFail
	statusWarn
	statusSkip
)

func (s status) String() string {
	switch s {
	case statusFail:
		return "FAIL"
	case statusWarn:
		return "WARN"
	case statusSkip:
		return "SKIP"
	default:
		return "PASS"
	}
}

type check struct {
	Name   string
	Status status
	Detail string
}

func runWithClient(client *github.Client, owner, repo string, stdout io.Writer) error {
	if globals.FromFile != "" {
		if err := globals.LoadBaselineFile(globals.FromFile); err != nil {
			return err
		}
	}
	if err := globals.ApplyPreset(globals.Preset); err != nil {
		return err
	}
	info, err := client.GetRepo(owner, repo)
	if err != nil {
		return fmt.Errorf("get repo: %w", err)
	}

	selector := steps.New(globals.SkipSteps, globals.OnlySteps)

	opts := github.BranchProtectionOptions{
		SkipCodeOwners:   strings.EqualFold(info.OwnerType, "Organization"),
		StrictChecks:     globals.BranchUpToDate,
		StrictChecksSet:  globals.BranchUpToDateSet,
	}
	m, err := templates.DetectManifests(func(path string) (bool, error) {
		return client.AnyFileExists(owner, repo, []string{path})
	})
	if err != nil {
		return err
	}
	pack, err := templates.ResolveCIPackFrom(globals.CIPack, m)
	if err != nil {
		return err
	}
	checksWanted := github.ResolveRequiredChecksForPack(globals.RequireChecks, pack)
	// On a free-tier private repo GitHub 403s both the rulesets and classic
	// branch-protection probes with an "Upgrade to GitHub Pro" body, so plan
	// all three absent (the report then shows SKIP plan-gated) rather than
	// aborting.
	protectionUnavailable := false
	branchPlan, err := client.PlanBranchRuleset(owner, repo, checksWanted, opts)
	protectionUnavailable = protectionUnavailable || github.IsBranchProtectionUnavailable(err)
	if err != nil && !github.IsBranchProtectionUnavailable(err) {
		return fmt.Errorf("check branch ruleset: %w", err)
	}
	tagPlan, err := client.PlanTagRuleset(owner, repo)
	protectionUnavailable = protectionUnavailable || github.IsBranchProtectionUnavailable(err)
	if err != nil && !github.IsBranchProtectionUnavailable(err) {
		return fmt.Errorf("check tag ruleset: %w", err)
	}
	classicExists, err := client.ClassicProtectionExists(owner, repo, info.DefaultBranch)
	protectionUnavailable = protectionUnavailable || github.IsBranchProtectionUnavailable(err)
	if err != nil && !github.IsBranchProtectionUnavailable(err) {
		return fmt.Errorf("check classic protection: %w", err)
	}
	dependabot, err := client.DependabotAlertsEnabled(owner, repo)
	if err != nil {
		return fmt.Errorf("check dependabot: %w", err)
	}

	data, err := apply.RenderData(client, info, pack, strings.EqualFold(info.OwnerType, "Organization"))
	if err != nil {
		return err
	}
	files, err := templates.Render(data)
	if err != nil {
		return fmt.Errorf("render templates: %w", err)
	}

	results := []check{
		branchCheck(branchPlan, classicExists, protectionUnavailable, selector),
		tagCheck(tagPlan, protectionUnavailable, selector),
		boolCheck("auto-delete branches", info.DeleteBranchOnMerge, "delete_branch_on_merge is false", steps.Settings, selector),
		boolCheck("Dependabot alerts", dependabot, "not enabled", steps.Security, selector),
		secretCheck(info, selector),
	}
	for _, f := range files {
		if !selector.IncludeFile(f.Path) {
			results = append(results, skipCheck(f.Path, selector))
			continue
		}
		results = append(results, fileCheck(client, owner, repo, f.Path, []byte(f.Content)))
	}

	_, _ = fmt.Fprint(stdout, formatReport(owner, repo, results))
	if failedRequired(results) {
		return fmt.Errorf("audit failed for %s/%s", owner, repo)
	}
	return nil
}

// skipCheck is a SKIP row for a step or file excluded via --skip / --only.
func skipCheck(name string, selector steps.Selector) check {
	return check{Name: name, Status: statusSkip, Detail: "opted out (" + selector.ExcludeReason(name) + ")"}
}

// stepSkipped returns a SKIP row when the step is opted out, otherwise ok=false.
func stepSkipped(step string, selector steps.Selector) (check, bool) {
	if selector.IncludeStep(step) {
		return check{}, false
	}
	return skipCheck(step, selector), true
}

// branchCheck judges protect-main: existing + not-weaker = PASS (stricter
// hand-tuned rulesets pass), drift = FAIL, classic only = FAIL, missing =
// FAIL, plan-gated = SKIP.
func branchCheck(plan github.RulesetPlan, classicExists, protectionUnavailable bool, selector steps.Selector) check {
	name := "protect-main"
	if c, ok := stepSkipped(steps.ProtectMain, selector); ok {
		c.Name = name
		return c
	}
	if protectionUnavailable && !plan.Exists && !classicExists {
		return check{Name: name, Status: statusSkip, Detail: "unavailable on this plan — set it in Settings → Branches"}
	}
	switch {
	case plan.Exists && len(plan.Drift) == 0:
		return check{Name: name, Status: statusPass}
	case plan.Exists:
		return check{Name: name, Status: statusFail, Detail: "weaker than baseline: " + strings.Join(plan.Drift, ", ")}
	case classicExists:
		return check{Name: name, Status: statusFail, Detail: "classic protection only"}
	default:
		return check{Name: name, Status: statusFail, Detail: "missing"}
	}
}

// tagCheck judges protect-version-tags as a WARN-level optional check.
func tagCheck(plan github.RulesetPlan, protectionUnavailable bool, selector steps.Selector) check {
	name := "protect-version-tags"
	if c, ok := stepSkipped(steps.ProtectVersionTags, selector); ok {
		c.Name = name
		return c
	}
	if protectionUnavailable && !plan.Exists {
		return check{Name: name, Status: statusSkip, Detail: "unavailable on this plan"}
	}
	switch {
	case plan.Exists && len(plan.Drift) == 0:
		return check{Name: name, Status: statusPass}
	case plan.Exists:
		return check{Name: name, Status: statusWarn, Detail: "drift: " + strings.Join(plan.Drift, ", ")}
	default:
		return check{Name: name, Status: statusWarn, Detail: "missing"}
	}
}

func boolCheck(name string, pass bool, failDetail, step string, selector steps.Selector) check {
	if c, ok := stepSkipped(step, selector); ok {
		c.Name = name
		return c
	}
	if pass {
		return check{Name: name, Status: statusPass}
	}
	return check{Name: name, Status: statusFail, Detail: failDetail}
}

// secretCheck judges secret scanning: public repos need it (WARN when off —
// free but optional per the baseline); private repos without GHAS SKIP.
func secretCheck(info github.Repo, selector steps.Selector) check {
	name := "secret scanning"
	if c, ok := stepSkipped(steps.Security, selector); ok {
		c.Name = name
		return c
	}
	if !github.IsPublicVisibility(info.Visibility) && !globals.AdvancedSecurity {
		return check{Name: name, Status: statusSkip, Detail: "private repo without GHAS (not required)"}
	}
	if info.SecretScanning && info.SecretPushProtection {
		return check{Name: name, Status: statusPass}
	}
	return check{Name: name, Status: statusWarn, Detail: "not enabled"}
}

// fileCheck compares a shipped file: missing = FAIL, alias present = PASS
// (legacy path), content drift = WARN with a diffstat. Content drift is
// surfaced (not ignored) but does not hard-fail — intentional local tweaks
// (action version bumps, custom exclude paths) escalate only with --strict.
func fileCheck(client *github.Client, owner, repo, path string, want []byte) check {
	action, detail, err := client.FileStatusDetail(owner, repo, path, want)
	if err != nil {
		return check{Name: path, Status: statusWarn, Detail: "status check failed: " + err.Error()}
	}
	switch action {
	case "skip":
		return check{Name: path, Status: statusPass}
	case "update":
		return check{Name: path, Status: statusWarn, Detail: "content differs from template (" + detail + ")"}
	default:
		// Missing at the canonical path — a legacy alias still counts.
		exists, err := client.AnyFileExists(owner, repo, otherAuditAliases(path))
		if err != nil {
			return check{Name: path, Status: statusWarn, Detail: "alias check failed: " + err.Error()}
		}
		if exists {
			return check{Name: path, Status: statusPass, Detail: "alias path in use"}
		}
		return check{Name: path, Status: statusFail, Detail: "missing"}
	}
}

// otherAuditAliases returns the alias variants for a path (the canonical
// path itself excluded — it was already checked).
func otherAuditAliases(path string) []string {
	variants := templates.FileCheckPaths(path)
	var out []string
	for _, v := range variants {
		if v != path {
			out = append(out, v)
		}
	}
	return out
}

// failedRequired reports whether the run must exit non-zero: any FAIL, plus
// any WARN when --strict escalates optional drift.
func failedRequired(results []check) bool {
	for _, c := range results {
		switch c.Status {
		case statusFail:
			return true
		case statusWarn:
			if globals.Strict {
				return true
			}
		}
	}
	return false
}

func formatReport(owner, repo string, results []check) string {
	var b strings.Builder
	fmt.Fprintf(&b, "fundamentum audit %s/%s\n\n", owner, repo)
	fmt.Fprintf(&b, "%-45s  %s\n", "Check", "Result")
	fmt.Fprintf(&b, "%-45s  %s\n", strings.Repeat("-", 45), strings.Repeat("-", 20))
	var failed, warns, skipped int
	for _, c := range results {
		switch c.Status {
		case statusFail:
			failed++
		case statusWarn:
			warns++
		case statusSkip:
			skipped++
		}
		line := c.Status.String()
		if c.Detail != "" {
			line += " — " + c.Detail
		}
		fmt.Fprintf(&b, "%-45s  %s\n", c.Name, line)
	}
	fmt.Fprintf(&b, "\n  %d failed", failed)
	if warns > 0 {
		fmt.Fprintf(&b, ", %d warn", warns)
	}
	if skipped > 0 {
		fmt.Fprintf(&b, ", %d skipped", skipped)
	}
	fmt.Fprint(&b, ".\n")
	return b.String()
}
