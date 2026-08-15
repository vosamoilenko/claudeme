package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/vosamoilenko/claudeme/internal/usage"
)

// Digest summarizes session transcripts into history/<date>/<project>.json,
// or manages the scheduled job that does it. Transcripts are only ever read:
// Claude Code owns their lifetime.
func Digest() {
	since := yesterday()
	limit := 0
	archivedOnly := false
	dryRun := false
	action := "run"

	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--since", "-s":
			if i+1 < len(args) {
				if !isDate(args[i+1]) {
					fmt.Fprintf(os.Stderr, "invalid --since %q — want YYYY-MM-DD\n", args[i+1])
					os.Exit(1)
				}
				since = args[i+1]
				i++
			}
		case "--all", "-a":
			since = ""
		case "--archived":
			// The archive is the only copy of those sessions, and it is the
			// set that has to be digested before it can be deleted.
			since = ""
			archivedOnly = true
		case "--limit", "-l":
			if i+1 < len(args) {
				n, err := strconv.Atoi(args[i+1])
				if err != nil || n < 1 {
					fmt.Fprintf(os.Stderr, "invalid --limit %q\n", args[i+1])
					os.Exit(1)
				}
				limit = n
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
		installDigestJob()
	case "uninstall":
		uninstallDigestJob()
	case "status":
		digestStatus()
	default:
		runDigest(since, limit, dryRun, archivedOnly)
	}
}

func runDigest(since string, limit int, dryRun, archivedOnly bool) {
	root := usage.DigestRoot()

	cands, err := usage.ScanSessions(usage.Roots())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if archivedOnly {
		archive := usage.ArchiveRoot() + string(filepath.Separator)
		kept := cands[:0]
		for _, c := range cands {
			if strings.HasPrefix(c.Path, archive) {
				kept = append(kept, c)
			}
		}
		cands = kept
	}
	if since != "" {
		kept := cands[:0]
		for _, c := range cands {
			if c.Date >= since {
				kept = append(kept, c)
			}
		}
		cands = kept
	}

	pending, err := usage.Pending(root, cands)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Sessions still being written are left for a later run: their transcript
	// is incomplete, and a digest is never revisited once written.
	live := len(pending)
	pending = usage.Settled(pending, time.Now(), usage.SettleCooloff)
	live -= len(pending)

	scope := "all dates"
	if since != "" {
		scope = "since " + since
	}
	if archivedOnly {
		scope = "archived only"
	}
	dropped := 0
	if limit > 0 && len(pending) > limit {
		dropped = len(pending) - limit
		pending = pending[:limit]
	}

	fmt.Printf("%s %s\n", HeaderStyle.Render("Digest"),
		DimStyle.Render(fmt.Sprintf("%d session%s to do, %s", len(pending), plural(len(pending)), scope)))
	if dropped > 0 {
		fmt.Println(DimStyle.Render(fmt.Sprintf("  --limit left %d for a later run", dropped)))
	}
	if live > 0 {
		fmt.Println(DimStyle.Render(fmt.Sprintf("  %d still being written, left for a later run", live)))
	}
	if len(pending) == 0 {
		return
	}

	if dryRun {
		for _, c := range pending {
			fmt.Printf("  %s  %s  %s\n", DimStyle.Render(c.Date),
				pad(NameStyle.Render(truncateUTF8(c.Project, 28)), 28), DimStyle.Render(c.Session))
		}
		fmt.Println(DimStyle.Render("  dry run — nothing written"))
		return
	}

	runner, err := usage.NewRunner()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer runner.Close()

	// A bulk run notifies once, at the end. Per-session popups are right for
	// the daily job (1–2 sessions, and a silent miss should be visible) and
	// plainly wrong for a backfill of hundreds.
	bulk := len(pending) > bulkNotifyThreshold
	runner.Quiet = bulk

	ok, failed := 0, 0
	start := time.Now()
	for i, c := range pending {
		fmt.Printf("  %s %s %s ",
			DimStyle.Render(fmt.Sprintf("[%d/%d]", i+1, len(pending))),
			DimStyle.Render(c.Date), pad(NameStyle.Render(truncateUTF8(c.Project, 24)), 24))

		summary, err := runner.Summarize(c.Path)
		if err != nil {
			failed++
			fmt.Println(WarnStyle.Render("failed"))
			fmt.Fprintf(os.Stderr, "    %s: %v\n", c.Session, err)
			continue
		}
		d := &usage.Digest{
			Session:    c.Session,
			Date:       c.Date,
			Cwd:        c.Cwd,
			Project:    c.Project,
			Transcript: c.Path,
			Model:      usage.DigestModel,
			DigestedAt: time.Now().UTC().Format(time.RFC3339),
			Summary:    summary,
		}
		if err := usage.PutDigest(root, d); err != nil {
			failed++
			fmt.Println(WarnStyle.Render("unwritable"))
			fmt.Fprintf(os.Stderr, "    %s: %v\n", c.Session, err)
			continue
		}
		ok++
		fmt.Println(DimStyle.Render(fmt.Sprintf("%.1fkB", float64(len(summary))/1024)))
	}

	fmt.Printf("%s %s\n", HeaderStyle.Render("Digest"),
		DimStyle.Render(fmt.Sprintf("%d done, %d failed, %s elapsed",
			ok, failed, time.Since(start).Round(time.Second))))
	fmt.Println(DimStyle.Render("  " + shortenHome(root)))
	if bulk && failed > 0 {
		notify("claudeme digest", fmt.Sprintf("%d done, %d failed — the next run retries them", ok, failed))
	}
	if failed > 0 {
		// Not fatal: a failed session keeps no record, so the next run retries
		// it. Exit non-zero so a scheduled run shows up as failed in its log.
		os.Exit(1)
	}
}

