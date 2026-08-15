# What triggers a digest

## Goal

Yesterday's sessions are summarized without a clock-driven job — the work happens because
Claude was used, not because it turned 05:00.

## Buckets

**Fixed** — macOS; `codex` on PATH; ~30–50s of model time per session; digests are keyed by
session id and idempotent (`usage.Pending`); a digest, once written, is never revisited;
launching Claude must not block on summarizing.

**Free** — everything about where the trigger lives, whether state is a file or the disk
itself, catch-up policy, concurrency, how failures retry.

**Assumed** *(any of these being false changes the answer)* —

1. Claude is normally started through `claudeme`, not a bare `claude`.
2. One machine. No two hosts draining the same `shared/` at once.
3. `codex` is usable whenever Claude is being used — the same laptop, the same network.
4. A per-session summary is worth having within a day, not within a minute.

## Two requirements every candidate needs

Neither is optional, and neither exists today:

- **A freshness gate.** A live session's transcript is incomplete. Digest it mid-flight and
  the partial summary is recorded as done and never revisited — silent data loss, the same
  failure mode as the oversized-line bug. Any trigger that can fire while a session is open
  needs to skip transcripts touched in the last N minutes, and skip the current session id.
  The 05:00 job has this hazard too; it is just less likely to hit it.
- **Single-flight.** Two launches must not both drain. A lock file next to the digest root,
  stale-after-N-hours, is enough.

## Candidates

### A. Launch-time catch-up *(smallest)*

`runClaude` spawns `claudeme digest --drain` detached, before `claude` starts, at most once
per day (a stamp file next to the digest root). The drainer is what already exists:
`Pending()` minus the freshness gate.

- **Touches** `internal/cmd/claude.go`, `internal/cmd/digest.go`.
- **Must be true** the user starts Claude through `claudeme` on any day they care about.
- **Cost** ~20 lines, a stamp file, a lock file. Nothing new to reason about.
- **Kill shot** the drain runs while the user works — codex calls competing with the
  session they just opened, on the same account.
- **Cheapest disproof** run a drain in the background during a normal working session and
  see whether anything is noticeable.
- **Door** reversible; delete the spawn.

### B. Daily launchd job *(the baseline being replaced)*

What the plan already built: `com.claudeme.digest`, 05:00, `RunAtLoad`.

- **Must be true** the machine is awake at 05:00, or launchd's catch-up-on-wake fires.
- **Cost** already written. Ongoing: a plist the user did not ask for, running whether or
  not Claude was used that day.
- **Kill shot** it is exactly what the user is trying to get rid of — and it burns model
  calls on days with no sessions to summarize.
- **Door** reversible (`--uninstall`).

### C. Claude Code `SessionEnd` hook *(buy / borrow)*

Claude Code fires hooks with the transcript path in the payload. `claudeme` already manages
the shared settings these profiles use, so it can install a `SessionEnd` hook that spawns
`claudeme digest --session <path>` detached. The session that just ended is summarized
minutes later, and the freshness gate becomes unnecessary for the common path — the session
is over by definition.

- **Touches** shared `settings.json` (hook install/uninstall alongside `digest --install`),
  a `--session` flag on digest.
- **Must be true** `SessionEnd` actually fires on the ways sessions really end — `/exit`,
  Ctrl-C, closing the terminal, a crash, the laptop sleeping.
- **Cost** half a day, plus owning a hook in the user's settings forever.
- **Kill shot** the hook silently does not fire on the messy exits, which are most of them —
  and unlike a scan-based trigger, nothing ever notices the gap.
- **Cheapest disproof** install a hook that appends one line to a log, use Claude normally
  for a day, compare the line count against the transcripts written that day. This is worth
  running whatever else gets built.
- **Door** reversible, but it edits a file the user also edits by hand.

### D. Digest on read *(inverted — no trigger at all)*

Nothing runs in the background. `history`, `heatmap` and `usage` are the only readers of
digests; they report `N sessions not summarized` and offer to fill in what the current view
needs.

- **Must be true** the user tolerates a wait, or a partial view, at the moment they look.
- **Cost** small, but it moves latency onto the one interactive moment that has a human
  waiting in front of it.
- **Kill shot** the backfill measured 30–50s per session — a month's view would sit there
  for over an hour. Fine as a *prompt*, unusable as a *fill*.
- **Door** reversible.

### E. Queue file plus drainer *(ambitious)*

A `queue.jsonl` that triggers append to and a drainer pops from, with per-item retry counts
and backoff for capacity rejections.

- **Must be true** that disk state is not already a sufficient queue.
- **Cost** days, and a second source of truth to keep honest forever.
- **Kill shot** it is redundant. `Pending()` = every transcript on disk minus every digest on
  record; that difference *is* the queue, it cannot drift from reality, it survives losing
  any state file, and it already handled a killed 178-session run with no bookkeeping. A
  queue file can disagree with the disk; a derived set cannot. The retry policy the queue
  would buy is already covered — a failed session simply stays pending.
- **Door** one-way in practice: once triggers write to a queue, everything downstream
  assumes it.

## Comparison

| | cost | risk | reversible | forecloses |
|---|---|---|---|---|
| **A** launch catch-up | ~20 lines | low | yes | nothing |
| **B** launchd 05:00 | done | low | yes | nothing — but it is the thing being removed |
| **C** `SessionEnd` hook | half a day | **medium** — unproven firing | yes | owns a file the user edits |
| **D** on read | small | high UX | yes | nothing |
| **E** queue + drainer | days | medium | **no** | a second source of truth |

## Recommendation — A, with C as a later addition

A wins because it needs no contract that has not been verified. It reuses the drain path
that just survived a real 168-session run, it cannot fire on a machine the user is not
sitting at, and on a day with no Claude usage it does nothing at all — which is the actual
complaint about the job. Being ~20 lines, it is cheap to delete if C proves better.

What is consciously given up: **freshness**. A session that ends at 14:00 is summarized when
Claude is next launched, likely the following morning. If that is too slow, C is the fix and
composes with A — the hook handles the fresh case, the launch drain stays as the net that
catches whatever the hook missed. Two triggers into one idempotent drain is not duplication;
it is the belt-and-braces version, and it costs one lock file.

E is rejected on the merits, not on cost: the disk already is the queue, and adding a file
that can disagree with it makes the system less reliable, not more.

## First move

Run C's disproof, because it is the only unknown and it costs a day of ordinary usage: a
`SessionEnd` hook that appends one line to a log, then compare that line count against the
transcripts written the same day. That number decides whether the eventual system is A alone
or A+C — and A can be built while it runs, since nothing about A depends on the answer.

## Decision (2026-08-15)

**B — the daily launchd job stays.** The user chose the baseline over the recommendation and
the plan continues as written. A, C, D and E are not built; this file is the record of what
they were and why they lost, should the clock-driven job start to grate.

One item from above outlives the decision: **the freshness gate is still missing, and B has
the hazard too.** A session left open overnight is a transcript the 05:00 job will digest
while it is still being written, recording a partial summary that is never revisited. Less
likely than under a launch-time trigger, not impossible. Not fixed here.

## Assumptions to break

The four in the buckets above. Two are worth a second look:

- **"Started through `claudeme`."** If Claude often gets started directly, A's trigger misses
  those days — though the drain still catches every session eventually, since it scans the
  disk rather than tracking what launched.
- **"A day is soon enough."** If the answer is no, the recommendation flips to C-first, and
  the disproof above stops being optional.
