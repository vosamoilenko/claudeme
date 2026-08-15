package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// bootstrap loads a launch agent. macOS 11+ wants bootstrap/bootout; older
// releases only know load/unload, so both are tried.
func bootstrap(path string) error {
	target := fmt.Sprintf("gui/%d", os.Getuid())
	if err := exec.Command("launchctl", "bootstrap", target, path).Run(); err == nil {
		return nil
	}
	return exec.Command("launchctl", "load", "-w", path).Run()
}

// bootoutLabel unloads one agent by label, falling back to the pre-11 verbs.
func bootoutLabel(label, path string) {
	target := fmt.Sprintf("gui/%d/%s", os.Getuid(), label)
	if err := exec.Command("launchctl", "bootout", target).Run(); err == nil {
		return
	}
	exec.Command("launchctl", "unload", path).Run()
}

// plistPath is where a user launch agent lives.
func plistPath(label string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist")
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
