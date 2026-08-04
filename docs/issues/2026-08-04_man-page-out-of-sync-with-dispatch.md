# The man page documents two commands that no longer exist

## Summary

`claudeme.1` documents `claudeme set <ALIAS|EMAIL>` (`:40`, example at `:197`) and
`claudeme current` with its `--dir` / `--short` flags (`:52`). Both were renamed —
to `use` and `me` — and neither old name is a dispatch case in `main.go`.

`main.go` forwards every unrecognised first argument to `claude`, so these do not error.
`claudeme set work` starts a Claude session with `args=set work`; `claudeme current --short`
starts one with `args=current --short`. Only `whoami` survived as an alias for `me`;
`me --dir` and `me --short` work as documented.

The reverse gap exists in the same file: `use`, `me`/`whoami`, `sessions`/`ss`, all four
`folder` subcommands, `tui`, `version` and `resolve` are implemented and undocumented.
`README.md` omits `hook`, `auto`, `version` and `resolve`, and lists `claudeme remove`
with no argument although one is required (`internal/cmd/profile.go`, `Remove`).

## Impact

Any script or statusline still calling `claudeme current --short` launches an interactive
Claude instead of printing a name. Silent: the exit status comes from `claude`.

## Workaround

Use `claudeme use` and `claudeme me`. `claudeme me --short` replaces `current --short`.

## Amendments

- (none)
