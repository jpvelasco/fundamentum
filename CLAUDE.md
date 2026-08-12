# CLAUDE.md

This file is a thin pointer — the canonical repo guidance lives in [AGENTS.md](AGENTS.md): commands, hooks, architecture, merge gates, conventions, and Codacy notes. Read it at the start of every session.

Quick orientation: fundamentum is a focused, free, open-source CLI (MIT License) for one-shot GitHub repo hardening — community files, branch protection, and security features via an interactive wizard. Go CLI (Cobra): `apply OWNER/REPO` hardens an existing repo, `init OWNER/REPO` creates one first. Enable hooks with `git config core.hooksPath .hooks`; the pre-commit gate is template drift → build → lint → test. PRs squash-merge off feature branches with required CI green (pr-auto skill).