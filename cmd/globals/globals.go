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
