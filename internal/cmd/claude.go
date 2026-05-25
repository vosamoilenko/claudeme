package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/vosamoilenko/claudeme/internal/config"
)

// LaunchClaudeLogin runs `claude` with CLAUDE_CONFIG_DIR set to the staging
// directory so the user authenticates into an isolated config.
func LaunchClaudeLogin() error {
	stagingDir := config.StagingDir()

	// Clean up any previous staging attempt
	os.RemoveAll(stagingDir)
	os.Remove(config.StagingConfigJSON())

	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		return fmt.Errorf("failed to create staging directory: %w", err)
	}

	fmt.Println("Launching Claude Code to authenticate...")
	fmt.Println(DimStyle.Render("Complete the login, then exit Claude (/exit or Ctrl+C)."))
	fmt.Println()

	cmd := exec.Command("claude")
	cmd.Env = append(os.Environ(), "CLAUDE_CONFIG_DIR="+stagingDir)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// ReadStagingAccount reads the oauthAccount from the staging profile's config.
func ReadStagingAccount() (email, org string) {
	return readAccountFrom("_staging")
}

// ReadProfileAccount reads the oauthAccount from a profile's Claude config.
func ReadProfileAccount(email string) (readEmail, org string) {
	return readAccountFrom(email)
}

// readAccountFrom reads oauthAccount from a profile's config, checking both
// possible locations where Claude Code stores its JSON.
func readAccountFrom(dirName string) (email, org string) {
	paths := []string{
		filepath.Join(config.ConfigDir(), "accounts", dirName+".json"),
		filepath.Join(config.ConfigDir(), "accounts", dirName, ".claude.json"),
	}

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var raw map[string]interface{}
		if json.Unmarshal(data, &raw) != nil {
			continue
		}

		if account, ok := raw["oauthAccount"].(map[string]interface{}); ok {
			if e, ok := account["emailAddress"].(string); ok {
				email = e
			}
			if o, ok := account["organizationName"].(string); ok {
				org = o
			}
			return email, org
		}
	}

	return "", ""
}

// FinalizeStagingProfile moves the staging directory to the final account
// directory, then replaces shared items with symlinks to the shared dir.
func FinalizeStagingProfile(email string) error {
	stagingDir := config.StagingDir()
	stagingJSON := config.StagingConfigJSON()
	finalDir := config.ProfileDir(email)
	finalJSON := config.ProfileConfigJSON(email)

	// Remove existing profile dir if re-adding
	os.RemoveAll(finalDir)
	os.Remove(finalJSON)

	// Move directory
	if err := os.Rename(stagingDir, finalDir); err != nil {
		return fmt.Errorf("failed to move staging dir: %w", err)
	}

	// Move .json config (may not exist if Claude stores it inside the dir)
	if _, err := os.Stat(stagingJSON); err == nil {
		if err := os.Rename(stagingJSON, finalJSON); err != nil {
			return fmt.Errorf("failed to move staging config: %w", err)
		}
	}

	// Replace shared items with symlinks
	if err := config.SetupAccountSymlinks(email); err != nil {
		return fmt.Errorf("failed to setup symlinks: %w", err)
	}

	return nil
}

// IsProfileAuthenticated checks if a profile has been authenticated.
func IsProfileAuthenticated(email string) bool {
	paths := []string{
		config.ProfileConfigJSON(email),
		filepath.Join(config.ProfileDir(email), ".claude.json"),
	}

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

// CleanupStaging removes leftover staging files.
func CleanupStaging() {
	os.RemoveAll(config.StagingDir())
	os.Remove(config.StagingConfigJSON())
}

// LaunchClaude forwards all arguments to the `claude` CLI with
// CLAUDE_CONFIG_DIR set to the active profile's directory.
func LaunchClaude(args []string) {
	cfg, err := config.LoadProfiles()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading profiles: %v\n", err)
		os.Exit(1)
	}

	if cfg.Active == "" {
		fmt.Fprintln(os.Stderr, "No active profile. Run: claudeme use <alias|email>")
		os.Exit(1)
	}

	configDir := config.ProfileDir(cfg.Active)

	cmd := exec.Command("claude", args...)
	cmd.Env = append(os.Environ(), "CLAUDE_CONFIG_DIR="+configDir)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		os.Exit(1)
	}
}
