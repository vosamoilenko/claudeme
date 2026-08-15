package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vosamoilenko/claudeme/internal/usage"
)

// Snapshot reads every transcript still on disk, extracts the per-day
// aggregates, and upserts them into the durable history file. Nothing is
// copied or moved: the transcripts stay where Claude Code put them, and when
// it deletes them the numbers survive here.
func Snapshot() {
	dryRun := false
	action := "run"

	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
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
		installSnapshotJob()
		return
	case "uninstall":
		uninstallSnapshotJob()
		return
	case "status":
		snapshotStatus()
		return
	}

	projects, err := usage.Discover(usage.Roots())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	rep, err := usage.Analyze(usage.Roots(), allDirs(projects))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fresh := usage.Snapshot(rep, usage.ProjectNames(projects))

	path := usage.HistoryPath()
	old, err := usage.LoadHistory(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot read %s: %v\n", path, err)
		os.Exit(1)
	}
	merged, res := usage.MergeHistory(old, fresh, time.Now().UTC().Format(time.RFC3339))

	if !dryRun {
		if err := usage.SaveHistory(path, merged); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	verb := "snapshot"
	if dryRun {
		verb = "dry run"
	}
	fmt.Printf("%s %s\n", HeaderStyle.Render("Usage "+verb),
		DimStyle.Render(fmt.Sprintf("%d transcripts read", rep.Files)))
	fmt.Println(DimStyle.Render(fmt.Sprintf("  %d days added, %d updated, %d on record",
		len(res.Added), len(res.Updated), len(merged.Days))))
	if len(res.Kept) > 0 {
		// A day that now reads lower than the stored snapshot has lost
		// transcripts to retention. The stored number is the truer one.
		fmt.Println(WarnStyle.Render(fmt.Sprintf("  %d day%s now read lower on disk — kept the recorded value: %s",
			len(res.Kept), plural(len(res.Kept)), strings.Join(res.Kept, " "))))
	}
	fmt.Println(DimStyle.Render("  " + shortenHome(path)))
}

// History prints the recorded time series. It reads only the snapshot file, so
// it is instant and covers days whose transcripts are long gone.
func History() {
	days := 30
	by := ""
	asJSON := false

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
		case "--all", "-a":
			days = 0
		case "--by", "-b":
			if i+1 < len(args) {
				by = args[i+1]
				i++
			}
		case "--json":
			asJSON = true
		default:
			fmt.Fprintf(os.Stderr, "unknown flag %q\n", args[i])
			os.Exit(1)
		}
	}

	path := usage.HistoryPath()
	h, err := usage.LoadHistory(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot read %s: %v\n", path, err)
		os.Exit(1)
	}
	if len(h.Days) == 0 {
		fmt.Println("no history recorded yet — claudeme snapshot")
		return
	}

	dates := h.Recent(days)
	if asJSON {
		out := map[string]*usage.Day{}
		for _, d := range dates {
			out[d] = h.Days[d]
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(out)
		return
	}

	if by != "" {
		printHistoryBy(h, dates, by)
		return
	}

	var total float64
	var calls, sessions int
	peak := 0.0
	for _, d := range dates {
		day := h.Days[d]
		total += day.Cost
		calls += day.Calls
		sessions += day.Sessions
		if day.Cost > peak {
			peak = day.Cost
		}
	}

	fmt.Println(HeaderStyle.Render("Usage history") + " " +
		DimStyle.Render(fmt.Sprintf("%s – %s", dates[0], dates[len(dates)-1])))
	fmt.Println()
	fmt.Println(DimStyle.Render(fmt.Sprintf("  %-12s%10s%9s%8s", "day", "cost", "calls", "sess")))

	for _, d := range dates {
		day := h.Days[d]
		fmt.Printf("  %s%s%s%s  %s\n",
			pad(NameStyle.Render(d), 12),
			lpad(MoneyStyle.Render(money(day.Bucket.Micros())), 10),
			DimStyle.Render(fmt.Sprintf("%9s", comma(day.Calls))),
			DimStyle.Render(fmt.Sprintf("%8d", day.Sessions)),
			DimStyle.Render(bar(day.Cost, peak)))
	}

	fmt.Println()
	fmt.Printf("  %s%s%s%s\n",
		pad(DimStyle.Render(fmt.Sprintf("%d day%s", len(dates), plural(len(dates)))), 12),
		lpad(HeaderStyle.Render(money(int64(total*1e6))), 10),
		DimStyle.Render(fmt.Sprintf("%9s", comma(calls))),
		DimStyle.Render(fmt.Sprintf("%8d", sessions)))
	if len(h.Days) > len(dates) {
		fmt.Println(DimStyle.Render(fmt.Sprintf("  %d more days on record — claudeme history --all",
			len(h.Days)-len(dates))))
	}
	fmt.Println(DimStyle.Render("  " + shortenHome(path)))
}

