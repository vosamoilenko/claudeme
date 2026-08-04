package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var configDir string

var sharedItems = []string{
	"projects",
	"sessions",
	"plans",
	"tasks",
	"todos",
	"file-history",
	"paste-cache",
	"backups",
	"history.jsonl",
	"settings.json",
	"plugins",
	"telemetry",
	"memory",
}

var perAccountItems = []string{
	".claude.json",
	"stats-cache.json",
	"policy-limits.json",
	"remote-settings.json",
	"mcp-needs-auth-cache.json",
	".last-cleanup",
	"session-env",
	"shell-snapshots",
	"ide",
	"cache",
}

func init() {
	home, _ := os.UserHomeDir()
	configDir = filepath.Join(home, ".config", "claudeme")
	os.MkdirAll(configDir, 0755)
	os.MkdirAll(filepath.Join(configDir, "accounts"), 0755)
	os.MkdirAll(filepath.Join(configDir, "shared"), 0755)
}

// ConfigDir returns the base config directory
func ConfigDir() string {
	return configDir
}

// SetConfigDir overrides the config directory (used by tests).
func SetConfigDir(dir string) {
	configDir = dir
	os.MkdirAll(configDir, 0755)
	os.MkdirAll(filepath.Join(configDir, "accounts"), 0755)
	os.MkdirAll(filepath.Join(configDir, "shared"), 0755)
}

// SharedDir returns the shared data directory.
func SharedDir() string {
	return filepath.Join(configDir, "shared")
}

// SharedItems returns the list of items that should be symlinked to shared.
func SharedItems() []string {
	return sharedItems
}

// PerAccountItems returns the list of items that stay per-account.
func PerAccountItems() []string {
	return perAccountItems
}

// ProfileDir returns the CLAUDE_CONFIG_DIR path for a given account (keyed by email).
func ProfileDir(email string) string {
	return filepath.Join(configDir, "accounts", email)
}

// ProfileConfigJSON returns the .json file Claude creates beside the config dir.
func ProfileConfigJSON(email string) string {
	return filepath.Join(configDir, "accounts", email+".json")
}

// StagingDir returns the temporary staging directory used during `add`.
func StagingDir() string {
	return filepath.Join(configDir, "accounts", "_staging")
}

// StagingConfigJSON returns the staging .json config path.
func StagingConfigJSON() string {
	return filepath.Join(configDir, "accounts", "_staging.json")
}

// SetupAccountSymlinks creates symlinks from an account dir to the shared dir.
// Any existing real files/dirs for shared items are removed first.
func SetupAccountSymlinks(email string) error {
	accountDir := ProfileDir(email)
	sharedDir := SharedDir()

	for _, item := range sharedItems {
		target := filepath.Join(sharedDir, item)
		link := filepath.Join(accountDir, item)

		// Skip if already a correct symlink
		if dest, err := os.Readlink(link); err == nil {
			if dest == target || dest == filepath.Join("../../shared", item) {
				continue
			}
		}

		// Remove existing file/dir at link path
		os.RemoveAll(link)

		// Ensure target exists in shared dir
		info, err := os.Stat(target)
		if os.IsNotExist(err) {
			if filepath.Ext(item) != "" {
				// It's a file — create empty
				os.WriteFile(target, []byte{}, 0644)
			} else {
				// It's a directory — create empty
				os.MkdirAll(target, 0755)
			}
		} else if err == nil && info.IsDir() {
			// exists, fine
		}

		if err := os.Symlink(target, link); err != nil {
			return fmt.Errorf("failed to symlink %s: %w", item, err)
		}
	}

	RewritePluginPaths()
	return nil
}

