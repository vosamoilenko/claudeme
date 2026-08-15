#!/usr/bin/env bash
# Run a pi model over a prompt file, extract final assistant text into OUT.
# Usage: ./run_pi.sh <model> <prompt.md> <out.raw.json> <log.jsonl>
set -uo pipefail
MODEL="$1"; PROMPT="$2"; OUT="$3"; LOG="$4"
PI=/Users/vo1/.nvm/versions/node/v22.22.3/bin/pi
"$PI" -p --mode json --model "$MODEL" --no-session "$(cat "$PROMPT")" > "$LOG" 2>&1
python3 - "$LOG" "$OUT" <<'PYEOF'
import json, sys
log_path, out_path = sys.argv[1], sys.argv[2]
last_text = None
with open(log_path) as f:
    for line in f:
        line = line.strip()
        if not line:
            continue
        try:
            ev = json.loads(line)
        except json.JSONDecodeError:
            continue
        if ev.get("type") == "turn_end":
            msg = ev.get("message", {})
            for c in msg.get("content", []):
                if c.get("type") == "text":
                    last_text = c.get("text")
if last_text is None:
    print("NO_TEXT_FOUND", file=sys.stderr)
    sys.exit(1)
text = last_text.strip()
if text.startswith("```"):
    lines = text.split("\n")
    if lines[0].startswith("```"):
        lines = lines[1:]
    if lines and lines[-1].strip().startswith("```"):
        lines = lines[:-1]
    text = "\n".join(lines).strip()
with open(out_path, "w") as f:
    f.write(text)
PYEOF
