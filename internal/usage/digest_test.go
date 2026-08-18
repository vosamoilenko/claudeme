package usage

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testDigest(session, date string, summary string) *Digest {
	return &Digest{
		Session:    session,
		Date:       date,
		Cwd:        "/Users/x/dev/thing",
		Project:    "thing",
		Transcript: "/tmp/" + session + ".jsonl",
		Model:      "gpt-5.6-luna",
		DigestedAt: "2026-08-14T05:00:00Z",
		Summary:    json.RawMessage(summary),
	}
}

const okSummary = `{"goal":"g","outcome":"completed","summary":"did the thing"}`

func TestPutDigestKeepsBothSessions(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"s1", "s2"} {
		if err := PutDigest(root, testDigest(id, "2026-08-13", okSummary)); err != nil {
			t.Fatalf("put %s: %v", id, err)
		}
	}

	f, err := LoadDigest(digestPathIn(root, "2026-08-13", "thing"))
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Ids(); len(got) != 2 || got[0] != "s1" || got[1] != "s2" {
		t.Fatalf("want [s1 s2], got %v", got)
	}
	if f.Project != "thing" || f.Date != "2026-08-13" || f.Version != digestVersion {
		t.Fatalf("header not filled: %+v", *f)
	}
}

func TestPutDigestReplacesRatherThanDuplicates(t *testing.T) {
	root := t.TempDir()
	if err := PutDigest(root, testDigest("s1", "2026-08-13", okSummary)); err != nil {
		t.Fatal(err)
	}
	again := testDigest("s1", "2026-08-13", `{"goal":"g2","outcome":"partial","summary":"redone"}`)
	if err := PutDigest(root, again); err != nil {
		t.Fatal(err)
	}

	f, err := LoadDigest(digestPathIn(root, "2026-08-13", "thing"))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Sessions) != 1 {
		t.Fatalf("want 1 session after re-digest, got %d", len(f.Sessions))
	}
	var got struct{ Outcome string }
	if err := json.Unmarshal(f.Sessions["s1"].Summary, &got); err != nil {
		t.Fatal(err)
	}
	if got.Outcome != "partial" {
		t.Fatalf("re-digest did not replace: outcome %q", got.Outcome)
	}
}

func TestDigestedReportsSeenSessions(t *testing.T) {
	root := t.TempDir()
	if err := PutDigest(root, testDigest("s1", "2026-08-13", okSummary)); err != nil {
		t.Fatal(err)
	}

	seen, err := Digested(root, "2026-08-13", "thing")
	if err != nil {
		t.Fatal(err)
	}
	if !seen["s1"] || seen["s2"] {
		t.Fatalf("want only s1 seen, got %v", seen)
	}

	// A date nobody digested yet is empty, not an error.
	seen, err = Digested(root, "2026-08-12", "thing")
	if err != nil || len(seen) != 0 {
		t.Fatalf("want empty/no error for a fresh date, got %v / %v", seen, err)
	}
}

func TestUsableRejectsEmptySummaries(t *testing.T) {
	cases := []struct {
		name    string
		summary string
		want    bool
	}{
		{"complete", okSummary, true},
		{"empty summary field", `{"outcome":"completed","summary":""}`, false},
		{"missing outcome", `{"summary":"text"}`, false},
		{"not json", `not json at all`, false},
		{"empty", ``, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := testDigest("s1", "2026-08-13", c.summary)
			if got := d.Usable(); got != c.want {
				t.Fatalf("Usable() = %v, want %v", got, c.want)
			}
		})
	}
	if (*Digest)(nil).Usable() {
		t.Fatal("nil digest must not be usable")
	}
}

// Metrics are re-derivable from the transcript; a summary is not. A record
// carrying only metrics must never be strong enough to gate a delete.
func TestUsableIgnoresMetrics(t *testing.T) {
	d := testDigest("s1", "2026-08-13", "")
	d.Metrics = json.RawMessage(`{"started":"2026-08-13T09:00:00Z","metrics":{"turns":9}}`)
	if d.Usable() {
		t.Fatal("a metrics-only digest must not be usable")
	}

	d.Summary = json.RawMessage(okSummary)
	if !d.Usable() {
		t.Fatal("metrics must not make a complete summary unusable")
	}
}

