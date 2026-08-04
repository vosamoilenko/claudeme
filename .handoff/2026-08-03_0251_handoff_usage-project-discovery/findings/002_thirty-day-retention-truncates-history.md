# The report only covers 30 days, and never says so

`claudeme projects --cost` prints a grand total that reads as all-time spend.
It isn't. Claude Code deletes transcripts on a rolling window, so the tool can
only ever report what survived it.

Measured 2026-08-03 against `~/.config/claudeme/shared/projects`:

```
oldest transcript        Jul  4 12:51   (exactly 30 days back)
newest transcript        Aug  3 03:03
files older than 30d     0
files within 30d         1254
```

Zero files past the boundary is not attrition, it is a cutoff. Neither
`~/.claude/settings.json` nor `~/.config/claudeme/shared/settings.json` sets
`cleanupPeriodDays`, so Claude Code's default of 30 applies.

## What that costs, in projects

`.claude.json` keeps a `projects` key per directory ever opened, and survives
the cleanup because it is not a transcript. Comparing it against the cwds that
still have transcripts:

```
directories ever opened (union of 3 accounts)   160
distinct cwds with transcripts                   72
opened but ZERO transcripts                     122
```

(72 + 122 exceeds 160 because some cwds with transcripts — subagent worktrees
under `.claude/worktrees/` — were never top-level project directories.)

The 122 are real work, not scratch dirs: `kbv-printservice` and its five
variants, `sclable.com`, `oebb/advisory-system/backend`, `rating-bot`,
`mattermost-cli`, `notion-cli`, `openclaw`, `bvbs-viewer`, `abs-viewer`, and
~110 more. Their spend is unrecoverable — `.claude.json` stores paths and
history, never token counts.

Reproduce:

```sh
python3 - <<'PY'
import json,os,glob
root=os.path.expanduser('~/.config/claudeme/shared/projects')
seen=set()
for dp,_,fns in os.walk(root):
    for fn in fns:
        if not fn.endswith('.jsonl'): continue
        for l in open(os.path.join(dp,fn),errors='ignore'):
            try: e=json.loads(l)
            except: continue
            if e.get('cwd'): seen.add(e['cwd']); break
keys=set()
for f in glob.glob(os.path.expanduser('~/.config/claudeme/accounts/*/.claude.json')):
    keys|=set(json.load(open(f)).get('projects',{}).keys())
print(len(seen),"cwds with transcripts;",len(keys),"opened;",
      len([k for k in keys if k not in seen]),"with none")
PY
```

## Where the accounts theory went wrong

`findings/001_accounts-share-one-projects-root.md` answered the chain's opening
question — *are projects missing because they live under another account?* —
and answered it correctly: no. Re-verified independently here. All three
accounts symlink `projects` to `~/.config/claudeme/shared/projects`,
`~/.claude/projects` is empty, and `find` turns up no `.jsonl` outside the
shared root. `~/.claude/backups/` and `shared/backups/` hold only
`.claude.json` copies.

The error was treating "not the accounts" as "nothing is missing". Projects
were missing the whole time; the accounts were simply never the reason.

## Why this matters beyond a caveat

Two different truths get conflated by the same number:

- **Within the window the totals are exact.** 1249/1249 transcripts read, and
  an independent recompute agrees to the cent (see
  `002_no-money-moved-in-the-per-cwd-refactor.md`).
- **The window itself is invisible.** Nothing in the output distinguishes "this
  project cost $446" from "this project cost $446 *in the last 30 days*", and
  a project that ran for six months reads identically to one that ran for six
  days.

Hence the two todos: raise the retention so future history survives
(`002_001`), and print the window so the number stops overclaiming
(`002_002`). Only the second is a fix — the first is damage limitation, and
neither recovers what is already gone.
