# Session Digest

`claudeme digest` turns session transcripts into
`~/.config/claudeme/shared/history/<date>/<project>.json`, one record per session keyed
by session id. Transcripts are only ever read — Claude Code owns their lifetime.

## Four independent halves of a record

A record is assembled by four passes that never depend on each other. Each writes its own
field and leaves the rest untouched, so any one can run, fail or be backfilled alone:

| Field | Produced by | Source | Cost | Survives transcript deletion |
| --- | --- | --- | --- | --- |
| `summary` | `summarize.sh` → codex (`gpt-5.6-luna`) | transcript | ~30 s + a model call | **No** — must be written while the transcript lives |
| `metrics` | `distill.py --metrics` | transcript | ~0.04 s | No |
| `tokens` | `SessionTokens` | transcript | ~7 ms | No |
| `prompts` | `ReadPromptHistory` | `history.jsonl` | ~1 ms | **Yes** — the only pass that reaches deleted sessions |

Only `summary` is irreplaceable. The other three are pure functions of a file, so a wrong
one is fixed by re-running rather than by migrating.

`metrics` holds `session_id`, `title`, `cwd`, `branches`, `started`, `ended`, `version`,
`skills_used` and a `metrics` block (wall time, turns, tool calls, files touched).
`metricsAt` records when it was derived, separate from `digestedAt`, because the two
happen in different runs.

It carries no token counts. `distill.py` computes some, but by summing every assistant
record's usage block with no dedupe — 1.5-1.8x the truth, because one API call can land as
several records sharing a requestId. `StripNaiveTokens` drops `tokens`, `cache_hit_rate`
and `subagent_output_tokens` in `PutDigest`, so the ledger in `tokens` is the only token
answer a record can give.

## Prompt history: the source that outlives everything

Claude Code appends every prompt the user types to
`~/.config/claudeme/shared/history.jsonl` — timestamp in epoch ms, project path, session
id — and **never rotates it**. Measured 2026-08-18:

| | transcripts | digests | prompt history |
| --- | --- | --- | --- |
| sessions | 1,327 | 1,665 | **4,970** |
| earliest | 2026-07-27 | 2026-07-03 | **2026-02-10** |

Backfilling it created **3,442 records for sessions that had no trace at all**, taking
`history/` from 41 to 178 date directories (8.4 MB). `history.jsonl.bak` is a strict
subset — the file has never been trimmed, so nothing older exists to recover.

It carries no model and no tokens, so it can never extend `claudeme cost`. What it
answers is *when a session ran and on what*.

`Prompts{Count, First, Last}` brackets the **prompts, not the session**: the closing
assistant turn runs past `Last`, and a one-prompt session has no span at all (`Last` is
omitted rather than set equal to `First`, so unknown is not reported as zero). Treat it
as a floor on duration. Median span 12 min, p90 100 min.

**Placement matters.** Project names are derived from the transcripts that still exist, so
a session whose transcript is gone can resolve to a different name than the one its record
was filed under — writing it there would split one session into two records. `IndexDigests`
maps every session to where it already lives, and the prompt pass writes there. Without it
the backfill created 3,483 records instead of 3,442; with it, 5,107 records for 5,107
distinct sessions, zero duplicates.

## Which half gates a delete

That asymmetry is why `Digest.Usable()` — the gate on deleting a transcript — reads
`summary` alone. A metrics-only record must never justify destroying the only copy of
what produced it.

## Which sessions a run picks up

Both predicates scan every transcript, then ask a different question of the record on
file:

- `Pending` → sessions with no `summary`. Drives the default run and the daily 05:00 job.
- `PendingMetrics` → sessions with no `metrics`. Drives `--metrics-only`.
- `PendingTokens` → sessions with no `tokens`. Drives `--tokens-only`.
- `--prompts-only` needs no predicate: it iterates the prompt history rather than the
  transcripts, and re-files a session only when its prompt count has changed.

They are independent: a session summarized before metrics existed is pending for
`--metrics-only`, and a metrics-only record is still pending for a summary. `Settled`
holds back transcripts written to in the last hour in both modes.

All the `--*-only` passes merge onto the record already there — the summary, `model` and
`digestedAt` of an already-digested session are left untouched.

## Filing date

`Candidate.Date` is the **first timestamp in the transcript** (UTC), so a record is
always filed under the date its session *started*. Verified across all 1,312 backfilled
records: no record's filed date differs from its `started`. About 3.7% (49) of sessions
end on a later UTC date than they started; the record still lives under the start date.
Consumers computing per-day spans must read `started`/`ended` rather than infer them
from the directory name.

## For consumers

- `branches` is present on every record: 1,283 carry one, 29 carry two or more. A
  consumer mapping a branch to a ticket key needs a tie-break rule for that 2%.
- **`wall_ms` is not working time.** A verified session shows 7,122,301 ms wall (118 min)
  against 829,824 ms of turn time (14 min) over 9 turns — the difference is the user
  thinking, reading or away.

## Amendments

- 2026-08-18 — `prompts` backfilled from `history.jsonl`, adding 3,442 sessions and five
  months of history that no other source retains. `IndexDigests` added to keep one
  session in one place.
- 2026-08-18 — `tokens` added by the token-ledger work; see
  [`2026-08-18_token-ledger-and-prices.md`](2026-08-18_token-ledger-and-prices.md).
- 2026-08-18 — `metrics`/`metricsAt` persisted on every digest, plus
  `digest --metrics-only` to backfill. Before this, `distill.py` computed the metrics on
  every run and `summarize.sh` deleted them with its temp dir.
- 2026-08-18 — `distill.py`'s undeduped token counts stripped from `metrics` in
  `PutDigest`, and swept from 1,312 existing records by `PruneNaiveTokens` (run by
  `--metrics-only`). They contradicted `tokens` and were inflated 1.5-1.8x.
