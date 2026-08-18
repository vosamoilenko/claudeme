# Persist Distilled Session Metrics In The Digest

## Summary

`distill.py` already computes, on every digest run, the deterministic facts about a
session — `started`, `ended`, `branches`, `cwd`, `title` and a `metrics` block (wall time,
turns, tool calls, tokens, files touched). `summarize.sh` writes them into a temp
`digest.txt`, feeds them to the model as context, and deletes the temp dir. Nothing
survives: across all 500 digested sessions in `~/.config/claudeme/shared/history/`, the
set of persisted metrics keys is empty.

This plan persists them. `Digest` gains a `metrics` field, the runner calls
`distill.py --metrics` (a mode the script already has) alongside the summary, and a new
`--metrics-only` backfill mode fills the field for sessions already digested — and for the
much larger set of transcripts that were never LLM-digested at all. No new dependency, no
model call, no cost on the backfill path.

## How user task was understood

> The digest drops every timestamp, so the summaries cannot be placed on a clock. Adjust
> claudeme so the timing is persisted and consumers can compute session spans.

The immediate consumer is `~/Developer/ttt` (a work-activity aggregator over GitLab,
Google Calendar and Jira), which needs two things this repo already has: *when* a session
ran, and *which branch* it ran on. The branch is the more valuable half — `branches:
["feat/DP-3282"]` is a ticket key on work that produced no commit message carrying one.
That consumer is out of scope here; this plan only makes claudeme stop discarding what it
computes.

"Timestamps" is read as the whole `--metrics` payload, not just `started`/`ended`. It is
one JSON object, it is free, and splitting it would mean choosing now which of its fields
a future consumer wants.

## Established facts

- `distill.py` already has the exact output mode needed:
  ```python
  if "--metrics" in sys.argv:
      json.dump({k: d[k] for k in ("session_id", "title", "cwd", "branches",
                                   "started", "ended", "version", "skills_used",
                                   "metrics")}, sys.stdout, indent=2)
  ```
  No change to the script is required for the happy path.
- Verified against a real transcript (session `1b8df218`, mitarbeiterportal, 2026-08-17):
  emits `started: 2026-08-17T18:20:53.322Z`, `ended: 2026-08-17T20:19:35.623Z`,
  `branches: ["feat/DP-3282", "sandbox"]`, and a 25-field `metrics` block.
- **Cost: 0.04s per transcript**, measured on that file. Over every transcript on disk
  (2,012 live in `shared/projects/` + 1,885 gzipped in `shared/archive/` = 3,897) that is
  roughly 3 minutes, single-threaded, plus gunzip. For comparison, the 2026-08-17 LLM
  digest of 34 sessions took 14m31s.
- `usage.Pending` skips any session already present in its date+project digest file, so
  the existing loop cannot be used to fill a field on records that already exist. The
  backfill needs its own predicate.
- `digestVersion` is documented as: "Bump it when a field changes meaning, never when one
  is added." Adding `metrics` is an addition — the version stays 1.
- `Digest.Usable()` gates transcript deletion on a non-empty summary. Metrics must not
  enter that check: a metrics-only record must never be strong enough to justify deleting
  the transcript it was derived from.

## Open questions

*All three resolved 2026-08-18 — answers inline.*

- [x] Does `distill.py --metrics` succeed on *every* transcript on disk, or only on
      well-formed ones? It counts `malformed_lines` rather than failing, which suggests it
      tolerates damage, but this is untested at scale. Resolve by running it over all 3,897
      transcripts and recording the failure count and their shapes before block 4 ships.
      Blocks block 4 only; blocks 1–3 are unaffected.
      **Answer: yes.** Swept all 2,565 top-level transcripts (1,321 live + 1,244 gzipped)
      through `distill.py --metrics`: **0 failures**, 16 s wall at 8-way parallelism. The
      real backfill then ran 1,312 sessions (the rest have no transcript left) in 47 s,
      also 0 failures.
- [x] What fraction of sessions have a usable `branches` entry, and how many carry more
      than one (the verified sample carries `["feat/DP-3282", "sandbox"]`)? This does not
      change what gets stored — it decides whether the consumer needs a disambiguation
      rule, and belongs in the answer handed to that consumer. Not a blocker.
      **Answer: every record has at least one.** Of 1,312: 1,283 carry exactly one, 21 carry
      two, 6 carry three, 2 carry four. The consumer needs a tie-break rule for ~2%.
- [x] Sessions that cross midnight: `started` and `ended` can fall on different dates,
      while the digest is filed under the single `Candidate.Date`. Confirm which date that
      is derived from and document the resulting skew rather than changing the filing
      scheme — moving the file path is a much bigger change than this plan wants.
      **Answer: the filed date is the session's start date.** `readCandidate` takes the first
      timestamp in the transcript; across all 1,312 records no filed date differs from
      `started`. 49 (3.7%) end on a later UTC date. Documented in
      `docs/architecture/2026-08-18_session-digest.md`, filing scheme unchanged.

## Building blocks

1. **`Digest.Metrics` field** — `Metrics json.RawMessage \`json:"metrics,omitempty"\`` on
   the `Digest` struct, held raw for the same reason `Summary` is: `distill.py` owns the
   shape, and a Go mirror of it would drift. `omitempty` keeps existing records byte-stable
   until they are backfilled. `digestVersion` unchanged. — depends on: nothing — touches:
   `internal/usage/digest.go`
