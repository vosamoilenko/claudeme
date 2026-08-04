# Raise cleanupPeriodDays so future history survives

Claude Code deletes transcripts after 30 days by default, which is why 122 of
160 projects have no spend data at all — see
`findings/002_thirty-day-retention-truncates-history.md`. Nothing recovers what
is already gone; this only protects what runs from now on.

## Do

Add `cleanupPeriodDays` to `~/.config/claudeme/shared/settings.json`. That file
is the one all three accounts share (`config.SharedDir()`,
`internal/config/config.go:64-66`), so a single edit covers every profile.
Neither it nor `~/.claude/settings.json` sets the key today.

```json
"cleanupPeriodDays": 3650
```

Insert it as a sibling of `"model"` / `"theme"` at the top level. Do not
reformat the rest of the file — it holds the user's hooks, statusLine and
enabledPlugins, and a stray rewrite is a much bigger diff than this warrants.

## Confirm the value with the user first

This is their retention policy, not a code decision. The tradeoff:

- **Disk.** 30 days currently costs 1254 files. A 10-year window means
  transcripts accumulate indefinitely; growth is roughly linear in sessions,
  and this tree is the same one `claudeme` symlinks across accounts.
- **Anything above ~365 is effectively "never delete".** If the user would
  rather cap it, 365 keeps a year of `usage` history at a twelfth of the
  unbounded growth.

Offer 3650 as the default and let them pick.

## Done when

- The key is present in `~/.config/claudeme/shared/settings.json` and the file
  is still valid JSON (`python3 -m json.tool < …` or `jq . <…`).
- The user has agreed to the number.
- No claim is made that anything was recovered. It wasn't.

## Not in scope

Teaching `claudeme` to manage this setting. A one-line JSON key does not need a
subcommand, and the shared-settings mechanism already makes it apply
everywhere.
