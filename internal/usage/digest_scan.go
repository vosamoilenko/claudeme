package usage

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// maxCandidateLine is the longest transcript line worth unmarshalling while
// looking for a date and a cwd. Anything larger is payload, not metadata.
const maxCandidateLine = 1024 * 1024

// Candidate is one session transcript that could be digested: everything
// needed to file its summary, read without running the model.
type Candidate struct {
	Path    string // transcript, live or gzipped in the archive
	Session string
	Date    string // YYYY-MM-DD, the first timestamp in the transcript
	Cwd     string
	Project string
}

// ProjectNames maps every cwd to the project name its sessions file under.
// Two projects sharing a basename are disambiguated by their parent
// directory, so "frontend" and "frontend" become "tts/frontend" and
// "berlinhyp/frontend" rather than colliding in one file.
func ProjectNames(projects []Project) map[string]string {
	count := map[string]int{}
	for _, p := range projects {
		count[p.Name]++
	}
	out := map[string]string{}
	for _, p := range projects {
		name := p.Name
		if count[p.Name] > 1 {
			name = filepath.Join(filepath.Base(filepath.Dir(p.Path)), p.Name)
		}
		for _, m := range p.Members {
			out[m.Cwd] = name
		}
	}
	return out
}

// ScanSessions lists every session transcript under the given roots, live and
// archived. Nested subagent transcripts are skipped: they are part of a
// session, not one of their own, and distill.py already folds them in.
//
// Sessions are returned oldest first, so a limited run digests the oldest
// backlog rather than an arbitrary slice of it.
func ScanSessions(roots []string) ([]Candidate, error) {
	projects, err := Discover(roots)
	if err != nil {
		return nil, err
	}
	names := ProjectNames(projects)

	var out []Candidate
	seen := map[string]bool{}
	for _, root := range roots {
		dirs, err := os.ReadDir(root)
		if err != nil {
			continue // a missing root is not an error; Discover already ruled on that
		}
		for _, dir := range dirs {
			if !dir.IsDir() {
				continue
			}
			dirPath := filepath.Join(root, dir.Name())
			paths, err := transcripts(dirPath)
			if err != nil {
				continue
			}
			for _, p := range paths {
				if filepath.Dir(p) != dirPath {
					continue // nested: a subagent transcript, not a session
				}
				c, ok := readCandidate(p, dir.Name(), names)
				if !ok || seen[c.Session] {
					continue // same session in both roots: the live copy wins
				}
				seen[c.Session] = true
				out = append(out, c)
			}
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date < out[j].Date
		}
		return out[i].Session < out[j].Session
	})
	return out, nil
}

// readCandidate reads the head of a transcript for the three facts filing a
// digest needs. A transcript with no usable timestamp is skipped: without a
// date there is no directory to file it under.
func readCandidate(path, dirName string, names map[string]string) (Candidate, bool) {
	c := Candidate{Path: path, Session: sessionID(path)}

	f, err := openTranscript(path)
	if err != nil {
		return c, false
	}
	defer f.Close()

	// bufio.Scanner is wrong here: one line over its buffer ends the scan, and
	// a transcript's early lines can be megabytes (a pasted file, an image).
	// That silently cost a real session its digest. Read whole lines instead
	// and simply decline to parse an implausibly long one.
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) == 0 && err != nil {
			break
		}
		var e struct {
			SessionID string `json:"sessionId"`
			Timestamp string `json:"timestamp"`
			Cwd       string `json:"cwd"`
		}
		if len(line) > maxCandidateLine || json.Unmarshal(line, &e) != nil {
			if err != nil {
				break
			}
			continue
		}
		if c.Cwd == "" {
			c.Cwd = e.Cwd
		}
		if e.SessionID != "" {
			c.Session = e.SessionID
		}
		if c.Date == "" && len(e.Timestamp) >= 10 {
			c.Date = e.Timestamp[:10]
		}
		if c.Date != "" && c.Cwd != "" {
			break
		}
		if err != nil {
			break
		}
	}
	if c.Date == "" || c.Session == "" {
		return c, false
	}

	if c.Cwd == "" {
		c.Cwd = unmangle(dirName)
	}
	c.Project = names[c.Cwd]
	if c.Project == "" {
		c.Project = filepath.Base(c.Cwd)
	}
	return c, true
}

