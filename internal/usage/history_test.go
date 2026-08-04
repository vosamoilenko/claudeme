package usage

import (
	"path/filepath"
	"testing"
)

// report builds a report with the day-crossed maps a snapshot needs.
func testReport(rows ...struct {
	day, model, skill, cwd, sess string
	cost                         int64
}) *Report {
	rep := newReport()
	for _, r := range rows {
		row := Stats{Cost: r.cost, Calls: 1, In: 10, Out: 20}
		bump(rep.Days, r.day, row)
		bump(rep.DayModels, r.day+Sep+r.model, row)
		bump(rep.DaySkills, r.day+Sep+r.skill, row)
		bumpCwd(rep.Cwds, r.cwd, r.day, r.skill, row)
		if rep.DaySess[r.day] == nil {
			rep.DaySess[r.day] = map[string]bool{}
		}
		rep.DaySess[r.day][r.sess] = true
		rep.Total.add(row)
	}
	return rep
}

type row = struct {
	day, model, skill, cwd, sess string
	cost                         int64
}

func TestSnapshotSplitsDays(t *testing.T) {
	rep := testReport(
		row{"2026-08-01", "opus", "(no skill)", "/a", "s1", 1_000_000},
		row{"2026-08-01", "sonnet", "docs", "/b", "s2", 500_000},
		row{"2026-08-02", "opus", "docs", "/a", "s3", 250_000},
		row{"unknown", "opus", "docs", "/a", "s4", 999_000},
	)

	days := Snapshot(rep, map[string]string{"/a": "alpha", "/b": "beta"})

	if len(days) != 2 {
		t.Fatalf("want 2 dated days, got %d: %v", len(days), days)
	}
	if _, ok := days["unknown"]; ok {
		t.Error("undated spend leaked into the series")
	}

	d := days["2026-08-01"]
	if d.Cost != 1.5 {
		t.Errorf("day cost = %v, want 1.5", d.Cost)
	}
	if d.Sessions != 2 {
		t.Errorf("sessions = %d, want 2", d.Sessions)
	}
	if d.Models["opus"].Cost != 1.0 || d.Models["sonnet"].Cost != 0.5 {
		t.Errorf("models = %+v", d.Models)
	}
	if d.Projects["alpha"].Cost != 1.0 || d.Projects["beta"].Cost != 0.5 {
		t.Errorf("projects = %+v", d.Projects)
	}
	if d.Skills["docs"].Cost != 0.5 {
		t.Errorf("skills = %+v", d.Skills)
	}

	// Every split must sum back to the day's total, or the file lies.
	for name, split := range map[string]map[string]*Bucket{
		"models": d.Models, "projects": d.Projects, "skills": d.Skills,
	} {
		var sum float64
		for _, b := range split {
			sum += b.Cost
		}
		if sum != d.Cost {
			t.Errorf("%s sum to %v, day total is %v", name, sum, d.Cost)
		}
	}
}

func TestSnapshotFilesCwdsSharingAName(t *testing.T) {
	rep := testReport(
		row{"2026-08-01", "opus", "docs", "/repo", "s1", 1_000_000},
		row{"2026-08-01", "opus", "docs", "/repo/worktree", "s2", 2_000_000},
	)
	days := Snapshot(rep, map[string]string{"/repo": "alpha", "/repo/worktree": "alpha"})
	if got := days["2026-08-01"].Projects["alpha"].Cost; got != 3.0 {
		t.Errorf("merged project cost = %v, want 3.0", got)
	}
}

