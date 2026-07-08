# fundamentum

Bootstrap and harden GitHub repos for OSS collaboration — in one shot.

**Free, open-source CLI (MIT).** One command applies professional community files, branch protection, security features, and starter workflows.

## Quick Start

```bash
# Harden an existing repo
fundamentum apply OWNER/REPO

# Create a new repo + harden it
fundamentum init OWNER/REPO

# Preview everything first
fundamentum --dry-run apply OWNER/REPO
```

## What it does

- **Community health files**: `CONTRIBUTING.md`, `CODEOWNERS`, `CODE_OF_CONDUCT.md`, `SECURITY.md`, pull request + issue templates, `dependabot.yml`, and more.
- **Branch protection**: Modern ruleset on `main` (PRs, CODEOWNERS review, status checks, no force-push/delete) + optional tag protection. Falls back gracefully on free-tier private repos.
- **Security**: Secret scanning + push protection, Dependabot alerts + updates, CodeQL (public repos).
- **Settings**: Auto-delete merged branches.
- **Opinionated starters**: Basic CI workflow + visibility-aware coverage/CodeQL workflows + `.codacy.yml`.

Everything is **idempotent** — re-running is safe and fast.

## Flags

| Flag              | Description                                      |
|-------------------|--------------------------------------------------|
| `--dry-run`       | Print actions without applying them              |
| `--verbose`       | Print every API call                             |
| `--token`         | GitHub token (defaults to `GITHUB_TOKEN`)        |
| `--no-overwrite`  | Skip any file that already exists                |
| `--pr`            | Apply file changes via PR instead of direct push |

`init` also supports `--private`.

See the interactive wizard: it shows a clear summary table, asks solo/team (when relevant), and lets you confirm or step through changes.

## Install (once released)

```bash
# Go
go install github.com/jpvelasco/fundamentum@latest

# npm (shim)
npm install -g fundamentum
```

Binaries also available on GitHub Releases.

## Development

```bash
git config core.hooksPath .hooks     # enable pre-commit (build + lint + test)

go build -o fundamentum.exe -v .     # Windows
go build -o fundamentum -v .         # Linux/macOS

golangci-lint run ./...
go test ./...
```

Internal planning docs (product spec, roadmap, technical spec, market research) live in the `planning/` directory (currently gitignored).

## Scope & Philosophy

Focused, high-quality, free forever. Good defaults for OSS and small teams. Not a compliance platform or org-scale tool.

## License

MIT

