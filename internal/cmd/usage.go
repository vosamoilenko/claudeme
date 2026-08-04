package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/vosamoilenko/claudeme/internal/config"
	"github.com/vosamoilenko/claudeme/internal/usage"
)

// Projects lists the projects found in the shared transcript directory.
func Projects() {
	args := os.Args[2:]
	withCost, showAll, expand := false, false, false
	var query string
	for _, a := range args {
		switch a {
		case "--cost", "-c":
			withCost = true
		case "--all", "-a":
			showAll = true
		case "--expand", "-e":
			expand = true
		default:
			query = a
		}
	}

	projects, err := loadProjects(query)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	empty := 0
	if !showAll {
		var kept []usage.Project
		for _, p := range projects {
			if p.Sessions == 0 {
				empty++
				continue
			}
			kept = append(kept, p)
		}
		projects = kept
	}

	fmt.Println(HeaderStyle.Render("Projects") + " " + DimStyle.Render(usage.ProjectsRoot()))
	fmt.Println()

	// Every project is a block: its counts on the name line, its path on the
	// next. A path column would either truncate the deep ones or push the
	// numbers off screen.
	if !withCost {
		fmt.Println(DimStyle.Render(fmt.Sprintf("  %-*s%6s%10s%11s",
			nameWidth, "name", "dirs", "sessions", "last")))
	}

	var total int64
	var collapsed int
	var first, last string

	// With costs in hand the list reads best sorted by spend; without them the
	// most recently touched project is the one being looked for.
	type row struct {
		p    usage.Project
		rep  *usage.Report
		cost int64
	}
	rows := make([]row, 0, len(projects))
	for _, p := range projects {
		r := row{p: p}
		if withCost {
			var err error
			if r.rep, err = usage.Analyze(usage.Roots(), p.Dirs); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			r.cost = memberCost(p, r.rep)
			total += r.cost
			// The window is the union of the projects listed, taken from the
			// reports already in hand rather than a second Analyze.
			if f, l := usage.Window(r.rep); f != "" {
				if first == "" || f < first {
					first = f
				}
				if l > last {
					last = l
				}
			}
		}
		rows = append(rows, r)
	}
	if withCost {
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].cost > rows[j].cost })
	}

	for _, r := range rows {
		p, rep := r.p, r.rep
		if withCost {
			// Name and cost, then the path on its own line: a block, not a row.
			fmt.Println("  " + pad(NameStyle.Render(truncateUTF8(p.Name, nameWidth-2)), nameWidth) +
				MoneyStyle.Render(money(r.cost)))
			fmt.Println("  " + DimStyle.Render(shortenHome(p.Path)))
		} else {
			fmt.Println("  " + pad(NameStyle.Render(truncateUTF8(p.Name, nameWidth-2)), nameWidth) +
				DimStyle.Render(fmt.Sprintf("%6d%10d%11s",
					len(p.Dirs), p.Sessions, lastSeen(p.Modified))))
			// Indented one step further than the name, so a name still starts
			// every project in a list with no blank lines between them.
			fmt.Println(breakdownIndent + DimStyle.Render(shortenHome(p.Path)))
		}
		if expand {
			printMembers(p, rep, r.cost)
		} else if len(p.Members) > 1 {
			collapsed += len(p.Members)
		}
		if printBreakdown(p, rep, r.cost) {
			fmt.Println()
		}
	}

	if !withCost {
		fmt.Println()
	}
	summary := fmt.Sprintf("  %d project%s", len(projects), plural(len(projects)))
	if withCost {
		summary += "   " + HeaderStyle.Render(money(total))
		if span := coveredRange(first, last); span != "" {
			summary += "   " + DimStyle.Render(span)
		}
	}
	fmt.Println(summary)
	if withCost {
		fmt.Println(DimStyle.Render("  " + retentionNote()))
	}
	if collapsed > 0 {
		fmt.Println(DimStyle.Render(fmt.Sprintf("  %d subfolders collapsed (--expand to show)", collapsed)))
	}
	if empty > 0 {
		fmt.Println(DimStyle.Render(fmt.Sprintf("  %d empty transcript dirs hidden (--all to show)", empty)))
	}
	fmt.Println(DimStyle.Render("  Report: claudeme usage <name>"))
}

