# Contributing to fundamentum

Thanks for your interest in contributing!

## Getting Started

1. Fork the repository
2. Clone your fork and create a branch:
   ```bash
   git clone git@github.com:<your-username>/fundamentum.git
   cd fundamentum
   git checkout -b feat/your-feature
   ```

## Pull Request Process

1. Create a branch from `main` with a descriptive name (`feat/`, `fix/`, `docs/`)
2. Keep PRs focused on a single concern
3. Use Conventional Commits (`feat:`, `fix:`, `docs:`, and so on), and explain **what** changed and **why** in the PR
4. Update both [README.md](../README.md) and [npm/README.md](../npm/README.md) when user-facing commands or behavior change; record unreleased changes in [CHANGELOG.md](../CHANGELOG.md)
5. Keep embedded templates and live counterparts in sync where applicable
6. All required CI checks must pass and actionable review threads must be addressed before squash merge

## Local Validation

Read [AGENTS.md](../AGENTS.md) and enable the repository hooks:

```bash
git config core.hooksPath .hooks
```

For source, template, workflow, or configuration changes, run these in order:

```bash
go test ./internal/templatefs/ -run TestCodecovTemplateDrift -count=1
go build ./...
golangci-lint run ./...
go test ./...
```

Use `go vet ./...` if `golangci-lint` is unavailable or blocked. After template changes, also run `go test ./internal/templatefs/ -run TestEmbeddedYAMLParses -count=1`. After PII helper changes, run `bash scripts/verify-pii-helper.sh` for real Git scenarios and the cold-run timing gate. Changes to the npm installer require `npm test --prefix npm`.

Documentation-only changes require `git diff --check`. Commit hooks still run the local Go gates. This repository uses Codecov patch coverage with a 90% target as a soft gate; Codecov is deliberately not a required status context.

## Code of Conduct

By participating, you agree to abide by the [Code of Conduct](CODE_OF_CONDUCT.md).