func TestMergeHistoryKeepsTheHigherRecord(t *testing.T) {
	old := &History{Version: 1, Days: map[string]*Day{
		"2026-07-01": {Bucket: Bucket{Cost: 9, Calls: 90}}, // transcripts since deleted
		"2026-08-01": {Bucket: Bucket{Cost: 1, Calls: 10}}, // still growing
		"2026-08-02": {Bucket: Bucket{Cost: 5, Calls: 50}}, // unchanged
	}}
	fresh := map[string]*Day{
		"2026-07-01": {Bucket: Bucket{Cost: 2, Calls: 20}},
		"2026-08-01": {Bucket: Bucket{Cost: 4, Calls: 40}},
		"2026-08-02": {Bucket: Bucket{Cost: 5, Calls: 50}},
		"2026-08-03": {Bucket: Bucket{Cost: 3, Calls: 30}},
	}

	got, res := MergeHistory(old, fresh, "now")

	if got.Days["2026-07-01"].Cost != 9 {
		t.Errorf("a day that shrank was overwritten: %v", got.Days["2026-07-01"])
	}
	if got.Days["2026-08-01"].Cost != 4 {
		t.Errorf("a growing day was not updated: %v", got.Days["2026-08-01"])
	}
	if got.Days["2026-08-03"].Cost != 3 {
		t.Error("a new day was not added")
	}
	if len(res.Added) != 1 || len(res.Updated) != 1 || len(res.Kept) != 1 {
		t.Errorf("result = %+v", res)
	}
	if old.Days["2026-08-01"].Cost != 1 {
		t.Error("MergeHistory mutated its input")
	}
	if got.Updated != "now" {
		t.Errorf("Updated = %q", got.Updated)
	}
}

func TestMergeHistoryIsIdempotent(t *testing.T) {
	fresh := map[string]*Day{"2026-08-01": {Bucket: Bucket{Cost: 1, Calls: 10}}}
	first, _ := MergeHistory(nil, fresh, "t1")
	second, res := MergeHistory(first, fresh, "t2")

	if len(second.Days) != 1 || second.Days["2026-08-01"].Cost != 1 {
		t.Errorf("days = %+v", second.Days)
	}
	if len(res.Added)+len(res.Updated)+len(res.Kept) != 0 {
		t.Errorf("re-running changed something: %+v", res)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage-history.json")

	missing, err := LoadHistory(path)
	if err != nil || len(missing.Days) != 0 {
		t.Fatalf("missing file: %v, %+v", err, missing)
	}

	h := &History{Version: 1, Updated: "t", Days: map[string]*Day{
		"2026-08-01": {
			Bucket:   Bucket{Cost: 1.25, Calls: 3},
			Sessions: 2,
			Models:   map[string]*Bucket{"opus": {Cost: 1.25, Calls: 3}},
		},
	}}
	if err := SaveHistory(path, h); err != nil {
		t.Fatal(err)
	}
	back, err := LoadHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	if back.Days["2026-08-01"].Cost != 1.25 || back.Days["2026-08-01"].Sessions != 2 {
		t.Errorf("round trip lost data: %+v", back.Days["2026-08-01"])
	}
	if back.Days["2026-08-01"].Models["opus"].Calls != 3 {
		t.Errorf("round trip lost the model split")
	}
}

func TestRecentAndAggregate(t *testing.T) {
	h := &History{Days: map[string]*Day{
		"2026-08-01": {Models: map[string]*Bucket{"opus": {Cost: 1, Calls: 1}}},
		"2026-08-02": {Models: map[string]*Bucket{"opus": {Cost: 2, Calls: 2}}},
		"2026-08-03": {Models: map[string]*Bucket{"sonnet": {Cost: 4, Calls: 4}}},
	}}

	if got := h.Recent(2); len(got) != 2 || got[0] != "2026-08-02" || got[1] != "2026-08-03" {
		t.Errorf("Recent(2) = %v", got)
	}
	if got := h.Recent(0); len(got) != 3 {
		t.Errorf("Recent(0) = %v, want all", got)
	}

	agg := h.Aggregate(h.Recent(0), func(d *Day) map[string]*Bucket { return d.Models })
	if agg["opus"].Cost != 3 || agg["opus"].Calls != 3 || agg["sonnet"].Cost != 4 {
		t.Errorf("aggregate = %+v", agg)
	}
	// Aggregate must copy, not alias the stored buckets.
	if h.Days["2026-08-01"].Models["opus"].Cost != 1 {
		t.Error("Aggregate mutated the history")
	}
}

func TestIsDate(t *testing.T) {
	for _, ok := range []string{"2026-08-01", "1999-12-31"} {
		if !isDate(ok) {
			t.Errorf("%q rejected", ok)
		}
	}
	for _, bad := range []string{"unknown", "", "2026-8-1", "2026/08/01", "2026-08-0x"} {
		if isDate(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}
