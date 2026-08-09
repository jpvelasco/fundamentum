#!/usr/bin/env bash
# Shared helpers for .hooks/* scripts. Source this file from each hook.

# setup_go_tmpdir: on Windows, redirect Go's temp dir away from %TEMP%
# so AppLocker doesn't block test executables.
setup_go_tmpdir() {
  if [[ "$OSTYPE" == "msys" || "$OSTYPE" == "win32" || "$OSTYPE" == "cygwin" ]]; then
    GOTMPDIR="${GOTMPDIR:-$HOME/.cache/go-test-tmp}"
    mkdir -p "$GOTMPDIR"
    export GOTMPDIR
  fi
}