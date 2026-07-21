# CLAUDE.md

Last updated: 2026-07-20

fundamentum is a focused, free, open-source CLI (MIT License) for GitHub repo hardening — community files, branch protection, and security features in one shot with an interactive wizard.

## Build / Lint / Test

```bash
go build -o fundamentum.exe -v .   # Windows
go build -o fundamentum -v .       # Linux/macOS
golangci-lint run ./...
go test ./...
go test -v ./internal/wizard
go test -v -run TestRender ./internal/templates
```

Pre-commit hooks: `git config core.hooksPath .hooks`

## Architecture

Go CLI (Cobra). Two commands: `apply OWNER/REPO` (harden an existing repo), `init OWNER/REPO` (create a new repo then harden).
`OWNER` is the GitHub username or org, `REPO` is the repository name.

- `internal/github/` — GitHub API via net/http (no SDK)
- `internal/wizard/` — summary table + Y/N interactive flow
- `internal/templates/` — `//go:embed` + `text/template` rendering
- `internal/templatefs/` — embedded filesystem (`//go:embed`) for template files

## Conventions

Mirrors ludus: two import groups (stdlib first, then third-party+internal), table-driven tests (stdlib only), `fmt.Errorf("ctx: %w", err)`, no raw `exec.Command`.

## Codacy

- **Cloud CLI (latest):** `npx --yes @codacy/codacy-cloud-cli@latest issues ...` (or with CODACY_API_TOKEN)
- **Local analysis (latest):** `npx --yes @codacy/analysis-cli@latest analyze` (globals optional)
- **CI:** Codacy runs as a required status check via cloud webhook — no local workflow needed
- `.codacy.yml` controls exclude paths and engine configs (see also AGENTS.md for current CLI usage)
