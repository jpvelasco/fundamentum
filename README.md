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

# Undo the last apply (writes a snapshot at apply time)
fundamentum revert OWNER/REPO --mode full
```

## Prerequisites

A GitHub personal access token with **`repo` scope** (classic) or **Contents + Metadata + Administration** permissions (fine-grained). Set it via the `GITHUB_TOKEN` environment variable, pass `--token`, or be signed in to the GitHub CLI — fundamentum falls back to `gh auth token` when the first two are absent.

**Workflow files need one more scope.** GitHub refuses writes to `.github/workflows/` (CI starter, CodeQL, the free-tier guard) through any API when the token lacks the **`workflow`** scope (classic) / **Workflows** permission (fine-grained) — the write returns 404. With a repo-only token, apply still lands every non-workflow file and skips the workflow ones with a visible warning; `audit` then flags what's missing. Add the scope (`gh auth refresh -s workflow`, or update your PAT) and re-run to ship them.

```bash
export GITHUB_TOKEN=ghp_xxxxx        # Linux/macOS
$env:GITHUB_TOKEN = "ghp_xxxxx"      # PowerShell
```

**Note:** Branch protection on free-tier private repos requires GitHub Pro for the API. If rulesets are unavailable, fundamentum falls back to classic branch protection — but if that also fails (403), you'll need to set up branch protection manually via the repo Settings → Branches page.

**Updating the free-tier guard.** The guard workflow is a shipped file: a routine apply with `--no-overwrite` skips it once it exists (like any other existing file), so a fundamentum release that changes the guard template leaves the installed guard stale — `audit` reports the drift as a WARN. Refresh it with a targeted apply:

```bash
fundamentum apply OWNER/REPO --only .github/workflows/protect-main-guard.yml
```

The guard's presence forces PR mode, so expect a pull request rather than a direct update.

## What it does

- **Community health files**: `CONTRIBUTING.md`, `CODEOWNERS`, `CODE_OF_CONDUCT.md`, `SECURITY.md`, pull request + issue templates, `dependabot.yml` (per-language ecosystems detected from your manifests), and more.
- **Branch protection**: Modern `protect-main` ruleset (PRs, optional CODEOWNERS review, shipped CI status checks, no force-push/delete) + optional tag protection. Reconcile **merges with your live ruleset** instead of replacing it: required checks keep their app identity (`integration_id`), extra checks and rules survive, and stricter settings are never weakened. Classic fallback only when GitHub says rulesets are unavailable on the plan.
- **Security**: Dependabot alerts + updates everywhere; secret scanning + push protection on public repos, opt-in on private/internal repos via `--advanced-security` (paid GHAS); CodeQL default setup on public repos unless a `codeql*.yml` workflow (or the shipped advanced one) already provides CodeQL.
- **Settings**: Auto-delete merged branches.
- **Opinionated starters**: CI pack is `auto` — Go / Node / Python / Rust from `go.mod`, `package.json`, `pyproject.toml`/`requirements.txt`, or `Cargo.toml`; otherwise a generic `CI` + Trivy workflow whose `CI` job fails with a "configure me" annotation until you give it real commands — a required check that always passes would be false confidence. The node pack runs the `lint`/`test` scripts it finds in `package.json` and fails with the same annotation when they are missing. Override with `--ci go|node|python|rust|generic|none`. `.codacy.yml` is generated from the detected pack (Go analyzers stay off non-Go repos).
- **Audit**: `fundamentum audit OWNER/REPO` prints a PASS/FAIL/WARN/SKIP report against the baseline: FAIL fails the run, WARN (content drift, optional checks) fails only with `--strict`, SKIP covers opted-out steps and plan-gated protection. A repo whose settings are stricter than the baseline passes.
- **Cloud-ID gate**: Ships a stdlib Go helper and `pii-gate` workflow for every visibility and CI pack. It scans every tracked PR-head blob, including unchanged files and symlink contents, for account and AWS resource ID shapes. Diagnostics show safe file locations and shape names, never matched values.

### Cloud-ID gate rollout

```bash
fundamentum pii-bootstrap OWNER/REPO --output .github/pii-allowlist.txt
# Review the local candidates, scrub inappropriate source values, and commit/merge
# only legitimate exact exemptions (an empty reviewed file is valid).
fundamentum apply OWNER/REPO --dry-run
fundamentum apply OWNER/REPO --pr
# Merge the workflow/helper PR, then enroll the check:
fundamentum apply OWNER/REPO --only protect-main
```

Bootstrap uses the GitHub API to scan one pinned default-branch commit. It creates no remote changes, refuses to overwrite local output, and supports `--dry-run` without creating directories. Its local output is deliberately unreviewed. A valid allowlist committed on the default branch signals operator review; the tool cannot prove a human reviewed it.

`.github/pii-allowlist.txt` is operator-owned and never shipped or overwritten by apply. Use one exact ID per line; blank lines, CRLF, duplicates, and `#` comments are accepted. Wildcards, regexes, prefixes, and multiple values are invalid. Only five synthetic values are exempt by default: `123456789012`, `000000000000`, `999999999999`, `ami-0123456789abcdef0`, and `ami-0abcdef1234567890`. Comments in the allowlist are scanned too.

