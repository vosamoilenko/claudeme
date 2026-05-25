package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/vosamoilenko/claudeme/internal/config"
)

// Auto detects the correct profile for the current directory based on rules.
// Outputs `export CLAUDE_CONFIG_DIR=...` to stdout (eval'd by the shell hook).
// Human-readable messages go to stderr (visible but not eval'd).
func Auto() {
	cwd, err := os.Getwd()
	if err != nil {
		return
	}

	cfg, err := config.LoadProfiles()
	if err != nil {
		return
	}

	rules, err := config.LoadRules()
	if err != nil {
		return
	}

	settings, err := config.LoadSettings()
	if err != nil {
		return
	}

	// Priority 1: Exact folder → profile mapping (remembers per-repo decisions)
	var profileEmail string
	var matchSource string

	fp, _ := config.LoadFolderProfiles()
	if fp != nil {
		if email, ok := fp.GetProfileForFolder(cwd); ok {
			profileEmail = email
			matchSource = "folder"
		}
	}

	// Priority 2: Pattern-based rules
	var rule *config.Rule
	if profileEmail == "" {
		rule = rules.FindRuleForPath(cwd)
		if rule == nil {
			return
		}
		profileEmail = rule.Profile
		matchSource = "rule: " + rule.Pattern
	}
	if _, exists := cfg.Profiles[profileEmail]; !exists {
		fmt.Fprintf(os.Stderr, "%s Match for %q but profile doesn't exist\n",
			WarnStyle.Render("!"), profileEmail)
		return
	}

	// Already on the right profile
	currentDir := os.Getenv("CLAUDE_CONFIG_DIR")
	expectedDir := config.ProfileDir(profileEmail)
	if cfg.Active == profileEmail && currentDir == expectedDir {
		return
	}

	aliases, _ := config.LoadAliases()

	if settings.AutoApply {
		cfg.SetActive(profileEmail)
		cfg.Save()

		// stdout: export for shell eval
		fmt.Printf("export CLAUDE_CONFIG_DIR=%s\n", expectedDir)

		// stderr: visible status
		name := profileEmail
		if aliases != nil {
			if a := aliases.FindAlias(profileEmail); a != "" {
				name = a
			}
		}
		fmt.Fprintf(os.Stderr, "%s Auto-switched to %s (%s)\n",
			SuccessStyle.Render("✻"), name, matchSource)
	} else {
		name := profileEmail
		if aliases != nil {
			if a := aliases.FindAlias(profileEmail); a != "" {
				name = a + " <" + profileEmail + ">"
			}
		}
		fmt.Fprintf(os.Stderr, "%s Profile mismatch!\n", WarnStyle.Render("!"))
		fmt.Fprintf(os.Stderr, "  Current:  %s\n", cfg.Active)
		fmt.Fprintf(os.Stderr, "  Expected: %s (%s)\n", name, matchSource)
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "%s\n", DimStyle.Render("Run 'claudeme use "+profileEmail+"' to switch"))
		fmt.Fprintf(os.Stderr, "%s\n", DimStyle.Render("Or 'claudeme config auto_apply on' to auto-switch"))
	}
}

