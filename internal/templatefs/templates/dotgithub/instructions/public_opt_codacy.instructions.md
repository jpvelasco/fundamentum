---
description: Optional Codacy MCP rules for AI coding agents. Applies ONLY when a Codacy MCP server is available; never block a task on Codacy.
applyTo: '**'
---

# Codacy Rules (optional)

These rules configure AI behavior when a Codacy MCP server is available in the
session. The rules are conditional: if no Codacy MCP tools are present, SKIP
Codacy analysis entirely and continue with the task — do not install anything
and do not stop work.

## Precondition — is Codacy MCP available?

Before any Codacy step, check whether Codacy MCP tools (for example
`codacy_cli_analyze`) are available in this session:

- **If yes**: follow the rules below.
- **If no**: skip Codacy analysis for the whole task. Do not prompt the user,
  do not attempt to install the CLI, and do not retry. Continue the task.

## When using any tool that accepts `provider`, `organization`, or `repository`

- ALWAYS use:
  - provider: gh
  - organization: {{.Owner}}
  - repository: {{.RepoName}}
- Avoid calling `git remote -v` unless really necessary.

## After ANY successful `edit_file` or `reapply` operation

- IF Codacy MCP tools are available, run `codacy_cli_analyze` for each edited file:
  - `rootPath`: set to the workspace path
  - `file`: set to the path of the edited file
  - `tool`: leave empty or unset
- If issues are found in the new edits, propose and apply fixes for them.
- This is a quality aid, not a hard requirement: if the tool call fails, note
  it and continue — do not retry in a loop.

## Dependencies and security checks

- After dependency changes (npm/yarn/pnpm install, package.json,
  requirements.txt, pom.xml, build.gradle, etc.), IF Codacy MCP tools are
  available, run `codacy_cli_analyze` with:
  - `rootPath`: set to the workspace path
  - `tool`: set to "trivy"
  - `file`: leave empty or unset
- If vulnerabilities are found, propose (and when possible apply) fixes
  before continuing with unrelated work.

## General

- Only run the analysis when the precondition holds — repeat it per task, not
  per file edit loop iteration.
- "Propose fixes" means both suggesting and, if possible, applying them.
- Do not run `codacy_cli_analyze` looking for duplicated code, complexity
  metrics, or code coverage.
- Do not manually install Codacy CLI with brew, npm, npx, or any other
  package manager; when tools are missing, the precondition already skips
  Codacy.
- When calling `codacy_cli_analyze`, only send provider, organization and
  repository if the project is a git repository.

## When a Codacy tool call returns a 404 for `repository` or `organization`

- Offer to run `codacy_setup_repository` (only when Codacy MCP is available),
  and only with the user's consent. Do not run it unprompted.
- After setup, retry the failed action once.
