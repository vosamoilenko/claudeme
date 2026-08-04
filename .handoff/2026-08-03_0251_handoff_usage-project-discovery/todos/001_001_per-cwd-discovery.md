# Make discovery per-cwd instead of per-directory

Invert the unit of discovery so every cwd that ever ran a session becomes a
`Member`, instead of one cwd per transcript directory. See
`findings/001_cwd-per-transcript-dir.md` for the measurement and the list of
22 cwds currently invisible.

## Shape

1. `scanDir` (`internal/usage/usage.go:161`) returns a `map[string]dirStats`
   keyed by cwd — files and mtime per cwd — instead of a single `cwd` + `files`
   + `modified`. It must walk nested transcripts too (`subagents/`), not just
   the top level, because those carry their own `cwd`. `transcripts()`
   (`usage.go:560`) already does that walk; reuse it rather than writing a
   second one.
2. `transcriptDir` (`usage.go:118`) becomes one entry per (directory, cwd)
   pair. `Discover` (`usage.go:136`) emits several entries per directory.
   `at()` and `cluster()` need no logic change — they already key on cwd.
3. `Analyze` takes directory names, so a `Member` whose cwd shares a directory
   with another member cannot be costed by directory alone. Either give
   `Analyze` a cwd filter, or accept per-directory granularity for member cost
   and say so in the sub-row. **Decide this before writing code** — it is the
   only genuinely new design question in the task.
4. Add `.claude/worktrees/agent-*` to the worktree collapse: those are subagent
   worktrees *inside* the repo, so `mainCheckout` (`usage.go:233`) resolves any
   that still exist, but deleted ones need a path rule next to
   `siblingCheckout` (`usage.go:266`).

## Done when

- `claudeme projects --cost` shows a `backend` sub-row under
  `phishen-impossible` and an `app` sub-row under `snitchcam`.
- Every project's sub-rows still sum to its project row, and the grand total is
  unchanged from today's `$3129`-ish figure — this refactor must not move any
  money, only relabel it.
- `go test ./...` passes with `TestCluster` fixtures updated to the new shape.
