package cmd

import (
	"fmt"
	"os"

	"github.com/vosamoilenko/claudeme/internal/config"
)

// ResolveProfile turns an alias or email into a known profile's email.
func ResolveProfile(input string) (string, error) {
	cfg, err := config.LoadProfiles()
	if err != nil {
		return "", fmt.Errorf("loading profiles: %w", err)
	}

	email := input
	if aliases, _ := config.LoadAliases(); aliases != nil {
		email = aliases.ResolveAlias(input)
	}

	if _, exists := cfg.Profiles[email]; !exists {
		return "", fmt.Errorf("profile not found: %s", input)
	}
	return email, nil
}

// Resolve prints the email an alias points at, for the shell hook to validate
// `claudeme pin <name>` before exporting anything.
func Resolve() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: claudeme resolve <alias|email>")
		os.Exit(1)
	}

	email, err := ResolveProfile(os.Args[2])
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	fmt.Println(email)
}

// As launches claude with a specific profile without touching the active
// profile, so several accounts can run side by side in separate terminals:
//
//	claudeme as work
//	claudeme as personal --resume
func As() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: claudeme as <alias|email> [claude-args...]")
		os.Exit(1)
	}

	email, err := ResolveProfile(os.Args[2])
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	runClaude(config.ProfileDir(email), os.Args[3:])
}
