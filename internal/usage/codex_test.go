package usage

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// codexFile writes a rollout in the modern envelope format.
func codexFile(t *testing.T, dir, name string, lines ...string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	var all string
	for _, l := range lines {
		all += l + "\n"
	}
	if err := os.WriteFile(path, []byte(all), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func meta(id, ts, cwd string) string {
	return fmt.Sprintf(`{"timestamp":"2026-02-25T15:52:45.954Z","type":"session_meta","payload":{"id":%q,"timestamp":%q,"cwd":%q,"cli_version":"0.104.0"}}`, id, ts, cwd)
}

func turn(model, cwd string) string {
	return fmt.Sprintf(`{"timestamp":"2026-02-25T15:53:00Z","type":"turn_context","payload":{"model":%q,"cwd":%q}}`, model, cwd)
}

func tokenCount(in, cached, out, reasoning, total int) string {
	return fmt.Sprintf(`{"timestamp":"2026-02-25T15:54:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":`+
		`{"input_tokens":%d,"cached_input_tokens":%d,"output_tokens":%d,"reasoning_output_tokens":%d,"total_tokens":%d},`+
		`"last_token_usage":{"input_tokens":%d,"cached_input_tokens":%d,"output_tokens":%d,"reasoning_output_tokens":%d,"total_tokens":%d}}}}`,
		in, cached, out, reasoning, total, in, cached, out, reasoning, total)
}

// The totals are cumulative and some CLI versions emit each event twice.
// Summing would roughly double every session on those versions.
func TestCodexTakesLastCumulativeNotSum(t *testing.T) {
	dir := t.TempDir()
	path := codexFile(t, dir, "rollout-2026-02-25T16-50-57-019c957f-0dcf-7ea0-848a-74dac7b851b5.jsonl",
		meta("019c957f-0dcf-7ea0-848a-74dac7b851b5", "2026-02-25T15:50:57.743Z", "/Users/x/dev/thing"),
		turn("gpt-5.6-luna", "/Users/x/dev/thing"),
		tokenCount(9092, 6528, 200, 52, 9292),
		tokenCount(9092, 6528, 200, 52, 9292), // the duplicate emission
		`{"timestamp":"2026-02-25T15:55:00Z","type":"event_msg","payload":{"type":"token_count","info":null}}`,
		tokenCount(10430908, 9742208, 61557, 18724, 10492465),
	)

	s, err := ReadCodexSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Tokens == nil {
		t.Fatal("no ledger")
	}
	got := s.Tokens.Total()
	// Uncached input = 10430908 - 9742208.
	if got.In != 688700 || got.CacheRead != 9742208 || got.Out != 61557 {
		t.Fatalf("took a sum rather than the last total: %+v", got)
	}
	if s.Session != "019c957f-0dcf-7ea0-848a-74dac7b851b5" {
		t.Fatalf("session id = %q", s.Session)
	}
	// The inner timestamp is the start, not the envelope one, and not the
	// filename (which is local time — 16-50-57 for a 15:50:57Z session).
	if s.Started != "2026-02-25T15:50:57.743Z" {
		t.Fatalf("started = %q", s.Started)
	}
}

// cached_input_tokens is a component of input_tokens, not an addition to it.
// Adding them would inflate billable input by the cache hit rate — 93% on the
// session above.
func TestCodexCachedInputIsSubset(t *testing.T) {
	dir := t.TempDir()
	path := codexFile(t, dir, "rollout-2026-08-18T00-51-31-01a011ec-44ab-7cd2-a5e8-761e499f4f4e.jsonl",
		meta("01a011ec-44ab-7cd2-a5e8-761e499f4f4e", "2026-08-18T00:51:31Z", "/Users/x/dev/thing"),
		turn("gpt-5.6-luna", "/Users/x/dev/thing"),
		tokenCount(1000, 800, 100, 30, 1100),
	)

	s, err := ReadCodexSession(path)
	if err != nil {
		t.Fatal(err)
	}
	got := s.Tokens.Total()
	if got.In != 200 {
		t.Fatalf("billable input = %d, want 1000-800 = 200", got.In)
	}
	if got.In+got.CacheRead != 1000 {
		t.Fatalf("the two halves must reconstruct input_tokens, got %d", got.In+got.CacheRead)
	}
	// reasoning is inside output; adding it would overstate every session.
	if got.Out != 100 {
		t.Fatalf("output = %d, want 100 with reasoning already inside it", got.Out)
	}
	// Cached tokens bill at a tenth, so the cheap read must cost less than
	// the same tokens uncached would have.
	micros, unpriced := s.Tokens.Cost()
	if len(unpriced) != 0 || micros <= 0 {
		t.Fatalf("cost = %d, unpriced = %v", micros, unpriced)
	}
}

// A file with a model but no usage event, and a legacy file with neither, are
// different states and must be counted apart rather than dropped.
func TestReadCodexSeparatesLegacyFromUntokened(t *testing.T) {
	root := t.TempDir()
	day := filepath.Join(root, "2026", "02", "25")

	codexFile(t, day, "rollout-2026-02-25T16-00-00-aaaa.jsonl",
		meta("aaaa", "2026-02-25T15:00:00Z", "/Users/x/dev/thing"),
		turn("gpt-5.6-luna", "/Users/x/dev/thing"),
		tokenCount(1000, 0, 100, 0, 1100),
	)
	codexFile(t, day, "rollout-2026-02-25T16-10-00-bbbb.jsonl",
		meta("bbbb", "2026-02-25T15:10:00Z", "/Users/x/dev/thing"),
		turn("gpt-5.6-luna", "/Users/x/dev/thing"),
	)
	legacy := filepath.Join(root, "2025", "09", "02")
	codexFile(t, legacy, "rollout-2025-09-02T17-17-41-cccc.jsonl",
		`{"id":"cccc","timestamp":"2025-09-02T17:17:41.762Z","instructions":null}`,
		`{"record_type":"state"}`,
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>\n  <cwd>/Users/x/dev/legacy-thing</cwd>\n</environment_context>"}]}`,
	)

	sessions, cov, err := ReadCodex(root)
	if err != nil {
		t.Fatal(err)
	}
	if cov.Files != 3 || cov.Tokened != 1 || cov.NoTokens != 1 || cov.Legacy != 1 {
		t.Fatalf("coverage = %+v", cov)
	}
	if len(sessions) != 1 || sessions[0].Session != "aaaa" {
		t.Fatalf("sessions = %+v", sessions)
	}

	// The legacy file's cwd is only in its first user message; losing it
	// would make a third of the corpus unattributable to a project.
	one, err := ReadCodexSession(filepath.Join(legacy, "rollout-2025-09-02T17-17-41-cccc.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if one.Cwd != "/Users/x/dev/legacy-thing" || !one.Legacy || one.Started != "2025-09-02T17:17:41.762Z" {
		t.Fatalf("legacy parse = %+v", *one)
	}
}

// A model the epoch deliberately refuses to price is reported, not valued at
// zero — "sonnet" through Codex is an Anthropic model that would otherwise be
// counted twice.
func TestReadCodexReportsUnpricedModels(t *testing.T) {
	root := t.TempDir()
	day := filepath.Join(root, "2026", "08", "01")
	codexFile(t, day, "rollout-2026-08-01T10-00-00-dddd.jsonl",
		meta("dddd", "2026-08-01T10:00:00Z", "/Users/x/dev/thing"),
		turn("sonnet", "/Users/x/dev/thing"),
		tokenCount(1000, 0, 100, 0, 1100),
	)

	_, cov, err := ReadCodex(root)
	if err != nil {
		t.Fatal(err)
	}
	if cov.Unpriced["sonnet"] != 1 {
		t.Fatalf("unpriced = %v", cov.Unpriced)
	}
	e, _ := EpochAt(OpenAI, "2026-08-01")
	if !e.Knows("sonnet") {
		t.Fatal("a deliberately unpriced model must still be known")
	}
	if e.Knows("some-model-nobody-ran") {
		t.Fatal("an unseen model must not read as known")
	}
}
