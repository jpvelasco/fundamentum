# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Last updated: 2026-07-23

fundamentum is a focused, free, open-source CLI (MIT License) for GitHub repo hardening — community files, branch protection, and security features in one shot with an interactive wizard. No cloud, no org batching, no audit subcommand.

## Build / Lint / Test

```bash
git config core.hooksPath .hooks   # enable pre-commit hooks (required)

go build -o fundamentum.exe -v .   # Windows
go build -o fundamentum -v .       # Linux/macOS
golangci-lint run ./...
go test ./...

go test ./internal/github/...      # single package
go test -v -run TestRender ./internal/templates
```

Pre-commit order: template drift → build → lint → test.

Run: `GITHUB_TOKEN` must be set (or pass `--token`), then `go run . apply OWNER/REPO` / `go run . init OWNER/REPO`. `OWNER` is the GitHub username or org, `REPO` is the repository name.

## Architecture

Go CLI (Cobra). Entry: `main.go` → `cmd/root/root.go`. Two subcommands: `apply` (harden existing repo), `init` (create repo, then delegate to apply).

- `cmd/root` — root command, persistent flags (`--dry-run`, `--verbose`, `--token`, `--no-overwrite`, `--pr`)
- `cmd/apply` — renders templates, checks existing state, builds item list, runs wizard
- `cmd/repoinit` — creates repo via API (the `init` command), then delegates to apply
- `cmd/globals` — shared mutable flag state (package-level vars; reset with `t.Cleanup` in tests)
- `cmd/util` — shared helpers (`ParseOwnerRepo`)
- `internal/github` — thin HTTP client for the GitHub API (net/http, no SDK)
- `internal/wizard` — summary table + Y/N interactive apply flow
- `internal/templates` — renders embedded templates via **plain string substitution** (not `text/template` — see below)
- `internal/templatefs` — `//go:embed` of template files under `templates/`

### Template rendering

`internal/templates/render.go` uses plain `strings.ReplaceAll` for `{{.Owner}}`, `{{.RepoName}}`, `{{.DefaultBranch}}`, `{{.Visibility}}` — no template engine (refactor #40). This avoids false-positive XSS flags from static analyzers on YAML/Markdown output. All `RepoData` fields are sanitized (regex-validated identifiers, whitelisted visibility) before substitution, and rendered output passes through `sanitizeOutput` (strips dangerous HTML tags) as defense-in-depth.

Path mapping in `resolveTarget`: `dotgithub/` → `.github/`, `dotcodacy.yml` → `.codacy.yml`. Filename prefixes gate by visibility and are stripped from the target: `public_` (public repos only), `private_` (private repos only). Top-level template files map to repo root (e.g. `public_codecov.yml` → `codecov.yml`, `socket.yml` → `socket.yml`).

Shipped CI follows the **fabrica standard**: `public_ci.yml` (a full Go CI workflow — Lint, Vulnerability scan, Build/Test OS matrices, gosec, Trivy — with Codecov coverage + Test Analytics folded into the Test job's Linux leg), `private_ci.yml` (same, minus Codecov — private repos keep `private_octocov.yml` for coverage), root `codecov.yml` (project/patch gates + components), and `public_codeql.yml` (3-language matrix). There is no standalone `codecov.yml` workflow anymore.

### Key behavior

- **Branch protection**: tries modern ruleset first, falls back to classic protection on 403. Classic API requires GitHub Pro — free-tier private repos must configure protection manually via Settings → Branches.
- **File aliasing** (`cmd/apply/apply.go`): checks path variants before deciding create/skip/update — e.g., root `CODEOWNERS` counts as existing even though the target is `.github/CODEOWNERS`.
- **Workflow 404 handling** (`internal/github/files.go`): GitHub locks workflow files; a PUT to update an existing workflow via the Contents API returns 404. Detected as `ErrWorkflowLocked`, returns `action="skipped"` so apply continues.
- **`--no-overwrite`**: skips any file that already exists, even if content differs.
- **`--pr`**: applies file changes through a PR instead of direct commits.
- Auth: `--token` or `GITHUB_TOKEN`, sent as Bearer token.

### Codecov drift gate

`TestCodecovTemplateDrift` (`internal/templatefs/codecov_drift_test.go`) compares the live `.github/workflows/ci.yml` Codecov upload settings against the embed template `public_ci.yml` (Codecov is folded into the CI Test job). Checks OIDC/token XOR auth, `use_pypi`, `fail_ci_if_error`, `-covermode=atomic`, coverage `files`, `override_commit/branch/pr`, `slug`, `report_type: test_results`, and that `codecov/codecov-action` is SHA-pinned. Runs in pre-commit (fail-fast) and the CI `Template drift` job. Action SHAs and branch names may differ intentionally. Branch protection uses `codecov/patch` as the required check (not `codecov/project` — see AGENTS.md).

## Testing conventions

- Create test clients with `github.NewClient(token, verbose).WithBaseURL(srv.URL)` — never construct `Client` directly (its `client *http.Client` field must be initialized).
- Wizard prompt functions accept `io.Reader`/`io.Writer` for testability.
- Table-driven, stdlib only.

## Conventions

Mirrors ludus: two import groups (stdlib first, then third-party + internal), `fmt.Errorf("ctx: %w", err)`, no raw `exec.Command`. golangci-lint v2 with staticcheck `-ST1005` excluded; gosec excludes G104, G204, G304, G704.

## Codacy

- **Cloud CLI:** `npx --yes @codacy/codacy-cloud-cli@latest issues gh jpvelasco fundamentum --overview` (or set `CODACY_API_TOKEN`)
- **Local analysis:** `npx --yes @codacy/analysis-cli@latest analyze`
- CI runs Codacy as a required status check via cloud webhook — no local workflow needed.
- `.codacy.yml` controls exclude paths and engine configs. Tools **cannot** be disabled via `.codacy.yml` — only languages (`languages.<lang>.enabled: false`); disable tools on the Codacy Code patterns page. See AGENTS.md for full CLI/Trivy notes.

## PR workflow

Use the pr-auto skill for the full PR lifecycle. Don't merge with red required checks. Squash-merge, feature branches off main. For admin actions (force-push, `--admin` merge, protection bypass): pause and ask first. After changes to `.github/`, `.codacy.yml`, or workflows, suggest a `go run . apply <owner>/<repo>` dry-run.
