#!/usr/bin/env python3
"""Transcript .jsonl -> metrics + compact digest.

Stage 1 of the spike. Two outputs, both deterministic:
  - metrics: counted, not judged. Trustworthy enough to aggregate.
  - digest:  trimmed dialogue for a model to summarize.
"""
import json
import re
import sys
from collections import Counter

TOOL_KEY = {
    "Read": "file_path", "Edit": "file_path", "Write": "file_path",
    "NotebookEdit": "notebook_path", "Bash": "command", "Grep": "pattern",
    "Glob": "pattern", "WebFetch": "url", "WebSearch": "query",
    "Task": "description", "Agent": "description", "Skill": "skill",
    "SendUserFile": "files", "TaskCreate": "description",
}
WRITE_TOOLS = {"Edit", "Write", "NotebookEdit"}
READ_TOOLS = {"Read", "NotebookRead"}
NOISE = re.compile(r"^\s*(ls|cat|pwd|echo|head|tail|wc|find|grep|which|cd)\b")
CLIP = 300


def clip(s, n=CLIP):
    return (lambda x: x if len(x) <= n else x[:n] + "…")(re.sub(r"\s+", " ", str(s)).strip())


def blocks(rec):
    c = rec.get("message", {}).get("content")
    if isinstance(c, list):
        return c
    return [{"type": "text", "text": c}] if isinstance(c, str) else []


