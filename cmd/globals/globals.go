// Package globals holds shared mutable state for all commands.
package globals

// DryRun prints actions without applying them when true.
var DryRun bool

// Verbose prints API calls when true.
var Verbose bool

// Token is the GitHub personal access token; falls back to GITHUB_TOKEN env var.
var Token string