New enrollment requires the committed valid policy, both exact baseline gate files, and a clean full-tree scan. Missing or changed assets defer enrollment until they land and are rechecked. `--no-overwrite`, declined file updates, workflow-scope refusals, and open file PRs cannot establish readiness. Explicit file skips, baseline `keep`, and skipping protection prevent new enrollment; existing required checks and app identities survive. `--only protect-main` can enroll already-live assets. An incompatible live `pii-gate` app pin requires operator attention.

`--ci none` disables starter language CI but still ships this gate, whose check becomes a default once eligible. `--require-checks` remains a complete override (empty means none); explicitly requesting `pii-gate` before readiness fails preflight with rollout guidance. Audit reports managed file drift and pending enrollment as WARN (fails under `--strict`), and an eligible missing default requirement as protection drift. GitHub plan restrictions still apply to private repositories. This checks current tracked content, not history, issues, PR text, or credentials; keep secret scanning enabled.

`--preset oss|private|strict` skips the wizard so a scripted apply reproduces the same harden. `oss` and `private` take the solo default and do not enable paid GHAS; `strict` turns on `--strict` and `--advanced-security`. Visibility still picks the public vs private file set. The Octopus Review workflow is **never** part of a preset — it is a third-party data-sharing service and requires an explicit `--octopus`.

**What each preset actually sets.** The names describe intent, not different baselines — `oss` and `private` set the same flags (they differ only in what you intend to run against):

| Preset    | Wizard prompts          | `--strict` | `--advanced-security` (GHAS prompt on private repos) | Shipped file set                     |
|-----------|-------------------------|------------|------------------------------------------------------|--------------------------------------|
| `oss`     | skipped (solo default)  | off        | off (GHAS only on public repos, which is free)       | follows repo visibility (usually public) |
| `private` | skipped (solo default)  | off        | off (no GHAS prompt; pass `--advanced-security` to opt in) | follows repo visibility (usually private) |
| `strict`  | skipped (solo default)  | **on**     | **on**                                               | follows repo visibility              |

What stays the same across all three: the shipped files (driven by repo **visibility**, not the preset name), branch-protection rulesets, security feature PUTs, and every other flag default. A preset only pre-answers the wizard — explicit flags always win. Presets compose with the other knobs: `--ci` still picks the starter CI pack independently, `--from` loads a baseline first and explicit flags (including a preset) override it, and `--strict` also changes what `--preset strict` fails on (optional tag/security drift fails the run).

Files land as **one commit** (the default) — no more one-commit-per-file history noise. `--pr` batches **file** changes into a pull request instead. Settings, security, labels, and branch protection still apply immediately via the API — they are not deferred until the PR merges. Required status checks are deferred so the open harden PR cannot deadlock on jobs that only exist after merge; re-apply after it lands to require them. Tag ruleset and security steps are optional unless you pass `--strict`. Required checks default to pack jobs plus eligible `pii-gate`, not Codacy (`--require-checks` overrides; existing required checks are merged, not replaced).

Everything is **idempotent** — re-running is safe and fast.

## Bot-managed repos (`--bot-managed`)

`--bot-managed` is an opt-in profile for repos where **automation opens the pull requests** (a dependency updater, a release bot, a codegen pipeline). Those repos need a tighter posture than a human-driven one, because the thing opening PRs is itself software holding a token. Pass the flag (or set `bot_managed: true` in a baseline) to harden for it:

