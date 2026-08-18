package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/vosamoilenko/claudeme/internal/cmd"
	"github.com/vosamoilenko/claudeme/internal/config"
	"github.com/vosamoilenko/claudeme/internal/ui"
	"github.com/vosamoilenko/claudeme/internal/version"
)

func main() {
	if len(os.Args) < 2 {
		// No args: launch claude with active profile
		cmd.LaunchClaude(nil)
		return
	}

	switch os.Args[1] {
	// Profile management
	case "list", "ls":
		cmd.List()
	case "add":
		cmd.Add()
	case "remove", "rm":
		cmd.Remove()
	case "use":
		cmd.Use()
	case "as":
		cmd.As()
	case "resolve":
		cmd.Resolve()
	case "me", "whoami":
		cmd.Current()
	case "reset":
		cmd.Reset()

	// Aliases
	case "alias":
		cmd.Alias()

	// Auto-switch
	case "auto":
		cmd.Auto()
	case "rule":
		cmd.Rule()
	case "folder":
		cmd.Folder()
	case "config":
		cmd.Config()
	case "hook":
		cmd.Hook()
	case "pin", "unpin":
		// Handled by the shell hook — a binary can't export into its parent shell.
		fmt.Fprintf(os.Stderr, "claudeme %s needs the shell hook: eval \"$(claudeme hook)\"\n", os.Args[1])
		fmt.Fprintln(os.Stderr, "Or set it yourself: export CLAUDEME_PROFILE=<alias|email>")
		os.Exit(1)

	// Sessions
	case "sessions", "ss":
		cmd.Sessions()

	// Usage
	case "projects":
		cmd.Projects()
	case "usage":
		cmd.Usage()
	case "snapshot":
		cmd.Snapshot()
	case "digest":
		cmd.Digest()
	case "history":
		cmd.History()
	case "cost":
		cmd.Cost()
	case "heatmap":
		cmd.Heatmap()

	// TUI picker
	case "tui":
		runTUI()

	// Version
	case "version", "-v", "--version":
		version.Print()

	// Help
	case "help", "-h", "--help":
		printHelp()

	default:
		// Forward unknown args to claude (e.g. --dangerously-skip-permissions)
		cmd.LaunchClaude(os.Args[1:])
	}
}

