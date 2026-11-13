# fundamentum-cli

**Bootstrap and harden a GitHub repository in one command.**

[GitHub](https://github.com/jpvelasco/fundamentum)

Fundamentum is a free, open-source CLI (MIT) for community files, branch protection, security settings, priority labels, and starter workflows. This package installs the Go CLI; it requires no Go toolchain for normal use.

<p align="center">
  <a href="https://github.com/jpvelasco/fundamentum/actions/workflows/ci.yml"><img src="https://github.com/jpvelasco/fundamentum/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/jpvelasco/fundamentum/releases/latest"><img src="https://img.shields.io/github/v/release/jpvelasco/fundamentum" alt="Release"></a>
  <a href="https://opensource.org/licenses/MIT"><img src="https://img.shields.io/badge/License-MIT-blue.svg" alt="MIT License"></a>
</p>

## Install

```bash
npm install -g fundamentum-cli
```

Try without a global install:

```bash
npx fundamentum-cli --version
npx fundamentum-cli apply OWNER/REPO
```

Works on **macOS** and **Linux** (`x64` and `arm64`) and **Windows** (`x64`). Postinstall downloads the matching prebuilt binary from GitHub Releases with embedded SHA-256 verification.

A git checkout keeps `package.json` at `0.0.0` and will not download a binary. Build it locally instead:

```bash
go build -o npm/bin/fundamentum .
# Windows: go build -o npm/bin/fundamentum.exe .
```

If npm's install-script policy blocks postinstall, the launcher tries downloading
the binary on first use when the package directory is writable. Otherwise, run
the installer with the same permissions used for installation. Manual recovery
on Linux/macOS:

```bash
node "$(npm root -g)/fundamentum-cli/install.js"
```

PowerShell:

```powershell
node (Join-Path (npm root -g) "fundamentum-cli/install.js")
```

The download requires access to GitHub Releases. A missing checksum or failed
verification stops installation.

## Quickstart

```bash
# Harden an existing repo
fundamentum apply OWNER/REPO

# Create a private repo + harden it (add --private=false for public)
fundamentum init OWNER/REPO

# Check the baseline
fundamentum audit OWNER/REPO

# Export and reuse a portable baseline
fundamentum --preset oss export -o baseline.json
fundamentum --from baseline.json apply OWNER/REPO

# Preview everything before touching anything
fundamentum --dry-run apply OWNER/REPO

# Apply file changes via a pull request
fundamentum --pr apply OWNER/REPO

# Preview undoing the last changing apply
fundamentum revert OWNER/REPO --mode full --dry-run
```

`--pr` batches file changes into a PR. Settings, security, labels, and protection
still change live; required checks are deferred only while a file PR is needed.
Re-apply after merging it to require the new checks.

## What you get

- **Community health files** — `CONTRIBUTING.md`, `CODEOWNERS`, `CODE_OF_CONDUCT.md`, `SECURITY.md`, issue + PR templates, `dependabot.yml`
- **Branch protection** — rulesets on the actual default branch, solo/team review settings, app-pinned starter checks, and no force-push or deletion. Reconciliation preserves extra checks and stricter live settings.
- **Security** — Dependabot alerts and updates; secret scanning and push protection on public repos, opt-in on private/internal repos via `--advanced-security` (paid); CodeQL for public repos.
- **Starter workflows** — auto-detected Go, Node, Python, Rust, or generic CI. Generic CI and missing Node scripts deliberately fail until configured. Public Go CI includes Codecov reports and a default-branch Codacy coverage job that needs one-time setup.
- **Priority labels** — `priority:critical`, `priority:high`, `priority:medium`, and `priority:low` describe importance. Apply updates metadata without reassigning issues; legacy effort labels require owner review. Opt out with `--skip labels`.
- **Cloud-ID gate** — a tracked PR-head scan with an operator-reviewed exact allowlist and staged check enrollment, including with `--ci none`.
- **Audit and revert** — PASS/FAIL/WARN/SKIP baseline reports and a snapshot of the last changing apply for undo.
- **Bot-managed profile** — opt-in squash-only merges, read-only default Actions tokens, pinned checks, and workflow/environment audit probes.

## Why fundamentum?

| Manual setup | fundamentum |
|---------------|-------------|
| 20+ files and settings across 6 GitHub pages | One command |
| "Which templates does everyone use?" | Opinionated, proven defaults |
| Copy-paste from another repo, hope it's current | Rendered fresh, version-controlled |
| "Did I forget CODEOWNERS?" | Full summary table before you apply |
| Private repo? Pro account? | Plan-aware protection with a documented Actions guard |

Built for solo devs and small teams who want their repos to feel like they were made by someone who cared.

## Commands

| Command | Purpose |
|---------|---------|
| `apply OWNER/REPO` | Harden an existing repo |
| `init OWNER/REPO` | Create a new repo, then harden it |
| `audit OWNER/REPO` | Verify the repo matches the harden baseline |
| `export` | Write a portable JSON baseline (`-o` for a file) |
| `pii-bootstrap OWNER/REPO` | Scan the remote default tree and write local exemptions for review (`--output`, refuses overwrite) |
| `mcp` | Serve agent plan, audit, and apply tools over stdio |
| `revert OWNER/REPO` | Undo the last changing apply (`--mode full\|files\|settings`) |

## Useful flags

| Flag | Purpose |
|------|---------|
| `--dry-run` | Preview the full plan without applying |
| `--pr` | Batch file changes into a PR; settings/security/labels/protection still apply live |
| `--no-overwrite` | Only add missing files, never touch existing ones |
| `--skip` / `--only` | Select steps or exact file paths; includes `labels`, `actions` (bot-managed), and `app-audit` (audit only) |
| `--advanced-security` | Enable GHAS (secret scanning, push protection) on private/internal repos (paid) |
| `--strict` | Fail the run when any core harden step fails, including optional tag/security items |
| `--require-checks` | Override default required contexts (pack jobs plus eligible `pii-gate`); empty requires none, existing checks survive reconciliation |
| `--ci` | CI pack: `auto` (detect go/node/python/rust, else generic), `go`, `node`, `python`, `rust`, `generic`, or `none` |
| `--preset` | Named baseline: `oss` (no prompts), `private` (solo, no GHAS prompt), `strict` (`--strict` + GHAS) |
| `--from` | Load a portable JSON baseline from `export` |
| `--bot-managed` | Opt in to hardening for automation-opened PRs |
| `--app-audit` | Audit named apps from baseline policy; defaults on with `--bot-managed`, explicit false opts out |
| `--app-private-key` | Repeatable `APP_ID=PEM_PATH` audit credential |
| `--app-private-key-env` | Repeatable `APP_ID=ENV_NAME` containing an app key |
| `--app-installation-token-env` | Repeatable `APP_ID=ENV_NAME` containing a token for partial evidence |
| `--branch-up-to-date` | Require branch currency; unset preserves the live policy on reconcile |
| `--octopus` | Opt in to third-party PR review and data sharing; refused with bot-managed |
| `--codacy-instructions` | Opt in to conditional Codacy agent instructions |

Use `fundamentum --help` or `fundamentum COMMAND --help` for all flags. Explicit
flags override baseline values, including `--flag=false`. See the
[full CLI documentation](https://github.com/jpvelasco/fundamentum#flags).

## Prerequisites

A GitHub personal access token with `repo` scope (classic), or fine-grained
Contents write, Administration write, Metadata read, and Issues write for the
default steps. Labels need Issues write; PR mode needs Pull requests write.
Security endpoints may need feature-specific permissions. Use `GITHUB_TOKEN`,
`--token`, or sign in to GitHub CLI for the `gh auth token` fallback.

Workflow writes also need `workflow` scope (classic) or Workflows write
(fine-grained). Missing permission skips workflow files with a warning; audit
then reports missing assets. Refresh the scope and re-apply.

Native branch protection for GitHub Free private repositories requires a paid
plan. Fundamentum ships an Actions guard that reverts direct default-branch
pushes after a grace window and routes file changes through PRs. It excludes
initial and GitHub Actions bot pushes and fails open on API errors. It cannot
enforce required checks before merging; the UI does not bypass plan limits.

## Cloud-ID gate rollout

```bash
fundamentum pii-bootstrap OWNER/REPO --output .github/pii-allowlist.txt
# Review candidates; scrub inappropriate values, then commit/merge legitimate
# exact exemptions. An empty reviewed allowlist is valid.
fundamentum apply OWNER/REPO --dry-run
fundamentum apply OWNER/REPO --pr
# Merge both gate assets, then enroll the required check:
fundamentum apply OWNER/REPO --only protect-main
```

The allowlist is operator-owned and never overwritten by apply. Enrollment
requires a committed valid policy, both exact baseline gate assets, and a clean
default tree. Diagnostics omit matched values. This scans tracked content,
including unchanged files and symlinks; it does not scan history or replace
secret scanning. See the [complete policy and rollout rules](https://github.com/jpvelasco/fundamentum#cloud-id-gate-rollout).

## Named GitHub App audit

Configure `app_audit.expected_apps` in a baseline with app IDs, permission ceilings,
and allowed repositories, then supply each named app's credential:

```bash
fundamentum --from baseline.json --app-private-key 12345=/path/app.pem audit OWNER/REPO
```

App-key inspection mints a short-lived metadata-only token to enumerate full
installation scope. It changes no repository or installation settings. Only
named apps are checked; this is not an inventory of unknown apps. Supplied
installation tokens always report partial evidence as WARN; no credentials
means SKIP. Confirmed policy failures fail the audit, and WARN fails with
`--strict`. Credentials are runtime-only and never exported. See the
[policy example and authentication limits](https://github.com/jpvelasco/fundamentum#github-app-audit).

## Undo an apply

`revert --mode full` restores recorded files, settings, security toggles, rulesets,
and labels. Files/settings modes leave labels alone. Full revert can delete
labels created by apply, which also removes issue assignments; those assignments
are not restored. Snapshots describe the last changing apply. With `--pr`, merge
the snapshot PR before relying on revert, which reads the default-branch snapshot.

## Agent use

`fundamentum mcp` serves `plan`, `audit`, and `apply` tools over stdio. Plan is
read-only; audit changes no repository settings, but app-key inspection mints
a metadata-only token. Apply is mutating — run plan first and confirm with the
operator. App credentials come from server startup configuration, never tool
arguments; failed audits retain their report with `isError`. See the
[repo README](https://github.com/jpvelasco/fundamentum#agent-use-fundamentum-mcp).

> **Disclaimer.** fundamentum is free open-source software, provided **as-is** with no warranty of any kind. It changes your repository's configuration. Whether you run it yourself or have an agent run it for you, **you are responsible for every change made to your repos.**

## License

MIT — see [LICENSE](https://github.com/jpvelasco/fundamentum/blob/main/LICENSE).

fundamentum is an independent project, not affiliated with GitHub.
