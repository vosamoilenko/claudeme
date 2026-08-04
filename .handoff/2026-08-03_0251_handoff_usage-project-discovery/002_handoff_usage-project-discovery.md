# 002 — Usage project discovery

**Plan:** none, still. The chain was opened without one by explicit user
instruction (see 001) and that has not changed. This file is the anchor. Do not
go write a plan, and do not widen past `Next`.

**Read the Files table below before opening anything else.** It is the map this
session already paid for; rediscovering it by search is the failure this
document exists to prevent.

## Files

Line ranges are current as of this handoff — `usage.go` grew ~90 lines this
session, so **every range in 001's table is stale**. Use these.

| Path | Lines | What's there | |
| --- | --- | --- | --- |
| `internal/cmd/usage.go` | 14-95 | `Projects` — header, rows, summary. Both label sites for the reporting window live here (48 header, 79-86 summary). | Read |
| `internal/cmd/usage.go` | 140-190 | `Usage` — the `"%d transcripts, %d sessions"` sub-header at 188 is the second place the window must appear. | Read |
| `internal/usage/usage.go` | 503-529 | `Report` + `newReport`. `Days` is keyed `YYYY-MM-DD`; min/max of its keys is the window, no new plumbing needed. | Read |
| `internal/usage/usage.go` | 745-760 | where the `day` key is derived from the entry timestamp — confirms `Days` covers every priced call. | Ref |
| `internal/usage/usage.go` | 594-634 | `Analyze` — per-dir walk, per-file cwd resolution, `rep.Files` accumulation. | Ref |
| `internal/usage/usage.go` | 190-236 | `scanDir` — groups a directory's transcripts by cwd. The per-cwd refactor's core. | Ref |
| `internal/usage/usage.go` | 120-137 | `transcriptDir` (now one per (dir,cwd)) + `at()`. | Ref |
| `internal/usage/usage.go` | 362-437 | `cluster` — dir-name dedup via `claimed`, member accumulation. | Ref |
| `internal/cmd/usage.go` | 97-129 | `memberCost` + `printMembers` — why sub-rows sum to the project row. | Ref |
| `internal/usage/usage_test.go` | 94-225 | `TestClusterMembers`, `TestScanDirGroupsByCwd`, `TestAnalyzeSplitsByCwd` — the fixture patterns to copy. | Ref |
| `internal/config/config.go` | 64-66 | `SharedDir()` — resolves `~/.config/claudeme/shared`, if todo 002_001 is done in code rather than by hand. | Ref |

## State

001's `Next` is **done**: discovery is per-cwd, `Analyze` splits spend by cwd,
and `TestCluster`'s successor fixtures cover member labelling and ordering.
`go vet` + `go test ./...` pass. Still uncommitted, still 0.3.0.

The session then failed a user challenge to the numbers, and the challenge was
right about something the tool never said: **the report only ever covers the
last 30 days** — see `findings/002_thirty-day-retention-truncates-history.md`.
That is the whole of `Next`, both halves of it.

## Decisions

- **`Analyze` gained a per-cwd breakdown (`Report.Cwds`), not a cwd filter.**
  This was 001's open design question. Dedupe by `requestId` is global, so
  costing each member with its own `Analyze` call would count a record replayed
  across a `cd` once per cwd it touched. One pass bucketing each row by its
  file's cwd makes sub-rows sum to the project row *by construction*, and cuts
  `projects --cost` from N+1 analyses per project to 1.
- **Project cost is the sum of the cwds a project owns (`memberCost`), not
  `rep.Total` over its directories.** They are equal today — verified, see
  Findings — but they diverge the moment one transcript directory's cwds
  cluster into two different projects, and then both projects would charge for
  the whole directory. Costing by cwd makes the invariant structural.
- **`cluster` claims each directory name once per project.** A directory now
  produces several `transcriptDir` entries; without the `claimed` set the name
  would land in `p.Dirs` repeatedly and `Analyze` would read it twice.