func printHelp() {
	fmt.Println(cmd.HeaderStyle.Render("claudeme") + " — Claude Code account switcher")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  claudeme                            Launch claude with active profile")
	fmt.Println("  claudeme [claude-args...]            Forward args to claude")
	fmt.Println("  claudeme tui                        Interactive TUI picker")
	fmt.Println("  claudeme add                        Add account (launches claude login)")
	fmt.Println("  claudeme list                       List accounts")
	fmt.Println("  claudeme use <alias|email>          Switch active account")
	fmt.Println("  claudeme as <alias|email> [args...] Launch with one account, don't switch")
	fmt.Println("  claudeme pin <alias|email>          Pin this shell to an account (needs hook)")
	fmt.Println("  claudeme unpin                      Unpin this shell")
	fmt.Println("  claudeme me                         Show active profile path")
	fmt.Println("  claudeme sessions (ss)              List sessions for current project")
	fmt.Println("  claudeme remove <alias|email>       Remove account")
	fmt.Println("  claudeme reset                      Remove all accounts")
	fmt.Println("  claudeme version                    Show version")
	fmt.Println()
	fmt.Println(cmd.HeaderStyle.Render("Usage reports:"))
	fmt.Println("  claudeme projects [--cost] [--all] [--expand]")
	fmt.Println("                                      List projects; --cost adds day/skill spend,")
	fmt.Println("                                      --expand shows subfolders")
	fmt.Println("  claudeme usage [name] [--top N]     Token/cost report (list pricing)")
	fmt.Println("  claudeme heatmap [name] [--weeks N] [--metric cost|calls|sessions] [--all]")
	fmt.Println("                                      Day-by-day activity grid")
	fmt.Println()
	fmt.Println(cmd.HeaderStyle.Render("Digest:"))
	fmt.Println("  claudeme digest [--since D] [--all] [--archived] [--limit N] [-n]")
	fmt.Println("                                      Summarize sessions into history/<date>/<project>.json")
	fmt.Println("  claudeme digest --metrics-only [-n] Fill in timings/branches only — no model, no cost")
	fmt.Println("  claudeme digest --tokens-only [-n]  Fill in the token ledger only — no model, no cost")
	fmt.Println("  claudeme digest --prompts-only [-n] Record when each session ran, from the prompt history")
	fmt.Println("  claudeme digest --install           Run it daily at 05:00 (launchd)")
	fmt.Println("  claudeme digest --uninstall         Remove the daily job")
	fmt.Println("  claudeme digest --status            Show schedule and what's digested")
	fmt.Println()
	fmt.Println(cmd.HeaderStyle.Render("History (survives Claude's 30-day cleanup):"))
	fmt.Println("  claudeme snapshot [-n]              Extract per-day usage into usage-history.json")
	fmt.Println("  claudeme snapshot --install         Record it twice daily (launchd)")
	fmt.Println("  claudeme snapshot --uninstall       Remove the job")
	fmt.Println("  claudeme snapshot --status          Show schedule and what's recorded")
	fmt.Println("  claudeme history [--days N] [--all] Print the recorded time series")
	fmt.Println("  claudeme history --by model|project|skill")
	fmt.Println("                                      Sum the range by dimension")
	fmt.Println("  claudeme history --json             Emit the raw records")
	fmt.Println("  claudeme cost [--at D] [--actual]   Value stored token ledgers at dated prices")
	fmt.Println("  claudeme cost --provider anthropic|openai|all")
	fmt.Println("  claudeme cost --by model|project|day|provider")
	fmt.Println()
	fmt.Println(cmd.HeaderStyle.Render("Aliases:"))
	fmt.Println("  claudeme alias add <name> <email>   Create alias for an account")
	fmt.Println("  claudeme alias list                 List aliases")
	fmt.Println("  claudeme alias rm <name>            Remove alias")
	fmt.Println()
	fmt.Println(cmd.HeaderStyle.Render("Auto-switch:"))
	fmt.Println("  claudeme rule add <path> <alias>    Auto-switch when entering directory")
	fmt.Println("  claudeme rule list                  List rules")
	fmt.Println("  claudeme rule rm <path>             Remove rule")
	fmt.Println("  claudeme folder list                List remembered folder mappings")
	fmt.Println("  claudeme folder set <alias|email>   Set profile for current directory")
	fmt.Println("  claudeme folder rm [path]           Remove folder mapping")
	fmt.Println("  claudeme folder clear               Clear all folder mappings")
	fmt.Println("  claudeme config auto_apply <on|off> Toggle auto-apply")
	fmt.Println("  claudeme hook                       Print shell hook for .zshrc")
	fmt.Println()
	fmt.Println(cmd.HeaderStyle.Render("How it works:"))
	fmt.Println("  Each account gets its own CLAUDE_CONFIG_DIR with auth.")
	fmt.Println("  Sessions, projects, and settings are shared across accounts via symlinks.")
	fmt.Println("  The shell hook exports CLAUDE_CONFIG_DIR so claude uses the right account.")
	fmt.Println()
	fmt.Println("Config: ~/.config/claudeme/")
}

func runTUI() {
	cfg, err := config.LoadProfiles()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading profiles: %v\n", err)
		os.Exit(1)
	}

	if len(cfg.Profiles) == 0 {
		fmt.Println("No profiles found.")
		fmt.Println("Add one with: claudeme add")
		return
	}

	model := ui.New(cfg)
	p := tea.NewProgram(model)

	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}

	m := finalModel.(ui.Model)

	switch m.Action() {
	case ui.ActionSelect:
		if email := m.Choice(); email != "" {
			cmd.ActivateProfile(email)
		}
	case ui.ActionDelete:
		if email := m.DeleteTarget(); email != "" {
			cmd.RemoveProfile(email)
		}
	}
}
