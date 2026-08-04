# Print the date range the report actually covers

`projects --cost` ends with `27 projects   $3146.70` and `usage` opens with
`1249 transcripts, 913 sessions`. Both read as all-time. Both cover only the
retention window — 30 days today, whatever `002_001` sets tomorrow. See
`findings/002_thirty-day-retention-truncates-history.md`.

## Where the data already is

`Report.Days` (`internal/usage/usage.go:503-529`) is keyed `YYYY-MM-DD` and is
bumped for every priced call (`usage.go:751`). Min and max of its keys is the
window. No new plumbing, no second pass over the transcripts.

Add a helper beside `Rank` — `func Window(rep *Report) (first, last string)` —
returning `"", ""` for an empty report so callers can skip the label rather
than print an empty range. Sorting the keys is enough; they are
lexicographically ordered by construction.

## Two call sites

- **`Usage`** (`internal/cmd/usage.go:140-190`): extend the sub-header at line
  188 — `1249 transcripts, 913 sessions, Jul 04 – Aug 03`. It already has a
  `rep` in hand.
- **`Projects`** (`internal/cmd/usage.go:14-95`): the summary at 79-86. Only
  reachable with `--cost`, which is the only mode that computes a `rep` at all
  — without it there are no `Days` to draw on, and the row costs are absent
  anyway, so printing nothing is right. Accumulate the min/max across the
  per-project reports in the existing loop rather than running a second
  `Analyze`.

## Say why, not just when

A bare date range still reads as "this is when you worked". It isn't — it is
when the surviving transcripts start. One dim line under the summary:

```
  transcripts older than <N>d are deleted by Claude Code; earlier spend is not recoverable
```

Read `cleanupPeriodDays` from the shared settings for `<N>` if it is cheap to
reach; otherwise phrase it without the number rather than hardcoding 30, which
`002_001` is about to change.

## Done when

- `claudeme usage` and `claudeme projects --cost` both show the covered range.
- A single-project query shows *that project's* range, not the global one —
  `usage phishen` should say Jul 04 – Aug 03 only if that project ran across
  it.
- An empty or `--all`-with-no-cost run prints no range and does not crash.
- `go test ./...` passes, with a test for `Window` covering the empty report
  and a multi-day one. `TestAnalyzeSplitsByCwd`
  (`internal/usage/usage_test.go:185`) shows the transcript-fixture pattern —
  its entries already carry a `timestamp`.