- **`transcriptDir` splits `files` from `sessions`.** The recursive walk pulls
  in `subagents/*.jsonl`, which are not sessions; counting them as such
  inflated every session count. Top-level transcripts only for `sessions`.
- **`mainCheckout` is memoized.** It shells out to git and the same cwd now
  arrives once per transcript directory that mentions it.
- **No code was needed for 001's todo step 4 (`.claude/worktrees/agent-*`).**
  Those worktrees are *inside* the repo, so the existing ancestor rule in
  `cluster` already folds them into the project; they now show as members with
  their relative path as label. Verified in the live output under both
  `mitarbeiterportal` and `snitchcam`.
- **Retention is left alone until the user acts on it.** Raising
  `cleanupPeriodDays` recovers nothing already deleted; it only protects future
  history. That is todo 002_001 and it is deliberately not done yet.

## Amendments

001 and its findings assert things this session disproved or completed.

- **001's Files table line ranges are all stale.** Use this file's table.
- **`findings/001_cwd-per-transcript-dir.md` is resolved.** The 22 invisible
  cwds are visible; discovery now finds all **72** cwds that have transcripts,
  against 50 before. The finding stays as the record of the defect, not as open
  work.
- **`todos/001_001_per-cwd-discovery.md` and `todos/001_002_untested-members.md`
  are both done.** Their "Done when" criteria were checked against live data,
  not just tests: `backend` appears under `phishen-impossible`, `app` under
  `snitchcam`, and no money moved.
- **`findings/001_accounts-share-one-projects-root.md` is true but was read too
  broadly.** Its account claim is confirmed independently this session — all
  three accounts symlink to one root, `~/.claude/projects` is empty, no
  transcripts exist anywhere else on disk. But 001's headline, *"no projects are
  missing across accounts"*, was taken through the rest of that session as *"no
  projects are missing"*. That is false: 122 directories have no transcripts at
  all. Accounts were never the reason anything was missing — retention is.
- **001's "grand total must stay ~$3129" is superseded as an acceptance test.**
  The total moves by several dollars an hour while sessions run; it drifted
  $3142.07 → $3146.70 during this session alone. Use the invariants in
  `findings/002_no-money-moved-in-the-per-cwd-refactor.md` instead.

## Findings

- `findings/002_thirty-day-retention-truncates-history.md` — **the reason the
  report is smaller than reality.** 160 directories opened, 72 with
  transcripts, 122 with none, zero files older than 30 days. Drives both todos.
- `findings/002_no-money-moved-in-the-per-cwd-refactor.md` — how the refactor
  was proved cost-neutral, and the two throwaway tests that did it. Re-run them
  before believing any future attribution change.
- `findings/001_cwd-per-transcript-dir.md` — resolved, see Amendments.
- `findings/001_accounts-share-one-projects-root.md` — narrowed, see Amendments.

## Next

Do **both**, in this order:

1. `todos/002_001_raise-cleanup-period.md` — stop the bleeding first. It is a
   settings change, it is cheap, and every hour it waits is history lost.
2. `todos/002_002_label-the-reporting-window.md` — then make the output stop
   implying all-time.

Start with 002_001. Confirm the value with the user before writing it: it is
their retention policy, not a code decision, and the todo lists the tradeoff.

## Out of scope

- Committing or tagging. Everything stays uncommitted by the user's standing
  preference. The pre-existing edits in `profile.go`, `config.go`,
  `migrate.go`, `docs/` are **not** part of this work.
- Recovering the 122 deleted projects. The transcripts are gone from disk;
  `.claude.json` keeps only directory paths and history, no token counts. Do
  not go looking for a backup — `~/.claude/backups/` and
  `shared/backups/` hold `.claude.json` copies only, already checked.
- Re-deriving whether the totals are correct. They were verified against an
  independent recompute this session; see the findings file.
- The `PER MODEL` table column collision (`internal/cmd/usage.go:258`,
  `printTable`) — still real, still cosmetic, still unrelated.
- `internal/ui/ui.go` fails `gofmt -l`. Pre-existing, untouched, not this
  chain's problem.

## Skills

None required. Go work in two files plus one JSON settings edit.
