# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/jpvelasco/fundamentum/compare/v0.2.2...HEAD
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