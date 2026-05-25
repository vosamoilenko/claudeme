package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/vosamoilenko/claudeme/internal/config"
)

// List shows all profiles by email, with alias if available
func List() {
	cfg, err := config.LoadProfiles()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading profiles: %v\n", err)
		os.Exit(1)
	}

	if len(cfg.Profiles) == 0 {
		fmt.Println("No profiles found.")
		fmt.Println(DimStyle.Render("Add one with: claudeme add"))
		return
	}

	aliases, _ := config.LoadAliases()

	home, _ := os.UserHomeDir()
	shorten := func(p string) string { return strings.Replace(p, home, "~", 1) }

	for email := range cfg.Profiles {
		marker := DimStyle.Render("✻")
		if email == cfg.Active {
			marker = SuccessStyle.Render("✻")
		}

		alias := ""
		if aliases != nil {
			if a := aliases.FindAlias(email); a != "" {
				alias = HeaderStyle.Render(a)
			}
		}

		label := email
		if alias != "" {
			label = alias
		}
		fmt.Printf("%s%s %s\n", marker, label, DimStyle.Render(shorten(config.ProfileDir(email))))
	}
	fmt.Printf("%s %s\n", DimStyle.Render("shared"), DimStyle.Render(shorten(config.SharedDir())))
}

// Add creates a new profile by launching Claude in a staging dir,
// then moving it to a directory named after the authenticated email.
func Add() {
	cfg, err := config.LoadProfiles()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading profiles: %v\n", err)
		os.Exit(1)
	}

	// Launch Claude with staging CLAUDE_CONFIG_DIR
	if err := LaunchClaudeLogin(); err != nil {
		fmt.Fprintf(os.Stderr, "\nClaude exited with error: %v\n", err)
		CleanupStaging()
		return
	}

	// Read the email from the authenticated session
	email, org := ReadStagingAccount()
	if email == "" {
		fmt.Fprintf(os.Stderr, "Could not read account info. Authentication may have failed.\n")
		CleanupStaging()
		return
	}

	// Check if profile already exists
	if _, exists := cfg.Profiles[email]; exists {
		fmt.Printf("%s Profile for %s already exists, updating...\n",
			WarnStyle.Render("!"), email)
	}

	// Move staging to final directory named after email
	if err := FinalizeStagingProfile(email); err != nil {
		fmt.Fprintf(os.Stderr, "Error finalizing profile: %v\n", err)
		CleanupStaging()
		os.Exit(1)
	}

	cfg.Profiles[email] = config.Profile{Email: email, Org: org}

	// First profile becomes active automatically
	if len(cfg.Profiles) == 1 || cfg.Active == "" {
		cfg.Active = email
	}

	if err := cfg.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving profiles: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n%s %s\n", SuccessStyle.Render("✓"), email)
	if org != "" {
		fmt.Printf("  %s\n", DimStyle.Render(org))
	}
}

// Remove deletes a profile
func Remove() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: claudeme remove <alias|email>\n")
		os.Exit(1)
	}

	input := os.Args[2]
	RemoveProfile(input)
}

// RemoveProfile removes a profile by alias or email (used by both CLI and TUI)
func RemoveProfile(input string) {
	cfg, err := config.LoadProfiles()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading profiles: %v\n", err)
		os.Exit(1)
	}

	aliases, _ := config.LoadAliases()
	email := input
	if aliases != nil {
		email = aliases.ResolveAlias(input)
	}

	if _, exists := cfg.Profiles[email]; !exists {
		fmt.Fprintf(os.Stderr, "Profile not found: %s\n", input)
		os.Exit(1)
	}

	// Remove account directory (symlinks get removed, shared data stays)
	os.RemoveAll(config.ProfileDir(email))
	os.Remove(config.ProfileConfigJSON(email))

	delete(cfg.Profiles, email)

	if cfg.Active == email {
		cfg.Active = ""
		for e := range cfg.Profiles {
			cfg.Active = e
			break
		}
	}

	if err := cfg.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving profiles: %v\n", err)
		os.Exit(1)
	}

	// Remove any aliases pointing to this email
	if aliases != nil {
		changed := false
		for name, e := range aliases.Aliases {
			if e == email {
				delete(aliases.Aliases, name)
				changed = true
			}
		}
		if changed {
			aliases.Save()
		}
	}

	fmt.Printf("%s Removed %s\n", SuccessStyle.Render("✓"), email)
}

