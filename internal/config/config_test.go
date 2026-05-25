package config

import (
	"os"
	"path/filepath"
	"testing"
)

func setupTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	SetConfigDir(dir)
	return dir
}

// ── FolderProfiles ──────────────────────────────────────────────

func TestFolderProfiles_SetAndGet(t *testing.T) {
	setupTestDir(t)

	fp := &FolderProfilesConfig{FolderProfiles: make(map[string]string)}
	fp.SetProfileForFolder("/Users/dev/project-a", "work@company.com")
	fp.SetProfileForFolder("/Users/dev/personal", "me@gmail.com")

	email, ok := fp.GetProfileForFolder("/Users/dev/project-a")
	if !ok || email != "work@company.com" {
		t.Fatalf("expected work@company.com, got %q (ok=%v)", email, ok)
	}

	email, ok = fp.GetProfileForFolder("/Users/dev/personal")
	if !ok || email != "me@gmail.com" {
		t.Fatalf("expected me@gmail.com, got %q (ok=%v)", email, ok)
	}

	_, ok = fp.GetProfileForFolder("/Users/dev/unknown")
	if ok {
		t.Fatal("expected no match for unknown folder")
	}
}

func TestFolderProfiles_OverwriteExisting(t *testing.T) {
	setupTestDir(t)

	fp := &FolderProfilesConfig{FolderProfiles: make(map[string]string)}
	fp.SetProfileForFolder("/repo", "old@example.com")
	fp.SetProfileForFolder("/repo", "new@example.com")

	email, ok := fp.GetProfileForFolder("/repo")
	if !ok || email != "new@example.com" {
		t.Fatalf("expected new@example.com after overwrite, got %q", email)
	}
}

func TestFolderProfiles_RemoveFolder(t *testing.T) {
	setupTestDir(t)

	fp := &FolderProfilesConfig{FolderProfiles: make(map[string]string)}
	fp.SetProfileForFolder("/repo", "user@example.com")

	if !fp.RemoveFolder("/repo") {
		t.Fatal("expected RemoveFolder to return true for existing entry")
	}
	if fp.RemoveFolder("/repo") {
		t.Fatal("expected RemoveFolder to return false for already-removed entry")
	}

	_, ok := fp.GetProfileForFolder("/repo")
	if ok {
		t.Fatal("expected no match after removal")
	}
}

func TestFolderProfiles_PathCleaning(t *testing.T) {
	setupTestDir(t)

	fp := &FolderProfilesConfig{FolderProfiles: make(map[string]string)}
	fp.SetProfileForFolder("/Users/dev/project/", "user@example.com")

	// Should match with or without trailing slash
	email, ok := fp.GetProfileForFolder("/Users/dev/project")
	if !ok || email != "user@example.com" {
		t.Fatalf("expected match without trailing slash, got %q (ok=%v)", email, ok)
	}
}

func TestFolderProfiles_SaveAndLoad(t *testing.T) {
	setupTestDir(t)

	fp := &FolderProfilesConfig{FolderProfiles: make(map[string]string)}
	fp.SetProfileForFolder("/repo-a", "a@example.com")
	fp.SetProfileForFolder("/repo-b", "b@example.com")

	if err := fp.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := LoadFolderProfiles()
	if err != nil {
		t.Fatalf("LoadFolderProfiles failed: %v", err)
	}

	if len(loaded.FolderProfiles) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(loaded.FolderProfiles))
	}

	email, ok := loaded.GetProfileForFolder("/repo-a")
	if !ok || email != "a@example.com" {
		t.Fatalf("expected a@example.com, got %q", email)
	}
}

func TestFolderProfiles_LoadNonExistent(t *testing.T) {
	setupTestDir(t)

	fp, err := LoadFolderProfiles()
	if err != nil {
		t.Fatalf("LoadFolderProfiles on missing file should not error: %v", err)
	}
	if len(fp.FolderProfiles) != 0 {
		t.Fatal("expected empty map for missing file")
	}
}

