# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.5.1] - 2026-10-28

**Patch release.** Keeps required status checks in policy-only PR-mode runs.

### Fixed

- **Policy-only `--pr` runs retain required checks.** When no file changes need a pull request, apply now reconciles the required checks immediately, including missing or incorrect GitHub Actions app pins in bot-managed mode. Check deferral still applies when file changes actually go to a pull request.

## [0.5.0] - 2026-10-28

**Feature release.** Adds opt-in hardening and audit for automation-managed repositories, tightens the shipped CI workflows, and restricts version-tag creation to authorized actors.

### Breaking changes

- **`protect-version-tags` now restricts tag creation to repository admins.** The new creation rule blocks `GITHUB_TOKEN` and release bots from creating `v*` tags unless their actor is explicitly authorized to bypass it. Update release automation to use an authorized actor before re-applying; repository admins retain the existing bypass.

### Added

- **`--bot-managed` profile** for automation-opened PRs: squash-only merges, read-only default Actions tokens without review approval, no branch-ruleset bypass actors, app-pinned policy checks, and workflow/environment audit probes. It can be combined with any preset and is off by default.

### Security

- **Coverage uploads no longer run in privileged contexts reachable by PRs.** Two related defaults are tightened for every hardened repo:
  - **Codacy coverage moved out of the privileged `workflow_run` forwarder into a push-only CI job.** The `codacy-coverage.yml` `workflow_run` workflow consumed a test-job artifact in a privileged context. It is no longer shipped; **users must manually remove any existing `.github/workflows/codacy-coverage.yml`** after configuring the replacement because `apply` never deletes files. The Codacy upload now runs as a `codacy-upload` job inside `ci.yml` that runs only on the default-branch push (`needs: test`) and reads `CODACY_REPOSITORY_API_TOKEN` from a `codacy` **environment** restricted to the default branch instead of a repository secret, so a pull request can never reach the token. `GITHUB_SETTINGS.md` now walks through creating that environment and secret.
  - **`id-token: write` moved out of the test job.** The Codecov OIDC grant now lives on a separate push-only `coverage-upload` job (`needs: test`); the test job — which runs untrusted PR code — no longer holds an OIDC token. Per-OS base coverage and Test Analytics upload on the trusted default-branch push. Public PR coverage uploads without a token or OIDC from the test job, using Codecov's unprotected-branch namespace for both same-repository and fork PRs.
- **Octopus Review template runs on `pull_request`, not `pull_request_target`.** The opt-in workflow no longer requests the privileged `pull_request_target` trigger; `pull-requests: write` is scoped to the review job only and the header documents the remaining risk of running third-party code with a PR-write token.

### Changed

- **Shipped required checks are pinned to the GitHub Actions app.** When Fundamentum adds the selected CI pack's default checks (Lint, Test, gosec, Trivy, …) to `protect-main`, it now sets that check's `integration_id` to the GitHub Actions app (id `15368`, overridable via `github.GitHubActionsAppID`), so only a real GitHub Actions check run with that exact name can satisfy it — a third-party app posting an identically-named green check cannot. Explicit `--require-checks` entries are never automatically pinned, even when their names match a shipped check. Ordinary reconciliation preserves existing pins. Bot-managed reconciliation corrects missing or wrong pins on intended shipped and external policy checks, preserving unrelated checks.

### Fixed

- **Bot-managed merge settings reconcile independently of branch deletion.** Apply repairs non-squash merge methods even when auto-delete is already on; audit also checks the merge settings.

- **Portable keep lists tolerate absent files.** Entries not shipped for the target visibility or CI pack are harmless. Whole-step keys in `keep` are ignored so it cannot disable protection or token lockdown; extensionless paths such as `CODEOWNERS` still work. Explicit `--skip` and `--only` typos still fail validation.
- **Bot policy checks stay opt-in.** Baseline policy checks are ignored when `--bot-managed` is off, including an explicit flag override.
- **Environment audit uses GitHub deployment policies.** Reads the top-level deployment settings and paginated branch/tag policies; only an exact default-branch rule with no tag access passes. API failures remain warnings instead of silently marking environments safe.
- **CodeQL scans the npm shim and release scripts.** Restores the JavaScript analysis configuration alongside Go and Actions.
- **Bot-managed workflow audit parses YAML per job.** The scan recognizes commented and quoted permission values, `write-all`, workflow-level inheritance, job overrides, and all trigger forms. A restricted environment on one job no longer hides another job's writes. Malformed YAML produces a warning.

## [0.4.3] - 2026-10-26

**Patch release.** The free-tier guard's first real run exposed the rest of its latent defect stack: a detection filter that could never match a legitimate merge, an association check that races GitHub's eventual consistency, and a revert step that could never have worked. Together they cancelled into a loud no-op — every push red-Xed, nothing was ever reverted — but each defect individually is serious, and fixing detection without the revert would have made the guard revert legitimate merges.

### Fixed

