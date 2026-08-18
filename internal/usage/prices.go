package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/vosamoilenko/claudeme/internal/config"
)

// Prices are a dated series, not a constant. A model's list price changes, and
// so does what a bare name like "opus" resolves to — "opus" meant a different
// model in July than it does now. Valuing a day's tokens therefore means
// asking what the table said *on that day*, which is what PriceAt does.
//
// Nothing here is scraped. There is no pricing API, list prices move a handful
// of times a year, and a scraper would be a fragile dependency on a marketing
// page. The table is hand-maintained; `claudeme snapshot` writes a dated copy
// of the epoch in effect so a later correction here cannot silently rewrite
// what was already recorded.

// Provider is who billed the tokens. Prices are per-provider because the two
// corpora are parsed separately and their model ids can collide in principle.
type Provider string

const (
	Anthropic Provider = "anthropic"
	OpenAI    Provider = "openai"
)

// Multipliers are the factors applied to the input price for cached tokens.
// They live inside the epoch because they are price policy and have changed
// before.
type Multipliers struct {
	Cache1h   float64 `json:"cache1h"`
	Cache5m   float64 `json:"cache5m"`
	CacheRead float64 `json:"cacheRead"`
}

// PriceEpoch is the whole pricing policy in effect from one date onward:
// the table, the cache multipliers, and what the bare model names meant.
type PriceEpoch struct {
	From     string            `json:"from"` // YYYY-MM-DD, inclusive
	Provider Provider          `json:"provider"`
	Table    map[string]Price  `json:"table"`
	Mult     Multipliers       `json:"mult"`
	Aliases  map[string]string `json:"aliases,omitempty"`
	Note     string            `json:"note,omitempty"`

	// Unpriced names models this epoch knows about and deliberately does not
	// price. Distinct from a model it has never seen: one is a decision, the
	// other is a gap.
	Unpriced []string `json:"unpriced,omitempty"`

	// Provisional marks a table that has not been checked against a real
	// invoice. Every surface that spends it must say so.
	Provisional bool `json:"provisional,omitempty"`
}

// Knows reports whether this epoch has an opinion about a model at all,
// priced or deliberately not.
func (e PriceEpoch) Knows(model string) bool {
	if e.resolve(model) != "" {
		return true
	}
	m := trimModelSuffixes(model)
	for _, u := range e.Unpriced {
		if u == m {
			return true
		}
	}
	return false
}

// epochs are ordered oldest first per provider. Seeded with one epoch per
// provider dated 2000-01-01, so every date in the corpus resolves to today's
// numbers and this change is bit-identical on day one. Real historical epochs
// get appended in front of it as they are sourced.
var epochs = []PriceEpoch{
	{
		From:     "2000-01-01",
		Provider: Anthropic,
		Note:     "seed: today's list prices, applied to all history until real epochs are sourced",
		Mult:     Multipliers{Cache1h: 2.0, Cache5m: 1.25, CacheRead: 0.1},
		Table: map[string]Price{
			"claude-opus-5":     {5.0, 25.0},
			"claude-opus-4-8":   {5.0, 25.0},
			"claude-opus-4-7":   {5.0, 25.0},
			"claude-opus-4-6":   {5.0, 25.0},
			"claude-fable-5":    {10.0, 50.0},
			"claude-mythos-5":   {10.0, 50.0},
			"claude-sonnet-5":   {3.0, 15.0},
			"claude-sonnet-4-6": {3.0, 15.0},
			"claude-haiku-4-5":  {1.0, 5.0},
		},
		// Deliberately unpriced: <synthetic> is Claude Code's own placeholder
		// for a turn no model billed, so pricing it would invent spend.
		Unpriced: []string{"<synthetic>"},
		Aliases: map[string]string{
			"opus":   "claude-opus-5",
			"sonnet": "claude-sonnet-5",
			"fable":  "claude-fable-5",
			"haiku":  "claude-haiku-4-5",
		},
	},
	{
		From:     "2000-01-01",
		Provider: OpenAI,
		Note: "PROVISIONAL — these are best-effort list prices, not sourced from an invoice. " +
			"The model strings are the 15 observed across all 2,586 rollouts on disk. " +
			"Correct them before treating any OpenAI figure as more than an order of magnitude.",
		Provisional: true,
		// OpenAI prices cached input at a discount and does not bill cache
		// creation separately — cache_write_input_tokens is a subset of
		// input_tokens. The 1h/5m multipliers are therefore never applied.
		Mult: Multipliers{Cache1h: 1.0, Cache5m: 1.0, CacheRead: 0.1},
		Table: map[string]Price{
			// gpt-5.x family
			"gpt-5.6-luna":       {1.25, 10.0},
			"gpt-5.6-sol":        {1.25, 10.0},
			"gpt-5.6-terra":      {1.25, 10.0},
			"gpt-5.6":            {1.25, 10.0},
			"gpt-5.5":            {1.25, 10.0},
			"gpt-5.4":            {1.25, 10.0},
			"gpt-5.4-mini":       {0.25, 2.0},
			"gpt-5.3-codex":      {1.25, 10.0},
			"gpt-5.2-codex":      {1.25, 10.0},
			"gpt-5.1-codex-mini": {0.25, 2.0},
			// retired, still in the corpus
			"gpt-4.1": {2.0, 8.0},
			"o3":      {2.0, 8.0},
			"o4-mini": {1.1, 4.4},
		},
		// Deliberately unpriced, so a scan reports them rather than valuing
		// them at zero: "sonnet" is an Anthropic model reached through Codex
		// and would be double-counted, and "codex-auto-review" is a harness
		// label, not a billable model.
		Unpriced: []string{"sonnet", "codex-auto-review"},
	},
}

