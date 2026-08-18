# Session Digest

`claudeme digest` turns session transcripts into
`~/.config/claudeme/shared/history/<date>/<project>.json`, one record per session keyed
by session id. Transcripts are only ever read — Claude Code owns their lifetime.

## Two halves of a record

Each record carries two independent payloads, both raw JSON because the producer owns
the shape:

| Field | Produced by | Cost | Re-derivable |
| --- | --- | --- | --- |
| `summary` | `summarize.sh` → codex (`gpt-5.6-luna`) | ~30 s and a model call per session | **No** — the transcript may be gone |
| `metrics` | `distill.py --metrics` | ~0.04 s, no network | **Yes** — pure function of the transcript |

`metrics` holds `session_id`, `title`, `cwd`, `branches`, `started`, `ended`, `version`,
`skills_used` and a 26-field `metrics` block (wall time, turns, tool calls, tokens, files
touched). `metricsAt` records when it was derived, separate from `digestedAt`, because
the two happen in different runs.

That asymmetry is why `Digest.Usable()` — the gate on deleting a transcript — reads
`summary` alone. A metrics-only record must never justify destroying the only copy of
what produced it.

## Which sessions a run picks up

Both predicates scan every transcript, then ask a different question of the record on
file:

- `Pending` → sessions with no `summary`. Drives the default run and the daily 05:00 job.
- `PendingMetrics` → sessions with no `metrics`. Drives `--metrics-only`.

They are independent: a session summarized before metrics existed is pending for
`--metrics-only`, and a metrics-only record is still pending for a summary. `Settled`
holds back transcripts written to in the last hour in both modes.

`--metrics-only` merges onto the record already there — the summary, `model` and
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

- 2026-08-18 — `metrics`/`metricsAt` persisted on every digest, plus
  `digest --metrics-only` to backfill. Before this, `distill.py` computed the metrics on
  every run and `summarize.sh` deleted them with its temp dir.
