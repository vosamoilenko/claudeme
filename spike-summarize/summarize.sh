#!/usr/bin/env bash
# Spike: transcript.jsonl -> structured summary JSON.
#   ./summarize.sh <transcript.jsonl> [out.json]
#
# Model is gpt-5.6-luna via `codex exec`, no fallback. Any failure fires a macOS
# notification and exits non-zero; a usage-cap rejection is named as such so the
# retry is an informed one.
# See docs/adr/2026-08-13_default-summarizer-model-gpt-5-6-luna.md
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
SRC="$1"
OUT="${2:-${SRC##*/}.summary.json}"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

MODEL="${SUMMARIZE_MODEL:-gpt-5.6-luna}"
CODEX_BIN="${CODEX_BIN:-codex}"
NOTIFIER_BIN="${NOTIFIER_BIN:-terminal-notifier}"
# SUMMARIZE_NOTIFY=0 keeps the stderr line but drops the desktop notification.
# A bulk backfill sets it: one popup per transient failure is noise there, and
# the caller reports the tally once at the end.
SUMMARIZE_NOTIFY="${SUMMARIZE_NOTIFY:-1}"

# --- helpers ---------------------------------------------------------------

# notify_failure <reason> — user-facing macOS notification + stderr line.
notify_failure() {
  local reason="$1"
  local msg="summarize.sh: $reason"
  echo "$msg" >&2
  [ "$SUMMARIZE_NOTIFY" = "1" ] || return 0
  # osascript string literals: drop quotes/backslashes rather than escape them.
  local safe="${msg//\\/}"
  safe="${safe//\"/}"
  if command -v "$NOTIFIER_BIN" >/dev/null 2>&1; then
    "$NOTIFIER_BIN" -title "summarize.sh failed" -message "$safe" >/dev/null 2>&1 || true
  elif command -v osascript >/dev/null 2>&1; then
    osascript -e "display notification \"$safe\" with title \"summarize.sh failed\"" \
      >/dev/null 2>&1 || true
  fi
}

# is_cap_failure <logfile> — true when the log shows a capacity/usage-cap
# rejection. Two distinct shapes: the account-level "usage limit" family, and
# the server-side "Selected model is at capacity. Please try a different
# model." seen in the 2026-08-14 archive backfill. Matched with spaces so
# codex's own `rate_limits` telemetry field never counts as a cap.
is_cap_failure() {
  local log="$1"
  [ -f "$log" ] || return 1
  if grep -qiE "you'?ve hit your usage limit|you'?ve reached your usage limit|usage limit reached|reached your usage limit|out of credits|too many requests|rate limit exceeded|rate limit reached|quota exceeded|is at capacity|try a different model" "$log"; then
    return 0
  fi
  if grep -i "error" "$log" | grep -qE '(^|[^0-9])429([^0-9]|$)'; then
    return 0
  fi
  return 1
}

# valid_summary <file> — non-empty, parses as JSON, and validates against
# schema.json when jsonschema is importable.
valid_summary() {
  [ -s "$1" ] || return 1
  python3 - "$1" "$HERE/schema.json" <<'PYEOF'
import json, sys
data = json.load(open(sys.argv[1]))
try:
    from jsonschema import validate
except ImportError:
    sys.exit(0)
validate(instance=data, schema=json.load(open(sys.argv[2])))
PYEOF
}

# run_model — 0 ok, 10 cap rejection, 1 other failure.
run_model() {
  rm -f "$OUT"
  local rc=0
  "$CODEX_BIN" exec \
    -m "$MODEL" \
    --output-schema "$HERE/schema.json" \
    --skip-git-repo-check \
    --sandbox read-only \
    --json \
    --output-last-message "$OUT" \
    - < "$WORK/prompt.md" > "$WORK/codex.log" 2>&1 || rc=$?
  if [ "$rc" -eq 0 ] && valid_summary "$OUT"; then
    return 0
  fi
  if [ "$rc" -ne 0 ] && is_cap_failure "$WORK/codex.log"; then
    return 10
  fi
  return 1
}

# --- main ------------------------------------------------------------------

python3 "$HERE/distill.py" "$SRC" > "$WORK/digest.txt"

cat > "$WORK/prompt.md" <<'EOF'
You are given a distilled Claude Code session log: deterministic facts, then a
trimmed transcript where [user]/[assistant] are the main thread, [user/sub] and
[assistant/sub] are subagents, and [tool] lines are tool calls.

Summarize it so a future reader never needs the raw transcript. Rules:

- Facts only from the log. Never guess a file path, command, or result.
- Prefer the user's words for intent; prefer tool evidence for what happened.
- If work was abandoned or failed, say so plainly — that is the useful part.
- Skip exploration noise (ls/cat/grep). Keep commands worth re-running.
- No praise, no "successfully", no restating the schema field names.

- The metrics line is already counted; do not restate numbers, use them only as context.
- learnings/dead_ends/environment_facts must be reusable outside this session; drop anything that is not.

=== SESSION LOG ===
EOF
cat "$WORK/digest.txt" >> "$WORK/prompt.md"

rc=0
run_model || rc=$?

case "$rc" in
  0)
    echo "$OUT"
    ;;
  10)
    tail -40 "$WORK/codex.log" >&2 || true
    notify_failure "$MODEL hit its usage cap — no summary for ${SRC##*/}"
    exit 1
    ;;
  *)
    tail -40 "$WORK/codex.log" >&2 || true
    notify_failure "$MODEL failed (not a usage cap) — no summary for ${SRC##*/}"
    exit 1
    ;;
esac