// EpochAt returns the pricing policy in effect for a provider on a date. The
// oldest epoch is dated 2000-01-01, so this only fails for an unknown provider.
func EpochAt(p Provider, date string) (PriceEpoch, bool) {
	var best PriceEpoch
	found := false
	for _, e := range epochs {
		if e.Provider != p || e.From > date {
			continue
		}
		if !found || e.From > best.From {
			best, found = e, true
		}
	}
	return best, found
}

// PriceAt returns what one model cost per MTok on one date, resolving aliases
// through the same epoch. A model with no list price returns false rather than
// a zero price: unpriced and free are different facts.
func PriceAt(p Provider, date, model string) (Price, bool) {
	e, ok := EpochAt(p, date)
	if !ok {
		return Price{}, false
	}
	key := e.resolve(model)
	if key == "" {
		return Price{}, false
	}
	price, ok := e.Table[key]
	return price, ok
}

// MultAt returns the cache multipliers in effect for a provider on a date.
func MultAt(p Provider, date string) Multipliers {
	e, ok := EpochAt(p, date)
	if !ok {
		return Multipliers{Cache1h: 1, Cache5m: 1, CacheRead: 1}
	}
	return e.Mult
}

// resolve maps a recorded model string to a key in this epoch's table, or ""
// when the epoch cannot price it.
func (e PriceEpoch) resolve(model string) string {
	if model == "" {
		return ""
	}
	if full, ok := e.Aliases[model]; ok {
		return full
	}
	m := trimModelSuffixes(model)
	if _, ok := e.Table[m]; ok {
		return m
	}
	return ""
}

// Epochs returns every epoch on record, oldest first, for the daily snapshot
// and for `claudeme cost` to report what it priced against.
func Epochs() []PriceEpoch {
	out := append([]PriceEpoch(nil), epochs...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		return out[i].From < out[j].From
	})
	return out
}

// usageTokens is one call's countable tokens, provider-agnostic. Both the
// Claude transcript parser and the Codex one reduce to this before any money
// arithmetic happens.
type usageTokens struct {
	In           int // billable uncached input
	Out          int
	CacheRead    int
	CacheWrite1h int
	CacheWrite5m int
}

// cost returns the list-price cost of one call's tokens, in micro-dollars.
// This is the only place in the codebase where money arithmetic happens.
func cost(u usageTokens, p Price, m Multipliers) int64 {
	dollars := (float64(u.In)*p.In +
		float64(u.CacheWrite1h)*p.In*m.Cache1h +
		float64(u.CacheWrite5m)*p.In*m.Cache5m +
		float64(u.CacheRead)*p.In*m.CacheRead +
		float64(u.Out)*p.Out) / 1e6
	return int64(dollars * 1e6)
}

// ============ Dated price record ============

// PricesRoot is where the daily price snapshot lives: one file per date,
// beside the transcripts but outside the tree Claude Code manages.
func PricesRoot() string {
	return filepath.Join(config.SharedDir(), "prices")
}

// PriceRecord is what the table said on one date. Written daily so a later
// correction to the seed epochs cannot silently rewrite what was already
// valued — the record on disk is the audit trail, independent of the binary.
type PriceRecord struct {
	Date    string       `json:"date"`
	Written string       `json:"written"`
	Epochs  []PriceEpoch `json:"epochs"`
}

// RecordPrices writes the epochs in effect on one date, unless an identical
// record is already there. Returns whether it wrote.
//
// Identical content is left alone rather than re-stamped: a no-op write every
// day would make `written` churn and hide the day a price actually moved.
func RecordPrices(root, date, now string) (bool, error) {
	var live []PriceEpoch
	for _, p := range []Provider{Anthropic, OpenAI} {
		if e, ok := EpochAt(p, date); ok {
			live = append(live, e)
		}
	}
	rec := PriceRecord{Date: date, Written: now, Epochs: live}

	path := filepath.Join(root, date+".json")
	if old, err := os.ReadFile(path); err == nil {
		var prev PriceRecord
		if json.Unmarshal(old, &prev) == nil && sameEpochs(prev.Epochs, live) {
			return false, nil
		}
	}

	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return false, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return false, err
	}
	return true, os.Rename(tmp, path)
}

// sameEpochs compares two recorded policies by content, ignoring the
// timestamp wrapped around them.
func sameEpochs(a, b []PriceEpoch) bool {
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && string(x) == string(y)
}

// LoadPriceRecord reads the record for one date, or reports that none exists.
func LoadPriceRecord(root, date string) (*PriceRecord, bool, error) {
	data, err := os.ReadFile(filepath.Join(root, date+".json"))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var rec PriceRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, false, err
	}
	return &rec, true, nil
}
