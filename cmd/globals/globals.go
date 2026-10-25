// Package globals holds shared mutable state for all commands.
package globals

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

// Octopus opts in to the third-party Octopus Review workflow (public repos).
// Off by default: it shares PR diffs with an external service.
var Octopus bool

// CodacyInstructions opts in to shipping .github/instructions/codacy.instructions.md
// (Codacy MCP rules for AI coding agents). Off by default: it assumes the
// repo is on Codacy and an MCP server is available.
var CodacyInstructions bool

// BranchUpToDate is the intended strict_required_status_checks_policy for
// protect-main (branches must be up to date before merging). Default true.
// BranchUpToDateSet records that the operator (or a baseline) chose a value
// explicitly; when unset, a reconcile keeps the ruleset's live policy instead
// of forcing it back.
var BranchUpToDate = true

// BranchUpToDateSet records whether BranchUpToDate was explicitly chosen.
var BranchUpToDateSet bool
