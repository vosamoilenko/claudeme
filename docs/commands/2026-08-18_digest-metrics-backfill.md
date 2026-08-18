# Backfill Session Metrics

Fills `metrics` (timings, branches, counted totals) onto every session record that lacks
it — both sessions already summarized and sessions never digested at all. No model call,
no network, no usage budget.

```bash
claudeme digest --metrics-only -n   # what it would do
claudeme digest --metrics-only      # do it
```

Safe to re-run: sessions that already carry `metrics` are skipped, and a session that
already carries a `summary` keeps it untouched.

Cost, measured 2026-08-18 over the full corpus: **1,312 sessions in 47 s, 0 failures**
(~0.04 s per transcript, single-threaded, gzip included). Compare the LLM digest of 34
sessions on 2026-08-17: 14 m 31 s.

Check what is on record:

```bash
python3 - <<'PY'
import json, glob, os
root = os.path.expanduser('~/.config/claudeme/shared/history')
tot = sm = me = 0
for f in glob.glob(root + '/*/*.json'):
    for s in json.load(open(f))['sessions'].values():
        tot += 1
        sm += bool(s.get('summary'))
        me += bool(s.get('metrics'))
print(f'{tot} records, {sm} with summary, {me} with metrics')
PY
```

## It also cleans as it goes

Each real run (not `-n`) first sweeps every record whose `metrics` still carries
`distill.py`'s undeduped `tokens`, `cache_hit_rate` and `subagent_output_tokens` — counts
superseded by the `tokens` ledger and inflated 1.5-1.8x by counting one API call once per
assistant record. It reads and writes only what is on disk, so it costs a pass over a few
megabytes and reports:

```
  dropped superseded token counts from 1312 records in 132 files
```

Idempotent — once the corpus is clean the line stops appearing.