def distill(path):
    meta = {"session_id": None, "title": None, "cwd": None, "branches": set(),
            "started": None, "ended": None, "version": None}
    m = Counter()            # scalar metrics
    tokens = Counter()
    tools = Counter()
    tool_errors = Counter()
    models = Counter()
    skills = set()
    agent_types = Counter()
    files_read, files_written = Counter(), Counter()
    agent_usage, agent_ms = Counter(), Counter()
    commands, git_cmds, errors, turns = [], [], [], []
    turn_durations = []

    for line in open(path, encoding="utf-8", errors="replace"):
        line = line.strip()
        if not line:
            continue
        try:
            rec = json.loads(line)
        except json.JSONDecodeError:
            m["malformed_lines"] += 1
            continue

        t = rec.get("type")
        meta["session_id"] = meta["session_id"] or rec.get("sessionId")
        if t == "ai-title":
            meta["title"] = rec.get("aiTitle")
        if rec.get("cwd"):
            meta["cwd"] = rec["cwd"]
        if rec.get("gitBranch"):
            meta["branches"].add(rec["gitBranch"])
        if rec.get("version"):
            meta["version"] = rec["version"]
        if (ts := rec.get("timestamp")):
            meta["started"] = min(meta["started"] or ts, ts)
            meta["ended"] = max(meta["ended"] or ts, ts)

        side = bool(rec.get("isSidechain"))

        if t == "system":
            sub = rec.get("subtype")
            if sub == "turn_duration":
                turn_durations.append(rec.get("durationMs", 0))
            m[f"system_{sub}"] += 1

        elif t == "queue-operation":
            # A task-notification re-fires for the same task-id (up to ~16x here)
            # and its usage is cumulative, so keep the max per id, never the sum.
            if rec.get("operation") == "enqueue":
                c = rec.get("content", "")
                tid = (re.search(r"<task-id>([^<]+)", c) or [None, None])[1]
                if tid:
                    if (tok := re.search(r"<subagent_tokens>(\d+)", c)):
                        agent_usage[tid] = max(agent_usage[tid], int(tok.group(1)))
                    if (d := re.search(r"<duration_ms>(\d+)", c)):
                        agent_ms[tid] = max(agent_ms[tid], int(d.group(1)))

        elif t == "user" and not rec.get("isMeta"):
            # Only a typed prompt is the human speaking. Everything else arriving
            # as a "user" record is machinery: tool results, task notifications.
            human = rec.get("promptSource") == "typed" or \
                (rec.get("origin") or {}).get("kind") == "human"
            for b in blocks(rec):
                if b.get("type") == "tool_result":
                    if b.get("is_error"):
                        m["tool_errors"] += 1
                        errors.append(clip(b.get("content"), 200))
                elif b.get("type") == "text" and (txt := b.get("text", "")).strip():
                    if "Request interrupted" in txt:
                        m["interruptions"] += 1
                    elif human:
                        m["sub_prompts" if side else "human_prompts"] += 1
                        m["human_prompt_chars"] += 0 if side else len(txt)
                        turns.append(("user/sub" if side else "user", clip(txt, 800)))
                    else:
                        m["notifications"] += 1
                        turns.append(("notify", clip(txt, 200)))

        elif t == "assistant":
            m["assistant_msgs"] += 1
            if (mod := rec.get("message", {}).get("model")):
                models[mod] += 1
            u = rec.get("message", {}).get("usage") or {}
            tokens["output"] += u.get("output_tokens", 0)
            tokens["input"] += u.get("input_tokens", 0)
            tokens["cache_read"] += u.get("cache_read_input_tokens", 0)
            tokens["cache_write"] += u.get("cache_creation_input_tokens", 0)
            for b in blocks(rec):
                bt = b.get("type")
                if bt == "thinking":
                    m["thinking_blocks"] += 1
                elif bt == "text" and b.get("text", "").strip():
                    turns.append(("assistant/sub" if side else "assistant",
                                  clip(b["text"], 500)))
                elif bt == "tool_use":
                    name, inp = b.get("name", "?"), (b.get("input") or {})
                    tools[name] += 1
                    arg = inp.get(TOOL_KEY.get(name, ""), "")
                    if name in WRITE_TOOLS and arg:
                        files_written[arg] += 1
                    elif name in READ_TOOLS and arg:
                        files_read[arg] += 1
                    elif name == "Bash" and arg:
                        cmd = clip(arg, 200)
                        commands.append(cmd)
                        if re.match(r"^\s*(git|gh|glab)\b", arg):
                            git_cmds.append(cmd)
                    elif name == "Skill":
                        skills.add(str(inp.get("skill", "")))
                    elif name in ("Agent", "Task"):
                        agent_types[inp.get("subagent_type", "default")] += 1
                    turns.append(("tool", f"{name}({clip(arg, 160)})"))

    wall = 0
    if meta["started"] and meta["ended"]:
        from datetime import datetime
        p = lambda s: datetime.fromisoformat(s.replace("Z", "+00:00"))
        wall = int((p(meta["ended"]) - p(meta["started"])).total_seconds() * 1000)

    metrics = {
        "wall_ms": wall,
        # turns overlap with background agents, so this can exceed wall_ms
        "turn_ms_sum": sum(turn_durations),
        "turns": len(turn_durations),
        "slowest_turn_ms": max(turn_durations, default=0),
        "human_prompts": m["human_prompts"],
        "human_prompt_chars": m["human_prompt_chars"],
        "notifications": m["notifications"],
        "assistant_msgs": m["assistant_msgs"],
        "thinking_blocks": m["thinking_blocks"],
        "interruptions": m["interruptions"],
        "tool_calls": sum(tools.values()),
        "tool_errors": m["tool_errors"],
        "error_rate": round(m["tool_errors"] / max(sum(tools.values()), 1), 4),
        "subagents": sum(agent_types.values()),
        "subagent_types": dict(agent_types),
        "subagent_output_tokens": sum(agent_usage.values()),
        "subagent_wall_ms": sum(agent_ms.values()),
        "tokens": dict(tokens) | {"subagent_output": sum(agent_usage.values())},
        "cache_hit_rate": round(
            tokens["cache_read"] / max(tokens["cache_read"] + tokens["cache_write"], 1), 4),
        "models": dict(models),
        "tool_counts": dict(tools.most_common()),
        "files_read": len(files_read),
        "files_written": len(files_written),
        "bash_calls": len(commands),
        "git_calls": len(git_cmds),
        "malformed_lines": m["malformed_lines"],
    }

    return {
        **{k: (sorted(v) if isinstance(v, set) else v) for k, v in meta.items()},
        "metrics": metrics,
        "skills_used": sorted(x for x in skills if x),
        "files_written_list": [f for f, _ in files_written.most_common(60)],
        "files_read_list": [f for f, _ in files_read.most_common(40)],
        "git_commands": git_cmds[:40],
        "commands": [c for c in commands if not NOISE.match(c)][:120],
        "errors": errors[:25],
        "dialogue": turns,
    }


def to_text(d):
    L = [f"# session {d['session_id']}"]
    for k in ("title", "cwd", "branches", "started", "ended", "version", "skills_used"):
        L.append(f"{k}: {d[k]}")
    L.append(f"metrics: {json.dumps(d['metrics'])}")
    for label, key in (("files written", "files_written_list"),
                       ("files read", "files_read_list"),
                       ("git commands", "git_commands"),
                       ("shell commands (noise filtered)", "commands"),
                       ("tool errors", "errors")):
        if d[key]:
            L.append(f"\n## {label}")
            L += [f"- {x}" for x in d[key]]
    L.append("\n## transcript")
    L += [f"[{r}] {t}" for r, t in d["dialogue"]]
    return "\n".join(L)


if __name__ == "__main__":
    d = distill(sys.argv[1])
    if "--metrics" in sys.argv:
        json.dump({k: d[k] for k in ("session_id", "title", "cwd", "branches",
                                     "started", "ended", "version", "skills_used",
                                     "metrics")}, sys.stdout, indent=2)
    elif "--json" in sys.argv:
        json.dump(d, sys.stdout, indent=2)
    else:
        sys.stdout.write(to_text(d))
