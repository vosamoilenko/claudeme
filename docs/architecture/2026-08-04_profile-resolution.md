# Profile resolution

## Summary

Every launch path ends in one decision: which directory `CLAUDE_CONFIG_DIR` points at.
Three inputs can decide it, in a fixed order:

1. `claudeme as <alias|email>` — explicit, one invocation only, never written to disk.
2. `$CLAUDEME_PROFILE` — the pinned shell. Set per terminal, overrides the active profile.
3. `active` in `~/.config/claudeme/profiles.json` — global, shared by every shell.

Aliases resolve to emails at every layer through `ResolveProfile`, so an alias and an
email are interchangeable everywhere a profile is named. An unknown name is a hard
error; it never falls back to the active profile.

Two terminals run two accounts because layers 1 and 2 are per-process and per-shell,
while only layer 3 is global state.

## Modules

- `internal/cmd/as.go:11` — `ResolveProfile`: alias-or-email → known email, or error.
- `internal/cmd/as.go:30` — `Resolve`: prints that email; the shell hook calls it to validate a pin.
- `internal/cmd/as.go:49` — `As`: launches with one profile, leaves `active` untouched.
- `internal/cmd/claude.go:138` — `LaunchClaude`: `$CLAUDEME_PROFILE` first, then `active`.
- `internal/cmd/claude.go:164` — `runClaude`: the only place `CLAUDE_CONFIG_DIR` is set on the child.
- `internal/cmd/auto.go:14` — `Auto`: returns immediately in a pinned shell, so directory rules cannot move it.
- `internal/cmd/auto.go:382` — hook `_claudeme_sync_env`: exports the pinned profile, or the active one.
- `internal/cmd/auto.go:416` — hook `pin` / `unpin`: shell-only, they set `$CLAUDEME_PROFILE`.
- `main.go:55` — `pin`/`unpin` reaching the binary means the hook is not loaded; error, not forwarded.

## Boundaries

- Only `runClaude` sets `CLAUDE_CONFIG_DIR` for the `claude` child process. No other path spawns it.
- `pin` and `unpin` exist only in the shell hook — a binary cannot export into its parent shell.
- A pinned shell is exempt from auto-switching: no switch, and no mismatch warning either.
- Unrecognised first arguments are forwarded to `claude`, so a name that is not a dispatch case silently becomes claude arguments — see [man page out of sync with dispatch](../issues/2026-08-04_man-page-out-of-sync-with-dispatch.md).

## Data flow

`cd` → zsh `chpwd` → `claudeme auto` → pinned shell? stop : rule/folder match → `export CLAUDE_CONFIG_DIR` in the shell.

`claudeme` (or a shell alias like `cc5`) → `LaunchClaude` → `$CLAUDEME_PROFILE` else `profiles.json:active` → `ProfileDir(email)` → `runClaude` → `claude` child with `CLAUDE_CONFIG_DIR` in its env.

`claudeme as x` → `ResolveProfile` → `runClaude`. `active` is neither read nor written.

## Amendments

- (none)
