# claudeme

A fast account switcher for [Claude Code](https://docs.anthropic.com/en/docs/claude-code). Manage multiple Anthropic accounts with shared sessions/settings and automatic profile switching based on your working directory.

## Install

```sh
go install github.com/vosamoilenko/claudeme@latest
```

Or build from source:

```sh
git clone https://github.com/vosamoilenko/claudeme.git
cd claudeme
make install   # installs to ~/.local/bin
```

## Quick start

```sh
# Add your first account (opens claude login)
claudeme add

# Add a second account
claudeme add

# Give them friendly names
claudeme alias add work alice@company.com
claudeme alias add personal bob@gmail.com

# Switch accounts
claudeme use work

# Launch claude with the active profile
claudeme
```

## Shell hook

Add to your `.zshrc` or `.bashrc` to keep `CLAUDE_CONFIG_DIR` in sync and enable auto-switching:

```sh
eval "$(claudeme hook)"
```

This gives you:
- `$CLAUDE_CONFIG_DIR` always points to the active profile
- `$CLAUDE_PROFILE` shows the current alias or email
- Automatic profile switching when you `cd` into directories with rules

### Recommended aliases

```sh
alias cc='claudeme'
alias ccyolo='cc --dangerously-skip-permissions \
  --plugin-dir /path/to/your/skills \
  --model "claude-opus-4-6[1m]" \
  --thinking medium'
alias ccc="cc --continue"
alias ccd="cc --debug"
```

## Commands

### Profile management

| Command | Description |
|---|---|
| `claudeme` | Launch claude with the active profile |
| `claudeme add` | Add an account (opens `claude login`) |
| `claudeme list` (`ls`) | List all accounts |
| `claudeme use <alias\|email>` | Switch active account |
| `claudeme me` (`whoami`) | Show active profile directory |
| `claudeme remove <alias\|email>` (`rm`) | Remove an account |
| `claudeme reset` | Remove all accounts |
| `claudeme tui` | Interactive TUI picker |

### Two accounts at once

`use` is global state — every terminal follows it. To run two accounts side by side, override per shell:

```sh
# terminal 1
claudeme as work

# terminal 2
claudeme as personal
```

`as` launches claude with that account and leaves the active profile alone.

To pin a whole terminal (so bare `claudeme`, aliases like `cc`, and `$CLAUDE_CONFIG_DIR` all follow it), use `pin` — it needs the shell hook:

```sh
claudeme pin personal   # this shell only
claudeme                # → personal
claudeme unpin          # back to the global active profile
```

Under the hood `pin` just sets `CLAUDEME_PROFILE`, so `CLAUDEME_PROFILE=work claudeme` works too. A pinned shell ignores auto-switch rules and mismatch warnings.

Sessions, projects, and settings stay shared, so both terminals see the same history.

| Command | Description |
|---|---|
| `claudeme as <alias\|email> [args...]` | Launch claude with one account, don't switch |
| `claudeme pin <alias\|email>` | Pin the current shell to an account |
| `claudeme unpin` | Unpin the current shell |

### Aliases

| Command | Description |
|---|---|
| `claudeme alias add <name> <email>` | Create alias for an account |
| `claudeme alias list` | List aliases |
| `claudeme alias rm <name>` | Remove alias |

### Auto-switch

Set up rules so claudeme automatically switches profiles when you enter a directory.

| Command | Description |
|---|---|
| `claudeme rule add <pattern> <alias\|email>` | Add auto-switch rule |
| `claudeme rule list` | List rules |
| `claudeme rule rm <pattern>` | Remove rule |
| `claudeme folder list` | List remembered folder mappings |
| `claudeme folder set <alias\|email>` | Set profile for current directory |
| `claudeme folder rm [path]` | Remove folder mapping |
| `claudeme folder clear` | Clear all folder mappings |
| `claudeme config auto_apply <on\|off>` | Toggle auto-apply (default: off) |

**Example:**

```sh
claudeme rule add ~/work/company work
claudeme config auto_apply on
# Now `cd ~/work/company/any-repo` automatically switches to the "work" profile
```

When `auto_apply` is off, claudeme warns about mismatches instead of switching.

### Sessions

```sh
claudeme sessions   # list recent sessions for the current project
claudeme ss         # shorthand
```

### Usage reports

Transcripts live in one shared directory, one folder per working directory. `projects` clusters those folders back into projects — nested subdirectories, prototypes, and worktree copies fold into the checkout they belong to.

```sh
claudeme projects            # what projects exist, sessions, last activity
claudeme projects --cost     # cost column, plus per-day and per-skill spend
claudeme projects --all      # include transcript dirs with no sessions

claudeme usage               # report across all projects
claudeme usage phishen       # one project (exact name wins, else substring)
claudeme usage phishen --top 50
```

`usage` breaks spend down by day, model, skill, session, main-loop vs subagent, and agent type, and counts tool calls.

Two things to know about the numbers:

- **Cost is public list pricing.** On a Max/Pro subscription the real invoice is $0 — read these as "what this would have cost on the API". Models with no list price (synthetic messages, non-Anthropic) are excluded and reported at the bottom.
- **Records are deduped by `requestId`.** Resuming a session replays earlier turns into the new transcript, so raw line counts double-count.

Cache tokens are priced at the input rate times 2× (1h write), 1.25× (5m write), and 0.1× (read).

Every report prints the date range it actually covers. That range is bounded by what is still on disk, not by when you worked — see below.

### Archiving transcripts

Claude Code deletes transcripts from its projects tree after `cleanupPeriodDays` (30 by default), and deleted history is gone: nothing else on disk records what a session spent. `archive` moves old transcripts into a gzipped mirror at `~/.config/claudeme/shared/archive/`, where the cleanup can never reach them. Reports read both trees, so history gets deeper over time instead of truncating.

```sh
claudeme archive              # move transcripts older than 7d into the archive
claudeme archive --days 30    # keep a month in the live tree instead
claudeme archive --dry-run    # what would move, and how much space it uses
claudeme archive --status     # schedule, plus the size of each tree
claudeme archive --install    # run it daily at 04:00 (launchd, macOS)
claudeme archive --uninstall  # remove the daily job
```

Archived sessions are readable by `usage` but not resumable by Claude Code — the live tree is what `--resume` scans, so keep `--days` at least as long as you expect to resume something.

Two things `archive` deliberately does not do: it never touches a transcript newer than the cutoff, and it never removes a transcript directory that was already empty — those empty directories are the last trace of projects whose history was deleted before archiving started.

### Long-term usage history

`archive` keeps the transcripts; `snapshot` keeps only the numbers. It reads every transcript still on disk, extracts per-day aggregates, and upserts them into one file — `~/.config/claudeme/shared/usage-history.json`. Nothing is copied or moved, and the file is a few KB a day, so it can outlive any transcript by years.

```sh
claudeme snapshot               # extract today's numbers (and backfill anything new)
claudeme snapshot --dry-run     # what it would record
claudeme snapshot --install     # record it at 03:00 and 15:00 (launchd, macOS)
claudeme snapshot --uninstall   # remove the job
claudeme snapshot --status      # schedule, days on record, file size

claudeme history                # last 30 recorded days: cost, calls, sessions
claudeme history --all          # everything on record
claudeme history --by project   # sum the range by project (or model, or skill)
claudeme history --json         # the raw records, for your own analysis
```

Each day holds its total plus the same total split by model, project and skill:

```json
"2026-08-02": {
  "cost": 568.40, "calls": 7167, "in": 2036, "out": 4292610,
  "cacheRead": 623884251, "cacheWrite": 34212805, "sessions": 224,
  "models":   { "claude-opus-5": { "cost": 402.11, "calls": 5210, ... } },
  "projects": { "phishen-impossible": { "cost": 331.02, "calls": 4188, ... } },
  "skills":   { "(no skill)": { "cost": 291.74, "calls": 3901, ... } }
}
```

Re-running is safe by design. Transcripts are append-only, so a full rescan of what is on disk plus a per-day upsert is idempotent — no ingest cursor, no double counting, and an interrupted run is fixed by the next one. A stored day is only overwritten when the rescan finds *at least as much* spend: once transcripts start ageing out, a lower number is loss, not news, and the recorded value wins. Days that shrink are reported, not silently kept.

Sessions are counted per day, so one running over midnight counts on both. The costs don't double-count — every record is deduped by `requestId` across the whole scan.

### Forwarding args to claude

Any unrecognized arguments are forwarded directly to `claude`:

```sh
claudeme --dangerously-skip-permissions
claudeme --resume abc123
```

## How it works

Each account gets a directory under `~/.config/claudeme/accounts/<email>/` that serves as its `CLAUDE_CONFIG_DIR`. Only auth (`.claude.json`) stays per-account — sessions, projects, history, settings, and plugins are shared across all accounts via symlinks to `~/.config/claudeme/shared/`.

```
~/.config/claudeme/
├── shared/              # one copy of all shared data
│   ├── projects/
│   ├── sessions/
│   ├── history.jsonl
│   ├── settings.json
│   └── ...
├── accounts/
│   ├── alice@company.com/
│   │   ├── .claude.json              # real (auth)
│   │   ├── projects -> ../../shared/projects  # symlink
│   │   └── ...
│   └── bob@gmail.com/
│       ├── .claude.json
│       └── ...
```

Auto-switch priority:
1. **Folder mappings** - exact directory-to-profile associations (created via `claudeme folder set` or automatically when running `claudeme use`)
2. **Rules** - pattern-based matching (e.g., `~/work` matches any subdirectory)

### Migrating from profiles/ layout

If you're upgrading from the old `profiles/`-based layout:

```sh
go run scripts/migrate.go
eval "$(claudeme hook)"
# verify, then:
rm -rf ~/.config/claudeme/profiles.bak
```

## Uninstall

```sh
make uninstall
rm -rf ~/.config/claudeme
```

## License

MIT
