# Archive backfill — digesting shared/archive before deleting it

## Summary

The commands driving block 7 of
[the digest plan](../../plans/2026-08-14_daily-session-digest.md): summarizing the 352
archived sessions so the 152M `shared/archive/` can be retired. Ad-hoc, run against real
data, kept because the run spans sessions.

## Run 1 (2026-08-14) — killed on purpose

Reached 178 done / 8 failed of 346 before being killed: every failed session fired its own
desktop notification, which is wrong for a bulk run. All 8 were the transient
`Selected model is at capacity` on luna. Log preserved at `/tmp/digest-archive-run1.log`.

Fixed before run 2 — `SUMMARIZE_NOTIFY=0` in `summarize.sh` drops the per-session popup and
keeps the stderr line; `digest` sets it whenever a run exceeds `bulkNotifyThreshold` (5)
and posts one notification with the tally at the end.

## Run 2 (2026-08-15) — finished, gate clean

168 done, 0 failed, 1h35m. luna held up for the whole run; no notifications fired.

Gate afterwards: **355 archived, 353 digested, 2 not deletable** — only the two
timestamp-less stubs below (~300 bytes together). Every digest file parses as JSON.

**Block 8 ran on 2026-08-15 with the user's go-ahead.** 353 transcripts plus their 23
`subagents/` directories removed — 376 paths, 150.1M. `shared/archive` is now 8K (the two
stubs), `shared/history` 2.0M. `claudeme history` still spans 2026-07-03 → 2026-08-04 and
`heatmap` renders, both off digests alone.

The delete was driven by a dry-run-first script, kept at
`scratchpad/block8_delete.py` for the shape of it: a session goes only when its digest has
a non-empty summary and outcome, and its nested `subagents/` directory goes with it.

### A session the scanner was silently dropping

The first gate run left a third undeletable file: a 4.3MB session with a valid date
inside it. `readCandidate` used a `bufio.Scanner` with a 1MB cap, and this transcript's
first line was bigger than that — the scan ended before any timestamp was read, so the
session never became a candidate and no digest was ever attempted. Silent: it showed up
neither as pending nor as failed.

Fixed in `internal/usage/digest_scan.go` (read whole lines, decline to parse ones over
`maxCandidateLine`), covered by `TestScanSessionsSurvivesAnOversizedFirstLine`, and the
session digested afterwards. Worth remembering when a count looks off by a few: the
scanner used to lose sessions rather than report them.

## Run 2 details

luna's capacity rejection has cleared (`codex exec -m gpt-5.6-luna` answers normally).

```sh
pgrep -f "claudeme-digest digest"
grep -c kB /tmp/digest-archive-run2.log        # done
grep -c "^    " /tmp/digest-archive-run2.log   # failed (stderr detail lines)
tail -3 /tmp/digest-archive-run2.log
```

171 sessions pending at the start of run 2, 3 of which can never clear the gate (below).
~30s each.

## Resume after the run dies, or a reboot

The binary under test is a build artifact, not installed:

```sh
cd /Users/vo1/Developer/claudeme
go build -o /tmp/claudeme-digest .
nohup /tmp/claudeme-digest digest --archived >| /tmp/digest-archive-run2.log 2>&1 &
```

`>|`, not `>>`: under zsh's `noclobber`, appending to a file that does not exist yet fails
and the job never starts.

Safe to re-run at any point: digests are keyed by session id, so anything already on
record is skipped and only the remainder costs a model call. The 2 capacity failures are
picked up automatically by a later run.

## Check what is on record

```sh
/tmp/claudeme-digest digest --status
find ~/.config/claudeme/shared/history -name '*.json' | wc -l
for f in ~/.config/claudeme/shared/history/*/*.json; do python3 -m json.tool "$f" >/dev/null || echo "BAD $f"; done
```

## Block 8 gate — run before deleting anything

Every archived `.gz` must have a digest whose `summary` and `outcome` are non-empty.
Prints the session ids that are NOT safe to delete:

```sh
python3 - <<'PY'
import json, glob, os
have = {}
for p in glob.glob(os.path.expanduser("~/.config/claudeme/shared/history/*/*.json")):
    for sid, d in json.load(open(p))["sessions"].items():
        s = d.get("summary") or {}
        have[sid] = bool(s.get("summary")) and bool(s.get("outcome"))
gz = glob.glob(os.path.expanduser("~/.config/claudeme/shared/archive/*/*.jsonl.gz"))
missing = [g for g in gz if not have.get(os.path.basename(g).replace(".jsonl.gz", ""))]
print(f"{len(gz)} archived, {len(gz)-len(missing)} digested, {len(missing)} NOT deletable")
for m in missing[:20]:
    print("  keep:", os.path.basename(m))
PY
```

### Three files the gate will never clear

`shared/archive/` holds 355 top-level `.jsonl.gz`; `ScanSessions` finds 352. The
difference is real, not a bug:

- 2 transcripts carry no timestamp on any line (151 and 157 bytes — an empty session and
  an `.orphaned-*` fragment). Without a date there is no `history/<date>/` to file them
  under, so they are skipped and stay undeletable. Both are ~150 bytes; leaving them costs
  nothing.
- The count of 502 in the plan was every `.gz` in the tree, including nested subagent
  transcripts. Those belong to a session and are folded into its digest by `distill.py`,
  so they are not scanned separately — and they must be deleted with their parent session
  or not at all.

Deleting is the only irreversible step in the plan, and needs the user's explicit
go-ahead. `usage-history.json` already covers 2026-07-03 → 2026-08-04, so the numbers
survive the delete; the raw bytes do not.

## Amendments

- (none)
