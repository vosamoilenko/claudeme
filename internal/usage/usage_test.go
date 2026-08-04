package usage

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
)

func TestNormalizeModel(t *testing.T) {
	cases := map[string]string{
		"claude-opus-5":             "claude-opus-5",
		"claude-opus-4-6[1m]":       "claude-opus-4-6",
		"claude-haiku-4-5-20251001": "claude-haiku-4-5",
		"opus":                      "claude-opus-5",
		"sonnet":                    "claude-sonnet-5",
		"gpt-5.2-codex":             "",
		"<synthetic>":               "",
		"":                          "",
	}
	for in, want := range cases {
		if got := NormalizeModel(in); got != want {
			t.Errorf("NormalizeModel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCluster(t *testing.T) {
	// Containers are workspace folders: they hold projects but aren't one.
	containers := map[string]bool{
		"/Users/x":                    true,
		"/Users/x/Developer":          true,
		"/Users/x/Developer/gitlab":   true,
		"/Users/x/Developer/gitlab/g": true,
		"/private/tmp":                true,
	}
	isContainer := func(p string) bool { return containers[p] }

	dirs := []transcriptDir{
		{name: "a", cwd: "/Users/x"},
		{name: "b", cwd: "/Users/x/Developer"},
		{name: "c", cwd: "/Users/x/Developer/audace"},
		{name: "d", cwd: "/Users/x/Developer/audace/1"},
		{name: "e", cwd: "/Users/x/Developer/audace/2"},
		{name: "f", cwd: "/Users/x/Developer/gitlab/g/phish"},
		{name: "g", cwd: "/Users/x/Developer/gitlab/g/phish/frontend"},
		{name: "h", cwd: "/private/tmp"},
		// A sibling whose name merely shares a prefix must not be absorbed.
		{name: "i", cwd: "/Users/x/Developer/audace-worktrees/v1"},
		// ...but one git resolved to a main checkout folds into it.
		{name: "j", cwd: "/Users/x/Developer/audace-worktrees/v2/sub",
			root: "/Users/x/Developer/audace"},
	}

	got := map[string][]string{}
	for _, p := range cluster(dirs, isContainer) {
		sort.Strings(p.Dirs)
		got[p.Path] = p.Dirs
	}

	want := map[string][]string{
		"/Users/x":                               {"a"},
		"/Users/x/Developer":                     {"b"},
		"/Users/x/Developer/audace":              {"c", "d", "e", "j"},
		"/Users/x/Developer/gitlab/g/phish":      {"f", "g"},
		"/private/tmp":                           {"h"},
		"/Users/x/Developer/audace-worktrees/v1": {"i"},
	}

	if len(got) != len(want) {
		t.Fatalf("got %d clusters %v, want %d", len(got), got, len(want))
	}
	for path, wantDirs := range want {
		gotDirs, ok := got[path]
		if !ok {
			t.Errorf("missing cluster %s", path)
			continue
		}
		if len(gotDirs) != len(wantDirs) {
			t.Errorf("%s: got %v, want %v", path, gotDirs, wantDirs)
			continue
		}
		for i := range wantDirs {
			if gotDirs[i] != wantDirs[i] {
				t.Errorf("%s: got %v, want %v", path, gotDirs, wantDirs)
				break
			}
		}
	}
}

func TestClusterMembers(t *testing.T) {
	root := "/Users/x/Developer/app"
	// One directory ("a") holding three cwds, plus a worktree that git
	// resolved back to this checkout.
	dirs := []transcriptDir{
		{name: "a", cwd: root, files: 2, sessions: 2},
		{name: "a", cwd: root + "/portal-client", files: 5, sessions: 5},
		{name: "a", cwd: root + "/.claude/worktrees/agent-1", files: 1, sessions: 0},
		{name: "b", cwd: "/Users/x/Developer/app-worktrees/feat/DP-1", root: root,
			files: 5, sessions: 5},
		{name: "c", cwd: "/Users/x/Developer/elsewhere/tree", root: root,
			files: 3, sessions: 3},
	}

	got := cluster(dirs, func(string) bool { return false })
	if len(got) != 1 {
		t.Fatalf("got %d projects, want 1", len(got))
	}
	p := got[0]
	// The shared directory must be named once, or Analyze reads it twice.
	if len(p.Dirs) != 3 {
		t.Errorf("Dirs = %v, want 3 unique names", p.Dirs)
	}
	if p.Files != 16 || p.Sessions != 15 {
		t.Errorf("files/sessions = %d/%d, want 16/15", p.Files, p.Sessions)
	}

	// Root first, then descending file count, then label.
	want := []struct {
		label    string
		sessions int
	}{
		{"./", 2},
		{"portal-client", 5},
		{"wt:feat/DP-1", 5},
		{"wt:tree", 3},
		{".claude/worktrees/agent-1", 0},
	}
	if len(p.Members) != len(want) {
		t.Fatalf("got %d members %v, want %d", len(p.Members), p.Members, len(want))
	}
	for i, w := range want {
		if p.Members[i].Label != w.label || p.Members[i].Sessions != w.sessions {
			t.Errorf("member %d = %q/%d, want %q/%d",
				i, p.Members[i].Label, p.Members[i].Sessions, w.label, w.sessions)
		}
	}
}

func TestScanDirGroupsByCwd(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "-Users-x-app")
	subs := filepath.Join(dir, "s2", "subagents")
	if err := os.MkdirAll(subs, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, cwd string) {
		t.Helper()
		body := "{}\n"
		if cwd != "" {
			body = fmt.Sprintf("{\"cwd\":%q}\n", cwd)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, "s1.jsonl"), "/Users/x/app")
	write(filepath.Join(dir, "s2.jsonl"), "/Users/x/app/backend")
	write(filepath.Join(dir, "s3.jsonl"), "") // no cwd: falls back to the first
	// A subagent worktree records its own cwd and is invisible without the
	// recursive walk.
	write(filepath.Join(subs, "agent-a.jsonl"), "/Users/x/app/.claude/worktrees/agent-a")

	got := map[string][2]int{}
	for _, d := range scanDir(dir) {
		got[d.cwd] = [2]int{d.files, d.sessions}
	}
	want := map[string][2]int{
		"/Users/x/app":                           {2, 2}, // s1 + s3
		"/Users/x/app/backend":                   {1, 1},
		"/Users/x/app/.claude/worktrees/agent-a": {1, 0},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for cwd, w := range want {
		if got[cwd] != w {
			t.Errorf("%s: files/sessions = %v, want %v", cwd, got[cwd], w)
		}
	}
}

func TestAnalyzeSplitsByCwd(t *testing.T) {
	// Members must partition a project's spend exactly: a record replayed
	// into a transcript with a different cwd is counted once, not twice.
	root := t.TempDir()
	proj := filepath.Join(root, "-tmp-p")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	line := func(req, cwd string, out int) string {
		return fmt.Sprintf(`{"requestId":%q,"sessionId":"s","cwd":%q,`+
			`"timestamp":"2026-01-01T00:00:00Z","message":{"model":"claude-opus-5",`+
			`"usage":{"input_tokens":10,"output_tokens":%d}}}`, req, cwd, out)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(proj, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("s1.jsonl", line("r1", "/tmp/p", 100)+"\n")
	// Resumed after a cd: replays r1, then adds its own turn.
	write("s2.jsonl", line("r1", "/tmp/p/sub", 100)+"\n"+line("r2", "/tmp/p/sub", 50)+"\n")

	rep, err := Analyze([]string{root}, []string{"-tmp-p"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Total.Out != 150 {
		t.Fatalf("total output = %d, want 150", rep.Total.Out)
	}
	var sum int64
	for _, s := range rep.Cwds {
		sum += s.Cost
	}
	if sum != rep.Total.Cost {
		t.Errorf("cwd costs sum to %d, want %d", sum, rep.Total.Cost)
	}
	if rep.Cwds["/tmp/p"].Out != 100 || rep.Cwds["/tmp/p/sub"].Out != 50 {
		t.Errorf("cwd split = %d/%d, want 100/50",
			rep.Cwds["/tmp/p"].Out, rep.Cwds["/tmp/p/sub"].Out)
	}
}

func TestWindow(t *testing.T) {
	if f, l := Window(newReport()); f != "" || l != "" {
		t.Errorf("empty report window = %q/%q, want empty", f, l)
	}

	rep := newReport()
	for _, day := range []string{"2026-07-04", "2026-08-03", "2026-07-19", "unknown"} {
		bump(rep.Days, day, Stats{Calls: 1})
	}
	f, l := Window(rep)
	if f != "2026-07-04" || l != "2026-08-03" {
		t.Errorf("window = %q/%q, want 2026-07-04/2026-08-03 ('unknown' is not a date)", f, l)
	}
}

func TestIsUnder(t *testing.T) {
	cases := []struct {
		path, root string
		want       bool
	}{
		{"/a/b", "/a/b", true},
		{"/a/b/c", "/a/b", true},
		{"/a/bc", "/a/b", false},
		{"/a", "/a/b", false},
	}
	for _, c := range cases {
		if got := isUnder(c.path, c.root); got != c.want {
			t.Errorf("isUnder(%q, %q) = %v, want %v", c.path, c.root, got, c.want)
		}
	}
}

func TestMatchPrefersExactName(t *testing.T) {
	projects := []Project{
		{Name: "audace-extra", Path: "/x/audace-extra"},
		{Name: "audace", Path: "/x/audace"},
	}
	got := Match(projects, "audace")
	if len(got) != 1 || got[0].Name != "audace" {
		t.Errorf("Match exact = %v, want [audace]", got)
	}
	if len(Match(projects, "auda")) != 2 {
		t.Errorf("Match partial should return both")
	}
}

func TestAnalyzeSubagents(t *testing.T) {
	// Subagent spend lives only in <session>/subagents/, never in the
	// session transcript, so the walk has to reach it.
	root := t.TempDir()
	proj := filepath.Join(root, "-tmp-p")
	subs := filepath.Join(proj, "s1", "subagents")
	if err := os.MkdirAll(subs, 0o755); err != nil {
		t.Fatal(err)
	}

	line := func(req, model string, out int, side bool) string {
		return fmt.Sprintf(`{"requestId":%q,"sessionId":"s1","isSidechain":%t,`+
			`"timestamp":"2026-01-01T00:00:00Z","message":{"model":%q,`+
			`"usage":{"input_tokens":10,"output_tokens":%d}}}`, req, side, model, out)
	}
	write := func(path, body string) {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(proj, "s1.jsonl"), line("r1", "claude-opus-5", 100, false)+"\n")
	write(filepath.Join(subs, "agent-a1.jsonl"), line("r2", "claude-opus-5", 40, true)+"\n")
	write(filepath.Join(subs, "agent-a1.meta.json"), `{"agentType":"Explore"}`)
	// No metadata beside this one, and journal.jsonl must be ignored entirely.
	write(filepath.Join(subs, "agent-a2.jsonl"), line("r3", "claude-opus-5", 20, true)+"\n")
	write(filepath.Join(subs, "journal.jsonl"), line("r4", "claude-opus-5", 999, true)+"\n")

	rep, err := Analyze([]string{root}, []string{"-tmp-p"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Total.Out != 160 {
		t.Errorf("total output = %d, want 160 (journal excluded)", rep.Total.Out)
	}
	if rep.Lanes["main"].Out != 100 || rep.Lanes["subagent"].Out != 60 {
		t.Errorf("lanes = main %d, subagent %d; want 100 and 60",
			rep.Lanes["main"].Out, rep.Lanes["subagent"].Out)
	}
	if rep.Agents["Explore"].Out != 40 {
		t.Errorf("Explore output = %d, want 40", rep.Agents["Explore"].Out)
	}
	if rep.Agents[unknownAgent].Out != 20 {
		t.Errorf("%s output = %d, want 20", unknownAgent, rep.Agents[unknownAgent].Out)
	}
}

func TestMainCheckout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	base := t.TempDir()
	repo := filepath.Join(base, "app")
	tree := filepath.Join(base, "app-worktrees", "feat")

	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.MkdirAll(filepath.Join(repo, "client"), 0o755); err != nil {
		t.Fatal(err)
	}
	run(repo, "init", "-q")
	run(repo, "commit", "-q", "--allow-empty", "-m", "init")
	run(repo, "worktree", "add", "-q", "-b", "feat", tree)
	if err := os.MkdirAll(filepath.Join(tree, "client"), 0o755); err != nil {
		t.Fatal(err)
	}

	// t.TempDir may hand back a symlinked path (/var -> /private/var on
	// macOS); git reports the resolved one.
	want, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, cwd := range []string{tree, filepath.Join(tree, "client")} {
		if got := mainCheckout(cwd); got != want {
			t.Errorf("mainCheckout(%s) = %q, want %q", cwd, got, want)
		}
	}
	// Inside its own checkout, and outside git entirely: no remapping.
	for _, cwd := range []string{repo, filepath.Join(repo, "client"), base} {
		if got := mainCheckout(cwd); got != "" {
			t.Errorf("mainCheckout(%s) = %q, want \"\"", cwd, got)
		}
	}
}

func TestSiblingCheckout(t *testing.T) {
	cases := map[string]string{
		"/u/x/berlinhyp/portal-worktrees/feat/DP-1/client": "/u/x/berlinhyp/portal",
		"/u/x/app-worktrees/sandbox":                       "/u/x/app",
		"/u/x/app":                                         "",
		"/u/x/-worktrees/v1":                               "",
		"":                                                 "",
	}
	for in, want := range cases {
		if got := siblingCheckout(in); got != want {
			t.Errorf("siblingCheckout(%q) = %q, want %q", in, got, want)
		}
	}
}
