# Discovery loses cwds: one dir can hold sessions from many

`scanDir` keeps a single cwd per transcript directory — the first `cwd` line of
the alphabetically-first top-level `.jsonl`. That assumes one directory == one
cwd. It isn't true: **6 of 68 directories hold sessions from more than one cwd,
and 22 distinct cwds are invisible to `projects` entirely.**

Measured 2026-08-03 against `~/.config/claudeme/shared/projects`, by walking
every transcript and taking the first `cwd` of each:

```
transcript dirs: 68   dirs holding >1 distinct cwd: 6
cwds never seen by scanDir: 22
```

The missing ones are real work, not noise:

```
~/Developer/scl-gitlab/tts/phishen-impossible/backend
~/Developer/scl-gitlab/tts/phishen-impossible/frontend
~/Developer/scl-gitlab/berlinhyp/mitarbeiterportal/portal-client
~/Developer/smk-github/vosamoilenko/snitchcam/app
~/Developer/infinite-canvas-anatomy/swift
~/Developer/infinite-canvas-anatomy/swift/Packages/CanvasCore
~/Developer/infinite-canvas-anatomy/docs/anatomy
~/Developer/github.com/vosamoilenko/agentic/skills
~/Developer/github.com/vosamoilenko/agentic/evals/{brainstorming,docs,docs-cleanup}
~/Developer/github.com/vosamoilenko/agentic/evals/{clarify,docs}/runs/<run>
~/Developer/github.com/vosamoilenko/agentic/skills/smk/workflow/terminology
<repo>/.claude/worktrees/agent-*          (8 of these, across 2 repos)
```

Reproduce:

```sh
cd ~/.config/claudeme/shared/projects && python3 - <<'PY'
import json,os,collections
for d in sorted(os.listdir('.')):
    if not os.path.isdir(d): continue
    cwds=set()
    for root,_,fs in os.walk(d):
        for fn in fs:
            if not fn.endswith('.jsonl'): continue
            for l in open(os.path.join(root,fn),errors='ignore'):
                try: e=json.loads(l)
                except: continue
                if e.get('cwd'): cwds.add(e['cwd']); break
    if len(cwds)>1: print(d, len(cwds))
PY
```

## What this does and does not break

- **Costs are unaffected.** `Analyze` globs every `.jsonl` under each directory
  regardless of cwd, so no spend is missing or double-counted. Project totals
  and the grand total are right.
- **Attribution is wrong.** Spend from a hidden cwd lands under whichever cwd
  `scanDir` happened to keep, so a `Member` sub-row can overstate itself and a
  cwd with no directory of its own never appears at all.
- The `Member` sub-rows added this session are therefore *complete with respect
  to transcript directories* and *incomplete with respect to reality*. They are
  not wrong about what they show; they show too few rows.

## Why one directory holds several cwds

Not confirmed, but consistent with the data: the directory name is mangled from
the cwd at session start, and both subagent worktrees
(`<repo>/.claude/worktrees/agent-*`) and sessions resumed after a `cd` write
their own `cwd` into a transcript that already lives under the original name.
The `subagents/` transcripts found earlier in this chain sit in the same tree
and carry their own `cwd` too.
