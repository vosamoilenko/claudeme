package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/vosamoilenko/claudeme/internal/config"
)

type sessionInfo struct {
	ID        string
	Timestamp time.Time
	Prompt    string
	Name      string
}

func projectDirName(cwd string) string {
	return strings.ReplaceAll(cwd, string(filepath.Separator), "-")
}

func Sessions() {
	cfg, err := config.LoadProfiles()
	if err != nil || cfg.Active == "" {
		fmt.Fprintln(os.Stderr, "No active profile.")
		os.Exit(1)
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Cannot determine working directory.")
		os.Exit(1)
	}

	projectDir := filepath.Join(config.ProfileDir(cfg.Active), "projects", projectDirName(cwd))
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		fmt.Println("No sessions found for this project.")
		return
	}

	var sessions []sessionInfo
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		sid := strings.TrimSuffix(e.Name(), ".jsonl")
		info := parseSessionFile(filepath.Join(projectDir, e.Name()), sid)
		if !info.Timestamp.IsZero() {
			sessions = append(sessions, info)
		}
	}

	if len(sessions) == 0 {
		fmt.Println("No sessions found for this project.")
		return
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Timestamp.After(sessions[j].Timestamp)
	})

	limit := 10
	if len(sessions) < limit {
		limit = len(sessions)
	}

	fmt.Println(HeaderStyle.Render("Sessions") + " " + DimStyle.Render(cwd))
	fmt.Println()
	for i, s := range sessions[:limit] {
		age := fmt.Sprintf("%-10s", formatAge(s.Timestamp))
		prompt := truncateUTF8(s.Prompt, 50)

		marker := "  "
		if i == 0 {
			marker = SuccessStyle.Render("* ")
		}

		fmt.Printf("  %s%s  %s %s\n", marker, DimStyle.Render(age), HeaderStyle.Render(s.ID), prompt)
	}

	fmt.Println()
	fmt.Println(DimStyle.Render("  Resume: ccyolo --resume <id>"))
	fmt.Println(DimStyle.Render("  Continue last: ccyolo --continue"))
}

func parseSessionFile(path, sid string) sessionInfo {
	info := sessionInfo{ID: sid}

	f, err := os.Open(path)
	if err != nil {
		return info
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)

	foundTimestamp := false
	for scanner.Scan() {
		var raw map[string]interface{}
		if json.Unmarshal(scanner.Bytes(), &raw) != nil {
			continue
		}

		if !foundTimestamp {
			if ts, ok := raw["timestamp"].(string); ok {
				if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
					info.Timestamp = t
					foundTimestamp = true
				}
			}
		}

		if info.Prompt == "" {
			if raw["type"] == "user" {
				if msg, ok := raw["message"].(map[string]interface{}); ok {
					if msg["role"] == "user" {
						content, _ := msg["content"].(string)
						if content != "" && !strings.HasPrefix(content, "<") {
							info.Prompt = strings.ReplaceAll(content, "\n", " ")
						}
					}
				}
			}
		}

		if foundTimestamp && info.Prompt != "" {
			break
		}
	}

	// Get the last modification time as the "real" timestamp (most recent activity)
	if fi, err := os.Stat(path); err == nil {
		info.Timestamp = fi.ModTime()
	}

	return info
}

func truncateUTF8(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-3]) + "..."
}

func formatAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "yesterday"
		}
		return fmt.Sprintf("%dd ago", days)
	}
}
