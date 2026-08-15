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

// Pending drops the candidates already on record, so a run only spends model
// calls on sessions that have none.
func Pending(root string, cands []Candidate) ([]Candidate, error) {
	digested := map[string]map[string]bool{}
	var out []Candidate
	for _, c := range cands {
		key := c.Date + Sep + c.Project
		seen, ok := digested[key]
		if !ok {
			var err error
			seen, err = Digested(root, c.Date, c.Project)
			if err != nil {
				return nil, err
			}
			digested[key] = seen
		}
		if !seen[c.Session] {
			out = append(out, c)
		}
	}
	return out, nil
}
