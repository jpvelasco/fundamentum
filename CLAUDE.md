# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Last updated: 2026-08-10

fundamentum is a focused, free, open-source CLI (MIT License) for GitHub repo hardening — community files, branch protection, and security features in one shot with an interactive wizard. No cloud, no org batching, no audit subcommand.

## Build / Lint / Test

```bash
git config core.hooksPath .hooks   # enable repo hooks (required)

go build -o fundamentum.exe -v .   # Windows
go build -o fundamentum -v .       # Linux/macOS
golangci-lint run ./...
go test ./...

go test ./internal/github/...      # single package
go test -v -run TestRender ./internal/templates
go test ./internal/templatefs/ -run TestCodecovTemplateDrift -count=1   # drift gate alone
```

Run: `GITHUB_TOKEN` must be set (or pass `--token`), then `go run . apply OWNER/REPO` / `go run . init OWNER/REPO`. `OWNER` is the GitHub username or org, `REPO` is the repository name.

### Hooks (`.hooks/`, shared code in `.hooks/lib.sh`)

- **pre-commit**: template drift → build → lint (falls back to `go vet` if `golangci-lint` isn't executable, e.g. blocked by AppLocker) → test.
- **commit-msg**: enforces Conventional Commits — `feat|fix|refactor|test|docs|chore|perf|ci|build|deps` + optional `(scope)` + optional `!`; merge/revert/fixup/squash commits are exempt.
- **pre-push**: fails the push if any changed non-test `.go` file has a function at exactly 0.0% coverage (full-suite `-coverpkg=./...` run, not just the changed package — a function's coverage may come from another package's tests). This is an early warning only; the real gate is CI's Codecov patch ≥90%. Bypass with `git push --no-verify` if truly needed. Build-constrained files for a different GOOS (can't be compiled/tested on this machine) are flagged as unverifiable rather than silently skipped.
- `.gitattributes` forces LF for Go/mod/sum/`.sh`/hooks/YAML/`.js` so Windows checkouts stay diff-clean against CI and the templatefs drift comparisons.

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

