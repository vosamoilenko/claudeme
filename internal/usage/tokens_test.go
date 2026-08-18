package usage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// usageLine writes one assistant turn with a usage block, in the shape the
// real transcripts use. House convention: no testdata/, JSONL into t.TempDir().
func usageLine(requestID, uuid, session, model, ts string, sidechain bool, in, out, read, c1h, c5m int) string {
	return fmt.Sprintf(`{"requestId":%q,"uuid":%q,"sessionId":%q,"timestamp":%q,"isSidechain":%t,`+
		`"message":{"model":%q,"usage":{"input_tokens":%d,"output_tokens":%d,"cache_read_input_tokens":%d,`+
		`"cache_creation":{"ephemeral_1h_input_tokens":%d,"ephemeral_5m_input_tokens":%d}}}}`+"\n",
		requestID, uuid, session, ts, sidechain, model, in, out, read, c1h, c5m)
}

func writeLines(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s1.jsonl")
	var all string
	for _, l := range lines {
		all += l
	}
	if err := os.WriteFile(path, []byte(all), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// Resuming a session replays earlier turns into the new transcript. Counting
// the replay would inflate every long session's ledger.
func TestSessionTokensDedupesReplayedRequests(t *testing.T) {
	path := writeLines(t,
		usageLine("req-1", "u1", "s1", "claude-opus-5", "2026-08-13T10:00:00Z", false, 100, 50, 1000, 10, 20),
		usageLine("req-1", "u2", "s1", "claude-opus-5", "2026-08-13T10:05:00Z", false, 100, 50, 1000, 10, 20),
		usageLine("", "u3", "s1", "claude-opus-5", "2026-08-13T10:06:00Z", false, 7, 3, 0, 0, 0),
		usageLine("", "u3", "s1", "claude-opus-5", "2026-08-13T10:07:00Z", false, 7, 3, 0, 0, 0),
	)

	tok, err := SessionTokens(path)
	if err != nil {
		t.Fatal(err)
	}
	total := tok.Total()
	if total.Calls != 2 {
		t.Fatalf("want 2 distinct calls after dedupe, got %d", total.Calls)
	}
	if total.In != 107 || total.Out != 53 {
		t.Fatalf("replayed turn was counted twice: in=%d out=%d", total.In, total.Out)
	}
}

// The lane split is what makes a session readable; the day split is what makes
// it priceable. Both come out of one pass.
func TestSessionTokensSplitsLanesAndDays(t *testing.T) {
	path := writeLines(t,
		usageLine("r1", "u1", "s1", "claude-opus-5", "2026-08-13T23:50:00Z", false, 100, 10, 0, 0, 0),
		usageLine("r2", "u2", "s1", "claude-sonnet-5", "2026-08-14T00:10:00Z", true, 40, 5, 0, 0, 0),
	)

	tok, err := SessionTokens(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(tok.Models) != 1 || tok.Models[0].Model != "claude-opus-5" {
		t.Fatalf("main lane = %+v", tok.Models)
	}
	if len(tok.Sidechain) != 1 || tok.Sidechain[0].Model != "claude-sonnet-5" {
		t.Fatalf("sidechain lane = %+v", tok.Sidechain)
	}
	if len(tok.Days) != 2 {
		t.Fatalf("a session over midnight must land on two days, got %v", sortedKeys(tok.Days))
	}
	if tok.Started != "2026-08-13T23:50:00Z" || tok.Ended != "2026-08-14T00:10:00Z" {
		t.Fatalf("span = %s → %s", tok.Started, tok.Ended)
	}
}

// The raw model string is stored, never the resolved one: "opus" meant a
// different model in July, and resolving at write time would bake today's
// answer into a fact about the past.
func TestSessionTokensStoresTheRawModelString(t *testing.T) {
	path := writeLines(t,
		usageLine("r1", "u1", "s1", "opus", "2026-08-13T10:00:00Z", false, 100, 10, 0, 0, 0),
	)
	tok, err := SessionTokens(path)
	if err != nil {
		t.Fatal(err)
	}
	if tok.Models[0].Model != "opus" {
		t.Fatalf("model was resolved at write time: %q", tok.Models[0].Model)
	}
	// It still prices, because the alias is resolved at read time by the epoch.
	if micros, unpriced := tok.Cost(); micros == 0 || len(unpriced) != 0 {
		t.Fatalf("alias did not price: %d micros, unpriced %v", micros, unpriced)
	}
}

// An unknown model is reported, never silently valued at zero.
func TestTokensCostReportsUnpricedModels(t *testing.T) {
	path := writeLines(t,
		usageLine("r1", "u1", "s1", "some-local-llama", "2026-08-13T10:00:00Z", false, 100, 10, 0, 0, 0),
	)
	tok, err := SessionTokens(path)
	if err != nil {
		t.Fatal(err)
	}
	micros, unpriced := tok.Cost()
	if micros != 0 || len(unpriced) != 1 || unpriced[0] != "some-local-llama" {
		t.Fatalf("cost = %d, unpriced = %v", micros, unpriced)
	}
}

// CostAt values everything at one date; Cost values each day at its own. With
// a single seed epoch the two agree, which is the invariant that will catch a
// future epoch being added without updating the day-aware path.
func TestCostAtAgreesWithCostUnderOneEpoch(t *testing.T) {
	path := writeLines(t,
		usageLine("r1", "u1", "s1", "claude-opus-5", "2026-08-13T23:50:00Z", false, 100, 10, 500, 20, 30),
		usageLine("r2", "u2", "s1", "claude-opus-5", "2026-08-14T00:10:00Z", false, 40, 5, 0, 0, 0),
	)
	tok, err := SessionTokens(path)
	if err != nil {
		t.Fatal(err)
	}
	actual, _ := tok.Cost()
	at, _ := tok.CostAt("2026-08-14")
	if actual != at {
		t.Fatalf("one epoch, two answers: actual %d vs at-date %d", actual, at)
	}
	if actual == 0 {
		t.Fatal("cost must not be zero for a priced session")
	}
}

// The ledger goes onto the record already there. Re-running must not disturb
// the summary, the metrics, or the numbers.
func TestPutTokensPreservesExistingSummaryAndIsIdempotent(t *testing.T) {
	root := t.TempDir()
	d := testDigest("s1", "2026-08-13", okSummary)
	d.Metrics = json.RawMessage(`{"started":"2026-08-13T10:00:00Z"}`)
	if err := PutDigest(root, d); err != nil {
		t.Fatal(err)
	}

	c := Candidate{Path: "/tmp/s1.jsonl", Session: "s1", Date: "2026-08-13", Project: "thing", Cwd: "/Users/x/dev/thing"}
	tok := &Tokens{
		Provider: Anthropic, Session: "s1", Started: "2026-08-13T10:00:00Z",
		Models:      []*ModelTokens{{Model: "claude-opus-5", Calls: 2, In: 107, Out: 53}},
		ExtractedAt: "2026-08-18T06:00:00Z",
	}
	for i := 0; i < 2; i++ {
		if err := PutTokens(root, c, tok); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}

	f, err := LoadDigest(digestPathIn(root, "2026-08-13", "thing"))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Sessions) != 1 {
		t.Fatalf("want 1 session, got %d", len(f.Sessions))
	}
	got := f.Sessions["s1"]
	if !got.Usable() || !got.HasMetrics() || !got.HasTokens() {
		t.Fatalf("record lost a half: usable=%v metrics=%v tokens=%v",
			got.Usable(), got.HasMetrics(), got.HasTokens())
	}
	if got.Model != "gpt-5.6-luna" || got.DigestedAt != "2026-08-14T05:00:00Z" {
		t.Fatalf("summary provenance disturbed: %+v", *got)
	}
	if n := got.Tokens.Total(); n.Calls != 2 || n.In != 107 {
		t.Fatalf("ledger doubled on the second write: %+v", n)
	}
	if f.Version != digestVersion {
		t.Fatalf("version moved: %d", f.Version)
	}
}

// A session that was never digested still gets a ledger, and that record must
// not look strong enough to justify deleting the transcript behind it.
func TestPutTokensOnANewSessionIsNotUsable(t *testing.T) {
	root := t.TempDir()
	c := Candidate{Path: "/tmp/s9.jsonl", Session: "s9", Date: "2026-08-13", Project: "thing"}
	tok := &Tokens{Provider: Anthropic, Session: "s9",
		Models: []*ModelTokens{{Model: "claude-opus-5", Calls: 1, In: 10}}}
	if err := PutTokens(root, c, tok); err != nil {
		t.Fatal(err)
	}

	got, err := GetDigest(root, "2026-08-13", "thing", "s9")
	if err != nil {
		t.Fatal(err)
	}
	if got.Usable() {
		t.Fatal("a tokens-only record must never gate a delete")
	}
	if !got.HasTokens() || got.HasSummary() {
		t.Fatalf("unexpected record shape: %+v", *got)
	}
}
