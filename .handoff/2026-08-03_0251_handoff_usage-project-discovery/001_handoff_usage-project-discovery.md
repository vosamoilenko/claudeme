# 001 — Usage project discovery

**Plan:** none. The user explicitly started this chain without one
(`/handoff-smk "without plan, i want to be sure that you found all projects
across all accounts"`). This handoff is the anchor; do not go write a plan
first, and do not widen the task beyond what `Next` says.

**Read the Files table below before opening anything else.** It is the map this
session already paid for; rediscovering it by search is the failure this
document exists to prevent.

## Files

| Path | Lines | What's there | |
| --- | --- | --- | --- |
| `internal/usage/usage.go` | 161-186 | `scanDir` — keeps ONE cwd per directory. This is the defect. | Read |
| `internal/usage/usage.go` | 118-133 | `transcriptDir` + `at()` — the struct that must become per-(dir,cwd) | Read |
| `internal/usage/usage.go` | 136-157 | `Discover` — builds one `transcriptDir` per directory | Read |
| `internal/usage/usage.go` | 313-393 | `cluster` + `memberAt` — groups by cwd already; needs no logic change | Read |
| `internal/usage/usage.go` | 81-116 | `Project`, `Member`, `memberLabel` — the output shape | Read |
| `internal/usage/usage.go` | 532-580 | `Analyze` + `transcripts` — takes dir names, walks nested transcripts | Read |
| `internal/usage/usage.go` | 226-286 | `mainCheckout` / `siblingCheckout` — worktree resolution, needs the `.claude/worktrees` case | Ref |
| `internal/cmd/usage.go` | 87-118 | `printMembers` + `truncateTail` — renders the sub-rows | Ref |
| `internal/usage/usage_test.go` | 30-93 | `TestCluster` — fixtures whose shape the refactor changes | Ref |
| `internal/usage/usage_test.go` | 171-232 | `TestMainCheckout`, `TestSiblingCheckout` — the git-fixture pattern to copy | Ref |

## State

The Go port of `usage`/`projects` is complete, installed as **0.3.0**, and all
of it is **uncommitted**. `go vet` + `go test ./...` pass. Delivered across this
session: subagent cost attribution, worktree clustering, and per-cwd `Member`
sub-rows under each project.

One defect is known and unfixed: discovery sees one cwd per transcript
directory, so 22 real cwds never appear — `findings/001_cwd-per-transcript-dir.md`.
Costs are correct; only attribution is wrong. That is the whole of `Next`.

## Decisions

- **Subagent spend lives in `<session>/subagents/agent-*.jsonl`, not in the
  session transcript.** An earlier belief in this session — that subagent cost
  was simply unattributable because no `isSidechain` line carried a `usage`
  block — was wrong. The files were never opened. `Analyze` now walks
  recursively. Zero `requestId` overlap with parent transcripts, so this is
  recovered money, not double-counting (phishen $921 → $984).
- **`journal.jsonl` is excluded** from the walk: workflow bookkeeping, no API
  calls.
- **Agent type comes from the sibling `agent-*.meta.json`**, not from the
  transcript. Drives the `PER AGENT TYPE` table.
- **Worktrees resolve via `git rev-parse --git-common-dir`**, not by path
  string — a worktree is a *sibling* of its repo, so no ancestor rule can ever
  reach it. `siblingCheckout` is the fallback because the common case is a
  worktree deleted long before its transcripts (3 of 4 here).
- **`mainCheckout` resolves symlinks on the cwd before comparing** to git's
  answer. Without it macOS `/var` → `/private/var` makes every path look like a
  worktree. A test caught this; keep the test.
- **Model ids are normalized** (`[1m]` suffix, date suffixes, bare `opus`).
  Unpriced models (`<synthetic>`, `gpt-*`) are reported at the bottom rather
  than silently dropped — this is a deliberate deviation from the old Python
  script, and the source of small cost differences against it.
- **Version stayed at 0.3.0** for the `Member` sub-rows: same unreleased,
  uncommitted change set as the worktree work. Bump on the next release, not
  per-feature.
- **`~`, `~/.claude`, `~/Developer` remain one-row projects.** They are
  containers, but sessions ran directly in them and that is real spend ($99 in
  `~`). Considered and rejected: hiding them, or bucketing as `(ad-hoc)`.

## Findings

- `findings/001_accounts-share-one-projects-root.md` — **the chain's opening
  question, answered: no projects are missing across accounts.** All three
  accounts symlink to one shared root.
- `findings/001_cwd-per-transcript-dir.md` — the real gap: 22 cwds invisible,
  with the reproduction script and the exact list.

## Next

Make discovery per-cwd instead of per-directory, following
`todos/001_001_per-cwd-discovery.md`. Start by deciding step 3 of that todo —
whether `Analyze` gains a cwd filter — because it determines whether member
costs stay directory-granular.

Fold `todos/001_002_untested-members.md` into the same change; its fixtures
change shape in the refactor and writing them twice is waste.

## Out of scope

- Committing or tagging. Everything is uncommitted by the user's standing
  preference; there are also pre-existing uncommitted edits in `profile.go`,
  `config.go`, `migrate.go`, `docs/` that are **not** part of this work.
- The `PER MODEL` table column collision (`14,847,7682,385,587,314` — output
  and cache-read run together in `printTable`, `internal/cmd/usage.go:238`).
  Real but cosmetic, and unrelated to discovery.
- Re-deriving whether subagent spend is attributable. It is; it was measured.
- Writing a plan for this chain. The user declined one.

## Skills

None required. Plain Go work in one package.
