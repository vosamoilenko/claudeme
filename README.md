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

## Commands

### Profile management

| Command | Description |
|---|---|
| `claudeme` | Launch claude with the active profile |
| `claudeme add` | Add an account (opens `claude login`) |
| `claudeme list` (`ls`) | List all accounts |
| `claudeme use <alias\|email>` | Switch active account |
| `claudeme me` (`whoami`) | Show active profile directory |
| `claudeme remove` (`rm`) | Remove an account |
| `claudeme reset` | Remove all accounts |
| `claudeme tui` | Interactive TUI picker |

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