- **Settings:** squash-merge only (merge commits and rebase merges off), auto-delete merged branches.
- **Actions default token:** the default `GITHUB_TOKEN` is locked to **read-only** and **cannot approve pull requests** (`actions/permissions/workflow`), so a compromised workflow can't write to the repo or self-approve an automation PR. The step always prints the before/after.
- **Branch ruleset:** solo-shaped PR params plus `allowed_merge_methods: ["squash"]`; shipped checks are pinned to the GitHub Actions app; no standing bypass actors (the reconcile sends an explicit empty list and prints any it removes).
- **Templates:** no `workflow_run` or `pull_request_target` triggers, no third-party review workflow (`--octopus` is refused), and private repos drop octocov's `pull-requests: write`. `codeql.yml` stays — its `security-events: write` on the analyze job is the one write scope the profile accepts, because CodeQL cannot report without it.
- **External policy checks:** a baseline can require external review checks via `bot_policy_checks` (`[{context, integration_id}]`). The `integration_id` is **required** — a check is never required without pinning which app must post it, so a same-named check from another app can't satisfy it. Bot-managed apply corrects missing or wrong pins on existing shipped and policy checks; unrelated checks keep their existing pins.
- **Keep list:** a baseline `keep` list names shipped files apply must never overwrite, so a hand-tuned workflow survives a re-apply.
- **Audit:** `audit --bot-managed` reports (never deletes) the Actions token posture, repo secret count, environment branch policies, and workflow concerns. It parses YAML to inspect triggers, local `./` actions, and each job's effective permissions, including inherited grants and `write-all`. Write permissions outside that job's default-branch-only environment and malformed YAML produce warnings.

