# Test Member labelling and ordering

`memberLabel` (`internal/usage/usage.go:105`) and the member sort in `cluster`
(`usage.go:313`, the `sort.SliceStable` near the end) shipped without tests.
`TestCluster` (`internal/usage/usage_test.go:30`) asserts only `Dirs`.

Cover:

- `./` for the project root itself.
- a plain relative path for a subdirectory (`portal-client`).
- `wt:<path-after--worktrees/>` for a worktree resolved back to the checkout.
- `wt:<basename>` fallback when the path has no `-worktrees` segment.
- ordering: root first, then descending file count, then label — the tie-break
  matters because two worktrees of the same branch series otherwise swap
  positions between runs.

Fold this into the per-cwd refactor (`001_001`) rather than doing it first —
the fixtures change shape there, and writing them twice is waste.
