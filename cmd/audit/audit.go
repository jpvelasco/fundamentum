// Package audit implements the "fundamentum audit" command.
package audit

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/cmd/util"
	"github.com/jpvelasco/fundamentum/internal/github"
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
without changing anything. Prints a compact PASS/FAIL report.

Core checks (always fail the run on drift):
  protect-main, auto-delete branches, Dependabot alerts, and required
  community/CI files.

Optional checks (fail only with --strict):
  protect-version-tags and secret scanning / push protection.

Examples:
  fundamentum audit OWNER/REPO
  fundamentum --preset oss audit OWNER/REPO
  fundamentum --ci generic audit OWNER/REPO
  fundamentum --strict audit OWNER/REPO`,
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
	return runWithClient(client, owner, repo, cmd.OutOrStdout())
}

type check struct {
	Name     string
	Pass     bool
	Detail   string
	Optional bool
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

	opts := github.BranchProtectionOptions{SkipCodeOwners: strings.EqualFold(info.OwnerType, "Organization")}
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
	// A 403 "rulesets not offered on this plan" (free-tier private) means no
	// rulesets can exist, so plan them absent (the audit then reports the
	// classic fallback or "missing") rather than aborting the whole report.
	branchPlan, err := client.PlanBranchRuleset(owner, repo, checksWanted, opts)
	if err != nil && !github.IsRulesetUnavailable(err) {
		return fmt.Errorf("check branch ruleset: %w", err)
	}
	tagPlan, err := client.PlanTagRuleset(owner, repo)
	if err != nil && !github.IsRulesetUnavailable(err) {
		return fmt.Errorf("check tag ruleset: %w", err)
	}
	classicExists, err := client.ClassicProtectionExists(owner, repo, info.DefaultBranch)
	if err != nil {
		return fmt.Errorf("check classic protection: %w", err)
	}
	dependabot, err := client.DependabotAlertsEnabled(owner, repo)
	if err != nil {
		return fmt.Errorf("check dependabot: %w", err)
	}

	data := templates.RepoData{
		Owner:         owner,
		RepoName:      repo,
		DefaultBranch: info.DefaultBranch,
		Visibility:    info.Visibility,
		CIPack:        pack,
	}
	files, err := templates.Render(data)
	if err != nil {
		return fmt.Errorf("render templates: %w", err)
	}

	results := []check{
		branchCheck(branchPlan, classicExists),
		tagCheck(tagPlan),
		boolCheck("auto-delete branches", info.DeleteBranchOnMerge, "delete_branch_on_merge is false", false),
		boolCheck("Dependabot alerts", dependabot, "not enabled", false),
		secretCheck(info),
	}
	for _, f := range files {
		exists, err := client.AnyFileExists(owner, repo, templates.FileCheckPaths(f.Path))
		if err != nil {
			return fmt.Errorf("check file %s: %w", f.Path, err)
		}
		results = append(results, boolCheck(f.Path, exists, "missing", false))
	}

	_, _ = fmt.Fprint(stdout, formatReport(owner, repo, results))
	if failedRequired(results) {
		return fmt.Errorf("audit failed for %s/%s", owner, repo)
	}
	return nil
}

func branchCheck(plan github.RulesetPlan, classicExists bool) check {
	switch {
	case plan.Exists && len(plan.Drift) == 0:
		return check{Name: "protect-main", Pass: true}
	case plan.Exists:
		return check{Name: "protect-main", Detail: strings.Join(plan.Drift, ", ")}
	case classicExists:
		return check{Name: "protect-main", Detail: "classic protection only"}
	default:
		return check{Name: "protect-main", Detail: "missing"}
	}
}

func tagCheck(plan github.RulesetPlan) check {
	c := check{Name: "protect-version-tags", Optional: true}
	switch {
	case plan.Exists && len(plan.Drift) == 0:
		c.Pass = true
	case plan.Exists:
		c.Detail = strings.Join(plan.Drift, ", ")
	default:
		c.Detail = "missing"
	}
	return c
}

func secretCheck(info github.Repo) check {
	c := check{Name: "secret scanning", Optional: true}
	if !github.IsPublicVisibility(info.Visibility) && !globals.AdvancedSecurity {
		c.Pass = true
		c.Detail = "private (not required)"
		return c
	}
	if info.SecretScanning && info.SecretPushProtection {
		c.Pass = true
		return c
	}
	c.Detail = "not enabled"
	return c
}

func boolCheck(name string, pass bool, failDetail string, optional bool) check {
	c := check{Name: name, Pass: pass, Optional: optional}
	if !pass {
		c.Detail = failDetail
	}
	return c
}

func failedRequired(results []check) bool {
	for _, c := range results {
		if c.Pass {
			continue
		}
		if !c.Optional || globals.Strict {
			return true
		}
	}
	return false
}

func formatReport(owner, repo string, results []check) string {
	var b strings.Builder
	fmt.Fprintf(&b, "fundamentum audit %s/%s\n\n", owner, repo)
	fmt.Fprintf(&b, "%-45s  %s\n", "Check", "Result")
	fmt.Fprintf(&b, "%-45s  %s\n", strings.Repeat("-", 45), strings.Repeat("-", 20))
	var failed, optionalFailed int
	for _, c := range results {
		status := "PASS"
		if !c.Pass {
			status = "FAIL"
			if c.Optional {
				optionalFailed++
				status = "FAIL (optional)"
			} else {
				failed++
			}
			if c.Detail != "" {
				status += " — " + c.Detail
			}
		}
		fmt.Fprintf(&b, "%-45s  %s\n", c.Name, status)
	}
	fmt.Fprintf(&b, "\n  %d failed", failed)
	if optionalFailed > 0 {
		fmt.Fprintf(&b, ", %d optional failed", optionalFailed)
	}
	fmt.Fprint(&b, ".\n")
	return b.String()
}