- **Guard detection matches `merge_commit_sha`, not PR state.** The detection step filtered the commit's associated pull requests on `.state == "merged"` — but REST PR list payloads only ever carry `open` or `closed`, so the filter never matched and every push, legitimate merges included, computed `merged=0` and attempted a revert. Detection now matches `merge_commit_sha ==` the pushed SHA (null until a PR merges; equal to the pushed SHA on squash/rebase/merge commits), verified live: the fixed filter matches a squash-merge commit and returns nothing for an orphaned direct-push commit.
- **Guard detection polls through the association grace window.** The commit→pull-request association is eventually consistent — a guard run seconds after a legitimate merge can see an empty list (observed live: empty at +6s, present a minute later). Detection now retries for ~90 seconds before concluding "direct push"; a check that never completes (persistent API errors) fails open with a red X rather than reverting.
- **Guard reverts via the API ref update, not a local git push.** The revert step did `git init` and then `git push --force` of the previous tip — impossible from an empty repository, which has no objects and cannot push a SHA it never fetched (`fatal: bad object`); the revert never worked. The revert is now `PATCH /repos/{repo}/git/refs/heads/{branch}` with force, which needs only the `contents: write` permission the workflow already holds. With the grace window, a direct push is reverted within roughly 90–105 seconds.

## [0.4.2] - 2026-10-26

**Patch release.** The free-tier guard shipped dead-on-arrival: its merged-PR check calls an endpoint the workflow token could not use, so the guard red-Xed every push and never reverted anything. Found by the ultimate live test — a deliberate direct push that sat unreverted while the guard run failed open.

### Fixed

- **Guard grants `pull-requests: read` for its merged-PR check.** The detection step calls `GET /repos/{repo}/commits/{sha}/pulls` to decide whether a pushed SHA is a merged pull request's `merge_commit_sha`. The shipped workflow's `permissions:` block granted only `contents: write`, so GitHub answered that call with 403 "Resource not accessible by integration" — the check step failed, the job red-Xed (correct fail-open behavior), and the revert never ran. Every repo with a v0.4.0/v0.4.1 guard has a guard that cannot verify any push. The template now grants `pull-requests: read` alongside `contents: write`, and a render assertion pins the permission so the guard cannot ship without it again. Re-apply to update an already-shipped guard.

## [0.4.1] - 2026-10-26

**Patch release.** Fixes the free-tier guard bootstrap failure found applying v0.4.0 to a private free-tier repo: GitHub refuses workflow-file writes on every API for a token without the `workflow` scope, and nothing classified the refusal on creates — so the apply hard-failed instead of shipping what it could.

### Fixed

- **Workflow-create 404 is the scope gate, not a hard failure.** GitHub gates every write to `.github/workflows/` behind the `workflow` token scope (classic) / Workflows permission (fine-grained): the Contents API PUT returns 404 on workflow creates as well as updates, and the git data API refuses to create a tree containing a workflow path with the same 404 (verified live against a free-tier private repo with a repo-scoped token — the assumption that the git data API bypasses the gate is false). `ErrWorkflowLocked` previously covered updates only, so the guard bootstrap — a workflow-file create — hard-failed the whole PR-mode apply. A 404 on a workflow path is now classified as the scope refusal for creates too: apply skips the file with a visible "add the workflow scope and re-run" warning, lands everything else, and `audit` FAILs the missing file as the honest follow-up. The README now documents the scope requirement.
- **PR-mode file changes land as one commit.** `ApplyViaPR` no longer writes one Contents-API commit per file on the harden branch: non-workflow files are batched into ONE commit via the git data API (blobs → tree → commit → ref update), and workflow files — which cannot ride the batch, per the tree gate above — go per-file through the Contents API, where a scope-refused write is skipped with the same warning instead of failing the PR. Nothing written at all means no PR. Direct mode keeps its fallback: a batch containing workflow paths fails at tree creation for scope-less tokens and falls back to per-file upserts, which now skip refused workflow writes (creates included) instead of failing the run.

## [0.4.0] - 2026-10-26

**Feature release.** The free-tier Actions guard enforces PR discipline on private repos where GitHub's plan refuses branch protection — plus the PR-rule param pass-through fix found on fabrica.

### Added

