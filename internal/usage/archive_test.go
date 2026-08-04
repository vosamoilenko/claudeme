package usage

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// archiveFixture writes a project directory with one session transcript, one
// subagent transcript and its metadata sidecar, then ages everything but the
// session by `old`.
func archiveFixture(t *testing.T) (src, dst string) {
	t.Helper()
	base := t.TempDir()
	src = filepath.Join(base, "projects")
	dst = filepath.Join(base, "archive")
	subs := filepath.Join(src, "-tmp-p", "s1", "subagents")
	if err := os.MkdirAll(subs, 0o755); err != nil {
		t.Fatal(err)
	}

	line := func(req string, out int, side bool) string {
		return fmt.Sprintf(`{"requestId":%q,"sessionId":"s1","cwd":"/tmp/p",`+
			`"isSidechain":%t,"timestamp":"2026-01-01T00:00:00Z",`+
			`"message":{"model":"claude-opus-5",`+
			`"usage":{"input_tokens":10,"output_tokens":%d}}}`+"\n", req, side, out)
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(src, "-tmp-p", "s1.jsonl"), line("r1", 100, false))
	write(filepath.Join(subs, "agent-a1.jsonl"), line("r2", 40, true))
	write(filepath.Join(subs, "agent-a1.meta.json"), `{"agentType":"Explore"}`)
	write(filepath.Join(src, "-tmp-p", "fresh.jsonl"), line("r3", 7, false))

	old := time.Now().AddDate(0, 0, -30)
	for _, p := range []string{
		filepath.Join(src, "-tmp-p", "s1.jsonl"),
		filepath.Join(subs, "agent-a1.jsonl"),
	} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	return src, dst
}

func TestArchiveMovesOnlyStaleFiles(t *testing.T) {
	src, dst := archiveFixture(t)
	cutoff := time.Now().AddDate(0, 0, -7)

	res, err := Archive(src, dst, cutoff, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 2 || res.Metas != 1 {
		t.Errorf("moved %d transcripts / %d metas, want 2 / 1", res.Files, res.Metas)
	}
	if res.After >= res.Before {
		t.Errorf("archive is %d bytes, live was %d — compression did nothing", res.After, res.Before)
	}

	// Stale files left the live tree and landed gzipped, sidecar included.
	for _, gone := range []string{"-tmp-p/s1.jsonl", "-tmp-p/s1/subagents/agent-a1.jsonl"} {
		if _, err := os.Stat(filepath.Join(src, gone)); !os.IsNotExist(err) {
			t.Errorf("%s still in the live tree", gone)
		}
	}
	for _, want := range []string{
		"-tmp-p/s1.jsonl.gz",
		"-tmp-p/s1/subagents/agent-a1.jsonl.gz",
		"-tmp-p/s1/subagents/agent-a1.meta.json",
	} {
		if _, err := os.Stat(filepath.Join(dst, want)); err != nil {
			t.Errorf("%s missing from the archive", want)
		}
	}
	// The recent one stays where Claude Code can resume it.
	if _, err := os.Stat(filepath.Join(src, "-tmp-p", "fresh.jsonl")); err != nil {
		t.Errorf("fresh transcript was archived: %v", err)
	}
}

func TestArchiveDryRunTouchesNothing(t *testing.T) {
	src, dst := archiveFixture(t)

	res, err := Archive(src, dst, time.Now().AddDate(0, 0, -7), true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 2 {
		t.Errorf("dry run reported %d files, want 2", res.Files)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Errorf("dry run created %s", dst)
	}
	if _, err := os.Stat(filepath.Join(src, "-tmp-p", "s1.jsonl")); err != nil {
		t.Errorf("dry run moved a transcript: %v", err)
	}
}

// The whole point of archiving: spend survives the move, is counted once, and
// keeps its agent attribution.
func TestAnalyzeReadsBothRoots(t *testing.T) {
	src, dst := archiveFixture(t)

	before, err := Analyze([]string{src, dst}, []string{"-tmp-p"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Archive(src, dst, time.Now().AddDate(0, 0, -7), false); err != nil {
		t.Fatal(err)
	}
	after, err := Analyze([]string{src, dst}, []string{"-tmp-p"})
	if err != nil {
		t.Fatal(err)
	}

	if after.Total.Out != before.Total.Out || after.Total.Out != 147 {
		t.Errorf("output %d before / %d after, want 147 both", before.Total.Out, after.Total.Out)
	}
	if after.Total.Cost != before.Total.Cost {
		t.Errorf("cost moved: %d → %d", before.Total.Cost, after.Total.Cost)
	}
	if after.Files != before.Files {
		t.Errorf("file count moved: %d → %d", before.Files, after.Files)
	}
	if after.Agents["Explore"] == nil || after.Agents["Explore"].Out != 40 {
		t.Errorf("agent attribution lost: %v", after.Agents)
	}
}

func TestDiscoverMergesRoots(t *testing.T) {
	src, dst := archiveFixture(t)
	if _, err := Archive(src, dst, time.Now().AddDate(0, 0, -7), false); err != nil {
		t.Fatal(err)
	}

	projects, err := Discover([]string{src, dst})
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 {
		t.Fatalf("got %d projects %v, want 1", len(projects), projects)
	}
	p := projects[0]
	// One name, both roots: Analyze must not be told to read it twice.
	if len(p.Dirs) != 1 || p.Dirs[0] != "-tmp-p" {
		t.Errorf("Dirs = %v, want [-tmp-p]", p.Dirs)
	}
	if p.Files != 3 || p.Sessions != 2 {
		t.Errorf("files/sessions = %d/%d, want 3/2", p.Files, p.Sessions)
	}
	// A member spanning both roots still lives in one directory.
	for _, m := range p.Members {
		if len(m.Dirs) != 1 {
			t.Errorf("member %q Dirs = %v, want one entry", m.Label, m.Dirs)
		}
	}
}

// An empty transcript directory is the last trace of a project retention
// already deleted. Archiving must not be what erases it.
func TestArchiveKeepsPreExistingEmptyDirs(t *testing.T) {
	src, dst := archiveFixture(t)
	ghost := filepath.Join(src, "-tmp-deleted-project")
	if err := os.MkdirAll(ghost, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := Archive(src, dst, time.Now().AddDate(0, 0, -7), false); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(ghost); err != nil {
		t.Errorf("archiving deleted an empty transcript dir: %v", err)
	}
	// ...but the subagents tree it did empty is gone.
	if _, err := os.Stat(filepath.Join(src, "-tmp-p", "s1")); !os.IsNotExist(err) {
		t.Errorf("emptied directory survived the prune")
	}
}

func TestDiscoverMissingArchiveRootIsFine(t *testing.T) {
	src, dst := archiveFixture(t)
	projects, err := Discover([]string{src, dst}) // dst does not exist yet
	if err != nil {
		t.Fatalf("missing archive root should be skipped: %v", err)
	}
	if len(projects) != 1 {
		t.Errorf("got %d projects, want 1", len(projects))
	}
	if _, err := Discover([]string{filepath.Join(src, "nope"), dst}); err == nil {
		t.Error("no readable root should be an error")
	}
}
