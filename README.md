# fundamentum

Bootstrap and harden GitHub repos for OSS collaboration — branch protection, security, community files, and Codacy in one shot.

## Usage

```bash
# Harden an existing repo
go run . apply OWNER/REPO

# Create a new repo and harden it
go run . init OWNER/REPO

# Preview without applying
go run . --dry-run apply OWNER/REPO
```

## What it does

- **Community files**: CONTRIBUTING.md, CODEOWNERS, CODE_OF_CONDUCT.md, SECURITY.md, PR template, issue templates
- **Branch protection**: modern rulesets on `main`, tag protection on `v*` tags (falls back to classic on free-tier repos)
- **Security**: Dependabot alerts, secret scanning + push protection, CodeQL
- **Settings**: auto-delete merged branches, default branch to `main`
- **Codacy**: `.codacy.yml` config at repo root

## Flags

| Flag | Description |
|---|---|
| `--dry-run` | Print actions without applying them |
| `--token` | GitHub token (default: `GITHUB_TOKEN` env var) |
| `--verbose` | Print API calls |
| `--no-overwrite` | Skip files that already exist |

## Setup

```bash
# Enable pre-commit hooks
git config core.hooksPath .hooks

# Build
go build -o fundamentum.exe .

# Test
go test ./...
```