// ── Rules ───────────────────────────────────────────────────────

func TestRules_FindRuleForPath_LongestMatch(t *testing.T) {
	setupTestDir(t)

	r := &RulesConfig{Rules: []Rule{
		{Pattern: "/Users/dev", Profile: "broad@example.com"},
		{Pattern: "/Users/dev/work/project", Profile: "specific@example.com"},
	}}

	rule := r.FindRuleForPath("/Users/dev/work/project/src")
	if rule == nil || rule.Profile != "specific@example.com" {
		t.Fatalf("expected specific rule, got %v", rule)
	}
}

func TestRules_FindRuleForPath_NoMatch(t *testing.T) {
	setupTestDir(t)

	r := &RulesConfig{Rules: []Rule{
		{Pattern: "/Users/dev/work", Profile: "work@example.com"},
	}}

	rule := r.FindRuleForPath("/Users/other/personal")
	if rule != nil {
		t.Fatalf("expected no match, got %v", rule)
	}
}

func TestRules_FindRuleForPath_BoundaryMatch(t *testing.T) {
	setupTestDir(t)

	r := &RulesConfig{Rules: []Rule{
		{Pattern: "work", Profile: "work@example.com"},
	}}

	// Should NOT match /working (not a path boundary)
	rule := r.FindRuleForPath("/Users/dev/working")
	if rule != nil {
		t.Fatal("expected no match for /working (not a path boundary)")
	}

	// Should match "work" as a path component
	rule = r.FindRuleForPath("/Users/dev/work/project")
	if rule == nil || rule.Profile != "work@example.com" {
		t.Fatalf("expected match for 'work' path component, got %v", rule)
	}
}

func TestRules_AddAndRemove(t *testing.T) {
	setupTestDir(t)

	r := &RulesConfig{Rules: []Rule{}}
	r.AddRule("~/work", "work@example.com")

	if len(r.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(r.Rules))
	}

	// Adding same pattern updates instead of duplicating
	r.AddRule("~/work", "new@example.com")
	if len(r.Rules) != 1 {
		t.Fatalf("expected 1 rule after update, got %d", len(r.Rules))
	}
	if r.Rules[0].Profile != "new@example.com" {
		t.Fatalf("expected updated profile, got %s", r.Rules[0].Profile)
	}

	if !r.RemoveRule("~/work") {
		t.Fatal("expected RemoveRule to return true")
	}
	if len(r.Rules) != 0 {
		t.Fatal("expected empty rules after removal")
	}
}

func TestRules_TildeExpansion(t *testing.T) {
	setupTestDir(t)

	home, _ := os.UserHomeDir()
	r := &RulesConfig{Rules: []Rule{
		{Pattern: "~/projects", Profile: "user@example.com"},
	}}

	rule := r.FindRuleForPath(filepath.Join(home, "projects", "foo"))
	if rule == nil || rule.Profile != "user@example.com" {
		t.Fatalf("expected tilde expansion to match, got %v", rule)
	}
}

func TestRules_SaveAndLoad(t *testing.T) {
	setupTestDir(t)

	r := &RulesConfig{Rules: []Rule{
		{Pattern: "~/work", Profile: "work@example.com"},
	}}
	if err := r.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := LoadRules()
	if err != nil {
		t.Fatalf("LoadRules failed: %v", err)
	}
	if len(loaded.Rules) != 1 || loaded.Rules[0].Pattern != "~/work" {
		t.Fatalf("unexpected loaded rules: %v", loaded.Rules)
	}
}

// ── Profiles ────────────────────────────────────────────────────

func TestProfiles_ActiveProfile(t *testing.T) {
	setupTestDir(t)

	cfg := &ProfilesConfig{Profiles: make(map[string]Profile)}
	cfg.Profiles["a@example.com"] = Profile{Email: "a@example.com", Org: "Org A"}
	cfg.Profiles["b@example.com"] = Profile{Email: "b@example.com"}

	cfg.SetActive("a@example.com")
	p, ok := cfg.GetActive()
	if !ok || p.Email != "a@example.com" {
		t.Fatalf("expected active=a@example.com, got %v (ok=%v)", p, ok)
	}

	cfg.SetActive("nonexistent@example.com")
	_, ok = cfg.GetActive()
	if ok {
		t.Fatal("expected false for nonexistent active profile")
	}
}