// Folder manages per-folder profile mappings. These are recorded automatically
// when you run `claudeme use` in a directory, or can be managed manually.
func Folder() {
	if len(os.Args) < 3 {
		// Default: list
		listFolders()
		return
	}

	switch os.Args[2] {
	case "list", "ls":
		listFolders()
	case "set":
		if len(os.Args) < 4 {
			fmt.Fprintf(os.Stderr, "Usage: claudeme folder set <alias|email>\n")
			fmt.Fprintf(os.Stderr, "Sets the profile for the current directory.\n")
			os.Exit(1)
		}
		input := os.Args[3]
		aliases, _ := config.LoadAliases()
		email := input
		if aliases != nil {
			email = aliases.ResolveAlias(input)
		}
		cfg, _ := config.LoadProfiles()
		if _, exists := cfg.Profiles[email]; !exists {
			fmt.Fprintf(os.Stderr, "%s Profile %q not found.\n", WarnStyle.Render("!"), input)
			os.Exit(1)
		}
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting current directory: %v\n", err)
			os.Exit(1)
		}
		fp, _ := config.LoadFolderProfiles()
		if fp == nil {
			fp = &config.FolderProfilesConfig{FolderProfiles: make(map[string]string)}
		}
		fp.SetProfileForFolder(cwd, email)
		if err := fp.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving folder profiles: %v\n", err)
			os.Exit(1)
		}
		display := displayName(email, aliases)
		fmt.Printf("%s %s -> %s\n", SuccessStyle.Render("*"), cwd, display)
	case "rm", "remove":
		var folder string
		if len(os.Args) >= 4 {
			folder = os.Args[3]
		} else {
			var err error
			folder, err = os.Getwd()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error getting current directory: %v\n", err)
				os.Exit(1)
			}
		}
		fp, _ := config.LoadFolderProfiles()
		if fp == nil || !fp.RemoveFolder(folder) {
			fmt.Fprintf(os.Stderr, "No mapping for: %s\n", folder)
			os.Exit(1)
		}
		if err := fp.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving folder profiles: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("%s Removed mapping for %s\n", SuccessStyle.Render("*"), folder)
	case "clear":
		fp := &config.FolderProfilesConfig{FolderProfiles: make(map[string]string)}
		if err := fp.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving folder profiles: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("%s Cleared all folder mappings\n", SuccessStyle.Render("*"))
	default:
		fmt.Fprintf(os.Stderr, "Unknown folder command: %s\n", os.Args[2])
		fmt.Fprintf(os.Stderr, "Usage: claudeme folder <list|set|rm|clear>\n")
		os.Exit(1)
	}
}

func listFolders() {
	fp, err := config.LoadFolderProfiles()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading folder profiles: %v\n", err)
		os.Exit(1)
	}

	if len(fp.FolderProfiles) == 0 {
		fmt.Println("No folder mappings.")
		fmt.Println(DimStyle.Render("Mappings are created automatically when you run 'claudeme use' in a directory."))
		return
	}

	aliases, _ := config.LoadAliases()

	fmt.Println(HeaderStyle.Render("Folder mappings:"))
	fmt.Println()
	for folder, email := range fp.FolderProfiles {
		display := displayName(email, aliases)
		fmt.Printf("  %s -> %s\n", folder, display)
	}
}

// Rule manages auto-switch rules. Profile references accept alias or email,
// but are stored as email.
func Rule() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: claudeme rule <add|list|rm> [args]\n")
		os.Exit(1)
	}

	subCmd := os.Args[2]

	rules, err := config.LoadRules()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading rules: %v\n", err)
		os.Exit(1)
	}

	switch subCmd {
	case "add":
		if len(os.Args) < 5 {
			fmt.Fprintf(os.Stderr, "Usage: claudeme rule add <pattern> <alias|email>\n")
			fmt.Fprintf(os.Stderr, "Example: claudeme rule add ~/work work\n")
			os.Exit(1)
		}
		pattern := os.Args[3]
		input := os.Args[4]

		// Resolve alias to email
		aliases, _ := config.LoadAliases()
		email := input
		if aliases != nil {
			email = aliases.ResolveAlias(input)
		}

		cfg, _ := config.LoadProfiles()
		if _, exists := cfg.Profiles[email]; !exists {
			fmt.Fprintf(os.Stderr, "%s Profile %q not found. Add it first with: claudeme add\n",
				WarnStyle.Render("!"), input)
			os.Exit(1)
		}

		rules.AddRule(pattern, email)
		if err := rules.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving rules: %v\n", err)
			os.Exit(1)
		}

		display := email
		if aliases != nil {
			if a := aliases.FindAlias(email); a != "" {
				display = a
			}
		}
		fmt.Printf("%s Added rule: %s -> %s\n", SuccessStyle.Render("*"), pattern, display)

	case "list", "ls":
		if len(rules.Rules) == 0 {
			fmt.Println("No rules configured.")
			fmt.Println(DimStyle.Render("Add one with: claudeme rule add <pattern> <alias|email>"))
			return
		}

		aliases, _ := config.LoadAliases()

		fmt.Println(HeaderStyle.Render("Auto-switch rules:"))
		fmt.Println()
		for _, r := range rules.Rules {
			display := r.Profile
			if aliases != nil {
				if a := aliases.FindAlias(r.Profile); a != "" {
					display = a + " <" + r.Profile + ">"
				}
			}
			fmt.Printf("  %s -> %s\n", r.Pattern, display)
		}

	case "rm", "remove":
		if len(os.Args) < 4 {
			fmt.Fprintf(os.Stderr, "Usage: claudeme rule rm <pattern>\n")
			os.Exit(1)
		}
		pattern := os.Args[3]
		if rules.RemoveRule(pattern) {
			if err := rules.Save(); err != nil {
				fmt.Fprintf(os.Stderr, "Error saving rules: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("%s Removed rule: %s\n", SuccessStyle.Render("*"), pattern)
		} else {
			fmt.Fprintf(os.Stderr, "Rule not found: %s\n", pattern)
			os.Exit(1)
		}

	default:
		fmt.Fprintf(os.Stderr, "Unknown rule command: %s\n", subCmd)
		fmt.Fprintf(os.Stderr, "Usage: claudeme rule <add|list|rm> [args]\n")
		os.Exit(1)
	}
}