// memberCost sums the spend of the cwds this project owns, rather than
// everything in its transcript directories.
//
// The two differ only when one directory holds cwds that clustered into
// different projects — sessions resumed after a cd elsewhere. Costing by cwd
// keeps each project's share to itself instead of both charging for the whole
// directory, and makes the sub-rows sum to this row by construction.
func memberCost(p usage.Project, rep *usage.Report) int64 {
	var cost int64
	for _, m := range p.Members {
		if s := rep.Cwds[m.Cwd]; s != nil {
			cost += s.Cost
		}
	}
	return cost
}

// printMembers lists the cwds a project folded together — worktrees and
// subdirectories — so a clustered row still shows where the sessions ran.
// A project with a single cwd has nothing to disclose. Costs come out of the
// project's own report, so the sub-rows sum to the row above them.
func printMembers(p usage.Project, rep *usage.Report, total int64) {
	if len(p.Members) < 2 {
		return
	}
	for _, m := range p.Members {
		// In a cost block the member rows share the skill rows' columns, so the
		// two lists read as one; without costs they keep the table's columns.
		// A trailing slash is what tells these rows apart from the skill rows
		// they sit above — same columns, different kind of thing.
		label := m.Label
		if !strings.HasSuffix(label, "/") {
			label += "/"
		}
		// The extra indent is taken out of the label column, so the numbers
		// stay under the parent's.
		label = truncateTail(label, skillWidth-2)
		if rep == nil {
			fmt.Println(breakdownIndent + DimStyle.Render(pad(label, nameWidth-2)+
				fmt.Sprintf("%6d%10d%11s", len(m.Dirs), m.Sessions, lastSeen(m.Modified))))
			continue
		}
		var cost int64
		if s := rep.Cwds[m.Cwd]; s != nil {
			cost = s.Cost
		}
		fmt.Println(breakdownIndent + DimStyle.Render(
			pad(label, skillWidth)+lpad(money(cost), 10)+lpad(share(cost, total), 6)))
	}
}

const (
	// maxSkills caps the stacked skill list. The tail is folded into one row, so
	// the block still accounts for the project's whole spend.
	maxSkills = 5
	// nameWidth is the project-name column of a cost block.
	nameWidth = 30
	// skillWidth is the skill-name column under it. Names longer than this are
	// truncated rather than allowed to shift the money column.
	skillWidth = 30
	// breakdownIndent aligns a block's detail under its name.
	breakdownIndent = "    "
)

// printBreakdown explains a project's cost under its name: what each skill
// spent, the plain remainder, then how long it ran and its worst day. Every
// number is summed from the cwds the project owns, the same way the headline
// cost is, so the rows add up to it rather than to whatever else shared the
// transcript directories. Reports whether anything was printed.
func printBreakdown(p usage.Project, rep *usage.Report, total int64) bool {
	if rep == nil {
		return false
	}
	days, skills := map[string]*usage.Stats{}, map[string]*usage.Stats{}
	for _, m := range p.Members {
		if s := rep.Cwds[m.Cwd]; s != nil {
			usage.Merge(days, s.Days)
			usage.Merge(skills, s.Skills)
		}
	}
	if len(days) == 0 {
		return false
	}
	printSkills(skills, total)
	printActivity(days, p.Sessions, p.Modified)
	return true
}

// printSkills stacks the skills a project spent on, widest first, with the tail
// folded into one row and the spend that ran with no skill loaded last — it is
// most projects' biggest slice and names nothing, so it closes the list.
func printSkills(skills map[string]*usage.Stats, total int64) {
	var bare int64
	named := make([]usage.Row, 0, len(skills))
	for _, r := range usage.Rank(skills) {
		if r.Key == "" || r.Key == usage.NoSkill {
			bare += r.Cost
			continue
		}
		named = append(named, r)
	}

	row := func(label string, cost int64, style lipgloss.Style) {
		fmt.Println(breakdownIndent +
			style.Render(pad(truncateUTF8(label, skillWidth-2), skillWidth)) +
			style.Render(lpad(money(cost), 10)) +
			DimStyle.Render(lpad(share(cost, total), 6)))
	}
	for i, r := range named {
		// Folding a single leftover into "+1" would hide a name and save no
		// space, so the cap only bites when at least two rows are hidden.
		if i == maxSkills && len(named) > maxSkills+1 {
			var rest int64
			for _, o := range named[i:] {
				rest += o.Cost
			}
			row(fmt.Sprintf("+%d more skills", len(named)-i), rest, DimStyle)
			break
		}
		row(skillLabel(r.Key), r.Cost, lipgloss.NewStyle())
	}
	if bare > 0 {
		row("plain (no skill)", bare, DimStyle)
	}
}

