# Daily session digest into `history/<date>/<project>.json`

## Summary

A new `claudeme digest` command turns each Claude Code session transcript into a
structured JSON summary and files it under
`~/.config/claudeme/shared/history/<YYYY-MM-DD>/<project>.json`, scheduled daily via
launchd like the other jobs. `claudeme archive` is removed — see
[the ADR](../adr/2026-08-14_drop-archive-for-digests.md).

Size: 2 new Go files in `internal/usage/` plus `internal/cmd/digest.go` and an embedded
assets dir carrying `spike-summarize/`'s scripts verbatim; deletes
`internal/cmd/archive.go` and `internal/usage/archive.go`; edits `main.go`, `README.md`,
`claudeme.1`. No Python port, no new Go dependency.

## How user task was understood

> Every session Claude Code writes ends up as a per-project, per-date JSON record of what
> the session was about, alongside the numbers `usage-history.json` already keeps — and
> nothing in claudeme moves or deletes a transcript any more.

The reading, checkable against the code:

- **"meta (script + AI summary)"** = `spike-summarize/distill.py`'s deterministic digest
  (cwd, files, commands, tools, timings) plus `summarize.sh`'s `schema.json`-valid AI
  summary (`goal`, `outcome`, `summary`, `projects`, `work_items`, `decisions`,
  `commands_of_note`, `open_threads`, `tags`, `learnings`, `dead_ends`,
  `user_preferences`, `environment_facts`, `next_session_brief`). Both already work and
  were verified on 2026-08-13.
- **"project"** = `usage.Project.Name`, the display name at `internal/usage/usage.go:95` —
  the same key `usage.Snapshot` uses for its `Projects` buckets
  (`internal/usage/history.go:92`). Two checkouts sharing a basename land in one file,
  matching `addBucket`'s accumulate-on-collision behaviour at
  `internal/usage/history.go:155`.
- **"one usage-history.json where we update the values"** = the existing
  `claudeme snapshot`, unchanged. Digest does not touch it.
- **"let Claude Code delete it itself"** = claudeme never moves or removes a `.jsonl`
  after this change.
- Scope boundary: top-level session transcripts only. Nested subagent transcripts and
  `journal.jsonl` are already folded into `distill.py`'s output.

## Open questions

- [x] How does the Go binary run the distill + AI-summary pipeline? → A Go command
      mirroring `archive`/`snapshot`, with `distill.py`, `summarize.sh` and `schema.json`
      `go:embed`-ed and unpacked to a temp dir at run time. Keeps the single-binary/brew
      property; no Python port. (2026-08-14)
- [x] What happens to `claudeme archive` once digests exist? → Dropped entirely, see
      [`adr/2026-08-14_drop-archive-for-digests.md`](../adr/2026-08-14_drop-archive-for-digests.md). (2026-08-14)
- [x] Which sessions does one run digest? → Every session not yet digested, keyed by
      session id. Default window is yesterday; `--all` backfills. Idempotent and
      resumable. (2026-08-14)

Assumptions taken without asking — break any that are wrong:

- "Drop archive entirely" means stop creating archives and remove the command. The
  existing `shared/archive/` (502 gzipped transcripts, 152M, 2026-07-04 → 2026-07-27,
  written by one manual run on 2026-08-03) is digested in samples (block 7) and deleted
  file-by-file as each one verifies (block 8). `usage.Roots()` keeps reading whatever is
  still there.
- Deleting it loses raw bytes only: `usage-history.json` already records
  2026-07-03 → 2026-08-04, so `claudeme history` keeps every number for that span.
  What is lost is anything `schema.json` does not capture — irreversibly.
- `claudeme archive --uninstall` is removed with the rest; nothing is installed on this
  machine (`launchctl list | grep claude` is empty), so no job is orphaned.
- Digest runs daily at 05:00 — after `snapshot` (03:00/15:00), clear of the 04:00 slot
  `archive` used.
- A per-session failure does not abort the run: it is counted, skipped, and retried next
  run. `summarize.sh` already fires a macOS notification on its own give-up paths.
- Writes are atomic (temp + rename), matching `usage.SaveHistory` at
  `internal/usage/history.go:237`.
