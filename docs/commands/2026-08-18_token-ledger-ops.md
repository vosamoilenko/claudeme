# Token Ledger Operations

## Freeze at-risk transcripts

Claude Code deletes its own transcripts after ~21 days regardless of
`cleanupPeriodDays`. Everything derived from a transcript — the token ledger, the 1h/5m
cache split — can only ever be extracted while it exists. Gzip-copy every live transcript
that the archive does not already hold, or holds a shorter copy of:

```bash
python3 docs/commands/scratch/2026-08-18_freeze-transcripts.py          # dry run
python3 docs/commands/scratch/2026-08-18_freeze-transcripts.py --apply
```

Idempotent and safe to re-run: it copies, never moves, and compares line counts so a
session that grew since its last freeze is re-frozen. Archived `.jsonl.gz` reads through
`openTranscript` identically to a live file, so nothing downstream changes.

Result 2026-08-18: 1,878 already current, 133 missing, 5 stale → **2,016/2,016 frozen**,
467 MB.

## Backfill the ledgers

```bash
claudeme digest --tokens-only -n   # what it would do
claudeme digest --tokens-only      # do it
```

No model, no network, no usage budget. Measured 2026-08-18: **1,312 sessions in 9 s,
0 failures.** Safe to re-run — sessions that already carry a ledger are skipped, and a
summary already on the record is never disturbed.

## Record the day's prices

`claudeme snapshot` writes `~/.config/claudeme/shared/prices/<date>.json` on every run,
skipping the write when nothing changed so the `written` stamp does not churn. Schedule it:

```bash
claudeme snapshot --install    # 03:00 and 15:00 daily via launchd
claudeme snapshot --status
```

## Query

```bash
claudeme cost                                   # all providers, by model, each day at its own prices
claudeme cost --at 2026-01-01                   # everything at one date's prices
claudeme cost --provider openai --by project
claudeme cost --by day --json
```

## Check what is covered

```bash
python3 - <<'PY'
import json, glob, os
root = os.path.expanduser('~/.config/claudeme/shared/history')
tot = sm = me = tk = 0
for f in glob.glob(root + '/*/*.json'):
    for s in json.load(open(f))['sessions'].values():
        tot += 1
        sm += bool(s.get('summary')); me += bool(s.get('metrics')); tk += bool(s.get('tokens'))
print(f'{tot} records: {sm} summarized, {me} with metrics, {tk} with a token ledger')

h = json.load(open(os.path.expanduser('~/.config/claudeme/shared/usage-history.json')))
split = [d for d, v in h['days'].items() if v.get('cacheWrite1h') or v.get('cacheWrite5m')]
print(f'{len(h["days"])} days recorded, {len(split)} with the 1h/5m cache split')
PY
```