// bulkNotifyThreshold is where a run stops being "today's sessions" and starts
// being a backfill.
const bulkNotifyThreshold = 5

// notify posts one desktop notification, best-effort. Mirrors summarize.sh's
// notifier so both halves of a run speak through the same channel.
func notify(title, msg string) {
	if bin := os.Getenv("NOTIFIER_BIN"); bin != "" {
		if path, err := exec.LookPath(bin); err == nil {
			_ = exec.Command(path, "-title", title, "-message", msg).Run()
			return
		}
	}
	if path, err := exec.LookPath("terminal-notifier"); err == nil {
		_ = exec.Command(path, "-title", title, "-message", msg).Run()
		return
	}
	if path, err := exec.LookPath("osascript"); err == nil {
		safe := strings.NewReplacer(`\`, "", `"`, "").Replace(msg)
		_ = exec.Command(path, "-e",
			fmt.Sprintf("display notification %q with title %q", safe, title)).Run()
	}
}

// yesterday is the default window: the sessions that finished while nobody
// was looking.
func yesterday() string {
	return time.Now().AddDate(0, 0, -1).Format("2006-01-02")
}

func isDate(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// ============ Scheduling ============

const digestJobLabel = "com.claudeme.digest"

func digestPlistPath() string { return plistPath(digestJobLabel) }

func digestLogPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "claudeme", "digest.log")
}

// digestPlist is a daily 05:00 launch agent — after `snapshot` has recorded
// the numbers, and clear of the hour `archive` used to run.
//
// PATH is set explicitly: launchd gives an agent a minimal one, and the
// summarizer shells out to `codex` and `python3`, neither of which is in it.
func digestPlist(bin, path string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>digest</string>
	</array>
	<key>EnvironmentVariables</key>
	<dict>
		<key>PATH</key>
		<string>%s</string>
	</dict>
	<key>RunAtLoad</key>
	<true/>
	<key>StartCalendarInterval</key>
	<dict>
		<key>Hour</key>
		<integer>5</integer>
		<key>Minute</key>
		<integer>0</integer>
	</dict>
	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
</dict>
</plist>
`, digestJobLabel, bin, path, digestLogPath(), digestLogPath())
}

// jobPath is the PATH a scheduled run gets: the one this shell has, plus the
// directories the summarizer's tools actually live in.
func jobPath() string {
	parts := strings.Split(os.Getenv("PATH"), ":")
	home, _ := os.UserHomeDir()
	for _, extra := range []string{
		filepath.Join(home, ".local", "bin"),
		"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin",
	} {
		parts = append(parts, extra)
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range parts {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return strings.Join(out, ":")
}

func installDigestJob() {
	if runtime.GOOS != "darwin" {
		fmt.Fprintln(os.Stderr, "scheduling is macOS-only; run `claudeme digest` from cron instead")
		os.Exit(1)
	}
	if _, err := exec.LookPath("codex"); err != nil {
		fmt.Fprintln(os.Stderr, "codex is not on PATH — `claudeme digest` needs it to summarize")
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

	path := digestPlistPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(path, []byte(digestPlist(bin, jobPath())), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	bootoutLabel(digestJobLabel, path)
	if err := bootstrap(path); err != nil {
		fmt.Fprintf(os.Stderr, "wrote %s but launchctl refused it: %v\n", path, err)
		os.Exit(1)
	}

	fmt.Println(HeaderStyle.Render("Digest") + " scheduled daily at 05:00")
	fmt.Println(DimStyle.Render("  sessions → " + shortenHome(usage.DigestRoot())))
	fmt.Println(DimStyle.Render("  " + path))
	fmt.Println(DimStyle.Render("  log: " + digestLogPath()))
}

func uninstallDigestJob() {
	path := digestPlistPath()
	bootoutLabel(digestJobLabel, path)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(HeaderStyle.Render("Digest") + " schedule removed")
	fmt.Println(DimStyle.Render("  digests already written stay in " + shortenHome(usage.DigestRoot())))
}

func digestStatus() {
	root := usage.DigestRoot()
	fmt.Println(HeaderStyle.Render("Digest") + " " + DimStyle.Render(shortenHome(root)))
	if _, err := os.Stat(digestPlistPath()); err != nil {
		fmt.Println("  not scheduled — claudeme digest --install")
	} else {
		fmt.Println("  scheduled daily at 05:00")
		fmt.Println(DimStyle.Render("  " + digestPlistPath()))
	}

	dates, sessions := digestCounts(root)
	if dates == 0 {
		fmt.Println(DimStyle.Render("  nothing digested yet"))
		return
	}
	fmt.Printf("  %d session%s across %d date%s  %s\n",
		sessions, plural(sessions), dates, plural(dates), DimStyle.Render(bytes(treeSize(root))))

	cands, err := usage.ScanSessions(usage.Roots())
	if err != nil {
		return
	}
	pending, err := usage.Pending(root, cands)
	if err != nil {
		return
	}
	fmt.Printf("  %d session%s not digested yet\n", len(pending), plural(len(pending)))
}

// digestCounts totals what is on record without loading every summary.
func digestCounts(root string) (dates, sessions int) {
	days, err := os.ReadDir(root)
	if err != nil {
		return 0, 0
	}
	for _, day := range days {
		if !day.IsDir() {
			continue
		}
		dates++
		files, err := os.ReadDir(filepath.Join(root, day.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".json") {
				continue
			}
			df, err := usage.LoadDigest(filepath.Join(root, day.Name(), f.Name()))
			if err != nil {
				continue
			}
			sessions += len(df.Sessions)
		}
	}
	return dates, sessions
}
