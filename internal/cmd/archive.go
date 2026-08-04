package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/vosamoilenko/claudeme/internal/usage"
)

// defaultArchiveDays is how long a transcript stays in the live tree. Short
// enough that the tree Claude Code scans stays small; anything older is still
// fully readable by `claudeme usage`, just gzipped.
const defaultArchiveDays = 7

// Archive moves old transcripts into the compressed archive, or manages the
// scheduled job that does it.
func Archive() {
	days := defaultArchiveDays
	dryRun := false
	action := "run"

	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--days", "-d":
			if i+1 < len(args) {
				n, err := strconv.Atoi(args[i+1])
				if err != nil || n < 0 {
					fmt.Fprintf(os.Stderr, "invalid --days %q\n", args[i+1])
					os.Exit(1)
				}
				days = n
				i++
			}
		case "--dry-run", "-n":
			dryRun = true
		case "--install":
			action = "install"
		case "--uninstall":
			action = "uninstall"
		case "--status":
			action = "status"
		default:
			fmt.Fprintf(os.Stderr, "unknown flag %q\n", args[i])
			os.Exit(1)
		}
	}

	switch action {
	case "install":
		installArchiveJob(days)
	case "uninstall":
		uninstallArchiveJob()
	case "status":
		archiveStatus()
	default:
		runArchive(days, dryRun)
	}
}

func runArchive(days int, dryRun bool) {
	cutoff := time.Now().AddDate(0, 0, -days)
	res, err := usage.Archive(usage.ProjectsRoot(), usage.ArchiveRoot(), cutoff, dryRun)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	verb := "archived"
	if dryRun {
		verb = "would archive"
	}
	fmt.Printf("%s %s %d transcripts older than %dd\n",
		HeaderStyle.Render("Archive"), verb, res.Files, days)
	if res.Files == 0 {
		return
	}
	if dryRun {
		fmt.Println(DimStyle.Render(fmt.Sprintf(
			"  %s in the live tree, plus %d metadata files", bytes(res.Before), res.Metas)))
		return
	}
	fmt.Println(DimStyle.Render(fmt.Sprintf(
		"  %s → %s (%s freed), %d metadata files, %d transcripts moved",
		bytes(res.Before), bytes(res.After), bytes(res.Saved()), res.Metas, res.Files)))
	fmt.Println(DimStyle.Render("  " + usage.ArchiveRoot()))
}

// ============ Scheduling ============

const archiveJobLabel = "com.claudeme.archive"

func archivePlistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", archiveJobLabel+".plist")
}

// archivePlist is a daily 04:00 launchd agent. RunAtLoad catches machines that
// were asleep at four.
func archivePlist(bin string, days int) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>archive</string>
		<string>--days</string>
		<string>%d</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>StartCalendarInterval</key>
	<dict>
		<key>Hour</key>
		<integer>4</integer>
		<key>Minute</key>
		<integer>0</integer>
	</dict>
	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
</dict>
</plist>
`, archiveJobLabel, bin, days, archiveLogPath(), archiveLogPath())
}

func archiveLogPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "claudeme", "archive.log")
}

func installArchiveJob(days int) {
	if runtime.GOOS != "darwin" {
		fmt.Fprintln(os.Stderr, "scheduling is macOS-only; run `claudeme archive` from cron instead")
		os.Exit(1)
	}
	bin, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if resolved, err := filepath.EvalSymlinks(bin); err == nil {
		bin = resolved // launchd needs a path that survives a brew relink
	}

	path := archivePlistPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(path, []byte(archivePlist(bin, days)), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	bootout()
	if err := bootstrap(path); err != nil {
		fmt.Fprintf(os.Stderr, "wrote %s but launchctl refused it: %v\n", path, err)
		os.Exit(1)
	}

	fmt.Println(HeaderStyle.Render("Archive") + " scheduled daily at 04:00")
	fmt.Println(DimStyle.Render(fmt.Sprintf("  transcripts older than %dd → %s", days, usage.ArchiveRoot())))
	fmt.Println(DimStyle.Render("  " + path))
	fmt.Println(DimStyle.Render("  log: " + archiveLogPath()))
}

func uninstallArchiveJob() {
	bootout()
	path := archivePlistPath()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(HeaderStyle.Render("Archive") + " schedule removed")
	fmt.Println(DimStyle.Render("  already-archived transcripts stay in " + usage.ArchiveRoot()))
}

func archiveStatus() {
	fmt.Println(HeaderStyle.Render("Archive") + " " + DimStyle.Render(usage.ArchiveRoot()))
	if _, err := os.Stat(archivePlistPath()); err != nil {
		fmt.Println("  not scheduled — claudeme archive --install")
	} else {
		fmt.Println("  scheduled daily at 04:00")
		fmt.Println(DimStyle.Render("  " + archivePlistPath()))
	}
	fmt.Printf("  live    %8s  %s\n", bytes(treeSize(usage.ProjectsRoot())), shortenHome(usage.ProjectsRoot()))
	fmt.Printf("  archive %8s  %s\n", bytes(treeSize(usage.ArchiveRoot())), shortenHome(usage.ArchiveRoot()))
}

// bootstrap loads the agent. macOS 11+ wants bootstrap/bootout; older releases
// only know load/unload, so both are tried.
func bootstrap(path string) error {
	target := fmt.Sprintf("gui/%d", os.Getuid())
	if err := exec.Command("launchctl", "bootstrap", target, path).Run(); err == nil {
		return nil
	}
	return exec.Command("launchctl", "load", "-w", path).Run()
}

func bootout() {
	target := fmt.Sprintf("gui/%d/%s", os.Getuid(), archiveJobLabel)
	if err := exec.Command("launchctl", "bootout", target).Run(); err == nil {
		return
	}
	exec.Command("launchctl", "unload", archivePlistPath()).Run()
}

func treeSize(root string) int64 {
	var total int64
	filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

func bytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for n/div >= unit && exp < 3 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGT"[exp])
}
