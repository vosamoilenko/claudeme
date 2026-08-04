# Per-shell profile override via `$CLAUDEME_PROFILE`

## Context

`claudeme use` writes `active` to `~/.config/claudeme/profiles.json`, and the shell hook
exports `CLAUDE_CONFIG_DIR` from that one field. Every terminal therefore follows the
same account, and two terminals cannot hold two accounts: switching in one changes the
other's next launch.

Two constraints shape the fix. A binary cannot export a variable into its parent shell,
so a per-shell switch cannot be pure Go. And the user's `~/.zshrc` aliases (`cc`, `cc5`)
expand to `claudeme <claude-args...>` with no slot for a profile argument, so any
mechanism that requires the profile as `argv[2]` does not compose with them.

## Options Considered

### A — one config directory per terminal, copied at launch

Full isolation, including `history.jsonl`. Lost: it multiplies auth state and breaks the
shared-session design in `README.md`, for a problem that is only about which account
signs the request.

### B — a per-shell environment variable, read by the binary

`$CLAUDEME_PROFILE` is per-shell by construction, needs no new state on disk, and is
already in the environment of anything the shell launches — including alias expansions
like `cc5`. Won.

## Decision

Resolve the profile as `as`-argument, then `$CLAUDEME_PROFILE`, then `active`; expose the
variable as `claudeme pin` / `claudeme unpin` in the shell hook, and `claudeme as` for a
single launch that changes no state.

## Consequences

- `cc5` and every other alias inherit the pin with no change to `~/.zshrc`.
- A pinned shell must opt out of auto-switching, or a `cd` would fight the pin — `Auto` returns early when the variable is set (`internal/cmd/auto.go:17`).
- `pin`/`unpin` work only with the hook loaded, and only in shells started after it changed. Already-open terminals need `exec zsh`.
- The pinned name has to be validated by the binary (`claudeme resolve`) because the hook cannot resolve aliases itself without duplicating the alias file format in `sed`.
- Sessions, projects and `history.jsonl` stay shared, so two concurrent accounts append to one history file. Not isolated, by design.

## References

[Profile resolution](../architecture/2026-08-04_profile-resolution.md)

## Amendments

- (none)
