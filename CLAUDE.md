# CLAUDE.md

This file is a thin pointer — the canonical repo guidance lives in [AGENTS.md](AGENTS.md): commands, hooks, architecture, merge gates, conventions, and Codacy notes. Read it at the start of every session.

Quick orientation: fundamentum is a free, open-source Go CLI (MIT) for GitHub repo hardening. Its seven subcommands are `apply`, `init`, `audit`, `export`, `pii-bootstrap`, `mcp`, and `revert`. Apply manages community files, protection, security, and priority labels; audit can also inspect named GitHub Apps. Enable hooks with `git config core.hooksPath .hooks`; the pre-commit gate is template drift → build → lint → test. PRs squash-merge off feature branches with required CI green (pr-auto skill).