- `shared/settings.json` currently has `cleanupPeriodDays: 3650`, so Claude Code deletes
  nothing today. The job therefore has no deadline, and re-scanning is safe.

## Building blocks

Each block fits in ≤100k agent context. Blocks with the same dependencies run in parallel.

1. Digest contract — `DigestRoot()` (= `config.SharedDir()/history`),
   `DigestPath(date, project)`, the per-session record (session id, date, cwd, project,
   transcript path, model, digested-at, summary as `json.RawMessage`), the per-file
   container keyed by session id, and atomic Load/Save. — depends on: — — touches:
   `internal/usage/digest.go`
2. Embedded pipeline runner — `internal/usage/assets/` holding `distill.py`,
   `summarize.sh`, `schema.json` copied verbatim from `spike-summarize/`, a `go:embed`
   FS, and a function that unpacks them to a 0700 temp dir and runs `summarize.sh` over
   one transcript, returning the parsed summary or an error. — depends on: 1 — touches:
   `internal/usage/assets/*`, `internal/usage/digest_run.go`
3. Session enumeration + idempotency — walk `usage.Roots()` (live tree **and** archive)
   for top-level `.jsonl` / `.jsonl.gz` transcripts via `usage.openTranscript`
   (`internal/usage/usage.go:278`, already gz-transparent), resolve each to (session id,
   date, project name) reusing `usage.Discover` / `findCwd` rather than re-implementing
   cwd resolution, and drop session ids already present in the target history file. —
   depends on: 1 — touches: `internal/usage/digest_scan.go`
4. Writer/merge — upsert one session into `history/<date>/<project>.json`: stable key
   order, existing entries preserved, a re-digest overwrites in place. — depends on: 1 —
   touches: `internal/usage/digest_merge.go`, `internal/usage/digest_test.go`
5. `claudeme digest` command — mirrors `archive.go`'s shape: `--since` (default
   yesterday) / `--all` / `--limit N` / `--dry-run` / `--install` / `--uninstall` /
   `--status`, launchd
   plist `com.claudeme.digest` daily at 05:00 with an explicit `PATH` (see blockers), log
   at `~/.config/claudeme/digest.log`, per-session failures counted not fatal, plus the
   `digest` case and help lines in `main.go`. — depends on: 2, 3, 4 — touches:
   `internal/cmd/digest.go`, `main.go`
6. Remove archive — delete `internal/cmd/archive.go`, `internal/usage/archive.go` and
   `internal/usage/archive_test.go`, keeping `ArchiveRoot()` and its place in `Roots()`
   (moved into `usage.go`) so old gzipped transcripts stay readable; drop the `archive`
   case and help lines from `main.go`; update `README.md` and `claudeme.1`. — depends on:
   5 (shares `main.go`) — touches: `internal/cmd/archive.go`, `internal/usage/archive.go`,
   `internal/usage/archive_test.go`, `internal/usage/usage.go`, `main.go`, `README.md`,
   `claudeme.1`
7. Digest a sample of the archive — `claudeme digest --all --limit 5` over
   `shared/archive/`, then read the 5 summaries and judge whether the output is worth
   502 codex calls (~5.5h serial at ~40s each). No code: an operation, with its result
   recorded in this plan's Amendments. Digesting the rest is a separate decision, taken
   after looking at these. — depends on: 5 — touches: nothing in the repo; writes
   `shared/history/2026-07-*/`
8. Retire what verified — delete only the archived `.gz` files whose session id has a
   digest entry with a non-empty `summary` and `outcome`. Anything not digested stays.
   The verification is run and reported first; the delete happens only on explicit
   go-ahead. — depends on: 7 — touches: nothing in the repo; removes files under
   `shared/archive/`

Waves: 1 → 2, 3, 4 in parallel → 5 → 6 → 7 → 8

Blocks 7 and 8 are operations, not code. They run after the feature is built and tested,
in that order, and 8 is the only irreversible step in this plan.

## Blockers

