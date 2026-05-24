package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/vosamoilenko/claudeme/internal/cmd"
	"github.com/vosamoilenko/claudeme/internal/config"
	"github.com/vosamoilenko/claudeme/internal/ui"
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

	// Sessions
	case "sessions", "ss":
		cmd.Sessions()

	// TUI picker
	case "tui":
		runTUI()

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
	fmt.Println("  claudeme me                         Show active profile path")
	fmt.Println("  claudeme sessions (ss)              List sessions for current project")
	fmt.Println("  claudeme remove <alias|email>       Remove account")
	fmt.Println("  claudeme reset                      Remove all accounts")
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
	fmt.Println("  Each account gets an isolated CLAUDE_CONFIG_DIR with its own auth.")
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