// Use sets the active profile (accepts alias or email)
func Use() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: claudeme use <alias|email>\n")
		os.Exit(1)
	}

	input := os.Args[2]
	ActivateProfile(input)
}

// ActivateProfile sets a profile as active. Accepts alias or email.
func ActivateProfile(input string) {
	cfg, err := config.LoadProfiles()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading profiles: %v\n", err)
		os.Exit(1)
	}

	aliases, _ := config.LoadAliases()
	email := input
	if aliases != nil {
		email = aliases.ResolveAlias(input)
	}

	if _, exists := cfg.Profiles[email]; !exists {
		fmt.Fprintf(os.Stderr, "Profile not found: %s\n", input)
		os.Exit(1)
	}

	if cfg.Active == email {
		printProfileSummary(email, aliases)
		return
	}

	// Refresh metadata
	if readEmail, org := ReadProfileAccount(email); readEmail != "" {
		p := cfg.Profiles[email]
		p.Org = org
		cfg.Profiles[email] = p
	}

	cfg.SetActive(email)
	if err := cfg.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving profiles: %v\n", err)
		os.Exit(1)
	}

	// Remember this profile for the current directory
	if cwd, err := os.Getwd(); err == nil {
		fp, _ := config.LoadFolderProfiles()
		if fp != nil {
			fp.SetProfileForFolder(cwd, email)
			fp.Save()
		}
	}

	printProfileSummary(email, aliases)
}

// Reset deletes all profiles
func Reset() {
	cfg, err := config.LoadProfiles()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading profiles: %v\n", err)
		os.Exit(1)
	}

	if len(cfg.Profiles) == 0 {
		fmt.Println("Nothing to reset.")
		return
	}

	for email := range cfg.Profiles {
		os.RemoveAll(config.ProfileDir(email))
		os.Remove(config.ProfileConfigJSON(email))
	}

	empty := &config.ProfilesConfig{Profiles: make(map[string]config.Profile)}
	if err := empty.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving profiles: %v\n", err)
		os.Exit(1)
	}

	// Clear aliases too
	emptyAliases := &config.AliasConfig{Aliases: make(map[string]string)}
	emptyAliases.Save()

	CleanupStaging()

	fmt.Printf("%s Removed all profiles and aliases.\n", SuccessStyle.Render("✓"))
	fmt.Println(DimStyle.Render("Start fresh with: claudeme add"))
}

// Current shows the active profile
func Current() {
	// --dir flag: output just the path (for shell hook)
	if len(os.Args) >= 3 && os.Args[2] == "--dir" {
		cfg, err := config.LoadProfiles()
		if err != nil || cfg.Active == "" {
			return
		}
		fmt.Print(config.ProfileDir(cfg.Active))
		return
	}

	// --short flag: output alias or email (for status line)
	if len(os.Args) >= 3 && os.Args[2] == "--short" {
		cfg, err := config.LoadProfiles()
		if err != nil || cfg.Active == "" {
			return
		}
		aliases, _ := config.LoadAliases()
		if aliases != nil {
			if a := aliases.FindAlias(cfg.Active); a != "" {
				fmt.Print(a)
				return
			}
		}
		fmt.Print(cfg.Active)
		return
	}

	cfg, err := config.LoadProfiles()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading profiles: %v\n", err)
		os.Exit(1)
	}

	if cfg.Active == "" {
		fmt.Println("No active profile.")
		fmt.Println(DimStyle.Render("Set one with: claudeme use <alias|email>"))
		return
	}

	fmt.Print(config.ProfileDir(cfg.Active))
}

// printProfileSummary prints the active profile in compact form.
func printProfileSummary(email string, aliases *config.AliasConfig) {
	name := email
	if aliases != nil {
		if a := aliases.FindAlias(email); a != "" {
			name = a
		}
	}
	fmt.Printf("*%s\n", name)
	fmt.Println(DimStyle.Render(email))
	fmt.Println(DimStyle.Render(config.ProfileDir(email)))
}

// displayName returns "alias (email)" if alias exists, otherwise just email.
func displayName(email string, aliases *config.AliasConfig) string {
	if aliases != nil {
		if a := aliases.FindAlias(email); a != "" {
			return fmt.Sprintf("%s <%s>", a, email)
		}
	}
	return email
}