- launchd gives a minimal `PATH` and the archive plist sets no `EnvironmentVariables`
  (`internal/cmd/archive.go:105`). `codex` lives at `~/.local/bin/codex` and `pi` at an
  nvm path, so a scheduled digest run would fail to find them. The digest plist must set
  an explicit `PATH` or resolve the binaries absolutely at install time; `summarize.sh`
  exposes `CODEX_BIN` / `PI_RUNNER` / `NOTIFIER_BIN` as the seams. Stops block 5 only —
  blocks 1-4 are unaffected.

## Steps

- [x] Block 1 — digest contract (`internal/usage/digest.go`)
- [x] Block 2 — embedded pipeline runner (`internal/usage/digest_run.go`, `assets/`)
- [x] Block 3 — session enumeration + idempotency (`internal/usage/digest_scan.go`)
- [x] Block 4 — writer/merge (`internal/usage/digest_merge.go`)
- [x] Block 5 — `claudeme digest` command (`internal/cmd/digest.go`, `launchd.go`)
- [x] Block 6 — remove archive
- [x] Block 7 — done 2026-08-15: 353 of 355 archived sessions digested, the 2 left being
      timestamp-less stubs that can never be filed. See
      [`commands/scratch/2026-08-14_digest-backfill.md`](../commands/scratch/2026-08-14_digest-backfill.md)
- [x] Block 8 — done 2026-08-15 on the user's go-ahead: 353 transcripts and their 23
      `subagents/` directories removed, 150.1M freed, `shared/archive` down to 8K. The 2
      timestamp-less stubs stay. `history` and `heatmap` verified unchanged afterwards.
- [x] Install the daily job (`claudeme digest --install`) — installed 2026-08-15 after the
      backfill, pointing at `~/.local/bin/claudeme`

## Two fixes this took that the plan did not foresee

- **Bulk runs no longer notify per session.** One desktop notification per failure is right
  for the daily job and wrong for a 346-session backfill. `SUMMARIZE_NOTIFY=0` suppresses
  the popup and keeps the stderr line; `digest` sets it above `bulkNotifyThreshold` (5) and
  posts one notification with the tally instead.
- **A freshness gate (`usage.Settled`).** `--install` fires a run immediately via
  `RunAtLoad`, and 30 of the 62 sessions then in scope were dated that same day — live
  transcripts still being written. Since a digest is written once and never revisited,
  summarizing one mid-write would have recorded a partial account of the session
  permanently. Candidates whose transcript was touched within `SettleCooloff` (1h) are now
  left for a later run.

Trigger design was revisited before installing — launch-time, `SessionEnd` hook, on-read and
queue-based alternatives were considered and the daily job kept. See
[`pdr/2026-08-15_digest-trigger.md`](../pdr/2026-08-15_digest-trigger.md).

## Verification

### Automated

- `go build ./...` and `go vet ./...` clean.
- `go test ./internal/usage/...` green, including a digest test that writes two sessions
  into one date+project file and asserts both survive, then re-runs one session and
  asserts one entry, not two.
- `claudeme digest --dry-run --all` lists sessions and writes nothing — the history dir
  is unchanged afterwards.
- `claudeme digest --since 2026-08-13` run twice: the second run reports 0 new sessions
  and exits without a codex call.
- After a real run, `ls ~/.config/claudeme/shared/history/2026-08-13/` is non-empty and
  every file parses: `for f in ~/.config/claudeme/shared/history/*/*.json; do python3 -m
  json.tool "$f" >/dev/null || echo "BAD $f"; done` prints nothing.
- `grep -rn '"archive"' main.go` returns nothing; `grep -rn 'ArchiveRoot' internal/usage/`
  still shows it used by `Roots()`.
- `claudeme usage` reports the same totals for a pre-archive day before and after block 6.
- `claudeme digest --all --limit 5` digests exactly 5 sessions and leaves the rest
  untouched; a second `--limit 5` picks 5 *different* sessions (the already-digested ones
  are skipped).
- Block 8 gate — a `.gz` is deletable only if its session id has a digest entry whose
  `summary` and `outcome` are both non-empty. The gate is computed and printed per file;
  files failing it are not deleted.
