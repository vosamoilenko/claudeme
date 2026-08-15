package usage

import (
	"sort"
	"time"
)

// A heatmap is the year seen at a glance: one square per day, darker where the
// day cost more. The grid is built here and drawn in internal/cmd, so the
// bucketing rules are testable without a terminal.

// Cell is one day of the grid. A cell outside the covered range — the padding
// before the first column starts or after today — keeps an empty Date and is
// drawn as blank rather than as a zero-spend day.
type Cell struct {
	Date  string // YYYY-MM-DD, "" when the cell is padding
	Value float64
	Level int // 0 none, 1..4 increasing intensity
}

// Grid is the heatmap: week columns, each holding Sunday..Saturday.
type Grid struct {
	Weeks [][7]Cell
	First string // first dated cell
	Last  string // last dated cell
}

// BuildGrid lays values out as GitHub does: columns are weeks running left to
// right, rows are days of the week, and the last column is the one holding
// end. Days with no entry in values are level 0.
func BuildGrid(values map[string]float64, end time.Time, weeks int) Grid {
	if weeks < 1 {
		weeks = 1
	}
	end = time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, end.Location())
	// The grid always ends on a Saturday so every column is a full week; the
	// days after end are padding.
	last := end.AddDate(0, 0, 6-int(end.Weekday()))
	start := last.AddDate(0, 0, -(weeks*7 - 1))

	g := Grid{Weeks: make([][7]Cell, weeks)}
	inRange := map[string]float64{}
	for w := 0; w < weeks; w++ {
		for d := 0; d < 7; d++ {
			day := start.AddDate(0, 0, w*7+d)
			if day.After(end) {
				continue
			}
			key := day.Format("2006-01-02")
			v := values[key]
			g.Weeks[w][d] = Cell{Date: key, Value: v}
			inRange[key] = v
			if g.First == "" {
				g.First = key
			}
			g.Last = key
		}
	}

	// Levels are relative to the window on screen, not to all of history: a
	// quiet month should still show its own busy days.
	t := thresholds(inRange)
	for w := range g.Weeks {
		for d := range g.Weeks[w] {
			c := &g.Weeks[w][d]
			if c.Date != "" {
				c.Level = level(c.Value, t)
			}
		}
	}
	return g
}

// thresholds returns the three cuts between levels 1..4, taken as quartiles of
// the non-zero values. Quartiles rather than an even split of the range so one
// runaway day doesn't flatten every other day to the lightest shade.
func thresholds(values map[string]float64) [3]float64 {
	nonzero := make([]float64, 0, len(values))
	for _, v := range values {
		if v > 0 {
			nonzero = append(nonzero, v)
		}
	}
	if len(nonzero) == 0 {
		return [3]float64{}
	}
	sort.Float64s(nonzero)
	at := func(p float64) float64 {
		i := int(p * float64(len(nonzero)-1))
		return nonzero[i]
	}
	return [3]float64{at(0.25), at(0.50), at(0.75)}
}

func level(v float64, t [3]float64) int {
	switch {
	case v <= 0:
		return 0
	case v <= t[0]:
		return 1
	case v <= t[1]:
		return 2
	case v <= t[2]:
		return 3
	default:
		return 4
	}
}

// Summary is what the grid adds up to, for the line under it.
type Summary struct {
	Total   float64
	Active  int    // days with any value
	Days    int    // dated cells in the grid
	Peak    string // busiest day
	PeakVal float64
	Current int // active days ending at the last dated cell
	Longest int // longest run of active days in the grid
}

// Summarize walks the grid in date order and reports the totals and streaks.
// A streak is consecutive active days; the current one is only a streak if it
// reaches the last day of the grid.
func Summarize(g Grid) Summary {
	var s Summary
	run := 0
	for _, cells := range g.Weeks {
		for _, c := range cells {
			if c.Date == "" {
				continue
			}
			s.Days++
			s.Total += c.Value
			if c.Value > 0 {
				s.Active++
				run++
				if run > s.Longest {
					s.Longest = run
				}
			} else {
				run = 0
			}
			if c.Value > s.PeakVal {
				s.PeakVal, s.Peak = c.Value, c.Date
			}
		}
	}
	s.Current = run
	return s
}

// MonthLabels maps a week column to the month name that starts in it, so the
// row above the grid reads Jan Feb Mar. A month is labelled at the first
// column whose Sunday falls inside it, and never twice in a row.
func MonthLabels(g Grid) map[int]string {
	labels := map[int]string{}
	prev := ""
	for w, cells := range g.Weeks {
		var first string
		for _, c := range cells {
			if c.Date != "" {
				first = c.Date
				break
			}
		}
		if first == "" {
			continue
		}
		t, err := time.Parse("2006-01-02", first)
		if err != nil {
			continue
		}
		m := t.Format("Jan")
		if m != prev {
			labels[w] = m
			prev = m
		}
	}
	return labels
}
