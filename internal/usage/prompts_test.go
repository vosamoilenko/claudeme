package usage

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func promptLines(t *testing.T, name string, rows ...[3]string) string {
	t.Helper()
	var all string
	for _, r := range rows {
		all += fmt.Sprintf(`{"display":"x","pastedContents":{},"timestamp":%s,"project":%q,"sessionId":%q}`+"\n",
			r[0], r[1], r[2])
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(all), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadPromptHistoryBuildsSessionWindows(t *testing.T) {
	path := promptLines(t, "history.jsonl",
		[3]string{"1770764428768", "/Users/x/dev/thing", "s1"},
		[3]string{"1770764480699", "/Users/x/dev/thing", "s1"},
		[3]string{"1770765229437", "/Users/x/dev/other", "s2"},
	)

	got, err := ReadPromptHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(got))
	}
	// Oldest first, so a limited run works the backlog rather than a slice.
	if got[0].Session != "s1" || got[1].Session != "s2" {
		t.Fatalf("order = %s, %s", got[0].Session, got[1].Session)
	}
	if got[0].Prompts.Count != 2 || got[0].Cwd != "/Users/x/dev/thing" {
		t.Fatalf("s1 = %+v", got[0])
	}
	if got[0].Date != "2026-02-10" {
		t.Fatalf("date = %q", got[0].Date)
	}
	// Epoch milliseconds become the same UTC stamp the other halves use, so a
	// prompt window and a transcript window are comparable without converting.
	if got[0].Prompts.First != "2026-02-10T23:00:28Z" || got[0].Prompts.Last != "2026-02-10T23:01:20Z" {
		t.Fatalf("window = %s → %s", got[0].Prompts.First, got[0].Prompts.Last)
	}

	// A one-prompt session has a start and no span. Reporting Last == First
	// would claim a zero-length session rather than an unknown one.
	if got[1].Prompts.Count != 1 || got[1].Prompts.Last != "" {
		t.Fatalf("s2 = %+v", got[1].Prompts)
	}
}

// The live file and its .bak overlap completely. Reading both must not double
// any session's prompt count.
func TestReadPromptHistoryDedupesAcrossFiles(t *testing.T) {
	rows := [][3]string{
		{"1770764428768", "/Users/x/dev/thing", "s1"},
		{"1770764480699", "/Users/x/dev/thing", "s1"},
	}
	live := promptLines(t, "history.jsonl", rows...)
	bak := promptLines(t, "history.jsonl.bak", rows...)

	got, err := ReadPromptHistory(live, bak)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Prompts.Count != 2 {
		t.Fatalf("overlap inflated the count: %+v", got)
	}

	// A missing .bak is not an error: most machines have never rotated one.
	if _, err := ReadPromptHistory(live, filepath.Join(t.TempDir(), "nope.jsonl")); err != nil {
		t.Fatalf("missing file should be skipped, got %v", err)
	}
}

// The prompt history is the only source that outlives a transcript, so it
// creates records — but a record it creates is evidence a session happened,
// not an account of it, and must never gate a delete.
func TestPutPromptsCreatesAnUnusableRecord(t *testing.T) {
	root := t.TempDir()
	c := Candidate{Session: "s1", Date: "2026-02-10", Project: "thing", Cwd: "/Users/x/dev/thing"}
	if err := PutPrompts(root, c, Prompts{Count: 2, First: "2026-02-10T23:00:28Z", Last: "2026-02-10T23:01:20Z"}); err != nil {
		t.Fatal(err)
	}

	d, err := GetDigest(root, "2026-02-10", "thing", "s1")
	if err != nil || d == nil {
		t.Fatalf("GetDigest = %v, %v", d, err)
	}
	if d.Usable() {
		t.Fatal("a prompts-only record must never gate a delete")
	}
	if !d.HasPrompts() || d.HasSummary() || d.HasTokens() || d.HasMetrics() {
		t.Fatalf("unexpected shape: %+v", *d)
	}
}

// Re-running must land on the existing record, not beside it.
func TestPutPromptsMergesOntoAnExistingRecord(t *testing.T) {
	root := t.TempDir()
	if err := PutDigest(root, testDigest("s1", "2026-08-13", okSummary)); err != nil {
		t.Fatal(err)
	}
	c := Candidate{Session: "s1", Date: "2026-08-13", Project: "thing", Cwd: "/Users/x/dev/thing"}
	if err := PutPrompts(root, c, Prompts{Count: 9, First: "2026-08-13T10:00:00Z", Last: "2026-08-13T12:00:00Z"}); err != nil {
		t.Fatal(err)
	}

	f, err := LoadDigest(digestPathIn(root, "2026-08-13", "thing"))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Sessions) != 1 {
		t.Fatalf("want 1 record, got %d", len(f.Sessions))
	}
	got := f.Sessions["s1"]
	if !got.Usable() || got.Prompts.Count != 9 {
		t.Fatalf("merge lost a half: %+v", *got)
	}
}

// A session whose transcript is gone can resolve to a different project name
// than the one its record was filed under. IndexDigests is what keeps that
// from splitting one session into two records.
func TestIndexDigestsFindsWhereASessionIsFiled(t *testing.T) {
	root := t.TempDir()
	d := testDigest("s1", "2026-08-13", okSummary)
	d.Project = "tts/frontend"
	if err := PutDigest(root, d); err != nil {
		t.Fatal(err)
	}

	idx, err := IndexDigests(root)
	if err != nil {
		t.Fatal(err)
	}
	if p := idx["s1"]; p.Date != "2026-08-13" || p.Project != "tts/frontend" {
		t.Fatalf("placement = %+v", p)
	}
	if _, ok := idx["nope"]; ok {
		t.Fatal("an unknown session must not be placed")
	}

	// An empty root is empty, not an error: the first run has nothing to read.
	idx, err = IndexDigests(filepath.Join(t.TempDir(), "missing"))
	if err != nil || len(idx) != 0 {
		t.Fatalf("missing root = %v / %v", idx, err)
	}
}