func TestProfiles_SaveAndLoad(t *testing.T) {
	setupTestDir(t)

	cfg := &ProfilesConfig{Profiles: make(map[string]Profile)}
	cfg.Profiles["user@example.com"] = Profile{Email: "user@example.com", Org: "My Org"}
	cfg.SetActive("user@example.com")

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := LoadProfiles()
	if err != nil {
		t.Fatalf("LoadProfiles failed: %v", err)
	}

	if loaded.Active != "user@example.com" {
		t.Fatalf("expected active=user@example.com, got %s", loaded.Active)
	}
	if loaded.Profiles["user@example.com"].Org != "My Org" {
		t.Fatal("org not preserved")
	}
}

// ── Aliases ─────────────────────────────────────────────────────

func TestAliases_ResolveAndFind(t *testing.T) {
	setupTestDir(t)

	a := &AliasConfig{Aliases: make(map[string]string)}
	a.SetAlias("work", "work@company.com")

	if email := a.ResolveAlias("work"); email != "work@company.com" {
		t.Fatalf("expected resolved email, got %s", email)
	}

	if email := a.ResolveAlias("unknown"); email != "unknown" {
		t.Fatalf("expected passthrough for unknown alias, got %s", email)
	}

	if alias := a.FindAlias("work@company.com"); alias != "work" {
		t.Fatalf("expected alias 'work', got %s", alias)
	}

	if alias := a.FindAlias("other@example.com"); alias != "" {
		t.Fatalf("expected empty for unknown email, got %s", alias)
	}
}

func TestAliases_Remove(t *testing.T) {
	setupTestDir(t)

	a := &AliasConfig{Aliases: make(map[string]string)}
	a.SetAlias("work", "work@company.com")

	if !a.RemoveAlias("work") {
		t.Fatal("expected true for existing alias removal")
	}
	if a.RemoveAlias("work") {
		t.Fatal("expected false for already-removed alias")
	}
}

// ── Symlinks ───────────────────────────────────────────────────

func TestSetupAccountSymlinks(t *testing.T) {
	setupTestDir(t)

	email := "test@example.com"
	accountDir := ProfileDir(email)
	os.MkdirAll(accountDir, 0755)

	if err := SetupAccountSymlinks(email); err != nil {
		t.Fatalf("SetupAccountSymlinks failed: %v", err)
	}

	for _, item := range SharedItems() {
		link := filepath.Join(accountDir, item)
		target, err := os.Readlink(link)
		if err != nil {
			t.Fatalf("expected symlink for %s, got error: %v", item, err)
		}
		expected := filepath.Join(SharedDir(), item)
		if target != expected {
			t.Fatalf("expected symlink target %s, got %s", expected, target)
		}
	}
}

func TestSetupAccountSymlinks_Idempotent(t *testing.T) {
	setupTestDir(t)

	email := "test@example.com"
	accountDir := ProfileDir(email)
	os.MkdirAll(accountDir, 0755)

	SetupAccountSymlinks(email)
	if err := SetupAccountSymlinks(email); err != nil {
		t.Fatalf("second SetupAccountSymlinks should not fail: %v", err)
	}
}

// ── Settings ────────────────────────────────────────────────────

func TestSettings_DefaultsAndPersist(t *testing.T) {
	setupTestDir(t)

	s, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings failed: %v", err)
	}
	if s.AutoApply {
		t.Fatal("expected auto_apply=false by default")
	}

	s.AutoApply = true
	if err := s.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings failed: %v", err)
	}
	if !loaded.AutoApply {
		t.Fatal("expected auto_apply=true after save")
	}
}
