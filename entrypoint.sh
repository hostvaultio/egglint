#!/bin/bash
# GitHub Action entrypoint.
#
# Action inputs arrive as fixed positional arguments, any of which may be empty.
# Empty ones must be dropped rather than passed through as empty strings, which
# the CLI would otherwise read as a path argument.
set -euo pipefail

PATHS="${1:-.}"
FORMAT="${2:-github}"
OUTPUT="${3:-}"
FAIL_ON="${4:-error}"
CONFIG="${5:-}"
DISABLE="${6:-}"
ALL_FILES="${7:-false}"
EXTRA="${8:-}"

args=()

# Written as if-blocks rather than `[ -n "$X" ] && args+=(...)`: under `set -e` a
# false test as the last command of a list exits the script, so the first empty
# input would silently end the run before egglint was ever invoked.
if [ -n "$FORMAT" ]; then
    args+=(--format "$FORMAT")
fi
if [ -n "$OUTPUT" ]; then
    args+=(--output "$OUTPUT")
fi
if [ -n "$FAIL_ON" ]; then
    args+=(--fail-on "$FAIL_ON")
fi
if [ -n "$CONFIG" ]; then
    args+=(--config "$CONFIG")
fi
if [ -n "$DISABLE" ]; then
    args+=(--disable "$DISABLE")
fi

if [ "$ALL_FILES" = "true" ]; then
    args+=(--all-files)
fi

# Extra flags and paths are intentionally word-split: both are lists.
if [ -n "$EXTRA" ]; then
    # shellcheck disable=SC2206
    args+=($EXTRA)
fi

# shellcheck disable=SC2206
args+=($PATHS)

echo "+ egglint ${args[*]}"
exec egglint "${args[@]}"
