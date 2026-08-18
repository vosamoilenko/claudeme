package usage

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Codex sessions are the OpenAI half of the corpus. They reduce to the same
// Tokens ledger as Claude sessions, so `claudeme cost` asks both providers one
// question and the price epoch decides what the answer is worth.
//
// The file format is not the Claude one and every rule below was measured
// against all 2,586 files on disk, not assumed:
//
//   - `payload.info.total_token_usage` on a `token_count` event is CUMULATIVE
//     per session. Summing `last_token_usage` double-counts on the CLI
//     versions that emit each event twice (measured 1.9987× on a 2026-02-25
//     session). The last cumulative total is the only safe read, and totals
//     are strictly monotonic in every file, so last == max.
//   - `payload.info` can be null. Take the last NON-null one.
//   - `cached_input_tokens` is a SUBSET of `input_tokens`, proven by
//     `total_tokens == input_tokens + output_tokens` on 9,303/9,303 events.
//     Billable uncached input is the difference.
//   - `reasoning_output_tokens` is likewise inside `output_tokens`.
//   - `cache_write_input_tokens` is optional (1,984/9,303 events) and is also
//     inside `input_tokens`, so it is recorded for information and never
//     billed a second time.
//   - the model is the nearest preceding `turn_context.payload.model`, always
//     resolvable, and only 2 files switch model mid-session. Since the totals
//     are cumulative, per-model attribution inside a session is impossible;
//     the session is attributed to the model in effect at its end.
//   - the session's start is `session_meta.payload.timestamp` (UTC), NOT the
//     envelope timestamp (which lags) and NOT the filename (local time).

// CodexRoot is where the Codex CLI writes its rollouts.
func CodexRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex", "sessions")
}

// CodexSession is one rollout file, reduced. Tokens is nil when the file
// records none — 877 files on disk are in that state, and 527 of those carry
// no model either. They are counted rather than dropped, so a total can say
// what it does not cover.
type CodexSession struct {
	Path    string
	Session string
	Cwd     string
	Model   string
	Started string
	Ended   string
	Tokens  *Tokens

	// CacheWrite is `cache_write_input_tokens`, recorded because it is
	// reported. It is a subset of the input tokens and is not billed again.
	CacheWrite int

	// Legacy marks a file with neither a model nor any usage event. Its
	// consumption is unrecoverable, not zero.
	Legacy bool
}

// codexTokenUsage is the cumulative usage block.
type codexTokenUsage struct {
	InputTokens     int `json:"input_tokens"`
	CachedInput     int `json:"cached_input_tokens"`
	CacheWriteInput int `json:"cache_write_input_tokens"`
	OutputTokens    int `json:"output_tokens"`
	ReasoningOutput int `json:"reasoning_output_tokens"`
	TotalTokens     int `json:"total_tokens"`
}

// codexLine is the modern envelope. Legacy files have no `type` and no
// `payload`, so every field here is simply absent for them.
type codexLine struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Payload   struct {
		Type      string `json:"type"`
		ID        string `json:"id"`
		Timestamp string `json:"timestamp"`
		Cwd       string `json:"cwd"`
		Model     string `json:"model"`
		Info      *struct {
			TotalTokenUsage *codexTokenUsage `json:"total_token_usage"`
		} `json:"info"`
	} `json:"payload"`
}

// legacyCwd pulls the working directory out of the environment_context block
// the legacy files embed in their first user message — the only place those
// files record it. Found in 527/527.
var legacyCwd = regexp.MustCompile(`<cwd>([^<]*)</cwd>`)

// codexFileID is the UUID suffix of a rollout filename, which matches
// `session_meta.payload.id` in all 2,131 modern files.
var codexFileID = regexp.MustCompile(`^rollout-\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}-(.+)\.jsonl$`)

// ScanCodex lists every rollout under root, oldest first.
func ScanCodex(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // an unreadable day is skipped, not fatal
		}
		if !d.IsDir() && strings.HasPrefix(d.Name(), "rollout-") && strings.HasSuffix(d.Name(), ".jsonl") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// ReadCodexSession parses one rollout into the shared ledger shape.
