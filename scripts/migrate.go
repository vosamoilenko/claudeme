// One-time migration script: profiles/ → accounts/ + shared/
//
// Usage: go run scripts/migrate.go
//
// Merges data from all existing profiles into a shared directory,
// then creates symlink-based account directories.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var baseDir string

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

var perAccountFiles = []string{
	".claude.json",
	"stats-cache.json",
	"policy-limits.json",
	"remote-settings.json",
	"mcp-needs-auth-cache.json",
	".last-cleanup",
}

var perAccountDirs = []string{
	"session-env",
	"shell-snapshots",
	"ide",
	"cache",
}

func main() {
	home, _ := os.UserHomeDir()
	baseDir = filepath.Join(home, ".config", "claudeme")
	profilesDir := filepath.Join(baseDir, "profiles")
	sharedDir := filepath.Join(baseDir, "shared")
	accountsDir := filepath.Join(baseDir, "accounts")

	if _, err := os.Stat(profilesDir); os.IsNotExist(err) {
		fmt.Println("No profiles/ directory found. Nothing to migrate.")
		return
	}

	if _, err := os.Stat(accountsDir); err == nil {
		fmt.Println("accounts/ directory already exists. Migration may have already run.")
		fmt.Print("Continue anyway? (y/n): ")
		var answer string
		fmt.Scanln(&answer)
		if answer != "y" {
			return
		}
	}

	entries, err := os.ReadDir(profilesDir)
	if err != nil {
		fatal("reading profiles dir: %v", err)
	}

	var profiles []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), "_") && !strings.HasPrefix(e.Name(), ".") {
			profiles = append(profiles, e.Name())
		}
	}

	if len(profiles) == 0 {
		fmt.Println("No profile directories found.")
		return
	}

	fmt.Printf("Found %d profiles: %s\n", len(profiles), strings.Join(profiles, ", "))

	os.MkdirAll(sharedDir, 0755)
	os.MkdirAll(accountsDir, 0755)

	// Merge shared items from all profiles
	for _, item := range sharedItems {
		fmt.Printf("  merging %s...\n", item)
		if item == "history.jsonl" {
			mergeHistoryFiles(profiles, profilesDir, sharedDir)
			continue
		}
		if item == "settings.json" {
			mergeSettingsFile(profiles, profilesDir, sharedDir)
			continue
		}
		mergeDirs(profiles, profilesDir, sharedDir, item)
	}

	// Create account directories with per-account files + symlinks
	for _, email := range profiles {
		fmt.Printf("  setting up account %s...\n", email)
		srcDir := filepath.Join(profilesDir, email)
		dstDir := filepath.Join(accountsDir, email)
		os.MkdirAll(dstDir, 0755)

		// Move per-account files
		for _, f := range perAccountFiles {
			src := filepath.Join(srcDir, f)
			dst := filepath.Join(dstDir, f)
			if _, err := os.Stat(src); err == nil {
				copyFile(src, dst)
			}
		}

		// Move per-account dirs
		for _, d := range perAccountDirs {
			src := filepath.Join(srcDir, d)
			dst := filepath.Join(dstDir, d)
			if info, err := os.Stat(src); err == nil && info.IsDir() {
				copyDirRecursive(src, dst)
			}
		}

		// Also move the .json file beside the profile dir
		sideJSON := filepath.Join(profilesDir, email+".json")
		dstJSON := filepath.Join(accountsDir, email+".json")
		if _, err := os.Stat(sideJSON); err == nil {
			copyFile(sideJSON, dstJSON)
		}

		// Create symlinks for shared items
		for _, item := range sharedItems {
			target := filepath.Join(sharedDir, item)
			link := filepath.Join(dstDir, item)

			// Ensure target exists
			if filepath.Ext(item) != "" {
				if _, err := os.Stat(target); os.IsNotExist(err) {
					os.WriteFile(target, []byte{}, 0644)
				}
			} else {
				os.MkdirAll(target, 0755)
			}

			os.Symlink(target, link)
		}
	}

	// Rewrite stale profiles/ paths in shared plugin JSON files
	rewritePluginPaths(sharedDir, baseDir)

	// Rename profiles/ to profiles.bak/ (keep as backup)
	backupDir := filepath.Join(baseDir, "profiles.bak")
	if err := os.Rename(profilesDir, backupDir); err != nil {
		fmt.Printf("  warning: could not rename profiles/ to profiles.bak/: %v\n", err)
		fmt.Println("  old profiles/ directory left in place")
	} else {
		fmt.Printf("  old profiles/ renamed to profiles.bak/\n")
	}

	fmt.Println()
	fmt.Println("Migration complete!")
	fmt.Printf("  shared data: %s\n", sharedDir)
	fmt.Printf("  accounts:    %s\n", accountsDir)
	fmt.Println()
	fmt.Println("Verify with: ls -la ~/.config/claudeme/accounts/*/projects")
	fmt.Println("Remove backup when satisfied: rm -rf ~/.config/claudeme/profiles.bak")
}

