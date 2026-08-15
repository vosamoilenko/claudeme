package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
	"github.com/vosamoilenko/claudeme/internal/usage"
)

// heatLevels are the five shades of a cell, from a day with nothing on it to
// the busiest quartile — the same ramp GitHub uses, in 256-colour terms.
var heatLevels = []lipgloss.Style{
	lipgloss.NewStyle().Foreground(lipgloss.Color("236")),
	lipgloss.NewStyle().Foreground(lipgloss.Color("22")),
	lipgloss.NewStyle().Foreground(lipgloss.Color("28")),
	lipgloss.NewStyle().Foreground(lipgloss.Color("34")),
	lipgloss.NewStyle().Foreground(lipgloss.Color("40")),
}

// cellGlyph is two columns wide, which makes a day square in most terminals.
const cellGlyph = "██"

// Heatmap draws a year of activity as a grid of days.
func Heatmap() {
	args := os.Args[2:]
	metric, query := "cost", ""
	weeks, live, all := 0, false, false

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--weeks", "-w":
			if i+1 < len(args) {
				n, err := strconv.Atoi(args[i+1])
				if err != nil || n < 1 {
					fmt.Fprintf(os.Stderr, "invalid --weeks %q\n", args[i+1])
					os.Exit(1)
				}
				weeks = n
				i++
			}
		case "--metric", "-m":
			if i+1 < len(args) {
				metric = strings.ToLower(args[i+1])
				i++
			}
		case "--live":
			live = true
		case "--all", "-a":
			all = true
		default:
			if strings.HasPrefix(args[i], "-") {
				fmt.Fprintf(os.Stderr, "unknown flag %q\n", args[i])
				os.Exit(1)
			}
			query = args[i]
		}
	}
	switch metric {
	case "cost", "calls", "sessions":
	default:
		fmt.Fprintf(os.Stderr, "unknown --metric %q — try cost, calls or sessions\n", metric)
		os.Exit(1)
	}

	values, scope := heatSeries(query, metric, live)
	if len(values) == 0 {
		fmt.Println("no usage recorded yet")
		return
	}

	end := time.Now()
	if weeks == 0 {
		weeks = fitWeeks()
		if all {
			weeks = weeksSince(earliest(values), end)
		}
	}

	g := usage.BuildGrid(values, end, weeks)
	s := usage.Summarize(g)

	fmt.Println(HeaderStyle.Render("Heatmap") + " " +
		DimStyle.Render(scope+" · "+metric+" · "+coveredRange(g.First, g.Last)))
	fmt.Println()
	printGrid(g)
	fmt.Println()
	printHeatSummary(s, metric)
}

// printGrid draws the month row, then the seven day rows.
func printGrid(g usage.Grid) {
	// The day column is three characters plus a space, so the month labels
	// above it start at the same offset as the first cell.
	const gutter = "    "

	// A label is three characters over two-wide columns, so it overhangs the
	// next one. Laying the row out as a rune buffer keeps every label above
	// its own column and simply drops one that the previous label ran into,
	// rather than losing the column after every label.
	labels := usage.MonthLabels(g)
	// One column of slack on the right so a label above the last week isn't
	// clipped to two characters.
	row := []rune(strings.Repeat(" ", 2*len(g.Weeks)+1))
	free := 0
	for w := 0; w < len(g.Weeks); w++ {
		m, ok := labels[w]
		if !ok || 2*w < free {
			continue
		}
		copy(row[2*w:], []rune(m))
		free = 2*w + len(m) + 1
	}
	fmt.Println(DimStyle.Render(gutter + strings.TrimRight(string(row), " ")))

	dayNames := [7]string{"", "Mon", "", "Wed", "", "Fri", ""}
	for d := 0; d < 7; d++ {
		var row strings.Builder
		row.WriteString(DimStyle.Render(fmt.Sprintf("%-3s ", dayNames[d])))
		for w := range g.Weeks {
			c := g.Weeks[w][d]
			if c.Date == "" {
				row.WriteString("  ")
				continue
			}
			row.WriteString(heatLevels[c.Level].Render(cellGlyph))
		}
		fmt.Println(row.String())
	}
}