// distill.py owns the metrics shape, so the round trip has to preserve it
// byte for byte rather than through a Go mirror of the fields.
func TestPutDigestRoundTripsMetrics(t *testing.T) {
	root := t.TempDir()
	raw := `{"session_id":"s1","branches":["feat/DP-3282","sandbox"],"started":"2026-08-17T18:20:53.322Z","metrics":{"wall_ms":7122301}}`

	d := testDigest("s1", "2026-08-13", okSummary)
	d.Metrics = json.RawMessage(raw)
	if err := PutDigest(root, d); err != nil {
		t.Fatal(err)
	}

	f, err := LoadDigest(digestPathIn(root, "2026-08-13", "thing"))
	if err != nil {
		t.Fatal(err)
	}
	got := f.Sessions["s1"].Metrics
	var want, have any
	if err := json.Unmarshal([]byte(raw), &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got, &have); err != nil {
		t.Fatalf("stored metrics are not JSON: %v", err)
	}
	if fmt.Sprint(want) != fmt.Sprint(have) {
		t.Fatalf("metrics changed in the round trip:\n want %s\n  got %s", raw, got)
	}
}

// A record written before metrics existed must stay byte-identical, so the
// backfill is the only thing that ever adds the key.
func TestDigestWithoutMetricsOmitsTheKey(t *testing.T) {
	data, err := json.Marshal(testDigest("s1", "2026-08-13", okSummary))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "metrics") {
		t.Fatalf("empty metrics must not be encoded: %s", data)
	}
}

func TestSaveDigestIsAtomic(t *testing.T) {
	root := t.TempDir()
	if err := PutDigest(root, testDigest("s1", "2026-08-13", okSummary)); err != nil {
		t.Fatal(err)
	}
	path := digestPathIn(root, "2026-08-13", "thing")
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temp file left behind")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &DigestFile{}); err != nil {
		t.Fatalf("written file does not parse: %v", err)
	}
}

func TestSafeNameFlattensPathCharacters(t *testing.T) {
	cases := map[string]string{
		"thing":            "thing",
		"my project":       "my-project",
		"a/b":              "a-b",
		"../escape":        "escape",
		"...":              "unknown",
		"scl-gitlab/tts:x": "scl-gitlab-tts-x",
	}
	for in, want := range cases {
		if got := safeName(in); got != want {
			t.Fatalf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
}

// writeTranscript makes a minimal session transcript under root/dir.
func writeTranscript(t *testing.T, root, dir, session, cwd, ts string, gz bool) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
		t.Fatal(err)
	}
	line := fmt.Sprintf(`{"sessionId":%q,"cwd":%q,"timestamp":%q,"uuid":"u1"}`+"\n", session, cwd, ts)
	path := filepath.Join(root, dir, session+".jsonl")
	if gz {
		path += gzExt
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		zw.Write([]byte(line))
		zw.Close()
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestScanSessionsCoversArchiveAndSkipsNested(t *testing.T) {
	live, arch := t.TempDir(), t.TempDir()
	dir := "-tmp-work-thing"
	cwd := "/tmp/work/thing"
	writeTranscript(t, live, dir, "s-live", cwd, "2026-08-13T10:00:00Z", false)
	writeTranscript(t, arch, dir, "s-arch", cwd, "2026-07-04T10:00:00Z", true)
	// a subagent transcript, one level down: part of a session, not one itself
	writeTranscript(t, live, filepath.Join(dir, "sub"), "s-nested", cwd, "2026-08-13T10:05:00Z", false)

	got, err := ScanSessions([]string{live, arch})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 sessions (nested excluded), got %d: %+v", len(got), got)
	}
	// oldest first, so a limited run digests the backlog rather than a slice
	if got[0].Session != "s-arch" || got[1].Session != "s-live" {
		t.Fatalf("want archive first by date, got %s then %s", got[0].Session, got[1].Session)
	}
	if got[0].Date != "2026-07-04" || got[0].Cwd != cwd {
		t.Fatalf("archived candidate misread: %+v", got[0])
	}
	if !strings.HasSuffix(got[0].Path, gzExt) {
		t.Fatalf("want the gzipped path, got %s", got[0].Path)
	}
}

// A transcript whose first line is a huge payload — a pasted file or an image —
// used to end the scan before any timestamp was seen, and the session was
// silently dropped. One real 4.3MB archived session was lost this way.
func TestScanSessionsSurvivesAnOversizedFirstLine(t *testing.T) {
	live := t.TempDir()
	dir, cwd := "-tmp-work-thing", "/tmp/work/thing"
	if err := os.MkdirAll(filepath.Join(live, dir), 0o755); err != nil {
		t.Fatal(err)
	}
	huge := fmt.Sprintf(`{"type":"image","data":%q}`+"\n", strings.Repeat("x", 2*maxCandidateLine))
	meta := fmt.Sprintf(`{"sessionId":"s1","cwd":%q,"timestamp":"2026-07-26T18:57:49Z"}`+"\n", cwd)
	if err := os.WriteFile(filepath.Join(live, dir, "s1.jsonl"), []byte(huge+meta), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ScanSessions([]string{live})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want the session found past the oversized line, got %d", len(got))
	}
	if got[0].Date != "2026-07-26" || got[0].Cwd != cwd {
		t.Fatalf("candidate misread: %+v", got[0])
	}
}

func TestScanSessionsPrefersLiveOverArchivedCopy(t *testing.T) {
	live, arch := t.TempDir(), t.TempDir()
	dir, cwd := "-tmp-work-thing", "/tmp/work/thing"
	writeTranscript(t, live, dir, "s1", cwd, "2026-08-13T10:00:00Z", false)
	writeTranscript(t, arch, dir, "s1", cwd, "2026-08-13T10:00:00Z", true)

	got, err := ScanSessions([]string{live, arch})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want the session once, got %d", len(got))
	}
	if strings.HasSuffix(got[0].Path, gzExt) {
		t.Fatal("want the live copy to win over the archived one")
	}
}

