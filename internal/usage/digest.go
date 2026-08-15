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
	Summary    json.RawMessage `json:"summary"`
}

// Usable reports whether a digest is worth keeping the transcript's deletion
// on. A record whose summary is absent or empty means the model ran and gave
// nothing back — it must never gate a delete.
func (d *Digest) Usable() bool {
	if d == nil || len(d.Summary) == 0 {
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