Two guardrails apply: the run **refuses a bot or app token** (it must be driven by a human operator, not the automation it's about to restrict), and it **refuses `--octopus`**.

> **Scope:** the profile hardens repository **settings and shipped templates**. It does **not** vet the code that automation writes — a bot-managed repo still needs review of what its bots merge, exactly as any repo does.

### Managed priority labels

Apply manages these four labels by default. Priority measures importance; effort and size belong in the issue body or a milestone.

| Label | Color | Definition |
|---|---|---|
| `priority:critical` | `#8b0000` | Code execution, destructive data paths, or silent data corruption — fix first |
| `priority:high` | `#d93f0b` | Broken core flows, state corruption, orphaned paid resources, or unencrypted credential paths |
| `priority:medium` | `#fbca04` | Conditional breakage, wrong output or cost figures, silent truncation |
| `priority:low` | `#c5def5` | Cosmetic inconsistencies, edge cases, minor DX papercuts |

Apply creates missing labels and updates divergent color/description in place, preserving issue assignments. The token needs permission to manage repository labels (for a fine-grained PAT, Issues write). Matching labels are skipped. Other labels remain repo-owned. Audit reports missing canonical labels as FAIL and metadata drift as WARN (fails with `--strict`). Use `--skip labels` to leave labels alone or `--only labels` to reconcile just this step. These selectors round-trip through `export` / `--from`; MCP plan and audit remain read-only.

A legacy `priority:*` or `P0`/`P1`/`P2` scheme triggers migration guidance in the plan and audit. Review each issue's impact against the definitions above before mapping it. `P0-quick-win`, `P1-medium`, and `P2-larger` describe effort: none maps automatically to an importance tier. The owner relabels issues and retires legacy labels. Fundamentum never relabels issues or changes legacy labels.

Only changed labels accepted after wizard selection enter the V1 snapshot at `.github/fundamentum-baseline.json`, with explicit prior presence and original metadata. Snapshot capture or persistence failure prevents label changes. With `--pr` (including automatic PR fallback), labels change **live** after the snapshot file is saved in the file PR; merge that PR before relying on `revert`, which reads the default branch's snapshot. A pending snapshot PR is not yet the default branch's undo record.

`revert --mode full` restores recorded metadata, recreates previously existing labels if missing, and deletes labels recorded as previously absent. Deleting a label removes its issue assignments; assignment history is not captured or restored. `--mode files` and `--mode settings` leave labels untouched, as do older snapshots without label entries. Snapshots describe the **last changing apply**, not installation history: a later apply can replace the undo record. Unrelated and declined labels never enter that record.

## Flags

| Flag                | Description                                      |
|---------------------|--------------------------------------------------|
| `--dry-run`         | Print actions without applying them (update rows say exactly what drifted) |
| `--verbose`         | Print every API call                             |
| `--token`           | GitHub token (defaults to `GITHUB_TOKEN`, then `gh auth token`) |
| `--no-overwrite`    | Skip any file that already exists                |
| `--pr`              | Batch file changes into a PR (default is one direct commit); settings/security/labels/protection still apply live |
| `--skip`            | Leave steps alone: `settings`, `protect-main`, `protect-version-tags`, `security`, `labels`, `files`, or an exact file path |
| `--only`            | When set, apply just the listed steps/paths (same keys as `--skip`) |
| `--advanced-security` | Enable GitHub Advanced Security (secret scanning, push protection) on private/internal repos (paid) |
| `--strict`          | Fail the run when any core harden step fails, including optional tag/security items |
| `--require-checks`  | Required status-check contexts for `protect-main` (merged with existing checks; default: pack jobs plus eligible `pii-gate`) |
| `--ci`              | CI pack: `auto` (detect go/node/python/rust, else generic), `go`, `node`, `python`, `rust`, `generic`, or `none` |
| `--preset`          | Named baseline: `oss` (no prompts), `private` (solo, no GHAS prompt), `strict` (`--strict` + GHAS) |
| `--from`            | Load a portable JSON baseline from `export` (explicit flags win, including `--flag=false`) |
| `--octopus`         | Opt in to the third-party Octopus Review workflow (public repos; shares PR diffs with octopus-review.ai) |
| `--codacy-instructions` | Opt in to `.github/instructions/codacy.instructions.md` (Codacy MCP rules for AI agents) |
| `--bot-managed`      | Opt in to the bot-managed profile: harden repos whose PRs are opened by automation (squash-only merges, read-only default Actions token, no privileged workflow triggers, pinned required checks) |
| `--mode`            | Revert scope: `full` (default), `files`, or `settings` |
| `--branch-up-to-date` | `protect-main`: require branches up to date before merging (default true; unset keeps the live policy on reconcile) |
| `--version`         | Print version and exit                           |

`init` also supports `--private` (default: `true`).

## How it works

1. fundamentum detects your repo's current state (existing files, branch protection, visibility, README/AGENTS.md filenames, package.json scripts).
2. It renders opinionated templates and shows a summary table of what will change — update rows carry the exact drift (ruleset fields, security features, content diffstats).
3. You confirm all defaults or step through items interactively.
4. Files land as one commit (or one PR with `--pr`); a 409 from branch protection switches to PR mode automatically. Settings, security, labels, and branch protection are applied via the GitHub API even in `--pr` mode — `--pr` is not a dry-run for those steps. A `--from` baseline fills flags the operator did not pass; an explicit flag wins, including `--strict=false`.

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

### Troubleshooting: Windows CRLF makes dry-run lie about template updates

If a Windows checkout shows a clean `git status` but `--dry-run` reports `would update` for templates you never touched, the embedded template files were rewritten with CRLF line endings. The `.gitattributes` clean filter normalizes CRLF→LF so git sees no diff, but `//go:embed` ships the CRLF bytes and they mismatch the LF blobs on GitHub. Fix: delete the affected files and `git checkout HEAD -- <file>` (or clone fresh), then verify with `git hash-object --no-filters <file>` — it must equal `git rev-parse HEAD:<file>`. See `AGENTS.md` ("Windows CRLF embed trap") for the full explanation.

## Scope & Philosophy

Focused, high-quality, free forever. Good defaults for OSS and small teams. Not a compliance platform or org-scale tool.

## Revert

`fundamentum revert OWNER/REPO` undoes the last apply using the snapshot written to `.github/fundamentum-baseline.json` at apply time.

- **`--mode full`** (default): restore files, repo settings, security toggles, and the managed rulesets to their pre-apply state.
- **`--mode files`**: restore only the files apply wrote (delete what it created, restore what it overwrote).
- **`--mode settings`**: restore files + repo settings + security toggles (leave the rulesets as they are).

`--dry-run` prints the plan without changing anything. Revert is idempotent: a snapshot field that already matches is skipped.

## Agent use (`fundamentum mcp`)

`fundamentum mcp` runs a [Model Context Protocol](https://modelcontextprotocol.io) server over stdio so a coding agent can drive the same commands locally. It exposes three tools:

- `plan` — dry-run a harden and return the plan table (never changes anything).
- `audit` — compare a repo to the baseline (read-only).
- `apply` — harden for real (solo defaults, prompts disabled). **Mutating** — run `plan` first and only call `apply` once the operator has confirmed the plan.

Tokens come from the same places as the CLI (`GITHUB_TOKEN`, `--token`, `gh auth token`) and are never written to tool output.

> **Disclaimer.** fundamentum is free open-source software, provided **as-is** with no warranty of any kind, express or implied. Branch protection, security toggles, and file writes change your repository's configuration. Whether you run it yourself or have an agent run it for you, **you are responsible for every change made to your repos.** Read the plan before you apply.

## License

MIT

