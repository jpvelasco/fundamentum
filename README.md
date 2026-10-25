# fundamentum

<p align="center">
  <a href="https://github.com/jpvelasco/fundamentum/actions/workflows/ci.yml"><img src="https://github.com/jpvelasco/fundamentum/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/jpvelasco/fundamentum/releases/latest"><img src="https://img.shields.io/github/v/release/jpvelasco/fundamentum" alt="Release"></a>
  <a href="https://github.com/jpvelasco/fundamentum/blob/main/go.mod"><img src="https://img.shields.io/github/go-mod/go-version/jpvelasco/fundamentum" alt="Go"></a>
  <a href="https://www.npmjs.com/package/fundamentum-cli"><img src="https://img.shields.io/npm/v/fundamentum-cli" alt="npm"></a>
  <a href="https://www.npmjs.com/package/fundamentum-cli"><img src="https://img.shields.io/npm/dm/fundamentum-cli" alt="npm downloads"></a>
  <a href="https://app.codecov.io/gh/jpvelasco/fundamentum"><img src="https://img.shields.io/codecov/c/github/jpvelasco/fundamentum" alt="Codecov"></a>
  <a href="https://github.com/jpvelasco/fundamentum/blob/main/LICENSE"><img src="https://img.shields.io/github/license/jpvelasco/fundamentum" alt="License"></a>
  <a href="https://app.codacy.com/gh/jpvelasco/fundamentum/dashboard?utm_source=gh&utm_medium=referral&utm_content=&utm_campaign=Badge_grade"><img src="https://app.codacy.com/project/badge/Grade/8e6e46246bfb42ac9257f1ca809bcc5f" alt="Codacy Grade"></a>
</p>

Bootstrap and harden GitHub repos for OSS collaboration — in one shot.

**Free, open-source CLI (MIT).** One command applies professional community files, branch protection, security features, and starter workflows.

## Quick Start

```bash
# Harden an existing repo
fundamentum apply OWNER/REPO

# Create a new repo + harden it
fundamentum init OWNER/REPO

# Verify the harden baseline
fundamentum audit OWNER/REPO

# Export / reapply a portable baseline
fundamentum --preset oss export -o baseline.json
fundamentum --from baseline.json apply OWNER/REPO

# Preview without changes
fundamentum --dry-run apply OWNER/REPO

# Apply via pull request
fundamentum --pr apply OWNER/REPO
```

## Prerequisites

A GitHub personal access token with **`repo` scope** (classic) or **Contents + Metadata + Administration** permissions (fine-grained). Set it via the `GITHUB_TOKEN` environment variable, pass `--token`, or be signed in to the GitHub CLI — fundamentum falls back to `gh auth token` when the first two are absent.

```bash
export GITHUB_TOKEN=ghp_xxxxx        # Linux/macOS
$env:GITHUB_TOKEN = "ghp_xxxxx"      # PowerShell
```

**Note:** Branch protection on free-tier private repos requires GitHub Pro for the API. If rulesets are unavailable, fundamentum falls back to classic branch protection — but if that also fails (403), you'll need to set up branch protection manually via the repo Settings → Branches page.

## What it does

- **Community health files**: `CONTRIBUTING.md`, `CODEOWNERS`, `CODE_OF_CONDUCT.md`, `SECURITY.md`, pull request + issue templates, `dependabot.yml` (per-language ecosystems detected from your manifests), and more.
- **Branch protection**: Modern `protect-main` ruleset (PRs, optional CODEOWNERS review, shipped CI status checks, no force-push/delete) + optional tag protection. Reconcile **merges with your live ruleset** instead of replacing it: required checks keep their app identity (`integration_id`), extra checks and rules survive, and stricter settings are never weakened. Classic fallback only when GitHub says rulesets are unavailable on the plan.
- **Security**: Dependabot alerts + updates everywhere; secret scanning + push protection on public repos, opt-in on private/internal repos via `--advanced-security` (paid GHAS); CodeQL default setup on public repos unless a `codeql*.yml` workflow (or the shipped advanced one) already provides CodeQL.
- **Settings**: Auto-delete merged branches.
- **Opinionated starters**: CI pack is `auto` — Go / Node / Python / Rust from `go.mod`, `package.json`, `pyproject.toml`/`requirements.txt`, or `Cargo.toml`; otherwise a generic `CI` + Trivy workflow whose `CI` job fails with a "configure me" annotation until you give it real commands — a required check that always passes would be false confidence. The node pack runs the `lint`/`test` scripts it finds in `package.json` and fails with the same annotation when they are missing. Override with `--ci go|node|python|rust|generic|none`. `.codacy.yml` is generated from the detected pack (Go analyzers stay off non-Go repos).
- **Audit**: `fundamentum audit OWNER/REPO` prints a PASS/FAIL/WARN/SKIP report against the baseline: FAIL fails the run, WARN (content drift, optional checks) fails only with `--strict`, SKIP covers opted-out steps and plan-gated protection. A repo whose settings are stricter than the baseline passes.