// RewritePluginPaths rewrites stale "profiles/" paths to "accounts/" in
// shared/plugins JSON files for all known accounts.
func RewritePluginPaths() error {
	pluginsDir := filepath.Join(SharedDir(), "plugins")
	files := []string{
		filepath.Join(pluginsDir, "known_marketplaces.json"),
		filepath.Join(pluginsDir, "installed_plugins.json"),
	}

	oldBase := filepath.Join(configDir, "profiles")
	newBase := filepath.Join(configDir, "accounts")

	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		updated := strings.ReplaceAll(string(data), oldBase, newBase)
		if updated != string(data) {
			os.WriteFile(path, []byte(updated), 0644)
		}
	}
	return nil
}

// ============ Profiles Config ============

// Profile holds metadata about a Claude Code account (keyed by email).
type Profile struct {
	Email string `json:"email"`
	Org   string `json:"org,omitempty"`
}

// ProfilesConfig holds all profiles and the active profile email.
type ProfilesConfig struct {
	Active   string             `json:"active"`
	Profiles map[string]Profile `json:"profiles"`
}

func profilesPath() string {
	return filepath.Join(configDir, "profiles.json")
}

func LoadProfiles() (*ProfilesConfig, error) {
	cfg := &ProfilesConfig{Profiles: make(map[string]Profile)}

	data, err := os.ReadFile(profilesPath())
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]Profile)
	}

	return cfg, nil
}

func (c *ProfilesConfig) Save() error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(profilesPath(), data, 0644)
}

func (c *ProfilesConfig) SetActive(email string) {
	c.Active = email
}

func (c *ProfilesConfig) GetActive() (Profile, bool) {
	if c.Active == "" {
		return Profile{}, false
	}
	p, ok := c.Profiles[c.Active]
	return p, ok
}

// ============ Aliases Config ============

// AliasConfig maps short names to profile emails.
type AliasConfig struct {
	Aliases map[string]string `json:"aliases"`
}

func aliasesPath() string {
	return filepath.Join(configDir, "aliases.json")
}

func LoadAliases() (*AliasConfig, error) {
	cfg := &AliasConfig{Aliases: make(map[string]string)}

	data, err := os.ReadFile(aliasesPath())
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	if cfg.Aliases == nil {
		cfg.Aliases = make(map[string]string)
	}

	return cfg, nil
}

func (a *AliasConfig) Save() error {
	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(aliasesPath(), data, 0644)
}

func (a *AliasConfig) SetAlias(name, email string) {
	a.Aliases[name] = email
}

func (a *AliasConfig) RemoveAlias(name string) bool {
	if _, ok := a.Aliases[name]; !ok {
		return false
	}
	delete(a.Aliases, name)
	return true
}

// ResolveAlias returns the email for an alias, or the input unchanged if not an alias.
func (a *AliasConfig) ResolveAlias(input string) string {
	if email, ok := a.Aliases[input]; ok {
		return email
	}
	return input
}

// FindAlias returns the alias for an email, or "" if none.
func (a *AliasConfig) FindAlias(email string) string {
	for name, e := range a.Aliases {
		if e == email {
			return name
		}
	}
	return ""
}

// ============ Rules Config ============

type Rule struct {
	Pattern string `json:"pattern"`
	Profile string `json:"profile"` // email address
}

type RulesConfig struct {
	Rules []Rule `json:"rules"`
}

func rulesPath() string {
	return filepath.Join(configDir, "rules.json")
}

func LoadRules() (*RulesConfig, error) {
	cfg := &RulesConfig{Rules: []Rule{}}

	data, err := os.ReadFile(rulesPath())
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (r *RulesConfig) Save() error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(rulesPath(), data, 0644)
}

func (r *RulesConfig) AddRule(pattern, profile string) {
	for i, rule := range r.Rules {
		if rule.Pattern == pattern {
			r.Rules[i].Profile = profile
			return
		}
	}
	r.Rules = append(r.Rules, Rule{Pattern: pattern, Profile: profile})
}

func (r *RulesConfig) RemoveRule(pattern string) bool {
	for i, rule := range r.Rules {
		if rule.Pattern == pattern {
			r.Rules = append(r.Rules[:i], r.Rules[i+1:]...)
			return true
		}
	}
	return false
}