Shipped CI follows the **fabrica standard**: `public_ci.yml` (a full Go CI workflow — Lint, Vulnerability scan, Build/Test OS matrices, gosec, Trivy — with Codecov coverage + Test Analytics folded into the Test job's Linux leg), `private_ci.yml` (same, minus Codecov — private repos keep `private_octocov.yml` for coverage), root `public_codecov.yml` → `codecov.yml` (project/patch gates + components), and `public_codeql.yml` (2-language matrix: `actions` + `go` with autobuild; add a commented-out `javascript-typescript` leg if the target repo ships JS/TS). There is no standalone `codecov.yml` workflow in `ci.yml` anymore — Codecov is a step inside the Test job.

fundamentum also ships root `socket.yml` (Socket Security supply-chain scanning via GitHub App; no visibility prefix, so it's included for every repo — requires the app installed on the target repo/org) and `public_octopus.yml` (Octopus Review, a PR-triaging bot via `pull_request_target`; public repos only, like any `public_`-prefixed template). `dependabot.yml` only watches the `github-actions` ecosystem (no `gomod` entry).

### Key behavior

- **Branch protection**: tries modern ruleset first, falls back to classic protection on 403. Classic API requires GitHub Pro — free-tier private repos must configure protection manually via Settings → Branches.
- **File aliasing** (`cmd/apply/apply.go`): checks path variants before deciding create/skip/update — e.g., root `CODEOWNERS` counts as existing even though the target is `.github/CODEOWNERS`.
- **Workflow 404 handling** (`internal/github/files.go`): GitHub locks workflow files; a PUT to update an existing workflow via the Contents API returns 404. Detected as `ErrWorkflowLocked`, returns `action="skipped"` so apply continues.
- **409 auto-fallback to PR** (`cmd/apply/apply.go` `applyItems`): in direct-commit mode, a 409 (`github.IsConflict409`, branch protection blocking direct pushes) switches the remaining file changes to a single batch PR mid-run — no re-invocation needed.
- **Retry/backoff** (`internal/github/client.go`): transient failures (429, 5xx, 403 rate-limit exhaustion) retry up to `maxAttempts` (3) with exponential backoff + full jitter (capped 30s), honoring `Retry-After`. Jitter uses `crypto/rand`, not `math/rand`, to satisfy gosec even though this is timing jitter, not a security boundary.
- **`--no-overwrite`**: skips any file that already exists, even if content differs.
- **`--pr`**: applies file changes through a PR instead of direct commits.
- **Dry-run labels**: `General settings (auto-delete branches)` and `Security (…)` are hardcoded `ActionCreate` in `cmd/apply/apply.go` — always show "would create" even when already enabled; applies are idempotent.
- **Windows CRLF embed trap**: template files under `internal/templatefs/templates/` rewritten externally with CRLF stay invisible to git (clean filter normalizes to LF), so `//go:embed` embeds CRLF and dry-run falsely reports "would update" against LF live blobs. Fix: delete the file, `git checkout HEAD -- <file>`; verify with `git hash-object --no-filters <file>`. See AGENTS.md.
- **Solo/team prompt**: `apply` asks "solo/team" interactively (default solo) only when branch protection will actually be created — skipped if the `protect-main` ruleset already exists. Solo disables the CODEOWNERS-review and stale-review-dismissal requirements (`github.BranchProtectionOptions.Solo`) so a single maintainer isn't deadlocked approving their own PRs.
- **Default required status check**: `github.DefaultStatusChecks` always includes `"Codacy Static Code Analysis"` (fundamentum always writes `.codacy.yml`, so that check is safe to require).
- Auth: `--token` or `GITHUB_TOKEN`, sent as Bearer token.
- `init` also takes `--private` (default `true`); pass `--private=false` for a public repo.

### Codecov drift gate

`TestCodecovTemplateDrift` (`internal/templatefs/codecov_drift_test.go`) compares the live `.github/workflows/ci.yml` Codecov upload settings against the embed template `public_ci.yml` (Codecov is folded into the CI Test job). Checks OIDC/token XOR auth, `use_pypi`, `fail_ci_if_error`, `-covermode=atomic`, coverage `files`, `override_commit/branch/pr`, `slug`, `report_type: test_results`, and that `codecov/codecov-action` is SHA-pinned. Runs in pre-commit (fail-fast) and inside CI's `Test` job (`go test ./...`) — there's no standalone `Template drift` job. Action SHAs and branch names may differ intentionally. Branch protection uses `codecov/patch` as the required check (not `codecov/project` — Codecov only posts the former on PRs).

## Testing conventions

- Create test clients with `github.NewClient(token, verbose).WithBaseURL(srv.URL)` — never construct `Client` directly (its `client *http.Client` field must be initialized).
- Wizard prompt functions accept `io.Reader`/`io.Writer` for testability.
- Table-driven, stdlib only.

## Conventions

Mirrors ludus: two import groups (stdlib first, then third-party + internal), `fmt.Errorf("ctx: %w", err)`, no raw `exec.Command`. golangci-lint v2 with staticcheck `-ST1005` excluded; gosec excludes G104, G204, G304, G704.

## Codacy

- **Cloud CLI:** `npx --yes @codacy/codacy-cloud-cli@latest issues gh jpvelasco fundamentum --overview` (or set `CODACY_API_TOKEN`)
- **Local analysis:** `npx --yes @codacy/analysis-cli@latest analyze`
- PR-level Codacy checks come from the GitHub integration webhook — no workflow involved. The main dashboard also updates on push to `main` via that same webhook/cloud analysis (verified end-to-end: analysis finished on the merge commit with no CLI job in the workflow). A `Codacy Analysis` CLI-upload job was briefly tried and **removed** — the CLI's final notification is always rejected for cloud-analyzed repos with `Feature "Repository Analysis" is disabled`, so it could never succeed and added 2+ minutes of dead runner time per push. If "Run analysis on your build server" is ever enabled in Codacy settings, re-adding a push-only CLI job would make sense — otherwise keep Codacy fully webhook-driven.
- `.github/workflows/codacy-coverage.yml` is an **embedded shipped template** (`internal/templatefs/templates/dotgithub/workflows/codacy-coverage.yml`, shared workflow) — a separate `workflow_run`-triggered job: it downloads the `codacy-coverage-*` artifact the CI Test job uploads and forwards it to Codacy via `codacy-coverage-reporter-action`. Split out because PR-triggered jobs must not receive the `CODACY_REPOSITORY_API_TOKEN` secret directly. Re-apply it to other repos via `go run . apply`, not manual copy.
- `.codacy.yml` controls exclude paths and engine configs. Tools **cannot** be disabled via `.codacy.yml` — only languages (`languages.<lang>.enabled: false`); disable tools on the Codacy Code patterns page. See AGENTS.md for full CLI/Trivy notes.

## PR workflow

Use the pr-auto skill for the full PR lifecycle. Don't merge with red required checks. Squash-merge, feature branches off main. For admin actions (force-push, `--admin` merge, protection bypass): pause and ask first. After changes to `.github/`, `.codacy.yml`, or workflows, suggest a `go run . apply <owner>/<repo>` dry-run.