// printActivity closes a block with the facts the money can't carry: how many
// days the project ran, which day cost the most, and how recent it all is.
func printActivity(days map[string]*usage.Stats, sessions int, modified time.Time) {
	peak := usage.Rank(days)[0] // Rank sorts by cost, so this is the worst day
	var withYear bool
	for d := range days {
		if len(d) >= 4 && d[:4] != peak.Key[:4] {
			withYear = true
		}
	}
	fmt.Println(breakdownIndent + DimStyle.Render(fmt.Sprintf(
		"%d active day%s · peak %s %s · %d session%s · last %s",
		len(days), plural(len(days)), shortDate(peak.Key, withYear), money(peak.Cost),
		sessions, plural(sessions), lastSeen(modified))))
}

// share renders a cost as a whole-percent share of its project.
func share(cost, total int64) string {
	if total <= 0 {
		return ""
	}
	return fmt.Sprintf("%d%%", cost*100/total)
}

// pad and lpad align a styled cell by display width; %-*s would count bytes and
// ANSI escapes instead.
func pad(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

func lpad(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return strings.Repeat(" ", d) + s
	}
	return s
}

// skillLabel shortens a skill id for a block row. The plugin prefix is the
// same on every skill from a pack and eats the width the actual name needs, so
// it goes; `claudeme usage` still prints ids in full.
func skillLabel(skill string) string {
	if _, after, found := strings.Cut(skill, ":"); found {
		skill = after
	}
	return skill
}

// truncateTail keeps the end of an over-long path. The leaf directory is what
// distinguishes two worktrees of the same branch series; the prefix is not.
func truncateTail(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return "..." + string(runes[len(runes)-(max-3):])
}

// Usage prints the token/cost report for one project, or all of them.
func Usage() {
	args := os.Args[2:]
	top := 20
	var query string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--top":
			if i+1 < len(args) {
				fmt.Sscanf(args[i+1], "%d", &top)
				i++
			}
		default:
			query = args[i]
		}
	}

	projects, err := loadProjects(query)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	var dirs []string
	for _, p := range projects {
		dirs = append(dirs, p.Dirs...)
	}

	rep, err := usage.Analyze(usage.Roots(), dirs)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	scope := "all projects"
	if query != "" {
		// Names collide across checkouts, so identify matches by path once
		// more than one project answers the query.
		labels := make([]string, 0, len(projects))
		for _, p := range projects {
			if len(projects) == 1 {
				labels = append(labels, p.Name)
			} else {
				labels = append(labels, shortenHome(p.Path))
			}
		}
		scope = strings.Join(labels, ", ")
	}
	fmt.Println(HeaderStyle.Render("Usage") + " " + DimStyle.Render(scope))
	sub := fmt.Sprintf("%d transcripts, %d sessions", rep.Files, len(rep.Sessions))
	if span := coveredRange(usage.Window(rep)); span != "" {
		sub += ", " + span
	}
	fmt.Println(DimStyle.Render(sub))
	fmt.Println(DimStyle.Render(retentionNote()))

	days := usage.Rank(rep.Days)
	sort.Slice(days, func(i, j int) bool { return days[i].Key < days[j].Key })
	printTable("PER DAY", "date", days, rep.Total.Cost, 0)

	printTable("PER MODEL", "model", usage.Rank(rep.Models), rep.Total.Cost, 0)
	printTable("MAIN LOOP vs SUBAGENTS", "lane", usage.Rank(rep.Lanes), rep.Total.Cost, 0)
	if len(rep.Agents) > 0 {
		printTable("PER AGENT TYPE", "agent", usage.Rank(rep.Agents), rep.Total.Cost, 0)
	}
	printTable("PER SKILL", "skill", usage.Rank(rep.Skills), rep.Total.Cost, 25)
	printTable("SESSIONS", "session id", usage.Rank(rep.Sessions), rep.Total.Cost, top)

	fmt.Println("\n" + HeaderStyle.Render("TOOL CALLS") + DimStyle.Render("  (not cost-attributed)"))
	for i, r := range usage.RankCounts(rep.Tools) {
		if i >= 30 {
			break
		}
		fmt.Printf("  %6d  %s\n", r.Calls, r.Key)
	}

	t := rep.Total
	calls := t.Calls
	if calls == 0 {
		calls = 1
	}
	fmt.Println()
	fmt.Printf("%s  %s over %s calls\n",
		HeaderStyle.Render("TOTAL"), HeaderStyle.Render(money(t.Cost)), comma(t.Calls))
	fmt.Println(DimStyle.Render(fmt.Sprintf(
		"output %s   cache writes %s   cache reads %s",
		comma(t.Out), comma(t.CacheWrite), comma(t.CacheRead))))
	fmt.Println(DimStyle.Render(fmt.Sprintf(
		"avg context re-read per call: %s tokens", comma(t.CacheRead/calls))))

	// Approximate: prices cache reads at 10% of the opus-tier input price.
	if t.Cost > 0 {
		readCost := float64(t.CacheRead) * 5.0 * 0.1
		fmt.Println(DimStyle.Render(fmt.Sprintf(
			"cache reads are ~%.0f%% of spend", 100*readCost/float64(t.Cost))))
	}

	if len(rep.Unpriced) > 0 {
		var names []string
		for m, n := range rep.Unpriced {
			names = append(names, fmt.Sprintf("%s (%d)", m, n))
		}
		sort.Strings(names)
		fmt.Println(WarnStyle.Render("\nno list price, excluded: " + strings.Join(names, ", ")))
	}
	fmt.Println(DimStyle.Render("\nList pricing. On a Max/Pro subscription the real invoice is $0."))
}