func ReadCodexSession(path string) (*CodexSession, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	s := &CodexSession{Path: path, Session: codexSessionID(path)}
	var last *codexTokenUsage
	var turnCwd string

	r := bufio.NewReader(f)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 && len(line) <= maxTokenLine {
			s.observe(line, &last, &turnCwd)
		}
		if err != nil {
			break
		}
	}

	if turnCwd != "" {
		s.Cwd = turnCwd // per-turn cwd beats the session one for attribution
	}
	if last != nil {
		s.CacheWrite = last.CacheWriteInput
		s.Tokens = &Tokens{
			Provider: OpenAI,
			Session:  s.Session,
			Started:  s.Started,
			Ended:    s.Ended,
			Models: []*ModelTokens{{
				Model: s.Model,
				Calls: 1, // one cumulative total; the per-call count is not recorded
				// Cached input is inside the input total, so the billable
				// uncached half is the difference. Cache writes are inside it
				// too and are therefore not billed a second time.
				In:        last.InputTokens - last.CachedInput,
				Out:       last.OutputTokens,
				CacheRead: last.CachedInput,
			}},
		}
		if day := dayOf(s.Started); day != "" {
			s.Tokens.Days = map[string][]*ModelTokens{day: s.Tokens.Models}
		}
	}
	s.Legacy = s.Tokens == nil && s.Model == ""
	return s, nil
}

// observe folds one rollout line into the session under construction.
func (s *CodexSession) observe(line []byte, last **codexTokenUsage, turnCwd *string) {
	var e codexLine
	if json.Unmarshal(line, &e) != nil {
		return
	}

	if e.Timestamp > s.Ended {
		s.Ended = e.Timestamp
	}

	switch {
	case e.Type == "session_meta":
		// The inner timestamp is the true start; the envelope one lags by
		// seconds to minutes.
		if e.Payload.Timestamp != "" {
			s.Started = e.Payload.Timestamp
		}
		if e.Payload.ID != "" {
			s.Session = e.Payload.ID
		}
		if s.Cwd == "" {
			s.Cwd = e.Payload.Cwd
		}
	case e.Type == "turn_context":
		if e.Payload.Model != "" {
			s.Model = e.Payload.Model // last one wins: only 2 files ever switch
		}
		if e.Payload.Cwd != "" {
			*turnCwd = e.Payload.Cwd
		}
	case e.Payload.Type == "token_count":
		if e.Payload.Info != nil && e.Payload.Info.TotalTokenUsage != nil {
			*last = e.Payload.Info.TotalTokenUsage
		}
	}

	if s.Started == "" && e.Type == "" {
		// Legacy bare header: id and timestamp sit at the top level.
		var legacy struct {
			ID        string `json:"id"`
			Timestamp string `json:"timestamp"`
		}
		if json.Unmarshal(line, &legacy) == nil && legacy.Timestamp != "" {
			s.Started = legacy.Timestamp
			if legacy.ID != "" {
				s.Session = legacy.ID
			}
		}
	}
	if s.Cwd == "" {
		if m := legacyCwd.FindSubmatch(line); m != nil {
			s.Cwd = string(m[1])
		}
	}
}

// codexSessionID is the UUID in the filename — verified equal to
// session_meta.payload.id in every modern file, and the only id a legacy
// file's name carries.
func codexSessionID(path string) string {
	if m := codexFileID.FindStringSubmatch(filepath.Base(path)); m != nil {
		return m[1]
	}
	return strings.TrimSuffix(filepath.Base(path), ".jsonl")
}

func dayOf(ts string) string {
	if len(ts) < 10 {
		return ""
	}
	return ts[:10]
}

// CodexCoverage is what a scan could and could not account for. Reported
// rather than folded into a total: a number that silently omits a third of
// its corpus is worse than one that says so.
type CodexCoverage struct {
	Files    int
	Tokened  int            // files with a usage total
	NoTokens int            // a model, but no usage events recorded
	Legacy   int            // neither model nor usage — unrecoverable
	Unpriced map[string]int // model → sessions, for models with no list price
}

// ReadCodex parses every rollout under root and returns the sessions with a
// ledger plus the coverage the scan achieved.
func ReadCodex(root string) ([]*CodexSession, CodexCoverage, error) {
	paths, err := ScanCodex(root)
	if err != nil {
		return nil, CodexCoverage{}, err
	}
	cov := CodexCoverage{Files: len(paths), Unpriced: map[string]int{}}

	var out []*CodexSession
	for _, p := range paths {
		s, err := ReadCodexSession(p)
		if err != nil {
			continue
		}
		switch {
		case s.Tokens != nil:
			cov.Tokened++
			date := dayOf(s.Started)
			if _, ok := PriceAt(OpenAI, date, s.Model); !ok {
				cov.Unpriced[s.Model]++
			}
			out = append(out, s)
		case s.Legacy:
			cov.Legacy++
		default:
			cov.NoTokens++
		}
	}
	return out, cov, nil
}
