# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/jpvelasco/fundamentum/compare/v0.1.2...HEAD
[0.1.2]: https://github.com/jpvelasco/fundamentum/releases/tag/v0.1.2
[0.1.1]: https://github.com/jpvelasco/fundamentum/releases/tag/v0.1.1
[0.1.0]: https://github.com/jpvelasco/fundamentum/releases/tag/v0.1.0