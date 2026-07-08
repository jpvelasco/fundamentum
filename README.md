# fundamentum

Bootstrap and harden GitHub repos for OSS collaboration — in one shot.

**Free, open-source CLI (MIT).** One command applies professional community files, branch protection, security features, and starter workflows.

## Quick Start

```bash
# Harden an existing repo
fundamentum apply OWNER/REPO

# Create a new repo + harden it
fundamentum init OWNER/REPO

# Preview without changes
fundamentum --dry-run apply OWNER/REPO

# Apply via pull request
fundamentum --pr apply OWNER/REPO
```

## Prerequisites

A GitHub personal access token with **`repo` scope** (classic) or **Contents + Metadata + Administration** permissions (fine-grained). Set it via the `GITHUB_TOKEN` environment variable or pass `--token`.

```bash
export GITHUB_TOKEN=ghp_xxxxx        # Linux/macOS
$env:GITHUB_TOKEN = "ghp_xxxxx"      # PowerShell
```

**Note:** Branch protection on free-tier private repos requires GitHub Pro for the API. If rulesets are unavailable, fundamentum falls back to classic branch protection — but if that also fails (403), you'll need to set up branch protection manually via the repo Settings → Branches page.

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
| `--version`       | Print version and exit                           |

`init` also supports `--private` (default: `true`).

## How it works

1. fundamentum detects your repo's current state (existing files, branch protection, visibility).
2. It renders opinionated templates and shows a summary table of what will change.
3. You confirm all defaults or step through items interactively.
4. Files are created or updated directly (or via PR with `--pr`). Settings, security, and branch protection are applied via the GitHub API.

## Install

```bash
# Go install
go install github.com/jpvelasco/fundamentum@latest

# Run from source
go run github.com/jpvelasco/fundamentum apply OWNER/REPO

# npm (shim, after v1.0)
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

