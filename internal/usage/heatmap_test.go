package usage

import (
	"testing"
	"time"
)

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestBuildGridEndsOnTheDayGiven(t *testing.T) {
	// 2026-08-06 is a Thursday.
	g := BuildGrid(map[string]float64{"2026-08-06": 1}, day("2026-08-06"), 4)
	if len(g.Weeks) != 4 {
		t.Fatalf("weeks = %d, want 4", len(g.Weeks))
	}
	if g.Last != "2026-08-06" {
		t.Errorf("last = %q, want 2026-08-06", g.Last)
	}
	if g.First != "2026-07-12" { // Sunday, 4 weeks back
		t.Errorf("first = %q, want 2026-07-12", g.First)
	}
	// Friday and Saturday of the final column are padding.
	if c := g.Weeks[3][5]; c.Date != "" {
		t.Errorf("future cell dated %q, want padding", c.Date)
	}
	if c := g.Weeks[3][4]; c.Date != "2026-08-06" || c.Value != 1 {
		t.Errorf("thursday cell = %+v", c)
	}
}

func TestLevelsSpreadOverQuartiles(t *testing.T) {
	values := map[string]float64{}
	// Eight active days with wildly different sizes, ending on the last day.
	for i, v := range []float64{1, 2, 3, 4, 5, 6, 7, 900} {
		values[day("2026-07-30").AddDate(0, 0, i).Format("2006-01-02")] = v
	}
	g := BuildGrid(values, day("2026-08-06"), 4)

	got := map[string]int{}
	for _, week := range g.Weeks {
		for _, c := range week {
			if c.Date != "" {
				got[c.Date] = c.Level
			}
		}
	}
	if got["2026-07-29"] != 0 {
		t.Errorf("empty day level = %d, want 0", got["2026-07-29"])
	}
	if got["2026-07-30"] != 1 {
		t.Errorf("cheapest day level = %d, want 1", got["2026-07-30"])
	}
	if got["2026-08-06"] != 4 {
		t.Errorf("outlier day level = %d, want 4", got["2026-08-06"])
	}
	// One runaway day must not flatten the rest to level 1.
	if got["2026-08-04"] < 2 {
		t.Errorf("mid day level = %d, want >= 2", got["2026-08-04"])
	}
}

func TestSummarizeStreaks(t *testing.T) {
	values := map[string]float64{
		"2026-07-20": 1, "2026-07-21": 1, "2026-07-22": 1, // 3-day run
		"2026-08-05": 2, "2026-08-06": 5, // current run, ends today
	}
	s := Summarize(BuildGrid(values, day("2026-08-06"), 4))
	if s.Active != 5 {
		t.Errorf("active = %d, want 5", s.Active)
	}
	if s.Total != 10 {
		t.Errorf("total = %v, want 10", s.Total)
	}
	if s.Peak != "2026-08-06" || s.PeakVal != 5 {
		t.Errorf("peak = %s %v", s.Peak, s.PeakVal)
	}
	if s.Longest != 3 {
		t.Errorf("longest = %d, want 3", s.Longest)
	}
	if s.Current != 2 {
		t.Errorf("current = %d, want 2", s.Current)
	}
}

func TestSummarizeCurrentStreakBreaksOnAnIdleToday(t *testing.T) {
	s := Summarize(BuildGrid(map[string]float64{"2026-08-04": 1}, day("2026-08-06"), 2))
	if s.Current != 0 {
		t.Errorf("current = %d, want 0", s.Current)
	}
	if s.Longest != 1 {
		t.Errorf("longest = %d, want 1", s.Longest)
	}
}

func TestMonthLabelsOncePerMonth(t *testing.T) {
	g := BuildGrid(nil, day("2026-08-06"), 12)
	labels := MonthLabels(g)
	seen := map[string]int{}
	for _, m := range labels {
		seen[m]++
	}
	for m, n := range seen {
		if n > 1 {
			t.Errorf("month %s labelled %d times", m, n)
		}
	}
	if len(labels) < 2 {
		t.Errorf("labels = %v, want at least two months over 12 weeks", labels)
	}
}

func TestEmptyValuesHaveNoLevels(t *testing.T) {
	g := BuildGrid(nil, day("2026-08-06"), 3)
	for _, week := range g.Weeks {
		for _, c := range week {
			if c.Level != 0 {
				t.Fatalf("cell %q level = %d, want 0", c.Date, c.Level)
			}
		}
	}
}
