package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vosamoilenko/claudeme/internal/config"
)

// A snapshot is the answer to retention: Claude Code deletes transcripts on a
// rolling window, so anything derived from them has to be written down before
// they go. What gets written down is the aggregate, not the transcripts — one
// JSON file of per-day numbers, a few KB a day, immune to any cleanup because
// nothing but this tool ever touches it.
//
// The whole design rests on one property: transcripts are append-only, so
// re-reading everything on disk and upserting the days it covers is
// idempotent. Nothing tracks which files were already ingested, nothing can
// double-count, and a run that crashes half way is fixed by the next one.

// Sep joins the two halves of a crossed breakdown key (day + model, say).
// A NUL can't occur in a date, a model id or a skill name.
const Sep = "\x00"

// historyVersion is bumped only when the on-disk shape changes incompatibly.
const historyVersion = 1

// Bucket is one aggregate: what a slice of a day cost, in dollars, and the
// tokens behind it. Dollars rather than micro-dollars because this file is
// meant to be read and queried by hand.
type Bucket struct {
	Cost       float64 `json:"cost"`
	Calls      int     `json:"calls"`
	In         int     `json:"in"`
	Out        int     `json:"out"`
	CacheRead  int     `json:"cacheRead"`
	CacheWrite int     `json:"cacheWrite"`

	// The two halves of CacheWrite, which bill at different multipliers.
	// omitempty because every day recorded before 2026-08-18 merged them
	// beyond recovery — an absent pair means "unknown", not "zero", and
	// Recomputable() is what tells those days apart.
	CacheWrite1h int `json:"cacheWrite1h,omitempty"`
	CacheWrite5m int `json:"cacheWrite5m,omitempty"`
}

// Recomputable reports whether this bucket can be re-priced exactly. A bucket
// whose cache writes are non-zero but unsplit was recorded before the split
// existed and its transcripts are gone: it can only be approximated.
func (b Bucket) Recomputable() bool {
	return b.CacheWrite == 0 || b.CacheWrite1h+b.CacheWrite5m == b.CacheWrite
}

// Day is one calendar day of spend: the total, then the same total split three
// ways. The splits are what make the file worth keeping — a bare daily cost
// can be re-derived from an invoice, "which project and which model" cannot.
//
// Sessions counts the distinct sessions that spent anything that day, so a
// session running over midnight is counted on both days. Summing the column
// over a range therefore over-counts slightly; the costs do not.
type Day struct {
	Bucket
	Sessions int                `json:"sessions"`
	Models   map[string]*Bucket `json:"models,omitempty"`
	Projects map[string]*Bucket `json:"projects,omitempty"`
	Skills   map[string]*Bucket `json:"skills,omitempty"`
}

// History is the file. Days are keyed YYYY-MM-DD.
type History struct {
	Version int             `json:"version"`
	Updated string          `json:"updated"`
	Days    map[string]*Day `json:"days"`
}

// HistoryPath is the shared file every account writes to. It lives beside the
// transcripts but outside the tree Claude Code manages.
func HistoryPath() string {
	return filepath.Join(config.SharedDir(), "usage-history.json")
}

// toBucket converts an aggregation row to a durable one.
func toBucket(s Stats) *Bucket {
	return &Bucket{
		Cost:         float64(s.Cost) / 1e6,
		Calls:        s.Calls,
		In:           s.In,
		Out:          s.Out,
		CacheRead:    s.CacheRead,
		CacheWrite:   s.CacheWrite,
		CacheWrite1h: s.CacheWrite1h,
		CacheWrite5m: s.CacheWrite5m,
	}
}

// Micros returns the bucket's cost in micro-dollars, for the money formatter.
func (b Bucket) Micros() int64 { return int64(b.Cost * 1e6) }

// Snapshot turns a report into per-day records. project maps a cwd to the name
// it should be filed under; a nil map files each cwd under its own path.
//
// Days with no usable timestamp are dropped: a time series has no row for
// "unknown", and their spend is already counted in the report the caller just
// printed.
func Snapshot(rep *Report, project map[string]string) map[string]*Day {
	days := map[string]*Day{}

	at := func(day string) *Day {
		d, ok := days[day]
		if !ok {
			d = &Day{
				Models:   map[string]*Bucket{},
				Projects: map[string]*Bucket{},
				Skills:   map[string]*Bucket{},
			}
			days[day] = d
		}
		return d
	}

	for day, s := range rep.Days {
		if !isDate(day) {
			continue
		}
		d := at(day)
		d.Bucket = *toBucket(*s)
		d.Sessions = len(rep.DaySess[day])
	}

	fill := func(m map[string]*Stats, pick func(*Day) map[string]*Bucket) {
		for key, s := range m {
			day, name, ok := splitKey(key)
			if !ok || !isDate(day) {
				continue
			}
			if _, seen := days[day]; !seen {
				continue // no total for the day: nothing to break down
			}
			addBucket(pick(days[day]), name, *s)
		}
	}
	fill(rep.DayModels, func(d *Day) map[string]*Bucket { return d.Models })
	fill(rep.DaySkills, func(d *Day) map[string]*Bucket { return d.Skills })

	for cwd, cs := range rep.Cwds {
		name := cwd
		if project != nil {
			if n, ok := project[cwd]; ok {
				name = n
			}
		}
		for day, s := range cs.Days {
			if !isDate(day) {
				continue
			}
			if _, seen := days[day]; !seen {
				continue
			}
			addBucket(days[day].Projects, name, *s)
		}
	}

	return days
}

