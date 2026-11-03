package mcp

import (
	"github.com/spf13/cobra"
)

// serverVersion is the MCP serverInfo version; main wires it to the CLI
// version so agents can tell what they're talking to.
var serverVersion = "dev"

// SetVersion records the CLI version for the initialize handshake.
func SetVersion(v string) { serverVersion = v }

// NewCmd returns the mcp subcommand.
func NewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run a stdio MCP server exposing plan, audit, and apply tools",
		Long: `Run a Model Context Protocol server over stdio (newline-delimited
JSON-RPC 2.0). Coding agents launch this as a local child process and call:

  plan  — dry-run a harden and return the plan table (never changes anything)
  audit — compare a repo to the baseline (read-only)
  apply — harden for real, solo defaults, prompts disabled (MUTATING)

Always run plan first and only call apply once the operator has confirmed
the plan. The same GITHUB_TOKEN / --token / gh-auth rule as the CLI applies;
tokens are never written to tool output.

fundamentum is free open-source software, provided as-is with no warranty
of any kind. You are responsible for whatever you or your agent run against
your repositories.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s := &server{tools: &toolSet{}}
			return s.serve(cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
}
