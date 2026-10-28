// Package globals holds shared mutable state for all commands.
package globals

import (
	"github.com/jpvelasco/fundamentum/internal/github"
	"github.com/jpvelasco/fundamentum/internal/steps"
)

// DryRun prints actions without applying them when true.
var DryRun bool

// Verbose prints API calls when true.
var Verbose bool

// Token is the GitHub personal access token; falls back to GITHUB_TOKEN env var.
var Token string

// NoOverwrite skips files that already exist rather than updating them.
var NoOverwrite bool

// ViaPR pushes file changes through a PR instead of direct commits.
// Useful when branch protection blocks direct pushes (409).
var ViaPR bool

// AdvancedSecurity enables paid GitHub Advanced Security features
// (secret scanning, push protection) on private/internal repos.
var AdvancedSecurity bool

// Strict treats optional core harden steps (tag ruleset, security) as
// required so a failed apply exits non-zero instead of printing Done.
var Strict bool

// RequireChecks is the explicit required-status-check list from
// --require-checks. nil means use the shipped checks for the resolved
// --ci pack. An empty slice requires no status checks.
var RequireChecks []string

// CIPack is the --ci value: auto, go, generic, or none. Empty means auto.
var CIPack string

// SkipSteps is the --skip list: canonical steps (settings, protect-main,
// protect-version-tags, security, files) or exact file paths to leave alone.
var SkipSteps []string

// OnlySteps is the --only list: when non-empty, only the listed steps/paths
// are applied; everything else is skipped.
var OnlySteps []string

// SkipTokens returns the effective skip set for the step selector: the --skip
// list plus the baseline keep list (KeepFiles), so a kept file is excluded
// from apply exactly as if it had been passed to --skip. The keep list is how
// a hand-tuned workflow survives a re-apply. Whole-step keys in KeepFiles
// are ignored: only an explicit --skip may disable an entire step.
func SkipTokens() []string {
	if len(KeepFiles) == 0 {
		return SkipSteps
	}
	out := make([]string, 0, len(SkipSteps)+len(KeepFiles))
	out = append(out, SkipSteps...)
	for _, path := range KeepFiles {
		if !steps.IsStep(path) {
			out = append(out, path)
		}
	}
	return out
}

// Octopus opts in to the third-party Octopus Review workflow (public repos).
// Off by default: it shares PR diffs with an external service.
var Octopus bool

// CodacyInstructions opts in to shipping .github/instructions/codacy.instructions.md
// (Codacy MCP rules for AI coding agents). Off by default: it assumes the
// repo is on Codacy and an MCP server is available.
var CodacyInstructions bool

// BotManaged opts in to the bot-managed profile: harden a repo whose PRs are
// opened by automation. It tightens templates (no workflow_run /
// pull_request_target, no --octopus, no octocov pull-requests: write),
// settings (squash-merge only), the Actions default token permissions, and the
// branch ruleset (squash-only merges, shipped checks pinned to the GitHub
// Actions app, no bypass actors). Off by default; with it off, every rendered
// file and API request body is byte-identical to the default baseline.
var BotManaged bool

// PolicyChecks are the optional external "policy" status checks the
// bot-managed profile requires on protect-main — external review checks an
// automation pipeline must wait for. Each entry names a context and the
// integration (app) id that must post it; the integration id is REQUIRED so a
// check is never required without pinning which app produces it. Set via the
// baseline (bot_policy_checks); there is no CLI flag.
var PolicyChecks []PolicyCheck

// PolicyCheck is one external policy status check: a context plus the app
// that must post it. It aliases github.PolicyCheck so the baseline JSON and
// the ruleset builder share one shape.
type PolicyCheck = github.PolicyCheck

// KeepFiles is the per-file keep list (baseline `keep`): target paths apply
// must never overwrite, even when content drifts, so hand-tuned workflows
// survive a re-apply. Matched with the same exact-path semantics as --skip.
var KeepFiles []string

// BranchUpToDate is the intended strict_required_status_checks_policy for
// protect-main (branches must be up to date before merging). Default true.
// BranchUpToDateSet records that the operator (or a baseline) chose a value
// explicitly; when unset, a reconcile keeps the ruleset's live policy instead
// of forcing it back.
var BranchUpToDate = true

// BranchUpToDateSet records whether BranchUpToDate was explicitly chosen.
var BranchUpToDateSet bool
