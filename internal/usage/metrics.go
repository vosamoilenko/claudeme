package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// naiveMetricFields are the token counts distill.py derives by summing the
// usage block of every assistant record. They double-count: one API call can
// land as several assistant records sharing a requestId, and distill.py has no
// dedupe rule — SessionTokens does (requestId, else uuid).
//
// Measured on session 1b8df218 (mitarbeiterportal, 2026-08-17): 123 assistant
// records carrying usage, 44 of them repeat ids. distill.py reports 67,883
// output and 12,352,931 cache-read tokens; the deduped ledger counts 38,080 and
// 8,033,269 — inflated by ~1.8x. cache_hit_rate is a ratio of the same two
// inflated numbers, and subagent_output_tokens is summed the same way, so all
// three go.
//
// Digest.Tokens is the authoritative record. Keeping these would leave two
// contradictory answers in one file, and the wrong one reads as the obvious
// one because it sits beside the timings everything else in metrics is good
// for.
var naiveMetricFields = []string{"cache_hit_rate", "subagent_output_tokens", "tokens"}

// StripNaiveTokens removes those fields from distill.py's --metrics payload
// and reports whether anything changed. Anything that is not the expected
// object-inside-object shape is returned untouched: this normalizes a known
// payload, it never rejects an unknown one.
//
// Key order is not preserved — Go marshals maps sorted — so a stripped payload
// is byte-different from distill.py's output beyond the removals. Nothing reads
// these files positionally.
func StripNaiveTokens(raw json.RawMessage) (json.RawMessage, bool) {
	if len(raw) == 0 {
		return raw, false
	}
	var outer map[string]json.RawMessage
	if err := json.Unmarshal(raw, &outer); err != nil {
		return raw, false
	}
	inner, ok := outer["metrics"]
	if !ok {
		return raw, false
	}
	var counts map[string]json.RawMessage
	if err := json.Unmarshal(inner, &counts); err != nil {
		return raw, false
	}
	changed := false
	for _, field := range naiveMetricFields {
		if _, present := counts[field]; present {
			delete(counts, field)
			changed = true
		}
	}
	if !changed {
		return raw, false
	}
	rebuiltInner, err := json.Marshal(counts)
	if err != nil {
		return raw, false
	}
	outer["metrics"] = rebuiltInner
	rebuilt, err := json.Marshal(outer)
	if err != nil {
		return raw, false
	}
	return rebuilt, true
}

// PruneNaiveTokens rewrites every digest under root whose metrics still carry
// the fields above, and reports how many records and files it touched. It reads
// and writes only what is already on disk: no transcript is reopened, no model
// is called, and a record that was already clean is left byte-identical.
//
// It exists because the metrics backfill wrote those fields before the token
// ledger existed. Once the affected records are through it, it is a no-op that
// costs one pass over a few megabytes.
func PruneNaiveTokens(root string) (files, records int, err error) {
	dates, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	for _, date := range dates {
		if !date.IsDir() {
			continue
		}
		dir := filepath.Join(root, date.Name())
		entries, err := os.ReadDir(dir)
		if err != nil {
			return files, records, err
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, name := range names {
			path := filepath.Join(dir, name)
			f, err := LoadDigest(path)
			if err != nil {
				return files, records, err
			}
			touched := 0
			for _, d := range f.Sessions {
				if !d.HasMetrics() {
					continue
				}
				if cleaned, changed := StripNaiveTokens(d.Metrics); changed {
					d.Metrics = cleaned
					touched++
				}
			}
			if touched == 0 {
				continue
			}
			if err := SaveDigest(path, f); err != nil {
				return files, records, err
			}
			files++
			records += touched
		}
	}
	return files, records, nil
}