`--preset oss|private|strict` skips the wizard so a scripted apply reproduces the same harden. `oss` and `private` take the solo default and do not enable paid GHAS; `strict` turns on `--strict` and `--advanced-security`. Visibility still picks the public vs private file set. The Octopus Review workflow is **never** part of a preset — it is a third-party data-sharing service and requires an explicit `--octopus`.

Files land as **one commit** (the default) — no more one-commit-per-file history noise. `--pr` batches **file** changes into a pull request instead. Settings, security, and branch protection still apply immediately via the API — they are not deferred until the PR merges. Required status checks are deferred so the open harden PR cannot deadlock on jobs that only exist after merge; re-apply after it lands to require them. Tag ruleset and security steps are optional unless you pass `--strict`. Required checks default to the jobs for the resolved `--ci` pack, not Codacy (`--require-checks` overrides; existing required checks are merged, not replaced).

Everything is **idempotent** — re-running is safe and fast.

## Flags

| Flag                | Description                                      |
|---------------------|--------------------------------------------------|
| `--dry-run`         | Print actions without applying them (update rows say exactly what drifted) |
| `--verbose`         | Print every API call                             |
| `--token`           | GitHub token (defaults to `GITHUB_TOKEN`, then `gh auth token`) |
| `--no-overwrite`    | Skip any file that already exists                |
| `--pr`              | Batch file changes into a PR (default is one direct commit); settings/security/protection still apply live |
| `--skip`            | Leave steps alone: `settings`, `protect-main`, `protect-version-tags`, `security`, `files`, or an exact file path |
| `--only`            | When set, apply just the listed steps/paths (same keys as `--skip`) |
| `--advanced-security` | Enable GitHub Advanced Security (secret scanning, push protection) on private/internal repos (paid) |
| `--strict`          | Fail the run when any core harden step fails, including optional tag/security items |
| `--require-checks`  | Required status-check contexts for `protect-main` (merged with existing checks; default: jobs for the resolved `--ci` pack) |
| `--ci`              | CI pack: `auto` (detect go/node/python/rust, else generic), `go`, `node`, `python`, `rust`, `generic`, or `none` |
| `--preset`          | Named baseline: `oss` (no prompts), `private` (solo, no GHAS prompt), `strict` (`--strict` + GHAS) |
| `--from`            | Load a portable JSON baseline from `export` (explicit flags win, including `--flag=false`) |
| `--octopus`         | Opt in to the third-party Octopus Review workflow (public repos; shares PR diffs with octopus-review.ai) |
| `--codacy-instructions` | Opt in to `.github/instructions/codacy.instructions.md` (Codacy MCP rules for AI agents) |
| `--branch-up-to-date` | `protect-main`: require branches up to date before merging (default true; unset keeps the live policy on reconcile) |
| `--version`         | Print version and exit                           |

`init` also supports `--private` (default: `true`).

## How it works

1. fundamentum detects your repo's current state (existing files, branch protection, visibility, README/AGENTS.md filenames, package.json scripts).
2. It renders opinionated templates and shows a summary table of what will change — update rows carry the exact drift (ruleset fields, security features, content diffstats).
3. You confirm all defaults or step through items interactively.
4. Files land as one commit (or one PR with `--pr`); a 409 from branch protection switches to PR mode automatically. Settings, security, and branch protection are applied via the GitHub API even in `--pr` mode — `--pr` is not a dry-run for those steps. A `--from` baseline fills flags the operator did not pass; an explicit flag wins, including `--strict=false`.

## Install

```bash
# npm (recommended)
npm install -g fundamentum-cli

# Go install
go install github.com/jpvelasco/fundamentum@latest

# Run from source
go run github.com/jpvelasco/fundamentum apply OWNER/REPO
```

Binaries are published on GitHub Releases.

## Development

```bash
git config core.hooksPath .hooks     # enable pre-commit (build + lint + test)

go build -o fundamentum.exe -v .     # Windows
go build -o fundamentum -v .         # Linux/macOS

golangci-lint run ./...
go test ./...
```

## Scope & Philosophy

Focused, high-quality, free forever. Good defaults for OSS and small teams. Not a compliance platform or org-scale tool.

## License

MIT