func TestSettledHoldsBackTranscriptsStillBeingWritten(t *testing.T) {
	live := t.TempDir()
	dir, cwd := "-tmp-work-thing", "/tmp/work/thing"
	open := writeTranscript(t, live, dir, "s-open", cwd, "2026-08-15T10:00:00Z", false)
	done := writeTranscript(t, live, dir, "s-done", cwd, "2026-08-15T09:00:00Z", false)

	now := time.Now()
	if err := os.Chtimes(open, now, now.Add(-5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(done, now, now.Add(-3*time.Hour)); err != nil {
		t.Fatal(err)
	}

	cands, err := ScanSessions([]string{live})
	if err != nil {
		t.Fatal(err)
	}
	got := Settled(cands, now, time.Hour)
	if len(got) != 1 || got[0].Session != "s-done" {
		t.Fatalf("want only the settled session, got %+v", got)
	}
	if len(cands) != 2 {
		t.Fatal("Settled must not mutate its input")
	}
}

func TestPendingDropsDigestedSessions(t *testing.T) {
	live := t.TempDir()
	root := t.TempDir()
	dir, cwd := "-tmp-work-thing", "/tmp/work/thing"
	writeTranscript(t, live, dir, "s1", cwd, "2026-08-13T10:00:00Z", false)
	writeTranscript(t, live, dir, "s2", cwd, "2026-08-13T11:00:00Z", false)

	cands, err := ScanSessions([]string{live})
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 2 {
		t.Fatalf("want 2 candidates, got %d", len(cands))
	}

	d := testDigest("s1", "2026-08-13", okSummary)
	d.Project = cands[0].Project
	if err := PutDigest(root, d); err != nil {
		t.Fatal(err)
	}

	pending, err := Pending(root, cands)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Session != "s2" {
		t.Fatalf("want only s2 pending, got %+v", pending)
	}
}

// The backfill's predicate: a summarized session still owes metrics, and a
// metrics-only session still owes a summary. Neither is finished by the other.
func TestPendingMetricsAndPendingAskDifferentQuestions(t *testing.T) {
	live := t.TempDir()
	root := t.TempDir()
	dir, cwd := "-tmp-work-thing", "/tmp/work/thing"
	writeTranscript(t, live, dir, "s1", cwd, "2026-08-13T10:00:00Z", false)
	writeTranscript(t, live, dir, "s2", cwd, "2026-08-13T11:00:00Z", false)
	writeTranscript(t, live, dir, "s3", cwd, "2026-08-13T12:00:00Z", false)

	cands, err := ScanSessions([]string{live})
	if err != nil {
		t.Fatal(err)
	}

	// s1: summarized, no metrics — what every record looked like before this.
	summarized := testDigest("s1", "2026-08-13", okSummary)
	summarized.Project = cands[0].Project
	// s2: metrics only — what the backfill writes for a session never digested.
	metricsOnly := testDigest("s2", "2026-08-13", "")
	metricsOnly.Project = cands[0].Project
	metricsOnly.Metrics = json.RawMessage(`{"started":"2026-08-13T11:00:00Z"}`)
	metricsOnly.MetricsAt = "2026-08-18T05:00:00Z"
	for _, d := range []*Digest{summarized, metricsOnly} {
		if err := PutDigest(root, d); err != nil {
			t.Fatal(err)
		}
	}

	pending, err := Pending(root, cands)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 || pending[0].Session != "s2" || pending[1].Session != "s3" {
		t.Fatalf("want s2 and s3 owing a summary, got %+v", pending)
	}

	needMetrics, err := PendingMetrics(root, cands)
	if err != nil {
		t.Fatal(err)
	}
	if len(needMetrics) != 2 || needMetrics[0].Session != "s1" || needMetrics[1].Session != "s3" {
		t.Fatalf("want s1 and s3 owing metrics, got %+v", needMetrics)
	}
}

// The backfill merges onto the record already there rather than replacing it:
// re-running the model over 500 sessions is exactly what it exists to avoid.
func TestBackfillingMetricsKeepsTheSummary(t *testing.T) {
	root := t.TempDir()
	if err := PutDigest(root, testDigest("s1", "2026-08-13", okSummary)); err != nil {
		t.Fatal(err)
	}

	d, err := GetDigest(root, "2026-08-13", "thing", "s1")
	if err != nil || d == nil {
		t.Fatalf("GetDigest = %v, %v", d, err)
	}
	d.Metrics = json.RawMessage(`{"started":"2026-08-13T10:00:00Z"}`)
	d.MetricsAt = "2026-08-18T05:00:00Z"
	if err := PutDigest(root, d); err != nil {
		t.Fatal(err)
	}

	got, err := GetDigest(root, "2026-08-13", "thing", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if !got.HasMetrics() {
		t.Fatal("metrics were not stored")
	}
	// SaveDigest re-indents raw JSON, so the summary is compared by content.
	var before, after any
	if err := json.Unmarshal([]byte(okSummary), &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got.Summary, &after); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(before) != fmt.Sprint(after) {
		t.Fatalf("backfill changed the summary: %s", got.Summary)
	}
	if got.Model != "gpt-5.6-luna" || got.DigestedAt != "2026-08-14T05:00:00Z" {
		t.Fatalf("backfill disturbed the summary record: %+v", *got)
	}

	// A missing session is nil, not an error: the backfill treats it as new.
	if got, err := GetDigest(root, "2026-08-13", "thing", "nope"); got != nil || err != nil {
		t.Fatalf("want nil/nil for an unknown session, got %v / %v", got, err)
	}
}

// Updated marks the newest write to the file. A backfill stamping an old
// session must not drag it backwards past a newer summary.
func TestPutDigestUpdatedOnlyMovesForward(t *testing.T) {
	root := t.TempDir()
	newest := testDigest("s1", "2026-08-13", okSummary)
	newest.DigestedAt = "2026-08-14T05:00:00Z"
	older := testDigest("s2", "2026-08-13", okSummary)
	older.DigestedAt = "2026-08-01T05:00:00Z"
	for _, d := range []*Digest{newest, older} {
		if err := PutDigest(root, d); err != nil {
			t.Fatal(err)
		}
	}

	f, err := LoadDigest(digestPathIn(root, "2026-08-13", "thing"))
	if err != nil {
		t.Fatal(err)
	}
	if f.Updated != "2026-08-14T05:00:00Z" {
		t.Fatalf("Updated went backwards: %q", f.Updated)
	}
}

func TestRunnerUnpacksScripts(t *testing.T) {
	r, err := NewRunner()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, name := range []string{"distill.py", "summarize.sh", "schema.json"} {
		info, err := os.Stat(filepath.Join(r.dir, name))
		if err != nil {
			t.Fatalf("%s not unpacked: %v", name, err)
		}
		if strings.HasSuffix(name, ".sh") && info.Mode().Perm()&0o100 == 0 {
			t.Fatalf("%s is not executable", name)
		}
	}
	dir := r.dir
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("Close left the temp dir behind")
	}
}