// sessionID is the transcript's filename without its extensions. Claude Code
// names each session file after its id; the transcript itself overrides this
// when it records one.
func sessionID(path string) string {
	name := filepath.Base(path)
	name = strings.TrimSuffix(name, gzExt)
	return strings.TrimSuffix(name, ".jsonl")
}

// SettleCooloff is how long a transcript must go untouched before it counts as
// a finished session. A digest is written once and never revisited, so
// summarizing a transcript that is still being appended to records a partial
// account of the session permanently. An hour clears anything but a session
// that is genuinely still open.
const SettleCooloff = time.Hour

// Settled drops the candidates whose transcript was written to recently — the
// sessions still in progress. Takes the clock as an argument so the rule can
// be tested without waiting an hour.
func Settled(cands []Candidate, now time.Time, cooloff time.Duration) []Candidate {
	out := make([]Candidate, 0, len(cands))
	for _, c := range cands {
		info, err := os.Stat(c.Path)
		if err != nil || now.Sub(info.ModTime()) >= cooloff {
			// An unstattable transcript is left in: the run itself will
			// report the failure, which beats dropping it silently.
			out = append(out, c)
		}
	}
	return out
}

// Pending drops the candidates already summarized, so a run only spends model
// calls on sessions that have none. A metrics-only record does not count: the
// backfill writes those without calling the model, and they are still owed a
// summary.
func Pending(root string, cands []Candidate) ([]Candidate, error) {
	return pendingBy(root, cands, func(d *Digest) bool { return d.HasSummary() })
}

// PendingMetrics drops the candidates whose record already carries distill.py's
// account of the session. Everything else is in: the sessions summarized before
// metrics were persisted, and the far larger set never digested at all.
func PendingMetrics(root string, cands []Candidate) ([]Candidate, error) {
	return pendingBy(root, cands, func(d *Digest) bool { return d.HasMetrics() })
}

// PendingTokens drops the candidates whose record already carries a token
// ledger. Like PendingMetrics and unlike Pending, it is free of the model: a
// session whose summary failed, or which was digested before this field
// existed, is still a candidate.
func PendingTokens(root string, cands []Candidate) ([]Candidate, error) {
	return pendingBy(root, cands, func(d *Digest) bool { return d.HasTokens() })
}

// pendingBy keeps the candidates whose record does not yet satisfy done,
// reading each date+project file once however many sessions it holds.
func pendingBy(root string, cands []Candidate, done func(*Digest) bool) ([]Candidate, error) {
	records := map[string]map[string]*Digest{}
	var out []Candidate
	for _, c := range cands {
		key := c.Date + Sep + c.Project
		seen, ok := records[key]
		if !ok {
			var err error
			seen, err = Records(root, c.Date, c.Project)
			if err != nil {
				return nil, err
			}
			records[key] = seen
		}
		if !done(seen[c.Session]) {
			out = append(out, c)
		}
	}
	return out, nil
}

// Placement is where a session's record already lives: its date and project
// directory. A session is keyed by id inside one file, so the same session
// filed under two project names would become two records of one thing —
// which is exactly what happens when a project's name is derived from a cwd
// whose transcripts have since been deleted.
type Placement struct {
	Date    string
	Project string
}

// IndexDigests maps every session already on record to where it is filed, so
// a writer working from a source other than a transcript can put its facts on
// the existing record rather than beside it.
func IndexDigests(root string) (map[string]Placement, error) {
	out := map[string]Placement{}
	days, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for _, day := range days {
		if !day.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(root, day.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".json") {
				continue
			}
			df, err := LoadDigest(filepath.Join(root, day.Name(), f.Name()))
			if err != nil {
				continue
			}
			for id := range df.Sessions {
				out[id] = Placement{Date: df.Date, Project: df.Project}
			}
		}
	}
	return out, nil
}