// printHeatSummary closes the grid with the legend and what the squares add up
// to: how much, how many days, the worst one, and the streaks.
func printHeatSummary(s usage.Summary, metric string) {
	var legend strings.Builder
	legend.WriteString("Less ")
	for i := range heatLevels {
		legend.WriteString(heatLevels[i].Render(cellGlyph))
	}
	legend.WriteString(" More")

	fmt.Printf("  %s   %s\n",
		DimStyle.Render(legend.String()),
		DimStyle.Render(fmt.Sprintf("%d of %d days active", s.Active, s.Days)))

	if s.Active == 0 {
		return
	}
	fmt.Printf("  %s %s   %s   %s\n",
		DimStyle.Render("total"), HeaderStyle.Render(heatValue(s.Total, metric)),
		DimStyle.Render(fmt.Sprintf("peak %s %s",
			shortDate(s.Peak, false), heatValue(s.PeakVal, metric))),
		DimStyle.Render(fmt.Sprintf("streak %d day%s · longest %d",
			s.Current, plural(s.Current), s.Longest)))
}

// heatValue formats a metric the way its unit reads: money for cost, a plain
// count for the rest.
func heatValue(v float64, metric string) string {
	if metric == "cost" {
		return money(int64(v * 1e6))
	}
	return comma(int(v + 0.5))
}

// heatSeries builds the day → value series, and the scope label for it.
//
// Two sources answer the same question over different spans: the snapshot
// history reaches back past Claude Code's retention window, while the
// transcripts on disk are the fresher read of what is still there. Days are
// merged by taking the larger of the two — both are list-price sums of the
// same calls, so the bigger number is the one that saw more of the day.
func heatSeries(query, metric string, live bool) (map[string]float64, string) {
	values := map[string]float64{}
	scope := "all projects"

	if !live {
		if h, err := usage.LoadHistory(usage.HistoryPath()); err == nil {
			for day, d := range h.Days {
				if v, ok := historyValue(d, query, metric); ok {
					values[day] = v
				}
			}
		}
	}

	projects, err := loadProjects(query)
	if err != nil {
		if len(values) > 0 {
			return values, scope // history knows this project, transcripts don't
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if query != "" {
		// Names collide across checkouts, so identify matches by path once
		// more than one project answers the query — as `usage` does.
		names := make([]string, 0, len(projects))
		for _, p := range projects {
			if len(projects) == 1 {
				names = append(names, p.Name)
			} else {
				names = append(names, shortenHome(p.Path))
			}
		}
		scope = strings.Join(names, ", ")
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
	for day, s := range rep.Days {
		var v float64
		switch metric {
		case "cost":
			v = float64(s.Cost) / 1e6
		case "calls":
			v = float64(s.Calls)
		case "sessions":
			v = float64(len(rep.DaySess[day]))
		}
		if v > values[day] {
			values[day] = v
		}
	}
	return values, scope
}

// historyValue picks one day's metric out of a snapshot record, scoped to the
// queried project when there is one. Reports false when the day has nothing
// for that project, so it stays an empty square rather than a zero.
func historyValue(d *usage.Day, query, metric string) (float64, bool) {
	if query == "" {
		switch metric {
		case "calls":
			return float64(d.Calls), true
		case "sessions":
			return float64(d.Sessions), true
		default:
			return d.Cost, true
		}
	}
	// A project's per-day sessions were never recorded, so a scoped sessions
	// heatmap comes from the transcripts alone.
	q := strings.ToLower(query)
	var cost float64
	var calls int
	found := false
	for name, b := range d.Projects {
		if !strings.Contains(strings.ToLower(name), q) {
			continue
		}
		found = true
		cost += b.Cost
		calls += b.Calls
	}
	if !found {
		return 0, false
	}
	switch metric {
	case "calls":
		return float64(calls), true
	case "sessions":
		return 0, false
	default:
		return cost, true
	}
}

// fitWeeks is as much of a year as the terminal can show without wrapping.
func fitWeeks() int {
	w, _, err := term.GetSize(os.Stdout.Fd())
	if err != nil || w <= 0 {
		w = 80
	}
	weeks := (w - 6) / 2
	if weeks > 53 {
		weeks = 53
	}
	if weeks < 4 {
		weeks = 4
	}
	return weeks
}

// weeksSince is how many columns it takes to reach back to day.
func weeksSince(day string, end time.Time) int {
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		return 53
	}
	weeks := int(end.Sub(t).Hours()/(24*7)) + 2
	if weeks < 4 {
		weeks = 4
	}
	return weeks
}

func earliest(values map[string]float64) string {
	first := ""
	for day := range values {
		if first == "" || day < first {
			first = day
		}
	}
	return first
}