// Config manages settings
func Config() {
	if len(os.Args) < 3 {
		settings, err := config.LoadSettings()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading settings: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(HeaderStyle.Render("Settings:"))
		fmt.Println()
		autoApplyStr := "off"
		if settings.AutoApply {
			autoApplyStr = "on"
		}
		fmt.Printf("  auto_apply: %s\n", autoApplyStr)
		return
	}

	key := os.Args[2]
	if len(os.Args) < 4 {
		fmt.Fprintf(os.Stderr, "Usage: claudeme config <key> <value>\n")
		os.Exit(1)
	}
	value := os.Args[3]

	settings, err := config.LoadSettings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading settings: %v\n", err)
		os.Exit(1)
	}

	switch key {
	case "auto_apply":
		switch strings.ToLower(value) {
		case "on", "true", "1", "yes":
			settings.AutoApply = true
		case "off", "false", "0", "no":
			settings.AutoApply = false
		default:
			fmt.Fprintf(os.Stderr, "Invalid value: %s (use on/off)\n", value)
			os.Exit(1)
		}
		if err := settings.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving settings: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("%s Set auto_apply = %s\n", SuccessStyle.Render("*"), value)
	default:
		fmt.Fprintf(os.Stderr, "Unknown setting: %s\n", key)
		os.Exit(1)
	}
}

// Hook prints the shell hook to be eval'd in .zshrc or .bashrc
func Hook() {
	fmt.Print(shellHook)
}

const shellHook = `# claudeme — Claude Code account switcher
# Add to your .zshrc or .bashrc:
#   eval "$(claudeme hook)"

_CLAUDEME_DIR="$HOME/.config/claudeme"

# Read active email and resolve alias from JSON files (no binary call needed).
_claudeme_sync_env() {
  local email="" alias_name=""
  if [[ -f "$_CLAUDEME_DIR/profiles.json" ]]; then
    email=$(sed -n 's/.*"active": *"\([^"]*\)".*/\1/p' "$_CLAUDEME_DIR/profiles.json")
  fi
  if [[ -n "$email" ]]; then
    export CLAUDE_CONFIG_DIR="$_CLAUDEME_DIR/accounts/$email"
    # Resolve alias
    if [[ -f "$_CLAUDEME_DIR/aliases.json" ]]; then
      alias_name=$(sed -n "s/.*\"\([^\"]*\)\": *\"${email}\".*/\1/p" "$_CLAUDEME_DIR/aliases.json")
    fi
    export CLAUDE_PROFILE="${alias_name:-$email}"
  else
    unset CLAUDE_CONFIG_DIR
    unset CLAUDE_PROFILE
  fi
}

# Wrap the claudeme binary so env is updated after profile changes.
claudeme() {
  command claudeme "$@"
  local rc=$?
  if [[ $rc -eq 0 ]]; then
    case "$1" in
      use|add|reset|remove|rm)
        _claudeme_sync_env
        ;;
    esac
  fi
  return $rc
}

# On directory change, check rules and export CLAUDE_CONFIG_DIR
_claudeme_chpwd() {
  eval "$(command claudeme auto 2>/dev/null)"
}

if [[ -n "$ZSH_VERSION" ]]; then
  autoload -U add-zsh-hook
  add-zsh-hook chpwd _claudeme_chpwd
  _claudeme_sync_env
elif [[ -n "$BASH_VERSION" ]]; then
  _claudeme_prompt_command() {
    local cwd="$PWD"
    if [[ "$cwd" != "$_claudeme_last_dir" ]]; then
      _claudeme_last_dir="$cwd"
      _claudeme_chpwd
    fi
  }
  PROMPT_COMMAND="_claudeme_prompt_command${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
  _claudeme_sync_env
fi
`