- **Free-tier Actions guard.** GitHub Free private repositories cannot have protected branches (the ruleset and classic-protection APIs both 403 "Upgrade to GitHub Pro"; per GitHub's docs, protected branches are a Pro feature for private repos). Fundamentum now ships a guard workflow to every private repo that enforces the same discipline via Actions: a push to the default branch whose SHA is not a merged pull request's `merge_commit_sha` (checked via the API) is force-reverted to the previous tip within seconds, with a red X on the offending commit. The initial branch push and `github-actions[bot]` pushes are excluded, and the check fails open with a loud signal on transient API errors so a hiccup never reverts a legitimate merge. Public repos never ship the guard (they get real rulesets); private Pro repos get it dormant behind their ruleset. When the guard is shipped or present, apply forces PR mode for file changes — including the bootstrap commit, which a direct push would otherwise revert seconds after landing — and audit judges plan-gated protection by the guard: PASS via the guard when present, FAIL with re-apply guidance when missing.

### Fixed

- **Reconcile preserves unowned pull_request parameters.** `strictestPRRule` rebuilt the params map from the five baseline keys, so a reconcile silently dropped a hand-tuned repo's `allowed_merge_methods` (e.g. squash-only merges), `require_extra_approval_for_unattributed_changes`, and `required_reviewers` — found on fabrica, where the merge would have wiped squash-only merging. The merge now copies every live parameter first and re-merges only the owned keys with baseline-or-stricter semantics; unowned keys pass through untouched, the same fidelity rule that keeps existing checks' `integration_id`.

## [0.3.3] - 2026-10-25

**Patch release.** The ruleset reconcile now passes through live pull_request parameters it does not own — the same fidelity rule that preserves existing checks' `integration_id` — so reconciling a hand-tuned repo no longer drops its merge posture.

### Fixed

- **Reconcile preserves unowned pull_request parameters.** `strictestPRRule` rebuilt the params map from the five baseline keys, so a reconcile silently dropped a hand-tuned repo's `allowed_merge_methods` (e.g. squash-only merges), `require_extra_approval_for_unattributed_changes`, and `required_reviewers` — found on fabrica, where the merge would have wiped squash-only merging. The merge now copies every live parameter first and re-merges only the owned keys with baseline-or-stricter semantics; unowned keys pass through untouched. Regression tests pin the fabrica-shaped fixture at both the unit and the full-body-merge level.

## [0.3.2] - 2026-10-25

**Patch release.** Fixes two shipped-template bugs found by running fundamentum v0.3.1 against a real second repo: the CodeQL guidance in GITHUB_SETTINGS.md was wrong for repos that scan via the committed advanced workflow, and the CodeQL config template lacked a trailing newline.

### Fixed

- **CodeQL guidance covers advanced workflows, not just default setup.** GITHUB_SETTINGS.md told maintainers to enable CodeQL default setup unconditionally — wrong for repos that scan via the committed `codeql.yml` advanced workflow, because GitHub refuses to run both for the same languages and the workflow's uploads fail ("CodeQL analyses from advanced configurations cannot be processed when the default setup is enabled"). The guidance now covers both cases: advanced workflow OR default setup, never both.
- **CodeQL config template ends with a trailing newline.** `public_codeql-config.yml` was missing its final newline, so apply/audit reported `+0/-0` content drift — a byte diff with zero line changes — on every repo whose live copy ended with a newline.

## [0.3.1] - 2026-10-25

**Patch release.** Markdown is pinned to LF in `.gitattributes` so Windows checkouts cannot reintroduce content drift against the LF shipped templates, and the opt-in Codacy instructions point to Codacy's official Agent Skills pack instead of bundling it.

### Documentation

- **Codacy's official Agent Skills are pointed to, not bundled.** The opt-in `.github/instructions/codacy.instructions.md` ends with a short section naming github.com/codacy/codacy-skills (cloud CLI, review enrichment, configuration tuning) as optional maintainer tooling, with per-agent install instructions living in that README. fundamentum ships baselines and points to extras, so vendored copies of the fast-moving pack can never go stale in the templates; the shipped rules apply with or without the pack. AGENTS.md records the decision.

### Other

- **`*.md` forced to LF in `.gitattributes`.** Markdown was the one text format not covered; a CRLF-committed `.md` (GITHUB_SETTINGS.md) showed up as permanent content drift against the LF shipped template until an apply normalized it. Windows checkouts and editors can no longer reintroduce the drift.

## [0.3.0] - 2026-10-25

**Feature release.** Per-step opt-outs, merge-based ruleset reconcile, honest CI starters, a PASS/FAIL/WARN/SKIP audit, and single-commit file apply — plus opt-in treatment for third-party data-sharing templates and the AI agent-instructions file.

### Added

- **`--skip` / `--only` per-step opt-outs.** `--skip settings,protect-main,protect-version-tags,security,files` (or exact file paths) leaves steps alone; `--only` restricts apply/audit to the listed keys. Opted-out rows show as skipped in the plan and SKIP in audit; unknown tokens are a hard error instead of a silent no-op. Both persist in `export` baselines (`skip` / `only`).
- **Merge-based ruleset reconcile.** Reconciling `protect-main` now merges with the live ruleset instead of rebuilding it: required checks already present keep their `integration_id` (only the app that posted them can satisfy them), extra checks and unowned rules survive, condition entries are kept, and stricter live review settings (approval count, last-push approval, stale dismissal, code-owner review) are never weakened. Drift is judged weaker-only — a hand-tuned stricter ruleset passes untouched.
- **`--branch-up-to-date`.** The `strict_required_status_checks_policy` ("require branches up to date") is now a flag (default `true`) and a tri-state baseline field; when left unset on a reconcile, the live policy is kept instead of being forced back.
- **Audit PASS/FAIL/WARN/SKIP.** `audit` now judges intent: FAIL (missing files, weakened ruleset, disabled core settings) fails the run; WARN (content drift on shipped files, tag-ruleset or secret-scanning drift) fails only with `--strict`; SKIP (opted-out steps, plan-gated protection, private-without-GHAS secret scanning) never fails. Required checks are judged "at least the intended ones", so a stricter-than-baseline repo passes, and shipped file content is compared — not just existence.
- **Single-commit file apply.** Direct-mode file changes land as ONE commit via the git data API instead of one commit per file; a protection rejection (409/422) switches the batch to PR mode, and any other batch failure falls back to the per-file Contents API (which still skips Actions-locked workflow files).
- **`gh auth token` fallback.** When `--token` and `GITHUB_TOKEN` are absent, the GitHub CLI's authenticated session is used.
- **Per-language Dependabot and Codacy configs.** `dependabot.yml` ships the detected ecosystem(s) — `gomod` / `npm` / `pip` / `cargo` alongside `github-actions` — and `.codacy.yml` is generated per CI pack, so Go analyzers (govet, staticcheck) no longer run on Node/Python/Rust targets.

### Changed

- **Octopus Review is an explicit opt-in (`--octopus`).** The third-party workflow (it uploads PR diffs plus a PR-write token to octopus-review.ai on `pull_request_target`) no longer ships by default and is not part of any preset.
- **Codacy agent instructions are an explicit opt-in (`--codacy-instructions`).** `.github/instructions/codacy.instructions.md` no longer ships by default; the content is rewritten to be conditional — with no Codacy MCP server available, agents skip Codacy analysis instead of stalling ("critical error" language removed).
- **Generic CI fails until configured.** The `generic` pack's `CI` job ends with a `::error::` "configure me" annotation instead of passing vacuously — a required check that always passed was false confidence. The `node` pack runs the real `lint`/`test` scripts from `package.json` and fails the same way when they are missing.
- **PR template standardizes on AGENTS.md.** The PR template detects the actual README filename and references the repo's agent guide — AGENTS.md (the standard) or a legacy CLAUDE.md when only that exists; the default for new repos is AGENTS.md.
- **Runtime errors no longer dump usage text.** Failed runs print the error, not the full help block.

### Fixed

- **Dry-run says what would update.** Update rows carry the exact drift: ruleset fields (`drift: …`), missing security features (`off: dependabot alerts, …`), and per-file content diffstats (`content differs (+3/-5)`).
- **Security planning is fail-closed.** A security status-check error aborts the plan instead of silently planning "would update"; CodeQL counts as enabled via default setup OR a `codeql*.yml` workflow, fixing the false positive on advanced-workflow repos.
- **`featureEnabled` accepts 200-with-body.** GitHub answers `automated-security-fixes` with `200 {"enabled":true,…}` on some deployments; only 204/404 were accepted, which aborted every plan against such a repo.
- **Solo inference reads the CODEOWNERS flag.** A solo maintainer keeping stale-review dismissal on was misread as team mode, which would have forced code-owner review back on and deadlocked their own PRs; `require_code_owner_review: false` is now the solo marker regardless.
- **`--skip`/`--only` file tokens match case-insensitively.** Flag tokens are lowercased but file lookups were not, so `--skip .github/CODEOWNERS` silently did nothing.
- **Audit file-probe errors FAIL.** A 5xx/403 while checking a baseline file previously WARNed (passing without `--strict`) — now it fails the audit, matching the fail-closed planning everywhere else.
- **Per-file 409 fallback reports PR-creation errors.** The error from the fallback PR is printed instead of being swallowed into a generic "required items failed".

### Documentation

- **AGENTS.md and README match the new baseline.** New flags, merge-reconcile semantics, audit states, opt-in templates, single-commit apply, and the one sanctioned `exec.Command` exception (the fixed-argv `gh auth token` fallback) are documented; live counterparts (`PULL_REQUEST_TEMPLATE.md`, `dependabot.yml`) are synced.

### Other

- **Template action pins synced to live versions** (checkout v7.0.1, codecov-action v7.1.1, upload/download-artifact 7.0.2/8.0.2, setup-node v7.1.0, codeql-action 9b325408), so re-applying no longer reverts Dependabot's bumps; the octocov starter's stale v4 checkout pin is unified with the v7 template set.

## [0.2.3] - 2026-10-24

**Patch release.** Correctness and security fixes for the npm shim and the shipped Node/Python CI packs, plus baseline flag, `--ci none`, and branch-protection reporting fixes, on a fully updated toolchain.

### Fixed

- **npm shim validates release archives against path traversal.** The install script now rejects archive members with absolute paths or `..` segments from the tar/unzip member list before extraction, closing the traversal vector independently of the checksum pin (defense-in-depth). A `node --test` suite covers the validator.
- **Shipped Node CI installs deps without a lockfile.** The `node` CI pack no longer requires a lockfile, so `--ci node`/`auto` targets without one lint and test successfully.
- **Shipped Python CI installs project deps before lint and test.** The `python` CI pack installs the project before the lint and test steps run.
- **`--ci none` requires no status checks.** Resolving the CI pack to `none` leaves `protect-main` without required checks instead of requiring CI contexts that never report.
- **Explicit `false` flags override baseline bools.** An explicitly passed `false` flag now overrides a `true` value loaded from a `--from` baseline instead of deferring to the baseline.
- **Dry-run reports plan-gated branch protection.** `apply --dry-run` on a plan-gated private repo now surfaces the unavailable branch-protection state in the plan instead of silently planning a create.
- **Plan-gate 403 detection narrowed.** The "branch protection unavailable" match is narrowed so other 403 bodies (token scope, SSO, IP allow list) are no longer misread as plan-gated.

### Documentation

- **Octopus data flow disclosed.** The shipped Octopus Review workflow template (and the live self-applied copy) now states that on `pull_request_target` the action uploads the PR diff and its scoped `GITHUB_TOKEN` to octopus-review.ai, and that deleting the file opts out.

### Other

- **Go 1.26.9 → 1.26.10.** GO-2026-6809 in `crypto/x509` is fixed in the 1.26.10 standard library. `govulncheck` moves to v1.9.0 (the earlier pin cannot load Go 1.26), and npm is pinned to 11.21.0 before the trusted-publishing step (OIDC publishing requires npm ≥ 11.5.1).
- **golangci-lint pin v2.12.2 → v2.15.0.** The v2.12.2 binary was built with go1.26.5 and its embedded type-checker panics under go1.27 toolchains; v2.15.0 (built with go1.27.0) lints clean.
- **GitHub Actions updates.** CodeQL actions 4.39.0, codecov-action 7.1.1, goreleaser-action 7.3.0, setup-node 7.1.0, artifact actions 7.0.2/8.0.2.

## [0.2.2] - 2026-09-10

**Patch release.** Codacy community files now ship only to public repos; private targets no longer receive templates Codacy's free Open Source plan cannot use.

### Fixed

- **Codacy templates are public-only.** `.codacy.yml`, the Codacy coverage workflow, and the Codacy instructions file are `public_`-prefixed so private and internal repos no longer get them. Codacy's free Open Source plan covers public repos only; convert a repo to public later and re-run `apply` to add them.

### Documentation

- **Contributor docs match live CI and the public-only Codacy file set.** AGENTS.md no longer claims a standalone Template-drift job or a Codacy Analysis CI job, and it documents that the three Codacy files do not ship to private repos.

## [0.2.1] - 2026-09-10

**Patch release.** `apply` and `audit` no longer abort during pre-flight on free-tier private repos, where GitHub plan-gates both the rulesets and classic branch-protection APIs.

### Fixed

- **Pre-flight no longer aborts when branch protection is plan-gated.** Free-tier private repos get the same "Upgrade to GitHub Pro…" `403` from both the rulesets and classic branch-protection APIs. The pre-flight existence checks treated every `403` as fatal, so the plan died before the classic fallback could run. Plan-gate `403`s on the ruleset, tag-ruleset, and classic-protection probes are now read as "feature unavailable" (plan state modeled as absent) and planning continues; other `403`s (token scope, SSO, IP allow list) still fail.
- **Actionable error when branch protection is unavailable.** When the rulesets API and the classic fallback are both plan-gated, `apply` fails the `protect-main` item with an error that tells the operator to set protection manually in repo Settings → Branches, preserving the underlying `403` cause for re-inspection.

## [0.2.0] - 2026-09-10

**Feature release.** Adds the `audit` command, `--strict`, named `--preset` baselines, portable `export`/`--from` baselines, non-Go CI packs, and ruleset reconciliation, makes required status checks configurable, and makes dry-run planning report existing settings accurately.

### Added

- **`audit` command.** `fundamentum audit OWNER/REPO` verifies a hardened repo against its baseline — comparing managed rulesets, classic protection, security toggles, and community files — so drift is caught before it bites.
- **`--strict`.** Optional core harden steps (previously best-effort) now fail the run when they cannot be applied, so a partial harden is never reported as success.
- **Configurable required status checks.** `--require-checks` overrides the checks `protect-main` requires; the default now requires the shipped CI jobs that report on PRs instead of Codacy Static Code Analysis (whose cloud check can lag).
- **Ruleset reconciliation.** `protect-main` and `protect-version-tags` are fetched and their Fundamentum-owned fields compared on drift, so re-applying an already-hardened repo reconciles changed rules instead of skipping on name match.
- **`--preset oss|private|strict`.** Named baselines skip the wizard so `apply`/`audit`/`init --dry-run` are fully non-interactive. `oss`/`private` keep the solo default and do not enable paid GHAS; `strict` turns on `--strict` plus `--advanced-security`. Visibility still selects the public vs private file set.
- **`export` / `--from`.** `fundamentum export` writes a portable JSON baseline (preset, CI pack, required checks, GHAS, strict). `apply --from baseline.json` and `audit --from` reapply it; explicit flags still win.
- **Language CI packs.** `--ci node|python|rust` (and `auto` detection from `package.json` / `pyproject.toml` / `requirements.txt` / `Cargo.toml`) ship Lint + Test + Trivy workflows that do not assume Go.

### Changed

- **`--pr` now states the live-vs-PR split at runtime.** File changes still go in a pull request; settings, security, and branch protection still apply live. Help, README, and a banner on `--pr` apply / `init --dry-run --pr` say so explicitly.

### Fixed

- **Non-Go repos no longer get a permanently red Go CI starter.** `--ci auto` (default) ships the full Go workflow only when the target has `go.mod`; otherwise it writes a generic `CI` + Trivy pack. `--ci go|generic|none` overrides detection. Required checks follow the resolved pack so `protect-main` does not demand `gosec` on a Node repo.
- **Dry-run reports existing settings accurately.** `apply` now threads the fetched repo state into planning so existing general settings and fully configured security baselines are reported as skip/update instead of always "would create". Optional security state probes that error (e.g. a token without `security_events` scope) no longer abort the plan.
- **npm shim no longer loops on the `0.0.0` in-repo version.** `install.js`/`run.js` stop the redundant re-download loop, and the `0.0.0` skip message no longer references a build path that does not exist in module scope.
- **Private dry-run lists the offered GHAS plan lines.** A private `apply --dry-run` now shows secret scanning as a would-create item, and the live private GHAS prompt path is covered by tests.
- **Progress and skip lines go to the injected `io.Writer`.** PR-mode and skip progress is written to the writer passed in rather than a package-level default, so it is testable and captured.
- **`WithBaseURL` accepts only true loopback hosts.** Non-loopback overrides are rejected so tests cannot be pointed at an arbitrary remote.
- **Ruleset lists are paginated.** A single unpaged `GET` missed `protect-main` on later pages and could falsely report it as absent; list responses are now paged (with `404` and `Link` edge cases covered).
- **Classic-protection fallback only when rulesets are unavailable.** A generic `403` (token scope, SSO, IP allow list) no longer triggers the classic path; fallback requires a plan-upgrade / not-available message.
- **Non-idempotent POSTs are not retried.** Transient-failure retry/backoff applies to idempotent requests only; `POST` (create) requests are not replayed.
- **Fails fast when no GitHub token is configured.** `apply`, `audit`, and live `init` now error locally if neither `--token` nor `GITHUB_TOKEN` is set. `init --dry-run` still works without a token.
- **Unused harden branches are deleted when PR mode writes nothing.** A `--pr` run with no file changes no longer leaves an empty branch behind.
- **PR branch names are query-escaped in Contents ref lookups.** Branch names with special characters no longer 404 during ref lookups.
- **`init --dry-run` plans without inspecting the missing repo.** Previewing a new repository no longer 404s; no `GET` is issued for a repo that does not exist yet.
- **Required checks are deferred in PR mode so harden PRs can merge.** `--pr` and the 409 fallback no longer create `protect-main` with checks that gate the PR they are part of; required status checks stay on for direct apply.
- **The run fails when `protect-main` cannot be applied.** Core branch-protection create is marked required so a ruleset or classic failure is not silently reported as skipped.
- **Existence helpers treat only `404` as missing.** `RulesetExists` / `ClassicProtectionExists` no longer map every non-200 to "does not exist", so a `403` or `500` fails the run instead of skipping creation.
- **Valid repo and branch names are not rewritten in templates.** Sanitization no longer mangles legitimate identifiers.

### Documentation

- **README and contributor workflow aligned with tip behavior.** The feature list, shipped CI required checks, live settings under `--pr`, and the contributor workflow are documented to match current behavior.

## [0.1.6] - 2026-08-24

**Patch release.** Fixes the private-repo CI workflow template, pre-flight file-status error handling, wizard prompt input handling, and a vacuous coverage-gate check; OWNER/REPO arguments are now validated strictly, and Dependabot groups action bumps into one PR.

### Fixed

- **Private-repo CI template was invalid YAML.** A lost indentation in `private_ci.yml` made GitHub Actions reject the whole workflow, so privately hardened repos shipped with no working CI; embedded templates are now parsed at test time so this class cannot ship silently.
- **Pre-flight file-status errors no longer fake "create".** Transient failures or a token without Contents read aborted planning with an actionable error instead of misreporting dry-runs and failing later with confusing PUTs.
- **Piped stdin no longer loses answers after the first prompt.** Per-prompt scanners buffered ahead, so scripted runs silently took defaults — declined items got applied; prompts now consume exactly one line each.
- **Solo/team typos can't enable team-only requirements.** Only exact `team`/`t` selects team; anything else takes the advertised solo default so a solo maintainer isn't deadlocked by CODEOWNERS review. `ConfirmDefaults` also accepts `yes`.
- **OWNER/REPO arguments validated strictly.** `/`, `owner/`, and `owner/repo/extra` fail fast with the expected shape instead of surfacing as confusing API 404s.
- **Codecov drift gate sees trailing comments.** `id-token: write  # …` previously registered as absent on both sides, making parity vacuous; the gate now requires both files to parse as actively enabled.

### Documentation

- **AGENTS.md refreshed.** Documents the new gates, one-line wizard prompts, strict OWNER/REPO parsing, and the single test-only dependency.

### Other

- `github/codeql-action` bumped to 4.37.7 across init/analyze/autobuild (live workflow + shipped template); Dependabot now groups github-actions bumps into one PR.

## [0.1.5] - 2026-08-15

**Documentation release.** Documents the `--advanced-security` opt-in for GitHub Advanced Security on private/internal repos across the repo README, npm README, and AGENTS.md.

### Documentation

- **`--advanced-security` documented.** The repo README flags table and npm README commands table now list the flag; both security bullets explain that secret scanning + push protection are opt-in (paid GHAS) on private/internal repos while public repos always get them. AGENTS.md gains a key-behavior entry for the gating.

## [0.1.4] - 2026-08-15

**Patch release.** Applies the repo's real default branch everywhere, routes `init` correctly for organizations, gates paid GitHub Advanced Security on private repos behind an explicit opt-in, and fixes a batch of apply/npm-shim edge cases.

### Fixed

- **Org logins in CODEOWNERS.** Organization logins cannot own files — render `* @OrgName` as a comment and drop the CODEOWNERS-review requirement instead of failing.
- **Default branch respected.** The repo's `default_branch` is read from the API instead of forcing `main`.
- **`init` under organizations.** Repo creation routes to `/user/repos` or `/orgs/{org}/repos` and always passes `auto_init`.
- **Paid GHAS gated on private repos.** Private/internal repos get Dependabot only unless `--advanced-security` or an explicit yes; internal visibility uses private templates.
- **Aliased community file updates.** Canonical paths now use file status so content drift can update; only non-canonical aliases skip.
- **`--pr` and 409 fallback in interactive mode.** Declined items are marked skip so PR batching and the 409 auto-fallback apply there too.
- **Required errors fail the run; no empty PRs.** Required item failures fail `apply`, and `ApplyViaPR` no longer opens an empty PR.
- **npm shim Windows ARM64 guard.** Install no longer requests a Windows ARM64 zip that GoReleaser does not publish.

### Documentation

- **CLAUDE.md trimmed to a pointer.** Root CLAUDE.md now points at AGENTS.md, with the notes absorbed.

### Other

- macOS build/test legs now run on PRs; `actions/checkout` bumped 7.0.0 → 7.0.1.

## [0.1.3] - 2026-08-10

**Template and docs release.** Codacy analysis and coverage ship as embedded templates, the standalone `Template drift` CI job is gone (the drift gate now runs inside the `Test` job), and this repo's own configs and instruction files are fully in line with the shipped behavior.

### Added

- **Codacy embedded templates.** `.codacy.yml` and the `codacy-coverage.yml` `workflow_run` coverage forwarder are now shipped templates, so `apply` gives every hardened repo the same webhook-driven Codacy analysis and coverage uploads fundamentum itself uses.

### Changed

- **Template drift gate folded into the `Test` job.** The standalone `Template drift` CI job was removed — `TestCodecovTemplateDrift` still gates every PR, but now inside `go test ./...` on each `Test` leg (plus pre-commit), instead of as a separate job.
- **`protect-main` ruleset updated.** The stale `Template drift` required check (which could no longer report after the job removal, deadlocking merges) was dropped; all other required checks unchanged.
- **YML configs normalized to LF.** `.github/codeql/codeql-config.yml`, `.github/dependabot.yml`, `codecov.yml`, and `socket.yml` were committed with CRLF blobs; normalized to LF so Windows checkouts no longer show a permanent dirty tree.

### Documentation

- **AGENTS.md / CLAUDE.md reality sync.** Corrected the required-check list, drift-gate location, Codacy template coverage, and `.gitattributes` scope; documented the Windows CRLF embed trap (externally CRLF-rewritten template files embed as CRLF and cause false "would update" in dry-runs) and the hardcoded `ActionCreate` dry-run labels for the settings items.

### Other

- Test-harness, hook-script, and test-client refactors (`lib.sh` GOTMPDIR setup, consolidated test server/client boilerplate).

## [0.1.2] - 2026-08-09

**Security fix release.** The npm shim now validates the packaged version before building the binary download URL, closing a CodeQL-reported file-to-HTTP data flow in `install.js`.

### Fixed

- **Shim download URL hardening.** `npm/install.js` validates the package version against a strict semver pattern before it reaches the release download URL, so malformed or tampered versions fail the install instead of flowing into an outbound request (CodeQL `js/file-access-to-http`).

## [0.1.1] - 2026-08-08

**Documentation release.** Tidies both READMEs — the npm registry drops the redundant Codacy coverage badge (Codecov already reports coverage), and the npm README gets a tighter badge row and platform-specific support notes.

### Documentation

- **README cleanup.** Removed the Codacy coverage badge from the repo README (kept Codacy grade; Codecov owns the coverage), dropped the self-referential npm version/downloads badges from the npm README, removed the stale planning-directory note from the repo README, and corrected the npm package platform claim (macOS/Linux on x64+arm64, Windows on x64).

## [0.1.0] - 2026-08-08

**First release.** One-shot GitHub repo hardening CLI: bootstrap an existing repo (or create a new one) with community health files, branch protection, security features, and starter workflows — idempotent, dry-run first, free forever.

### Added

- **`apply OWNER/REPO` command.** Upserts community health files (`CONTRIBUTING.md`, `CODEOWNERS`, `CODE_OF_CONDUCT.md`, `SECURITY.md`, issue/PR templates, `dependabot.yml`, `.codacy.yml`), sets branch protection via modern rulesets (with classic fallback on 403), enables secret scanning + push protection, Dependabot alerts/updates, and CodeQL where visibility allows.
- **`init OWNER/REPO` command.** Creates a new repo via the GitHub API, then runs the full `apply` hardening in one shot. `--private` flag (default `true`) controls visibility.
- **`--dry-run` flag.** Non-interactive plan summary of every intended change, with no API mutations.
- **`--pr` mode.** Batches file changes into a single pull request instead of direct commits; auto-falls back to a PR when branch protection rejects a direct commit with 409.
- **`--no-overwrite` flag.** Adds only missing files, never touches existing ones.
- **Solo/team prompt.** Asks solo (default) or team on first apply; solo mode disables CODEOWNERS-review and stale-review-dismissal requirements so a single maintainer isn't deadlocked.
- **File aliasing.** Path variants (e.g. root `CODEOWNERS` vs `.github/CODEOWNERS`) are detected before deciding create/skip/update.
- **Retry with backoff.** Transient GitHub API failures are retried with exponential backoff using `crypto/rand` jitter.
- **Embedded templates.** All community files and workflows render from `//go:embed` templates with plain string substitution; visibility-aware CI, coverage, and CodeQL starters are shipped.
- **Workflow 404 handling.** GitHub Actions workflow-file updates via the Contents API (HTTP 404 lock) are detected and reported as `skipped` rather than failing the run.
- **`npm` shim.** `fundamentum-cli` package published via OIDC trusted publishing; postinstall downloads the matching platform binary with SHA-256 verification (fail-closed when a published checksum is missing).
- **Quality gate pipeline.** Template-drift gate (Codecov), lint (incl. Windows), gosec, Trivy, CodeQL, security/review checks, GoReleaser snapshot build, and Codacy coverage uploads.

### Security

- **SSRF and path hardening.** GitHub API paths validated to prevent server-side request forgery and template injection; defense-in-depth HTML tag stripping on template output (dangerous tags only, Markdown angle brackets preserved).
- **SHA-pinned actions.** Every third-party GitHub Action pinned to a commit SHA.

### Fixed

- **StatusCode error handling.** Structural HTTP error handling in `internal/github` so non-2xx responses surface consistently.
- **CodeQL default-setup skip.** Advanced `codeql.yml` no longer collides with GitHub default setup.
- **Octopus alias detection.** `octopus-review.yml` counted as existing for the `octopus.yml` alias.
- **Codacy drift.** Trivy noise removal and Codecov template aligned with the live workflow; phantom `codecov/project` required check dropped.

### Documentation

- **README badge suite.** CI, release, Go version, npm version/downloads, Codecov, and Codacy coverage/grade badges (Go Report Card excluded — service retired).

[Unreleased]: https://github.com/jpvelasco/fundamentum/compare/v0.5.1...HEAD
[0.5.1]: https://github.com/jpvelasco/fundamentum/releases/tag/v0.5.1
[0.5.0]: https://github.com/jpvelasco/fundamentum/releases/tag/v0.5.0
[0.4.3]: https://github.com/jpvelasco/fundamentum/releases/tag/v0.4.3
[0.4.2]: https://github.com/jpvelasco/fundamentum/releases/tag/v0.4.2
[0.4.1]: https://github.com/jpvelasco/fundamentum/releases/tag/v0.4.1
[0.4.0]: https://github.com/jpvelasco/fundamentum/releases/tag/v0.4.0
[0.3.3]: https://github.com/jpvelasco/fundamentum/releases/tag/v0.3.3
[0.3.2]: https://github.com/jpvelasco/fundamentum/releases/tag/v0.3.2
[0.3.1]: https://github.com/jpvelasco/fundamentum/releases/tag/v0.3.1
[0.3.0]: https://github.com/jpvelasco/fundamentum/releases/tag/v0.3.0
[0.2.3]: https://github.com/jpvelasco/fundamentum/releases/tag/v0.2.3
[0.2.2]: https://github.com/jpvelasco/fundamentum/releases/tag/v0.2.2
[0.2.1]: https://github.com/jpvelasco/fundamentum/releases/tag/v0.2.1
[0.2.0]: https://github.com/jpvelasco/fundamentum/releases/tag/v0.2.0
[0.1.6]: https://github.com/jpvelasco/fundamentum/releases/tag/v0.1.6
[0.1.5]: https://github.com/jpvelasco/fundamentum/releases/tag/v0.1.5
[0.1.4]: https://github.com/jpvelasco/fundamentum/releases/tag/v0.1.4
[0.1.3]: https://github.com/jpvelasco/fundamentum/releases/tag/v0.1.3
[0.1.2]: https://github.com/jpvelasco/fundamentum/releases/tag/v0.1.2
[0.1.1]: https://github.com/jpvelasco/fundamentum/releases/tag/v0.1.1
[0.1.0]: https://github.com/jpvelasco/fundamentum/releases/tag/v0.1.0