- After block 8, `claudeme history` still lists 2026-07-04 → 2026-07-27 with unchanged
  numbers (they live in `usage-history.json`, which already covers 2026-07-03 → 2026-08-04).

### Manual

- `claudeme digest --install`, then `launchctl list | grep com.claudeme.digest` shows it
  loaded; after it fires, `~/.config/claudeme/digest.log` shows a completed run with no
  `command not found` (the PATH blocker).
- Open one generated `history/<date>/<project>.json` and confirm the summary describes the
  session recognisably. The AI step is the one thing no assertion can prove.

## Out of scope

- Deleting live transcripts. claudeme removes no `.jsonl` from `shared/projects/`; only
  Claude Code's own retention does. The one-off archive delete in block 8 is a manual
  operation, not a behaviour of any command.
- A tool to restore the archive into the live tree, or any `claudeme` subcommand that
  deletes transcripts.
- Changing `claudeme snapshot` or the shape of `usage-history.json`.
- Changing `schema.json`, `distill.py`, or `summarize.sh`'s model choice — the scripts are
  embedded verbatim. `summarize.sh` runs `gpt-5.6-luna` with no fallback, per
  `spike-summarize/docs/adr/2026-08-14_no-fallback-model.md`; a capped session simply stays
  undigested and is retried next run.
- Gating archive on digested-ness: considered and rejected, archive is going away instead.
- A size or cost threshold that skips trivial sessions: considered and rejected, every
  session gets digested.
- Any surface for reading digests back — no `claudeme digest --show`, no search. Writing
  the files is the deliverable.
- Linux/cron scheduling. `--install` stays macOS-only, as `internal/cmd/archive.go:141`
  already is.

## Amendments

- 2026-08-14 — block 7 in progress, state kept in
  [`commands/scratch/2026-08-14_digest-backfill.md`](../commands/scratch/2026-08-14_digest-backfill.md)
  so a later session resumes without re-deriving anything. 118/346 done, 2 failed. Both
  failures are codex `Selected model is at capacity`, a shape `is_cap_failure` did not
  recognise — fixed in both copies of `summarize.sh`, recorded in
  `spike-summarize/docs/learnings/2026-08-14_codex-capacity-vs-usage-limit.md`. Added
  `claudeme digest --archived`, without which the archived sessions could not be targeted
  separately from the 800+ live ones.

- 2026-08-14 — blocks 1-7 built. Deviations from the plan as written, all verified:
  - `distill.py` cannot read `.gz`, so `Runner.decompress` writes a plain copy through the
    existing `openTranscript` instead. The embedded scripts stay byte-identical to the
    spike, as the plan required.
  - `cwdProjects` in `internal/cmd/history.go:285` already disambiguated colliding project
    names by parent directory — better than this plan's assumption that they merge. It
    moved to `usage.ProjectNames` and both callers share it, so digests and
    `usage-history.json` file projects under identical names.
  - `bootstrap`, `bytes` and `treeSize` lived in the deleted `archive.go`; they moved to
    the new `internal/cmd/launchd.go` first. This was the required refactoring, unrecorded
    in the plan.
  - Scan found 1491 sessions (1139 live + 352 archived). The archive holds 502 `.gz`
    files; the other 150 are nested subagent transcripts, correctly excluded.
  - Block 7 result: 6 archived sessions digested across two runs, 0 failures, ~30s each.
    The second run picked 6 *different* sessions and skipped the first — idempotency
    holds against real data. Output is recognisable and schema-shaped; the remaining
    1485 sessions are still a ~12h serial job, so the rest is not started.

- 2026-08-14 — qwen fallback dropped from `summarize.sh` (see
  `spike-summarize/docs/adr/2026-08-14_no-fallback-model.md`). Block 5 gains `--limit N`,
  and blocks 7/8 now work in samples: digest a few archived sessions, judge the output,
  delete only what verified.
- 2026-08-14 — added blocks 7 and 8: digest the 502 already-archived transcripts, then
  delete `shared/archive/` once every one of them verifies. Block 3 now scans
  `usage.Roots()` instead of `ProjectsRoot()` — the archived July sessions are not in the
  live tree, so a live-tree-only scan would have skipped the only copy that exists.
