package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// metricsWith is distill.py's --metrics shape, trimmed to the fields these
// tests care about: the timings that must survive and the counts that must not.
const metricsWith = `{"session_id":"s1","started":"2026-08-17T18:20:53.322Z",` +
	`"ended":"2026-08-17T20:19:35.623Z","branches":["feat/DP-3282"],` +
	`"metrics":{"wall_ms":7122301,"turns":9,"tokens":{"output":67883,"input":246},` +
	`"cache_hit_rate":0.8724,"subagent_output_tokens":0,"tool_calls":70}}`

func TestStripNaiveTokensDropsSupersededCounts(t *testing.T) {
	cleaned, changed := StripNaiveTokens(json.RawMessage(metricsWith))
	if !changed {
		t.Fatal("expected the payload to change")
	}

	var outer struct {
		Started  string          `json:"started"`
		Branches []string        `json:"branches"`
		Metrics  map[string]any  `json:"metrics"`
		Session  json.RawMessage `json:"session_id"`
	}
	if err := json.Unmarshal(cleaned, &outer); err != nil {
		t.Fatalf("stripped payload is not valid JSON: %v", err)
	}
	for _, gone := range []string{"tokens", "cache_hit_rate", "subagent_output_tokens"} {
		if _, present := outer.Metrics[gone]; present {
			t.Errorf("%s survived the strip", gone)
		}
	}
	// The timings are the whole reason metrics are stored; they must be
	// untouched by a change aimed at the counts beside them.
	if outer.Started != "2026-08-17T18:20:53.322Z" {
		t.Errorf("started = %q, want the original instant", outer.Started)
	}
	if len(outer.Branches) != 1 || outer.Branches[0] != "feat/DP-3282" {
		t.Errorf("branches = %v, want the original branch", outer.Branches)
	}
	if outer.Metrics["wall_ms"] != float64(7122301) {
		t.Errorf("wall_ms = %v, want it preserved", outer.Metrics["wall_ms"])
	}
	if outer.Metrics["tool_calls"] != float64(70) {
		t.Errorf("tool_calls = %v, want it preserved", outer.Metrics["tool_calls"])
	}
}

func TestStripNaiveTokensIsIdempotent(t *testing.T) {
	once, _ := StripNaiveTokens(json.RawMessage(metricsWith))
	twice, changed := StripNaiveTokens(once)
	if changed {
		t.Error("a second strip reported a change")
	}
	if string(once) != string(twice) {
		t.Errorf("a second strip rewrote the payload:\n%s\n%s", once, twice)
	}
}

func TestStripNaiveTokensLeavesUnknownShapesAlone(t *testing.T) {
	for name, raw := range map[string]string{
		"empty":       ``,
		"not json":    `distill blew up`,
		"no metrics":  `{"session_id":"s1","started":"2026-08-17T18:20:53.322Z"}`,
		"metrics str": `{"metrics":"none"}`,
		"already ok":  `{"metrics":{"wall_ms":1}}`,
	} {
		t.Run(name, func(t *testing.T) {
			out, changed := StripNaiveTokens(json.RawMessage(raw))
			if changed {
				t.Error("reported a change on a payload it should not touch")
			}
			if string(out) != raw {
				t.Errorf("rewrote the payload: %q -> %q", raw, out)
			}
		})
	}
}

func TestPutDigestStripsNaiveTokens(t *testing.T) {
	root := t.TempDir()
	d := &Digest{
		Session: "s1", Date: "2026-08-17", Project: "p",
		Metrics: json.RawMessage(metricsWith), MetricsAt: "2026-08-18T00:00:00Z",
	}
	if err := PutDigest(root, d); err != nil {
		t.Fatal(err)
	}
	f, err := LoadDigest(digestPathIn(root, "2026-08-17", "p"))
	if err != nil {
		t.Fatal(err)
	}
	var stored struct {
		Metrics map[string]any `json:"metrics"`
	}
	if err := json.Unmarshal(f.Sessions["s1"].Metrics, &stored); err != nil {
		t.Fatal(err)
	}
	if _, present := stored.Metrics["tokens"]; present {
		t.Error("PutDigest wrote distill.py's token counts to disk")
	}
}

func TestPruneNaiveTokensRewritesOnlyDirtyFiles(t *testing.T) {
	root := t.TempDir()
	// Written past PutDigest so the dirty payload actually reaches disk —
	// PutDigest is the thing that stops this happening from now on.
	dirty := &DigestFile{
		Version: digestVersion, Date: "2026-08-17", Project: "dirty",
		Sessions: map[string]*Digest{"s1": {
			Session: "s1", Date: "2026-08-17", Project: "dirty",
			Metrics: json.RawMessage(metricsWith),
		}},
	}
	dirtyPath := digestPathIn(root, "2026-08-17", "dirty")
	if err := SaveDigest(dirtyPath, dirty); err != nil {
		t.Fatal(err)
	}
	cleanPath := digestPathIn(root, "2026-08-17", "clean")
	clean := &DigestFile{
		Version: digestVersion, Date: "2026-08-17", Project: "clean",
		Sessions: map[string]*Digest{"s2": {
			Session: "s2", Date: "2026-08-17", Project: "clean",
			Metrics: json.RawMessage(`{"metrics":{"wall_ms":5}}`),
		}},
	}
	if err := SaveDigest(cleanPath, clean); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(cleanPath)
	if err != nil {
		t.Fatal(err)
	}

	files, records, err := PruneNaiveTokens(root)
	if err != nil {
		t.Fatal(err)
	}
	if files != 1 || records != 1 {
		t.Errorf("pruned %d file(s)/%d record(s), want 1/1", files, records)
	}
	after, err := os.ReadFile(cleanPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("an already-clean file was rewritten")
	}

	// A second pass has nothing left to do.
	files, records, err = PruneNaiveTokens(root)
	if err != nil {
		t.Fatal(err)
	}
	if files != 0 || records != 0 {
		t.Errorf("second pass touched %d file(s)/%d record(s), want 0/0", files, records)
	}
}

func TestPruneNaiveTokensOnMissingRootIsNotAnError(t *testing.T) {
	files, records, err := PruneNaiveTokens(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("missing root should be a no-op, got %v", err)
	}
	if files != 0 || records != 0 {
		t.Errorf("touched %d/%d on a missing root", files, records)
	}
}