// addBucket sums a row into a named bucket. Several cwds can share a project
// name, so this accumulates rather than assigns.
func addBucket(m map[string]*Bucket, name string, s Stats) {
	b, ok := m[name]
	if !ok {
		m[name] = toBucket(s)
		return
	}
	b.Cost += float64(s.Cost) / 1e6
	b.Calls += s.Calls
	b.In += s.In
	b.Out += s.Out
	b.CacheRead += s.CacheRead
	b.CacheWrite += s.CacheWrite
	b.CacheWrite1h += s.CacheWrite1h
	b.CacheWrite5m += s.CacheWrite5m
}

// MergeResult is what one merge changed.
type MergeResult struct {
	Added   []string // days the file had never seen
	Updated []string // days whose numbers grew
	Kept    []string // days on disk that now read lower — retention, not truth
}

// MergeHistory folds a fresh snapshot into the stored one and returns a new
// History; neither argument is modified.
//
// A stored day is only overwritten when the fresh scan finds at least as much
// spend. That single rule is what makes the file safe to regenerate forever:
// once transcripts start ageing out, a rescan sees less of a day than the
// snapshot taken while it was whole, and the lower number is loss, not news.
func MergeHistory(old *History, fresh map[string]*Day, now string) (*History, MergeResult) {
	out := &History{Version: historyVersion, Updated: now, Days: map[string]*Day{}}
	var res MergeResult

	if old != nil {
		for day, d := range old.Days {
			out.Days[day] = d
		}
	}

	for day, d := range fresh {
		prev, ok := out.Days[day]
		switch {
		case !ok:
			res.Added = append(res.Added, day)
		case d.Cost < prev.Cost:
			res.Kept = append(res.Kept, day)
			continue
		case d.Cost == prev.Cost && d.Calls == prev.Calls && !gainsSplit(prev, d):
			continue // unchanged
		case d.Cost == prev.Cost && d.Calls == prev.Calls:
			// Same money, more detail: the stored day predates the 1h/5m
			// split and this scan can still see it. Without this arm the
			// split would only ever reach days that also changed.
			res.Updated = append(res.Updated, day)
		default:
			res.Updated = append(res.Updated, day)
		}
		out.Days[day] = d
	}

	sort.Strings(res.Added)
	sort.Strings(res.Updated)
	sort.Strings(res.Kept)
	return out, res
}

// gainsSplit reports whether the fresh day can say how its cache writes broke
// down and the stored one cannot. True only while the transcripts behind that
// day still exist; once they age out the stored day is approximate forever.
func gainsSplit(prev, fresh *Day) bool {
	return !prev.Recomputable() && fresh.Recomputable()
}

// LoadHistory reads the snapshot file. A missing file is an empty history, not
// an error — the first run has nothing to read.
func LoadHistory(path string) (*History, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &History{Version: historyVersion, Days: map[string]*Day{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var h History
	if err := json.Unmarshal(data, &h); err != nil {
		return nil, err
	}
	if h.Days == nil {
		h.Days = map[string]*Day{}
	}
	return &h, nil
}

// SaveHistory writes the snapshot file atomically, so an interrupted run can
// never leave a truncated history where a complete one used to be.
func SaveHistory(path string, h *History) error {
	data, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Dates returns the days on record, oldest first.
func (h *History) Dates() []string {
	dates := make([]string, 0, len(h.Days))
	for day := range h.Days {
		dates = append(dates, day)
	}
	sort.Strings(dates)
	return dates
}

// Recent returns the last n days on record, oldest first. n <= 0 returns all.
func (h *History) Recent(n int) []string {
	dates := h.Dates()
	if n > 0 && len(dates) > n {
		dates = dates[len(dates)-n:]
	}
	return dates
}

// Aggregate sums one breakdown over the given days. pick chooses which
// breakdown — Models, Projects or Skills.
func (h *History) Aggregate(days []string, pick func(*Day) map[string]*Bucket) map[string]*Bucket {
	out := map[string]*Bucket{}
	for _, day := range days {
		d, ok := h.Days[day]
		if !ok {
			continue
		}
		for name, b := range pick(d) {
			cur, ok := out[name]
			if !ok {
				cp := *b
				out[name] = &cp
				continue
			}
			cur.Cost += b.Cost
			cur.Calls += b.Calls
			cur.In += b.In
			cur.Out += b.Out
			cur.CacheRead += b.CacheRead
			cur.CacheWrite += b.CacheWrite
			cur.CacheWrite1h += b.CacheWrite1h
			cur.CacheWrite5m += b.CacheWrite5m
		}
	}
	return out
}

// splitKey undoes the day+Sep+name join.
func splitKey(key string) (day, name string, ok bool) {
	day, name, ok = strings.Cut(key, Sep)
	return day, name, ok
}

// isDate reports whether a day key is a real YYYY-MM-DD, excluding "unknown".
func isDate(day string) bool {
	if len(day) != 10 || day[4] != '-' || day[7] != '-' {
		return false
	}
	for i, c := range day {
		if i == 4 || i == 7 {
			continue
		}
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
