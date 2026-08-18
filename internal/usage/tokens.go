package usage

import (
	"bufio"
	"encoding/json"
	"sort"
)

// A session's token ledger: what it actually consumed, per model, kept as
// counted facts rather than as one pre-computed cost. Money is derived from
// this at query time against the price epoch in effect on the day, so a later
// correction to the price table re-values history instead of contradicting it.
//
// This is the "meta" half of a session record. The "semantic" half is the
// model-written `summary`, and the deterministic session shape is `metrics`.
// All three live in the same per-session record.

// ModelTokens is one model's consumption inside one session, split the way it
// bills. Model is the string the transcript recorded, untouched: "opus" meant
// a different model in July, and resolving it at write time would bake today's
// answer into a fact about the past.
type ModelTokens struct {
	Model        string `json:"model"`
	Calls        int    `json:"calls"`
	In           int    `json:"in"`
	Out          int    `json:"out"`
	CacheRead    int    `json:"cacheRead"`
	CacheWrite1h int    `json:"cacheWrite1h"`
	CacheWrite5m int    `json:"cacheWrite5m"`
}

// Tokens is the ledger for one session.
type Tokens struct {
	Provider  Provider       `json:"provider"`
	Session   string         `json:"session"`
	Started   string         `json:"started,omitempty"`
	Ended     string         `json:"ended,omitempty"`
	Models    []*ModelTokens `json:"models"`
	Sidechain []*ModelTokens `json:"sidechain,omitempty"`

	// Days is per-day, per-model consumption, so a session running over
	// midnight can be valued at each day's prices rather than one of them.
	Days map[string][]*ModelTokens `json:"days,omitempty"`

	// ExtractedAt records when this ledger was derived. Extraction is
	// deterministic, so a differing value across two runs over the same
	// transcript means the transcript grew, not that the numbers moved.
	ExtractedAt string `json:"extractedAt,omitempty"`
}

// Total sums the main-loop and sidechain ledgers.
func (t *Tokens) Total() ModelTokens {
	var out ModelTokens
	if t == nil {
		return out
	}
	for _, set := range [][]*ModelTokens{t.Models, t.Sidechain} {
		for _, m := range set {
			out.Calls += m.Calls
			out.In += m.In
			out.Out += m.Out
			out.CacheRead += m.CacheRead
			out.CacheWrite1h += m.CacheWrite1h
			out.CacheWrite5m += m.CacheWrite5m
		}
	}
	return out
}

// Cost values the ledger at each day's prices — the honest reading, since a
// session spanning a price change is billed on both sides of it. Sessions with
// no per-day detail fall back to the session's start date. Returns the total
// in micro-dollars and the raw model strings that had no price.
func (t *Tokens) Cost() (micros int64, unpriced []string) {
	if t == nil {
		return 0, nil
	}
	missing := map[string]bool{}

	value := func(date string, ms []*ModelTokens) {
		for _, m := range ms {
			p, ok := PriceAt(t.Provider, date, m.Model)
			if !ok {
				missing[m.Model] = true
				continue
			}
			micros += cost(usageTokens{
				In: m.In, Out: m.Out, CacheRead: m.CacheRead,
				CacheWrite1h: m.CacheWrite1h, CacheWrite5m: m.CacheWrite5m,
			}, p, MultAt(t.Provider, date))
		}
	}

	if len(t.Days) > 0 {
		for _, day := range sortedKeys(t.Days) {
			value(day, t.Days[day])
		}
	} else {
		date := t.Started
		if len(date) >= 10 {
			date = date[:10]
		}
		value(date, t.Models)
		value(date, t.Sidechain)
	}

	for m := range missing {
		unpriced = append(unpriced, m)
	}
	sort.Strings(unpriced)
	return micros, unpriced
}

// CostAt values the whole ledger at one date's prices, which is how "what
// would this year have cost at today's prices" is asked.
func (t *Tokens) CostAt(date string) (micros int64, unpriced []string) {
	if t == nil {
		return 0, nil
	}
	frozen := &Tokens{Provider: t.Provider, Started: date, Models: t.Models, Sidechain: t.Sidechain}
	return frozen.Cost()
}

// SessionTokens reads one transcript and returns its ledger. Deterministic and
// model-free: no network, no codex, no cost beyond the parse, so it can be run
// over every session on every pass.
//
// Records are deduped by requestId (falling back to uuid) exactly as Analyze
// does: resuming a session replays earlier turns into the new transcript, and
// counting the replay would inflate the ledger.
func SessionTokens(path string) (*Tokens, error) {
	f, err := openTranscript(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	t := &Tokens{Provider: Anthropic, Session: sessionID(path), Days: map[string][]*ModelTokens{}}
	main := map[string]*ModelTokens{}
	side := map[string]*ModelTokens{}
	byDay := map[string]map[string]*ModelTokens{}
	seen := map[string]bool{}

	// Whole lines, not bufio.Scanner: a transcript's early lines can be
	// megabytes and a Scanner silently ends the scan on the first one over
	// its buffer. See readCandidate, where that cost a real session.
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 && len(line) <= maxTokenLine {
			var e entry
			if json.Unmarshal(line, &e) == nil {
				t.observe(&e, seen, main, side, byDay)
			}
		}
		if err != nil {
			break
		}
	}

	t.Models = flatten(main)
	t.Sidechain = flatten(side)
	for day, models := range byDay {
		t.Days[day] = flatten(models)
	}
	if len(t.Days) == 0 {
		t.Days = nil
	}
	return t, nil
}

// maxTokenLine is the longest line worth unmarshalling. Anything larger is a
// pasted file or an image, which carries no usage block.
const maxTokenLine = 8 * 1024 * 1024

// observe folds one transcript line into the ledger under construction.
func (t *Tokens) observe(e *entry, seen map[string]bool, main, side map[string]*ModelTokens, byDay map[string]map[string]*ModelTokens) {
	if e.SessionID != "" {
		t.Session = e.SessionID
	}
	if ts := e.Timestamp; ts != "" {
		if t.Started == "" || ts < t.Started {
			t.Started = ts
		}
		if ts > t.Ended {
			t.Ended = ts
		}
	}

	if e.Message.Usage == nil || e.Message.Model == "" {
		return
	}
	key := e.RequestID
	if key == "" {
		key = e.UUID
	}
	if key != "" {
		if seen[key] {
			return
		}
		seen[key] = true
	}

	u := e.Message.Usage
	lane := main
	if e.IsSidechain {
		lane = side
	}
	bumpTokens(lane, e.Message.Model, u)

	if len(e.Timestamp) >= 10 {
		day := e.Timestamp[:10]
		if byDay[day] == nil {
			byDay[day] = map[string]*ModelTokens{}
		}
		// The per-day view folds the two lanes together: a lane split matters
		// for reading a session, a date split matters for pricing it.
		bumpTokens(byDay[day], e.Message.Model, u)
	}
}

func bumpTokens(m map[string]*ModelTokens, model string, u *transcriptUsage) {
	row, ok := m[model]
	if !ok {
		row = &ModelTokens{Model: model}
		m[model] = row
	}
	row.Calls++
	row.In += u.InputTokens
	row.Out += u.OutputTokens
	row.CacheRead += u.CacheReadTokens
	row.CacheWrite1h += u.CacheCreation.Ephemeral1h
	row.CacheWrite5m += u.CacheCreation.Ephemeral5m
}

// flatten orders a lane by model id, so two runs over the same transcript
// produce byte-identical JSON.
func flatten(m map[string]*ModelTokens) []*ModelTokens {
	out := make([]*ModelTokens, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Model < out[j].Model })
	if len(out) == 0 {
		return nil
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