func loadProjects(query string) ([]usage.Project, error) {
	root := usage.ProjectsRoot()
	all, err := usage.Discover(usage.Roots())
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", root, err)
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("no transcripts found in %s", root)
	}
	matched := usage.Match(all, query)
	if len(matched) == 0 {
		return nil, fmt.Errorf("no project matches %q — try: claudeme projects", query)
	}
	return matched, nil
}

func printTable(title, label string, rows []usage.Row, total int64, limit int) {
	if len(rows) == 0 {
		return
	}
	if total == 0 {
		total = 1
	}

	fmt.Println("\n" + HeaderStyle.Render(title))
	header := fmt.Sprintf("%-34s%7s%11s%13s%10s%7s",
		label, "calls", "output", "cache rd", "cost", "%")
	fmt.Println(DimStyle.Render(header))

	shown := rows
	if limit > 0 && len(rows) > limit {
		shown = rows[:limit]
	}
	for _, r := range shown {
		fmt.Printf("%-34s%7d%11s%13s%10s%6.1f%%\n",
			truncateUTF8(r.Key, 33), r.Calls, comma(r.Out), comma(r.CacheRead),
			money(r.Cost), 100*float64(r.Cost)/float64(total))
	}
	if len(shown) < len(rows) {
		var rest int64
		for _, r := range rows[len(shown):] {
			rest += r.Cost
		}
		fmt.Println(DimStyle.Render(fmt.Sprintf("%-34s%7s%11s%13s%10s",
			fmt.Sprintf("... %d more", len(rows)-len(shown)), "", "", "", money(rest))))
	}
}

// coveredRange renders the span a report covers — "Jul 04 – Aug 03" — or ""
// when nothing in it carries a date. Years appear only when the range crosses
// one, which is the only time they disambiguate anything.
func coveredRange(first, last string) string {
	if first == "" {
		return ""
	}
	withYear := first[:4] != last[:4]
	if first == last {
		return shortDate(first, withYear)
	}
	return shortDate(first, withYear) + " – " + shortDate(last, withYear)
}

func shortDate(day string, withYear bool) string {
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		return day
	}
	if withYear {
		return t.Format("Jan 02 2006")
	}
	return t.Format("Jan 02")
}

// retentionNote explains why the range starts where it does. Without an
// archive the answer is that Claude Code deleted the earlier transcripts; with
// one, history only gets deeper from here, but whatever was already gone when
// archiving began stays gone.
func retentionNote() string {
	if entries, err := os.ReadDir(usage.ArchiveRoot()); err == nil && len(entries) > 0 {
		return "archived transcripts included; anything deleted before archiving began is not in here"
	}
	return fmt.Sprintf(
		"covers what Claude Code still keeps (cleanupPeriodDays %d); run `claudeme archive` to stop losing history",
		config.RetentionDays())
}

// lastSeen renders a transcript mtime, or a dash when there is none.
func lastSeen(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return formatAge(t)
}

func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || !strings.HasPrefix(path, home) {
		return path
	}
	return "~" + strings.TrimPrefix(path, home)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func money(micros int64) string {
	return "$" + fmt.Sprintf("%.2f", float64(micros)/1e6)
}

func comma(n int) string {
	s := fmt.Sprintf("%d", n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}
