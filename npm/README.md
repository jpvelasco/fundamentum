# fundamentum-cli

**One command to make your GitHub repo look (and behave) like a pro open-source project.**

[GitHub](https://github.com/jpvelasco/fundamentum)

Fundamentum bootstraps and hardens GitHub repos for OSS collaboration — community health files, branch protection, secret scanning, and starter CI workflows. You bring the code, it brings the polish.

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

> **npm 9+ / Ubuntu 26+ note:** npm's `allow-scripts` security policy may block the postinstall
> script, so the binary won't be downloaded at install time. No worries — if `fundamentum` is invoked
> without a binary present it detects this and downloads automatically. You can also trigger it
> manually:
> ```bash
> node $(npm root -g)/fundamentum-cli/install.js
> ```

## Quickstart

```bash
# Harden an existing repo
fundamentum apply OWNER/REPO

# Create a new repo + harden it in one shot
fundamentum init OWNER/REPO

# Preview everything before touching anything
fundamentum --dry-run apply OWNER/REPO

# Apply file changes via a pull request
fundamentum --pr apply OWNER/REPO
```

That's it. A repo that went from bare-bones to release-ready in under a minute.

## What you get

- **Community health files** — `CONTRIBUTING.md`, `CODEOWNERS`, `CODE_OF_CONDUCT.md`, `SECURITY.md`, issue + PR templates, `dependabot.yml`
- **Branch protection** — modern rulesets on `main` (PR-only pushes, CODEOWNERS review, required checks, no force-push)
- **Security** — Dependabot alerts everywhere; secret scanning + push protection on public repos, opt-in on private/internal repos via `--advanced-security`; CodeQL for public repos
- **Starter workflows** — CI, coverage, and CodeQL pipelines that work out of the box
- **Idempotent by design** — re-running is always safe

## Why fundamentum?

| Manual setup | fundamentum |
|---------------|-------------|
| 20+ files and settings across 6 GitHub pages | One command |
| "Which templates does everyone use?" | Opinionated, proven defaults |
| Copy-paste from another repo, hope it's current | Rendered fresh, version-controlled |
| "Did I forget CODEOWNERS?" | Full summary table before you apply |
| Private repo? Pro account? | Graceful fallbacks for free-tier |

Built for solo devs and small teams who want their repos to feel like they were made by someone who cared.

## Commands

| Command | Purpose |
|---------|---------|
| `apply OWNER/REPO` | Harden an existing repo |
| `init OWNER/REPO` | Create a new repo, then harden it |
| `--dry-run` | Preview the full plan without applying |
| `--pr` | Batch file changes into a pull request |
| `--no-overwrite` | Only add missing files, never touch existing ones |
| `--advanced-security` | Enable GHAS (secret scanning, push protection) on private/internal repos (paid) |

## Prerequisites

A GitHub token with `repo` scope (classic) or Contents + Metadata + Administration (fine-grained) — via `GITHUB_TOKEN` or `--token`.

## License

MIT — see [LICENSE](https://github.com/jpvelasco/fundamentum/blob/main/LICENSE).

fundamentum is an independent project, not affiliated with GitHub.
