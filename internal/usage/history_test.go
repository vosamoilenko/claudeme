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

// The two halves must always reconstruct the legacy field, or a stored day
// contradicts itself.
func TestCacheWriteSplitSumsToLegacyField(t *testing.T) {
	s := Stats{Cost: 1_000_000, Calls: 3, In: 100, Out: 50, CacheRead: 900,
		CacheWrite: 300, CacheWrite1h: 200, CacheWrite5m: 100}
	b := toBucket(s)
	if b.CacheWrite1h+b.CacheWrite5m != b.CacheWrite {
		t.Fatalf("%d + %d != %d", b.CacheWrite1h, b.CacheWrite5m, b.CacheWrite)
	}
	if !b.Recomputable() {
		t.Fatal("a split bucket must be recomputable")
	}

	addBucket(map[string]*Bucket{"x": b}, "x", s)
	if b.CacheWrite1h+b.CacheWrite5m != b.CacheWrite {
		t.Fatalf("addBucket broke the invariant: %+v", *b)
	}

	// A day recorded before the split existed reports itself as approximate
	// rather than claiming its cache writes were all 5m.
	legacy := &Bucket{CacheWrite: 300}
	if legacy.Recomputable() {
		t.Fatal("an unsplit day must not claim to be recomputable")
	}
	// A day that wrote no cache at all has nothing to split and is exact.
	if !(&Bucket{}).Recomputable() {
		t.Fatal("a day with no cache writes is trivially recomputable")
	}
}

// Rescanning a day whose money has not moved must still be allowed to add the
// split — otherwise the detail only ever reaches days that also changed.
func TestMergeHistoryAddsTheSplitToAnUnchangedDay(t *testing.T) {
	stored := &Day{Bucket: Bucket{Cost: 12.5, Calls: 40, CacheWrite: 300}}
	fresh := &Day{Bucket: Bucket{Cost: 12.5, Calls: 40, CacheWrite: 300, CacheWrite1h: 200, CacheWrite5m: 100}}

	old := &History{Version: historyVersion, Days: map[string]*Day{"2026-08-01": stored}}
	out, res := MergeHistory(old, map[string]*Day{"2026-08-01": fresh}, "2026-08-18T00:00:00Z")

	if len(res.Updated) != 1 {
		t.Fatalf("want the day updated, got %+v", res)
	}
	if !out.Days["2026-08-01"].Recomputable() {
		t.Fatal("the split did not land")
	}

	// Second pass: nothing left to gain, so nothing is rewritten.
	out2, res2 := MergeHistory(out, map[string]*Day{"2026-08-01": fresh}, "2026-08-18T01:00:00Z")
	if len(res2.Updated) != 0 || len(res2.Added) != 0 {
		t.Fatalf("a settled day must not churn: %+v", res2)
	}
	if !out2.Days["2026-08-01"].Recomputable() {
		t.Fatal("the split was lost on the second pass")
	}

	// A rescan that sees LESS money is still refused: that is retention, not
	// news, and the split must not be a way around the rule.
	lower := &Day{Bucket: Bucket{Cost: 3.0, Calls: 10, CacheWrite: 100, CacheWrite1h: 60, CacheWrite5m: 40}}
	out3, res3 := MergeHistory(out, map[string]*Day{"2026-08-01": lower}, "2026-08-18T02:00:00Z")
	if len(res3.Kept) != 1 || out3.Days["2026-08-01"].Cost != 12.5 {
		t.Fatalf("a lower rescan overwrote the recorded day: %+v / %v", res3, out3.Days["2026-08-01"].Cost)
	}
}
