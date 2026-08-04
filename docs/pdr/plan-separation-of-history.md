# Plan: Separation of History + Handoff Between Accounts

## Problem

`history.jsonl` is symlinked to `shared/` — both accounts see each other's full prompt history. This leaks:
- Work project details to personal account (and vice versa)
- Credentials pasted in prompts
- Internal URLs, code snippets, project structures

## Solution

Move `history.jsonl` from shared to per-account. Add a `claudeme handoff` command that exports current session context so the other account can pick up where you left off.

## Changes

### 1. Move `history.jsonl` to per-account

**File:** `internal/config/config.go`

- Remove `"history.jsonl"` from `sharedItems`
- Add `"history.jsonl"` to `perAccountItems`

**Migration:** `SetupAccountSymlinks` already handles this — on next run it will skip creating the symlink. Need a one-time migration to:
1. Copy current `shared/history.jsonl` into each account dir
2. Remove the symlink from each account dir
3. Optionally scrub work entries from personal copy and vice versa

### 2. Add `claudeme handoff` command

**New file:** `internal/cmd/handoff.go`

```
claudeme handoff           # export current session for the other account
claudeme handoff --pick    # import a pending handoff into current session
```

**Flow:**

1. `claudeme handoff` (export):
   - Reads the most recent session from current account's `sessions/` dir
   - Runs `claude --print-conversation` or reads the session JSONL directly
   - Summarizes into a compact markdown (like the /handoff skill output)
   - Saves to `shared/handoffs/<timestamp>-<source-email>.md`

2. `claudeme handoff --pick` (import) — or auto-detect on launch:
   - Checks `shared/handoffs/` for pending files not from current account
   - Injects as `--context` flag when launching claude
   - Moves consumed handoff to `shared/handoffs/consumed/`

### 3. Auto-detect on launch (optional)

In `LaunchClaude()`, before spawning `claude`:
- Check `shared/handoffs/` for pending handoffs from the other account
- If found, prompt: "Pending handoff from <email>. Pick up? [Y/n]"
- If yes, append `--context <handoff-file>` to claude args

### 4. Shared handoffs directory

Add `"handoffs"` to `sharedItems` so both accounts can read/write handoff files.

## File Changes Summary

| File | Change |
|------|--------|
| `internal/config/config.go` | Move `history.jsonl` shared→per-account, add `handoffs` to shared |
| `internal/cmd/handoff.go` | New — handoff export/import logic |
| `internal/cmd/claude.go` | `LaunchClaude` — check for pending handoffs |
| `main.go` | Add `case "handoff"` to switch |
| `README.md` | Document `handoff` command |

## Migration Steps (one-time)

```bash
# For each account:
cd ~/.config/claudeme/accounts/<email>
rm history.jsonl                          # remove symlink
cp ../../shared/history.jsonl ./          # copy full history (or start fresh)

# Create handoffs dir
mkdir -p ~/.config/claudeme/shared/handoffs/consumed
```

## Open Questions

- Should handoff auto-summarize (compact) or dump raw conversation?
  - Recommendation: use claude's `/handoff` skill output format — it's already designed for this
- Should we also split `telemetry/` per-account?
  - Lower priority but same leak vector
- Max handoff file size / retention policy?
