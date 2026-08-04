# How the per-cwd refactor was proved cost-neutral

001's acceptance test — *"the grand total is unchanged from today's $3129-ish
figure"* — cannot be used as written. The total climbs while sessions run; it
moved $3142.07 → $3146.70 during the session that did the refactor, none of it
from the refactor. Compare invariants instead.

## The three that hold

**1. Every transcript on disk is read.**

```sh
claudeme usage | head -2        # "1249 transcripts, 913 sessions"
find ~/.config/claudeme/shared/projects -name '*.jsonl' ! -name journal.jsonl | wc -l   # 1249
```

`journal.jsonl` is excluded by design (workflow bookkeeping, no API calls), so
1254 files on disk minus 5 journals is the 1249 the tool reports.

**2. The arithmetic matches an independent implementation.**

A standalone Python recompute over the same tree — same pricing table, same
`requestId`-then-`uuid` dedupe, same cache multipliers — gave **$3146.99**
against the tool's **$3146.70**. The gap is live spend between the two runs
plus per-call truncation: cost is accumulated in whole micro-dollars, so each
of 36,234 calls can lose up to 1µ$, bounding truncation at ~$0.036. Both runs
independently found the same 28 `<synthetic>` calls as unpriced.

**3. No directory is charged twice, and members partition their project.**

These are the two ways the refactor could silently move money, and both were
checked against the live tree with throwaway tests in `internal/usage/`
(written, run, deleted — recreate them if attribution changes again):

```go
// no dir claimed by two projects: 44 projects, 69 dir claims, 69 unique
for _, p := range ps { for _, d := range p.Dirs { /* owner[d] must be unset */ } }

// members sum to the directory total, for every project
for _, p := range ps {
    rep, _ := Analyze(ProjectsRoot(), p.Dirs)
    var sum int64
    for _, m := range p.Members { if s := rep.Cwds[m.Cwd]; s != nil { sum += s.Cost } }
    // sum == rep.Total.Cost
}
```

Both pass. The second is why `memberCost` (`internal/cmd/usage.go:97`) is safe
to use as the project row: it equals `rep.Total` today, and stays correct if a
directory's cwds ever split across two projects, where `rep.Total` would not.

## Why per-project dedupe is still sound

`Analyze` is called once per project, so the `seen` set is per-project, not
global. A `requestId` appearing under two different projects would be counted
in both. It does not happen today — no directory is shared between projects
(invariant 3) — and if it ever does, the cost lands on the cwd that owns it,
because `Report.Cwds` buckets by the transcript's own cwd rather than by the
directory it was found in.

## Spot-check the rendering too

Sub-rows must sum to the row above them, to within display rounding:

```
phishen-impossible   $989.33  =  510.71 + 452.58 + 22.43 + 3.60   (989.32, 1¢ rounding)
infinite-canvas      $256.28  =  207.90 + 28.20 + 10.80 + 4.40 + 2.61 + 2.37
mitarbeiterportal    $308.80  =  62.17 + 223.60 + 9.68 + 6.87 + 3.83 + 1.80 + 0.84
snitchcam            $110.06  =  95.25 + 13.83 + 0.98
```

The cent-level gaps are `%.2f` rounding of each sub-row, not lost money — the
underlying micro-dollar sums are exact, which is what test 3 asserts.
