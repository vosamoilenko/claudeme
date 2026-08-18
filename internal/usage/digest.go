package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/vosamoilenko/claudeme/internal/config"
)

// digestVersion is the shape of a per-date/per-project digest file. Bump it
// when a field changes meaning, never when one is added.
const digestVersion = 1

// DigestRoot is where digests live: one directory per date, one file per
// project inside it. It sits beside the transcripts but outside the tree
// Claude Code manages, so its own retention never touches it.
func DigestRoot() string {
	return filepath.Join(config.SharedDir(), "history")
}

// DigestPath is the file holding every digested session for one project on
// one date.
func DigestPath(date, project string) string {
	return digestPathIn(DigestRoot(), date, project)
}

// digestPathIn is DigestPath against an explicit root, so tests write to a
// temp dir instead of the user's real history.
func digestPathIn(root, date, project string) string {
	return filepath.Join(root, date, safeName(project)+".json")
}

// unsafeName is every character that has meaning to a shell or a filesystem.
// Project names come from directory basenames, so they can hold anything.
var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// safeName makes a project name usable as a filename without collapsing two
// distinct projects onto one path more often than the name itself already
// does — see Snapshot, which keys projects by the same display name.
func safeName(project string) string {
	s := unsafeName.ReplaceAllString(project, "-")
	s = strings.Trim(s, "-.")
	if s == "" {
		return "unknown"
	}
	return s
}

// Digest is one session, summarized. Summary is the model's output held as
// raw JSON: schema.json owns its shape, and re-encoding it here would mean
// two definitions of the same thing drifting apart.
type Digest struct {
	Session    string          `json:"session"`
	Date       string          `json:"date"`
	Cwd        string          `json:"cwd"`
	Project    string          `json:"project"`
	Transcript string          `json:"transcript"`
	Model      string          `json:"model"`
	DigestedAt string          `json:"digestedAt"`
	Summary    json.RawMessage `json:"summary,omitempty"`

	// Metrics is distill.py's deterministic account of the session — when it
	// ran, on which branches, and the counted totals. Held raw for the same
	// reason Summary is: distill.py owns the shape. omitempty keeps records
	// written before this field byte-stable until they are backfilled.
	Metrics json.RawMessage `json:"metrics,omitempty"`

	// MetricsAt is when the metrics above were derived. Separate from
	// DigestedAt because the two can happen in different runs: the backfill
	// writes metrics onto records the model summarized months earlier, and
	// onto sessions no model has seen at all.
	MetricsAt string `json:"metricsAt,omitempty"`

	// Tokens is what the session consumed, counted per model and per day and
	// left unvalued. Cost is derived from it at query time against the price
	// epoch in effect on the day, so correcting a price re-values history
	// rather than contradicting a stored number.
	Tokens *Tokens `json:"tokens,omitempty"`
}

// Usable reports whether a digest is worth keeping the transcript's deletion
// on. A record whose summary is absent or empty means the model ran and gave
// nothing back — it must never gate a delete. Metrics deliberately do not
// count: they are re-derivable from the transcript, so a metrics-only record
// is no reason to destroy the only copy of what produced it.
func (d *Digest) Usable() bool {
	if d == nil || !d.HasSummary() {
		return false
	}
	var fields struct {
		Summary string `json:"summary"`
		Outcome string `json:"outcome"`
	}
	if err := json.Unmarshal(d.Summary, &fields); err != nil {
		return false
	}
	return fields.Summary != "" && fields.Outcome != ""
}

// HasSummary reports whether the model ever wrote anything into this record.
// A metrics-only record has none: the backfill writes it without ever calling
// the model, and a later run still owes it a summary.
func (d *Digest) HasSummary() bool {
	return d != nil && len(d.Summary) > 0 && string(d.Summary) != "null"
}

// HasMetrics reports whether distill.py's account of the session is on
// record. False for every digest written before metrics were persisted.
func (d *Digest) HasMetrics() bool {
	return d != nil && len(d.Metrics) > 0 && string(d.Metrics) != "null"
}

// HasTokens reports whether the token ledger is on record. Extraction is
// deterministic and model-free, so this gates a cheap re-run, not an expensive
// one — see PendingTokens.
func (d *Digest) HasTokens() bool {
	return d != nil && d.Tokens != nil && len(d.Tokens.Models)+len(d.Tokens.Sidechain) > 0
}

// DigestFile is every digested session for one project on one date, keyed by
// session id so a re-digest replaces rather than duplicates.
type DigestFile struct {
	Version  int                `json:"version"`
	Date     string             `json:"date"`
	Project  string             `json:"project"`
	Updated  string             `json:"updated"`
	Sessions map[string]*Digest `json:"sessions"`
}

// Ids returns the sessions on record, oldest id order, for stable output.
func (f *DigestFile) Ids() []string {
	ids := make([]string, 0, len(f.Sessions))
	for id := range f.Sessions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// LoadDigest reads one digest file. A missing file is an empty one, not an
// error: the first run of any date starts here.
func LoadDigest(path string) (*DigestFile, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &DigestFile{Version: digestVersion, Sessions: map[string]*Digest{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var f DigestFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	if f.Sessions == nil {
		f.Sessions = map[string]*Digest{}
	}
	return &f, nil
}

// SaveDigest writes a digest file atomically, so an interrupted run can never
// leave a truncated file where a complete one used to be.
func SaveDigest(path string, f *DigestFile) error {
	data, err := json.MarshalIndent(f, "", "  ")
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