func (r *RulesConfig) FindRuleForPath(path string) *Rule {
	var bestMatch *Rule
	bestLen := 0
	for i, rule := range r.Rules {
		if matchesPattern(path, rule.Pattern) && len(rule.Pattern) > bestLen {
			bestMatch = &r.Rules[i]
			bestLen = len(rule.Pattern)
		}
	}
	return bestMatch
}

func matchesPattern(path, pattern string) bool {
	if len(pattern) > 0 && pattern[0] == '~' {
		home, _ := os.UserHomeDir()
		pattern = home + pattern[1:]
	}
	if len(pattern) == 0 {
		return false
	}

	path = filepath.Clean(path)
	pattern = filepath.Clean(pattern)

	if path == pattern {
		return true
	}

	return containsPathBoundary(path, pattern)
}

func containsPathBoundary(path, pattern string) bool {
	for start := 0; ; {
		i := strings.Index(path[start:], pattern)
		if i == -1 {
			return false
		}

		idx := start + i
		leftOK := idx == 0 || path[idx-1] == filepath.Separator
		rightEnd := idx + len(pattern)
		rightOK := rightEnd == len(path) || path[rightEnd] == filepath.Separator

		if leftOK && rightOK {
			return true
		}
		start = idx + 1
	}
}

// ============ Folder Profiles Config ============

// FolderProfilesConfig maps absolute folder paths to profile emails.
// This remembers which account was used in each directory.
type FolderProfilesConfig struct {
	FolderProfiles map[string]string `json:"folder_profiles"`
}

func folderProfilesPath() string {
	return filepath.Join(configDir, "folder_profiles.json")
}

func LoadFolderProfiles() (*FolderProfilesConfig, error) {
	cfg := &FolderProfilesConfig{FolderProfiles: make(map[string]string)}

	data, err := os.ReadFile(folderProfilesPath())
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	if cfg.FolderProfiles == nil {
		cfg.FolderProfiles = make(map[string]string)
	}

	return cfg, nil
}

func (f *FolderProfilesConfig) Save() error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(folderProfilesPath(), data, 0644)
}

func (f *FolderProfilesConfig) SetProfileForFolder(folder, email string) {
	f.FolderProfiles[filepath.Clean(folder)] = email
}

func (f *FolderProfilesConfig) GetProfileForFolder(folder string) (string, bool) {
	email, ok := f.FolderProfiles[filepath.Clean(folder)]
	return email, ok
}

func (f *FolderProfilesConfig) RemoveFolder(folder string) bool {
	key := filepath.Clean(folder)
	if _, ok := f.FolderProfiles[key]; !ok {
		return false
	}
	delete(f.FolderProfiles, key)
	return true
}

// defaultCleanupPeriodDays is Claude Code's own default: transcripts older
// than this are deleted from the projects tree on startup.
const defaultCleanupPeriodDays = 30

// RetentionDays reports how long Claude Code keeps a transcript, read from the
// settings file every account shares. Falls back to Claude Code's default when
// the key is absent or the file is unreadable.
func RetentionDays() int {
	data, err := os.ReadFile(filepath.Join(SharedDir(), "settings.json"))
	if err != nil {
		return defaultCleanupPeriodDays
	}
	var s struct {
		CleanupPeriodDays *int `json:"cleanupPeriodDays"`
	}
	if json.Unmarshal(data, &s) != nil || s.CleanupPeriodDays == nil {
		return defaultCleanupPeriodDays
	}
	return *s.CleanupPeriodDays
}

// ============ Settings Config ============

type Settings struct {
	AutoApply bool `json:"auto_apply"`
}

func settingsPath() string {
	return filepath.Join(configDir, "settings.json")
}

func LoadSettings() (*Settings, error) {
	s := &Settings{AutoApply: false}

	data, err := os.ReadFile(settingsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, s); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Settings) Save() error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath(), data, 0644)
}
