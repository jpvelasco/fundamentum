# AGENTS.md

fundamentum is a **free, open-source CLI** (MIT License) for one-shot GitHub repo hardening. Focused feature set — no cloud, no org batching, no audit subcommand.

## Commands

```bash
# Enable repo hooks (required — hooks live in .hooks/)
git config core.hooksPath .hooks

`.hooks/pre-commit` runs template drift → build → lint → test (fail-fast).
`.hooks/commit-msg` enforces Conventional Commits (`feat:|fix:|refactor:|test:|docs:|chore:|perf:|ci:|build:|deps:` + optional scope; merge/revert/fixup/squash allowed).
`.hooks/pre-push` fails any changed Go file with a 0.0%-coverage function (early warning — the real gate is CI's Codecov patch >= 90%). Emergency bypass: `git push --no-verify`.

`.gitattributes` forces LF for Go sources, mod/sum, `.sh`, `.hooks/*`, all YAML (`*.yml`/`*.yaml`, including `.github/workflows/`), `.js`, and `.gitattributes` itself so Windows checkouts stay diff-clean against CI and the templatefs parity checks.

# Build
go build -o fundamentum.exe -v .

# Lint
golangci-lint run ./...

# Test
go test ./...

# Test single package
go test ./internal/github/...

# Template drift gate (also runs first in pre-commit)
go test ./internal/templatefs/ -run TestCodecovTemplateDrift -count=1

# Run
echo $env:GITHUB_TOKEN   # must be set, or pass --token
# OWNER is the GitHub username or organization, REPO is the repository name.
go run . apply OWNER/REPO
go run . init OWNER/REPO
```

Pre-commit order: template drift → build → lint → test.

**Codecov template drift gate:** `TestCodecovTemplateDrift` (`internal/templatefs/codecov_drift_test.go`) compares live `.github/workflows/ci.yml` Codecov upload settings against the embed template `public_ci.yml` (Codecov is folded into the CI Test job, fabrica standard). Checks: `id-token: write`, `use_oidc` (literal `true` or the XOR `${{ secrets.CODECOV_TOKEN == '' }}` expression), `use_pypi`, `fail_ci_if_error`, `-covermode=atomic`, coverage `files`/`-coverprofile`, `override_commit`/`override_branch`/`override_pr`, `slug`, `report_type: test_results`, and SHA-pinned `codecov/codecov-action`. Runs in pre-commit (fail-fast) and in CI inside the `Test` job's `go test ./...` — there is no standalone `Template drift` job anymore. Action SHAs and branch names may differ intentionally.

**Codecov required check (current):** branch protection requires `codecov/patch`, not `codecov/project` — Codecov only posts the former on PRs, so requiring the latter would deadlock merges. Re-add `codecov/project` only if Codecov starts posting that check.

**CI job names (fabrica standard):** `.github/workflows/ci.yml` jobs are `Lint`,
`Lint (Windows)`, `Vulnerability scan`, `Build (ubuntu-latest|windows-latest|macos-latest)`,
`Test (ubuntu-latest|…)`, `gosec`, `Trivy`, `Release build (snapshot)` (GoReleaser build-only —
never publishes); PR-level Codacy checks come from the GitHub integration webhook — no `Codacy
Analysis` CI job needed (verified: cloud analysis updates the dashboard on push to main, and the
CLI's upload completion is rejected for cloud-analyzed repos with `Feature "Repository Analysis"
is disabled`). CodeQL runs as `Analyze (actions|go)` in `codeql.yml`. macOS legs run only on
push to `main` (PRs skip them to save minutes), so **do not** require macOS contexts in branch
protection. Required checks in the `protect-main` ruleset must match the reportable jobs on PRs —
currently `Lint`, `security`, `review`, `Vulnerability scan`,
`Build (ubuntu-latest|windows-latest)`, `Test (ubuntu-latest|windows-latest)`, `gosec`, `Trivy`,
`Analyze (actions)`, `Analyze (go)`, and `Lint (Windows)`. `Release build (snapshot)` is NOT a
required check. When renaming/adding jobs, update the ruleset required-status-checks list to
match, or PRs deadlock on contexts that never report.

## Release (tag `v*`)

`.github/workflows/release.yml` runs GoReleaser on version tags, then publishes the npm shim
(`fundamentum-cli`): `scripts/embed-checksums.js` embeds GoReleaser's `dist/checksums.txt` into
`npm/package.json` (`binaryChecksums`), then `npm publish` runs from `npm/` via OIDC trusted
publishing (`id-token: write`). `.goreleaser.yml` pins version ldflags to
`cmd/root.Version` (`{{.Version}}`), archives tar.gz (zip on Windows) as
`fundamentum_<version>_<os>_<arch>.{tar.gz,zip}`, ignores Windows/arm64 — the npm shim
(`npm/install.js` + `npm/run.js`) must match that naming and the release-asset layout
(nyx/ludus/juggernaut pattern). `npm/package.json` version stays `0.0.0` in the repo; the
workflow sets it to the tag version before publishing. The CI snapshot job
(`Release build (snapshot)`) verifies the config compiles all platforms and
smoke-tests the linux/amd64 binary's `--version` output (must not be ` dev`). Local check:
`goreleaser build --snapshot --clean`, then run `dist/.../fundamentum.exe --version`. Never
publish a release from CI without an explicit tag.

## PR Workflow (use with pr-auto / pr-doctor skills)

For PRs: use pr-auto for full lifecycle (create, fix CI/reviews, land safely).
- Do not merge if CI is red on required checks. Exception: if the check is a known flaky failure unrelated to the PR changes, document the issue and proceed.
- Address and resolve review threads with substantive replies that include code changes or clear justifications.
- For admin actions (force-push, `--admin` merge, branch protection bypass): pause and ask the human before proceeding.
- After changes to `.github/`, `.codacy.yml`, or workflows: suggest `go run . apply <owner>/<repo>` with a dry-run first.
- Load AGENTS.md and CLAUDE.md at the start of a session. Follow squash-merge default, feature branches, and CI-wait rules.
- Use supporting skills as needed:
  - check-work — verify fixes before committing
  - review — surface code quality findings
  - monitor — track long-running CI jobs

## Architecture

Go CLI (Cobra). Entry point: `main.go` → `cmd/root/root.go`.

Two subcommands:
- **apply OWNER/REPO** — harden an existing repo: upsert community health files, set branch protection, enable security features
- **init OWNER/REPO** — create a new repo then apply hardening

Shared flags on root: `--dry-run`, `--verbose`, `--token`, `--no-overwrite`, `--pr`.
`init` also takes `--private` (default `true`; pass `--private=false` for public).

### Packages

- `cmd/root` — root Cobra command, flags
- `cmd/apply` — apply logic: renders templates, checks existing state, builds item list, runs wizard
- `cmd/repoinit` — creates repo via API, then delegates to apply
- `cmd/globals` — shared mutable flag state (DryRun, Token, Verbose, NoOverwrite, ViaPR)
- `cmd/util` — shared utilities (ParseOwnerRepo)
- `internal/github` — thin HTTP client for GitHub API (net/http, no SDK)
- `internal/wizard` — interactive summary table + Y/N apply flow
- `internal/templates` — renders embedded templates via plain string substitution (not `text/template`; see `render.go`)
- `internal/templatefs` — `//go:embed` of template files; `dotgithub/` maps to `.github/`, `dotcodacy.yml` to `.codacy.yml`; `public_`/`private_` filename prefixes gate by visibility and are stripped from the target

### Key behavior

- **Branch protection**: tries modern ruleset first (names `protect-main`, `protect-version-tags`), falls back to classic protection on 403. **Limitation:** the classic protection API requires GitHub Pro — free-tier private repos must configure branch protection manually via Settings → Branches. Use the `--no-overwrite` flag if you only want to add missing files.
- **File aliasing** (`cmd/apply/apply.go` `aliases` map in `buildItems`): checks path variants before deciding create/skip/update — e.g., `CODEOWNERS` at root counts as existing even though target is `.github/CODEOWNERS`
- **Workflow 404 handling** (`internal/github/files.go` `putFileContents`, sentinel `ErrWorkflowLocked` in `pr.go`): GitHub Actions locks workflow files — HTTP PUT returns 404 when updating an existing workflow via the Contents API. Returns `action="skipped"` so apply continues.
- **409 auto-fallback to PR mode** (`cmd/apply/apply.go` `applyItems`): a direct file commit rejected with 409 (branch protection requires PR) switches the remaining file changes into a single batch PR — no re-run needed.
- **Solo/team prompt**: `apply` asks solo/team (default solo) only when the `protect-main` ruleset doesn't already exist. Solo disables the CODEOWNERS-review and stale-review-dismissal requirements so a single maintainer isn't deadlocked.
- **`--no-overwrite`**: skips any file that already exists, even if content differs
- **`--pr`**: batches file changes into a single PR; non-file items (settings, security, protection) still apply directly
- **Dry-run "would create" labels**: `General settings (auto-delete branches)` and `Security (…)` are hardcoded `ActionCreate` in `cmd/apply/apply.go` `buildItems` — they always show "would create" even when already enabled. The applies are idempotent PUTs; verify live state via `gh api repos/O/R --jq '{delete_branch_on_merge, security_and_analysis}'` instead of trusting the label.
- **Windows CRLF embed trap**: template files under `internal/templatefs/templates/` that get rewritten externally with CRLF (editor, scripting) become invisible to git — the clean filter normalizes CRLF→LF, matching the LF blob, so `git status` stays clean and checkout/restore never rewrite the bytes. `//go:embed` then embeds the CRLF bytes and dry-run falsely reports `would update` for content-identical files (live blobs are LF). Fix: delete the file and `git checkout HEAD -- <file>` (or clone fresh). Verify with `git hash-object --no-filters <file>` — it must equal `git rev-parse HEAD:<file>`.
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

- **Cloud CLI (latest, no install confusion):** `npx --yes @codacy/codacy-cloud-cli@latest issues gh jpvelasco fundamentum --overview` (or set CODACY_API_TOKEN)
- **Local analysis (latest):** `npx --yes @codacy/analysis-cli@latest analyze ...` (or `codacy-analysis` if globally installed)
- `.codacy.yml` controls exclude paths and engine configs (`engines:` section)
- **Cannot disable tools via `.codacy.yml`.** The `enabled: false` option only works for languages (`languages.<lang>.enabled: false`). Disable tools on the [Code patterns page](https://docs.codacy.com/repositories-configure/configuring-code-patterns/) instead.
- **Legacy `tools:` key ignored.** The `.codacy/codacy.yaml` format is from Codacy CLI v2 and not recognized by the current cloud config.
- **Use npm CLIs via npx.** For local and cloud interaction, use `@codacy/codacy-cloud-cli` and `@codacy/analysis-cli`.
- **Trivy noise:** Trivy reports "no patterns configured" on repos without Dockerfiles or Kubernetes manifests. Disable Trivy in the Codacy UI per-repo to eliminate this noise. If the repo adds container files later, re-enable Trivy.
- **This-repo workflows (now shipped templates):** `.github/workflows/codacy-coverage.yml` (`workflow_run`) re-sends the CI test artifact's coverage to Codacy — part of the embedded templates (`codacy-coverage.yml` is a shared workflow). Codacy analysis itself is entirely webhook/cloud-driven: PR checks come from the GitHub integration, and the main dashboard updates on push without any CI job (the former `Codacy Analysis` CLI-upload job was removed after proving it can never succeed — the CLI's final notification is rejected for cloud-analyzed repos with `Feature "Repository Analysis" is disabled`). `github.DefaultStatusChecks` always includes `Codacy Static Code Analysis`.
