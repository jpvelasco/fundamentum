# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/jpvelasco/fundamentum/compare/v0.1.5...HEAD
[0.1.5]: https://github.com/jpvelasco/fundamentum/releases/tag/v0.1.5
[0.1.4]: https://github.com/jpvelasco/fundamentum/releases/tag/v0.1.4
[0.1.3]: https://github.com/jpvelasco/fundamentum/releases/tag/v0.1.3
[0.1.2]: https://github.com/jpvelasco/fundamentum/releases/tag/v0.1.2
[0.1.1]: https://github.com/jpvelasco/fundamentum/releases/tag/v0.1.1
[0.1.0]: https://github.com/jpvelasco/fundamentum/releases/tag/v0.1.0