// printHistoryBy sums one breakdown over the range instead of listing days.
func printHistoryBy(h *usage.History, dates []string, by string) {
	var pick func(*usage.Day) map[string]*usage.Bucket
	switch strings.ToLower(by) {
	case "model", "models":
		pick = func(d *usage.Day) map[string]*usage.Bucket { return d.Models }
	case "project", "projects":
		pick = func(d *usage.Day) map[string]*usage.Bucket { return d.Projects }
	case "skill", "skills":
		pick = func(d *usage.Day) map[string]*usage.Bucket { return d.Skills }
	default:
		fmt.Fprintf(os.Stderr, "unknown --by %q — try model, project or skill\n", by)
		os.Exit(1)
	}

	rows := h.Aggregate(dates, pick)
	type row struct {
		name string
		b    *usage.Bucket
	}
	list := make([]row, 0, len(rows))
	var total float64
	for name, b := range rows {
		list = append(list, row{name, b})
		total += b.Cost
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].b.Cost != list[j].b.Cost {
			return list[i].b.Cost > list[j].b.Cost
		}
		return list[i].name < list[j].name
	})

	fmt.Println(HeaderStyle.Render("Usage by "+strings.ToLower(by)) + " " +
		DimStyle.Render(fmt.Sprintf("%s – %s", dates[0], dates[len(dates)-1])))
	fmt.Println()
	for _, r := range list {
		fmt.Printf("  %s%s   %s\n",
			pad(NameStyle.Render(truncateUTF8(r.name, nameWidth-2)), nameWidth),
			lpad(MoneyStyle.Render(money(r.b.Micros())), 10),
			DimStyle.Render(fmt.Sprintf("%s calls", comma(r.b.Calls))))
	}
	fmt.Println()
	fmt.Printf("  %s%s\n", pad(DimStyle.Render(fmt.Sprintf("%d entries", len(list))), nameWidth),
		lpad(HeaderStyle.Render(money(int64(total*1e6))), 10))
}

// bar draws a proportional bar for a day's cost.
func bar(v, max float64) string {
	const width = 24
	if max <= 0 {
		return ""
	}
	n := int(v / max * width)
	if n < 1 && v > 0 {
		n = 1
	}
	return strings.Repeat("▪", n)
}

// allDirs is every transcript directory across the discovered projects, so one
// Analyze pass covers the whole tree.
func allDirs(projects []usage.Project) []string {
	seen := map[string]bool{}
	var dirs []string
	for _, p := range projects {
		for _, d := range p.Dirs {
			if !seen[d] {
				seen[d] = true
				dirs = append(dirs, d)
			}
		}
	}
	sort.Strings(dirs)
	return dirs
}

// ============ Scheduling ============

const snapshotJobLabel = "com.claudeme.snapshot"

func snapshotPlistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", snapshotJobLabel+".plist")
}

func snapshotLogPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "claudeme", "snapshot.log")
}

// snapshotPlist runs twice a day. Once would do for the numbers, but a machine
// that is asleep at 03:00 would skip a day, and RunAtLoad only covers boots.
func snapshotPlist(bin string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>snapshot</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>StartCalendarInterval</key>
	<array>
		<dict>
			<key>Hour</key>
			<integer>3</integer>
			<key>Minute</key>
			<integer>0</integer>
		</dict>
		<dict>
			<key>Hour</key>
			<integer>15</integer>
			<key>Minute</key>
			<integer>0</integer>
		</dict>
	</array>
	<key>LowPriorityIO</key>
	<true/>
	<key>Nice</key>
	<integer>5</integer>
	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
</dict>
</plist>
`, snapshotJobLabel, bin, snapshotLogPath(), snapshotLogPath())
}

func installSnapshotJob() {
	if runtime.GOOS != "darwin" {
		fmt.Fprintln(os.Stderr, "scheduling is macOS-only; run `claudeme snapshot` from cron instead")
		os.Exit(1)
	}
	bin, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if resolved, err := filepath.EvalSymlinks(bin); err == nil {
		bin = resolved // launchd needs a path that survives a reinstall
	}

	path := snapshotPlistPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(path, []byte(snapshotPlist(bin)), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	bootoutLabel(snapshotJobLabel, path)
	if err := bootstrap(path); err != nil {
		fmt.Fprintf(os.Stderr, "wrote %s but launchctl refused it: %v\n", path, err)
		os.Exit(1)
	}

	fmt.Println(HeaderStyle.Render("Snapshot") + " scheduled daily at 03:00 and 15:00")
	fmt.Println(DimStyle.Render("  " + shortenHome(usage.HistoryPath())))
	fmt.Println(DimStyle.Render("  " + path))
	fmt.Println(DimStyle.Render("  log: " + snapshotLogPath()))
}

func uninstallSnapshotJob() {
	path := snapshotPlistPath()
	bootoutLabel(snapshotJobLabel, path)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(HeaderStyle.Render("Snapshot") + " schedule removed")
	fmt.Println(DimStyle.Render("  recorded history stays in " + shortenHome(usage.HistoryPath())))
}

func snapshotStatus() {
	path := usage.HistoryPath()
	fmt.Println(HeaderStyle.Render("Snapshot") + " " + DimStyle.Render(shortenHome(path)))
	if _, err := os.Stat(snapshotPlistPath()); err != nil {
		fmt.Println("  not scheduled — claudeme snapshot --install")
	} else {
		fmt.Println("  scheduled daily at 03:00 and 15:00")
		fmt.Println(DimStyle.Render("  " + snapshotPlistPath()))
	}

	h, err := usage.LoadHistory(path)
	if err != nil {
		fmt.Println(WarnStyle.Render("  history unreadable: " + err.Error()))
		return
	}
	if len(h.Days) == 0 {
		fmt.Println("  no history recorded yet — claudeme snapshot")
		return
	}
	dates := h.Dates()
	var total float64
	for _, b := range h.Days {
		total += b.Cost
	}
	if info, err := os.Stat(path); err == nil {
		fmt.Printf("  %d days  %s – %s  %s  %s\n", len(dates), dates[0], dates[len(dates)-1],
			MoneyStyle.Render(money(int64(total*1e6))), DimStyle.Render(bytes(info.Size())))
	}
	if h.Updated != "" {
		fmt.Println(DimStyle.Render("  last run " + h.Updated))
	}
}
