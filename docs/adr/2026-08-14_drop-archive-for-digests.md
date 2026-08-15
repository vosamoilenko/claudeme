# Drop `claudeme archive` in favour of per-session digests

## Context

`claudeme archive` gzips transcripts older than 7 days out of the live tree into
`~/.config/claudeme/shared/archive` (`internal/usage/archive.go:39`), today 152M across 41
directories. `usage.Roots()` returns the live tree **and** the archive
(`internal/usage/usage.go:88`), so archived transcripts still count toward
`claudeme usage` and `claudeme snapshot`.

Archive preserves bytes and nothing else: the summaries, decisions and dead ends inside a
session stay locked in raw JSONL that nobody reads. `claudeme snapshot` already preserves
the numbers in `usage-history.json` independently of it
(`internal/cmd/history.go:21`). The gap is meaning, and a per-session digest closes it —
see [the plan](../plans/2026-08-14_daily-session-digest.md).

`shared/settings.json` has `cleanupPeriodDays: 3650`, so Claude Code currently deletes no
transcripts. Nothing is racing archive to the files.

## Options Considered

### A — keep archive as-is

Digests are purely additive; archive keeps being the raw-transcript safety net and keeps
old days accurate for `usage`. Costs a second scheduled job doing work whose only
remaining value is disk-layout hygiene, on a machine where Claude Code deletes nothing.

### B — gate archive on digested-ness

Archive only moves a transcript once a digest exists for it. Guarantees nothing leaves the
live tree unsummarized, but couples the two jobs, adds a digest lookup to `archive.go`,
and keeps a job whose purpose the digest already serves.

### C — drop archive (chosen)

One job instead of two. Transcripts stay where Claude Code put them and leave only when
Claude Code's own retention removes them, which matches the rule that claudeme never
deletes a transcript.

## Decision

Remove the `claudeme archive` command and `internal/usage/archive.go`. Keep
`ArchiveRoot()` in `usage.Roots()` so the digest job reads the archive, digest its 502
transcripts once, verify every one produced a usable summary, then delete
`shared/archive/`.

## Consequences

- One scheduled job (`com.claudeme.digest`) replaces two; no transcript is moved by
  claudeme again.
- 152M freed once the archive is digested and deleted. That delete is irreversible and
  lossy by design: anything `schema.json` does not capture is gone.
- The numbers survive. `usage-history.json` records 2026-07-03 → 2026-08-04, which spans
  the archive's entire 2026-07-04 → 2026-07-27 range, so `claudeme history` is unaffected.
  `claudeme usage`, which reads transcripts, loses those July days.
- No raw-transcript safety net for future sessions either. `claudeme usage` accuracy for
  future old days now depends on Claude Code's `cleanupPeriodDays` rather than on
  claudeme's archive.
- The live tree grows unbounded until Claude Code prunes it. At `cleanupPeriodDays: 3650`
  that is effectively never, so disk growth is now the user's setting to manage.

## References

- [`plans/2026-08-14_daily-session-digest.md`](../plans/2026-08-14_daily-session-digest.md) — the digest job that replaces it.
- `spike-summarize/docs/adr/2026-08-13_default-summarizer-model-gpt-5-6-luna.md` — the summarizer the digest calls.

## Amendments

- 2026-08-14 — corrected before the decision shipped: the archive is deleted after its
  502 transcripts are digested and verified, not kept on disk indefinitely.