func mergeDirs(profiles []string, profilesDir, sharedDir, item string) {
	dstDir := filepath.Join(sharedDir, item)
	os.MkdirAll(dstDir, 0755)

	for _, email := range profiles {
		srcDir := filepath.Join(profilesDir, email, item)
		if info, err := os.Stat(srcDir); err != nil || !info.IsDir() {
			continue
		}
		copyDirRecursive(srcDir, dstDir)
	}
}

func mergeHistoryFiles(profiles []string, profilesDir, sharedDir string) {
	type historyLine struct {
		Timestamp string `json:"timestamp"`
		raw       string
	}

	var allLines []historyLine

	for _, email := range profiles {
		path := filepath.Join(profilesDir, email, "history.jsonl")
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			var entry struct {
				Timestamp string `json:"timestamp"`
			}
			json.Unmarshal([]byte(line), &entry)
			allLines = append(allLines, historyLine{Timestamp: entry.Timestamp, raw: line})
		}
		f.Close()
	}

	sort.Slice(allLines, func(i, j int) bool {
		return allLines[i].Timestamp < allLines[j].Timestamp
	})

	// Deduplicate by raw content
	seen := make(map[string]bool)
	dst := filepath.Join(sharedDir, "history.jsonl")
	out, err := os.Create(dst)
	if err != nil {
		fatal("creating merged history: %v", err)
	}
	defer out.Close()

	for _, l := range allLines {
		if seen[l.raw] {
			continue
		}
		seen[l.raw] = true
		fmt.Fprintln(out, l.raw)
	}
}

func mergeSettingsFile(profiles []string, profilesDir, sharedDir string) {
	dst := filepath.Join(sharedDir, "settings.json")

	// Use the first profile's settings that exists
	for _, email := range profiles {
		src := filepath.Join(profilesDir, email, "settings.json")
		if _, err := os.Stat(src); err == nil {
			copyFile(src, dst)
			fmt.Printf("    using settings.json from %s\n", email)
			return
		}
	}
}

func copyFile(src, dst string) {
	in, err := os.Open(src)
	if err != nil {
		return
	}
	defer in.Close()

	info, _ := in.Stat()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return
	}
	defer out.Close()

	io.Copy(out, in)
}

func copyDirRecursive(src, dst string) {
	filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			os.MkdirAll(target, 0755)
			return nil
		}

		// Skip if destination already exists (first profile wins for conflicts)
		if _, err := os.Stat(target); err == nil {
			return nil
		}

		copyFile(path, target)
		return nil
	})
}

func rewritePluginPaths(sharedDir, baseDir string) {
	pluginsDir := filepath.Join(sharedDir, "plugins")
	files := []string{
		filepath.Join(pluginsDir, "known_marketplaces.json"),
		filepath.Join(pluginsDir, "installed_plugins.json"),
	}

	oldBase := filepath.Join(baseDir, "profiles")
	newBase := filepath.Join(baseDir, "accounts")

	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		updated := strings.ReplaceAll(string(data), oldBase, newBase)
		if updated != string(data) {
			os.WriteFile(path, []byte(updated), 0644)
			fmt.Printf("  rewrote paths in %s\n", filepath.Base(path))
		}
	}
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "fatal: "+format+"\n", args...)
	os.Exit(1)
}
