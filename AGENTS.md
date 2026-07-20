# AGENTS.md

fundamentum is a **free, open-source (MIT) CLI** for one-shot GitHub repo hardening. Focused feature set — no cloud, no org batching, no audit subcommand.

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

Pre-commit order: template drift → build → lint → test.

**Codecov template drift gate:** `TestCodecovTemplateDrift` compares live `.github/workflows/codecov.yml` upload settings against the embed template `public_codecov.yml` (auth, Python uploader, coverage flags, pinned action versions). Runs in pre-commit (fail-fast) and CI job `Template drift`. Action pins may differ intentionally.

**Codecov required check (current):** branch protection uses `codecov/patch`.

- Probe PR #25: uploads succeeded and Codecov had project totals.
- GitHub only received `codecov/patch`, not `codecov/project`.
- Prefer not re-adding `codecov/project` as required until a PR shows that check posting.
- Exception: re-add if Codecov starts emitting `codecov/project` and you want a project gate.

## PR Workflow (use with pr-auto / pr-doctor skills)

For PRs: use pr-auto for full lifecycle (create, fix CI/reviews, land safely).
- Never merge if CI red on required checks.
- Always address/resolve review threads with substantive replies.
- Admin actions (force, --admin merge, protection bypass) → STOP and ask human first.
- After changes to .github/, .codacy.yml, workflows, etc.: suggest `go run . apply <owner>/<repo>` (dry-run first).
- Load AGENTS.md + CLAUDE.md at start. Follow squash default, feature branches, CI wait.
- Use supporting skills: check-work (verify fixes), review (findings), monitor (long CI).

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

- **Branch protection**: tries modern ruleset first, falls back to classic protection on 403. **Limitation:** the classic protection API requires GitHub Pro — free-tier private repos must configure branch protection manually via Settings → Branches.
- **File aliasing** (`cmd/apply/apply.go:89`): checks path variants before deciding create/skip/update — e.g., `CODEOWNERS` at root counts as existing even though target is `.github/CODEOWNERS`
- **Workflow 404 handling** (`internal/github/files.go`): GitHub Actions locks workflow files — PUT returns 404 when updating an existing workflow via Contents API. Detected as `ErrWorkflowLocked`, returns `action="skipped"` so apply continues.
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

- **Cloud CLI (latest, no install confusion):** `npx --yes @codacy/codacy-cloud-cli@latest issues gh jpvelasco fundamentum --overview` (or set CODACY_API_TOKEN)
- **Local analysis (latest):** `npx --yes @codacy/analysis-cli@latest analyze ...` (or `codacy-analysis` if globally installed)
- `.codacy.yml` controls exclude paths and engine configs (`engines:` section)
- **Cannot disable tools via `.codacy.yml`.** The `enabled: false` option only works for languages (`languages.<lang>.enabled: false`). Disable tools on the [Code patterns page](https://docs.codacy.com/repositories-configure/configuring-code-patterns/) instead.
- **Legacy `tools:` key ignored.** The `.codacy/codacy.yaml` format is from Codacy CLI v2 and not recognized by the current cloud config.
- **Use npm CLIs via npx.** For local and cloud interaction, use `@codacy/codacy-cloud-cli` and `@codacy/analysis-cli`.
- **Trivy noise:** Trivy errors with "no patterns configured" on repos without Dockerfiles/K8s manifests. Must be disabled in the Codacy UI per-repo.
