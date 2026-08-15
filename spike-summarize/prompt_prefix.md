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

Output ONLY a single raw JSON object that validates against this JSON Schema. No markdown
fences, no prose before or after, no explanation. Just the JSON object.

=== JSON SCHEMA ===