2. **`Runner.Metrics(transcript)`** — runs `python3 distill.py <src> --metrics` and returns
   the raw JSON, reusing `Summarize`'s gzip `decompress` path so archived transcripts work
   identically. Returns an error rather than an empty object on failure; the caller decides
   whether that is fatal. — depends on: nothing — touches: `internal/usage/digest_run.go`
3. **Wire into the daily loop** — `digest.go`'s per-session body calls block 2 before
   `runner.Summarize` and sets `d.Metrics`. A metrics failure must not abort a session
   whose summary succeeded: log it, leave the field absent, continue. — depends on: 1, 2 —
   touches: `internal/cmd/digest.go`
4. **`claudeme digest --metrics-only` backfill** — same scan, but a `PendingMetrics`
   predicate (`Digested` reports the session, but its stored record has no `metrics`) plus
   the sessions `Pending` already yields. Skips `Summarize` entirely, so it needs no codex,
   no network and no usage budget; a run over everything on disk should be minutes, not
   hours. Writes via `PutDigest`, which already replaces a session in place. — depends on:
   1, 2, open question 1 — touches: `internal/cmd/digest.go`, `internal/usage/digest_scan.go`
5. **Keep `Usable()` summary-only** — an explicit test that a `Digest` carrying metrics and
   no summary reports `Usable() == false`, so a backfilled record can never gate a
   transcript deletion. — depends on: 1 — touches: `internal/usage/digest_test.go`
6. **Docs** — `--metrics-only` in the `digest` usage block in `main.go` and in
   `docs/commands/`, and a note in the digest architecture doc that metrics are
   deterministic and re-derivable while summaries are not. — depends on: 4 — touches:
   `main.go`, `docs/`

Waves: 1 → 2 → (3, 5 parallel) → open question 1 → 4 → 6.

## Required refactorings

None. `Summarize` already isolates the temp-dir setup and gzip handling that block 2
reuses; a second exported method sits beside it without touching the existing one.

## Blockers

- Open question 1 blocks block 4 only. Shipping a backfill that dies partway through 3,897
  transcripts on a shape nobody checked is the failure mode worth ten minutes of measuring
  first. Blocks 1–3 can land before it is answered.

## Steps

- [x] Block 1 — `Digest.Metrics` field
- [x] Block 2 — `Runner.Metrics`
- [x] Block 3 — wire into the daily digest loop
- [x] Block 5 — `Usable()` regression test
- [x] Resolve open question 1 (full-corpus `--metrics` dry run)
- [x] Block 4 — `--metrics-only` backfill
- [x] Block 6 — docs

## Verification

### Automated

- `go test ./internal/...` passes, including the existing digest tests unchanged — the new
  field is additive and `omitempty` keeps old fixtures valid.
- A new test asserts a round trip: `PutDigest` a record with `metrics`, `LoadDigest` it
  back, and get byte-identical raw JSON.
- Block 5's test: metrics present, summary absent → `Usable() == false`.
- `--metrics-only` on a temp root containing one already-summarized session adds `metrics`
  and leaves `summary`, `model` and `digestedAt` untouched.

### Manual

- Run `claudeme digest --metrics-only -n` and confirm the pending count matches
  expectation: ~500 already-digested sessions plus every transcript never digested.
- Run it for real, then confirm on a known session (`1b8df218`, 2026-08-17,
  mitarbeiterportal) that the stored `started`/`ended`/`branches` match the direct
  `distill.py --metrics` output above.
- Confirm the next scheduled 05:00 run writes `metrics` on new sessions without a codex
  call beyond the usual one per session, and without a wall-clock regression (0.04s/session
  against a ~30s model call is noise, but measure rather than assume).

## Out of scope

- The consumer side in `~/Developer/ttt` — reading these digests, mapping `cwd` to a repo,
  extracting a ticket key from `branches`, or turning `wall_ms` into bookable time. Note
  for whoever does it: **`wall_ms` is not working time.** The verified session shows
  7,122,301 ms wall (118 min) against 829,824 ms of turn time (14 min) over 9 turns — the
  difference is the user thinking, reading, or away. Treating wall time as booked hours
  would overstate every session.
- Any change to `schema.json`, `prompt_prefix.md`, or the summarizer model.
- Re-running the LLM summary over already-digested sessions.
- Changing the `history/<date>/<project>.json` layout, including the midnight-crossing
  question above.
- Retention or deletion behaviour. Metrics deliberately do not participate in `Usable()`.

## Outcome (2026-08-18)

Shipped. Beyond the plan as written:

- `Digest.MetricsAt` records when metrics were derived, separate from `digestedAt` — the
  two happen in different runs, and without it a metrics-only record has no timestamp and
  the file's `Updated` never advances.
- `Summary` gained `omitempty`, so a metrics-only record omits the key instead of storing
  `null`. Existing records, which all have summaries, are unaffected on disk.
- `Pending` changed from "is this session on record?" to "does this record have a
  summary?". Without that, backfilling metrics onto a never-digested session would have
  permanently barred it from ever being summarized. `PendingMetrics` is its twin, asking
  the same question of `metrics`; both go through one `pendingBy` helper.
- `PutDigest`'s `Updated` only moves forward, so a backfill re-filing an old session
  cannot drag the file's timestamp backwards past a newer summary.
- `GetDigest` added so the backfill merges onto the existing record rather than replacing
  it.

Verified: `go test ./internal/...` green; 500 summaries all intact after the backfill;
session `1b8df218` stores `started 2026-08-17T18:20:53.322Z`, `ended
2026-08-17T20:19:35.623Z`, `branches ["feat/DP-3282","sandbox"]`, 26 metric fields, with
its summary, model and `digestedAt` untouched.
