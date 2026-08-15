#!/usr/bin/env bash
# Run a codex model over a prompt file with timeout, write raw.json + log.
# Usage: ./run_codex.sh <model> <prompt.md> <out.raw.json> <log>
set -uo pipefail
MODEL="$1"; PROMPT="$2"; OUT="$3"; LOG="$4"
rm -f "$OUT"
codex exec -m "$MODEL" --skip-git-repo-check --sandbox read-only \
  --output-schema schema.json --json --output-last-message "$OUT" \
  - < "$PROMPT" > "$LOG" 2>&1
echo "exit=$?" >> "$LOG"
