# AGENTS.md

## Commands

```bash
# Enable pre-commit hooks (required — hooks live in .hooks/)
git config core.hooksPath .hooks

# Build
go build -o fundamentum.exe -v .

# Lint
golangci-lint run ./...

# Test
go test ./...

# Test single package
go test ./internal/github/...

# Run
echo $env:GITHUB_TOKEN   # must be set, or pass --token
go run . apply OWNER/REPO
go run . init OWNER/REPO
```

Pre-commit order: build → lint → test.

## Architecture

Go CLI (Cobra). Entry point: `main.go` → `cmd/root/root.go`.

Two subcommands:
- **apply OWNER/REPO** — harden an existing repo: upsert community health files, set branch protection, enable security features
- **init OWNER/REPO** — create a new repo then apply hardening

Shared flags on root: `--dry-run`, `--verbose`, `--token`, `--no-overwrite`.

### Packages

- `cmd/root` — root Cobra command, flags
- `cmd/apply` — apply logic: renders templates, checks existing state, builds item list, runs wizard
- `cmd/repoinit` — creates repo via API, then delegates to apply
- `cmd/globals` — shared mutable flag state (DryRun, Token, Verbose, NoOverwrite)
- `cmd/util` — shared utilities (ParseOwnerRepo)
- `internal/github` — thin HTTP client for GitHub API (net/http, no SDK)
- `internal/wizard` — interactive summary table + Y/N apply flow
- `internal/templates` — renders embedded templates via text/template
- `internal/templatefs` — `//go:embed` of template files; `dotgithub/` maps to `.github/`, `dotcodacy.yml` to `.codacy.yml`

### Key behavior

- **Branch protection**: tries modern ruleset first, falls back to classic protection on 403 (free-tier private repos)
- **File aliasing** (`cmd/apply/apply.go:89`): checks path variants before deciding create/skip/update — e.g., `CODEOWNERS` at root counts as existing even though target is `.github/CODEOWNERS`
- **`--no-overwrite`**: skips any file that already exists, even if content differs
- Auth: `--token` flag or `GITHUB_TOKEN` env var, used as Bearer token

### Testing

- Always use `github.NewClient(token, verbose).WithBaseURL(srv.URL)` to create test clients — never construct `Client` directly (the `client *http.Client` field must be initialized)
- All wizard prompt functions accept `io.Reader`/`io.Writer` for testability
- `cmd/globals` is mutable package-level state — use `t.Cleanup` to reset after tests

### Conventions

- Two import groups: stdlib first, then third-party + internal (mirrors ludus)
- Error wrapping: `fmt.Errorf("ctx: %w", err)`
- No `exec.Command` anywhere
- Table-driven tests, stdlib only
- golangci-lint v2 with staticcheck `-ST1005` excluded; gosec excludes G104, G204, G304, G704

## Codacy

- **Cloud CLI:** `npx @codacy/codacy-cloud-cli issues gh jpvelasco fundamentum --overview` (requires CODACY_API_TOKEN)
- **Local analysis:** `codacy-analysis analyze` (requires `@codacy/analysis-cli` installed globally)
- **CI:** Codacy runs as a required status check via cloud webhook — no local workflow needed
- `.codacy.yml` controls exclude paths and engine configs
