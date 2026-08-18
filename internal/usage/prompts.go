package usage

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/vosamoilenko/claudeme/internal/config"
)

// Claude Code keeps every prompt the user ever typed in one append-only file,
// with a millisecond timestamp, the project it was typed in, and the session
// it belonged to. Unlike a transcript it is never rotated: it reaches back to
// 2026-02-10 where the oldest surviving transcript reaches 2026-07-27.
//
// It carries no tokens and no model, so it can never extend `claudeme cost`.
// What it can say is *when a session ran and on what*, for thousands of
// sessions whose transcripts are long gone — which is the question a work-log
// consumer actually asks.

// PromptHistoryPath is the file Claude Code appends to.
func PromptHistoryPath() string {
	return filepath.Join(config.SharedDir(), "history.jsonl")
}

// Prompts is what the prompt history knows about one session: how many turns
// the user opened, and the wall-clock window they span.
//
// First and Last bracket the *prompts*, not the session: the closing assistant
// turn continues past Last, and a one-prompt session has no span at all. It is
// a floor on the session's duration, never the duration itself.
type Prompts struct {
	Count int    `json:"count"`
	First string `json:"first"`
	Last  string `json:"last,omitempty"`
}

// PromptSession is one session as the prompt history sees it.
type PromptSession struct {
	Session string
	Cwd     string
	Date    string // YYYY-MM-DD of the first prompt, UTC
	Prompts Prompts
}

// promptLine is one recorded prompt. The prompt text itself is deliberately
// not decoded: it stays in the file it is already in rather than being copied
// into the digest tree.
type promptLine struct {
	Timestamp int64  `json:"timestamp"` // epoch milliseconds
	Project   string `json:"project"`
	SessionID string `json:"sessionId"`
}

// ReadPromptHistory folds one or more prompt-history files into per-session
// records. Passing the file and its .bak is safe: sessions are keyed by id and
// timestamps are deduped, so an overlapping backup cannot inflate a count.
func ReadPromptHistory(paths ...string) ([]PromptSession, error) {
	type acc struct {
		cwd   string
		seen  map[int64]bool
		first int64
		last  int64
	}
	byID := map[string]*acc{}

	for _, path := range paths {
		f, err := os.Open(path)
		if os.IsNotExist(err) {
			continue // a missing .bak is not an error
		}
		if err != nil {
			return nil, err
		}
		// Whole lines rather than a Scanner: a pasted prompt can be large,
		// and a Scanner silently ends the scan on the first line over its
		// buffer rather than reporting it.
		r := bufio.NewReader(f)
		for {
			line, rerr := r.ReadBytes('\n')
			if len(line) > 0 && len(line) <= maxTokenLine {
				var p promptLine
				if json.Unmarshal(line, &p) == nil && p.SessionID != "" && p.Timestamp > 0 {
					a, ok := byID[p.SessionID]
					if !ok {
						a = &acc{seen: map[int64]bool{}, first: p.Timestamp, last: p.Timestamp}
						byID[p.SessionID] = a
					}
					if !a.seen[p.Timestamp] {
						a.seen[p.Timestamp] = true
					}
					if p.Timestamp < a.first {
						a.first = p.Timestamp
					}
					if p.Timestamp > a.last {
						a.last = p.Timestamp
					}
					if a.cwd == "" {
						a.cwd = p.Project
					}
				}
			}
			if rerr != nil {
				break
			}
		}
		f.Close()
	}

	out := make([]PromptSession, 0, len(byID))
	for id, a := range byID {
		first := msTime(a.first)
		s := PromptSession{
			Session: id,
			Cwd:     a.cwd,
			Date:    first[:10],
			Prompts: Prompts{Count: len(a.seen), First: first},
		}
		if a.last != a.first {
			s.Prompts.Last = msTime(a.last)
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Prompts.First != out[j].Prompts.First {
			return out[i].Prompts.First < out[j].Prompts.First
		}
		return out[i].Session < out[j].Session
	})
	return out, nil
}

// msTime renders epoch milliseconds as the UTC RFC3339 stamp the rest of the
// records use, so a prompt window and a transcript window are comparable
// without converting anything at read time.
func msTime(ms int64) string {
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}

// Candidate turns a prompt-history session into the shape PutDigest files by,
// resolving the project name the same way a transcript-derived candidate does.
func (p PromptSession) Candidate(names map[string]string) Candidate {
	c := Candidate{Session: p.Session, Date: p.Date, Cwd: p.Cwd}
	c.Project = names[p.Cwd]
	if c.Project == "" {
		c.Project = filepath.Base(p.Cwd)
	}
	if c.Project == "" || c.Project == "." || c.Project == "/" {
		c.Project = "unknown"
	}
	return c
}

// PutPrompts files a prompt window onto the record already there, creating the
// record when the session has no other trace. A record created this way holds
// no summary and no tokens, so Usable() still refuses to let it gate a delete
// — which is right: it is evidence a session happened, not an account of it.
func PutPrompts(root string, c Candidate, p Prompts) error {
	d, err := GetDigest(root, c.Date, c.Project, c.Session)
	if err != nil {
		return err
	}
	if d == nil {
		d = &Digest{Session: c.Session, Date: c.Date, Cwd: c.Cwd, Project: c.Project}
	}
	d.Prompts = &p
	return PutDigest(root, d)
}
