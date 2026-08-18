// Package usage reads Claude Code JSONL transcripts and reports token
// spend, grouped into projects.
//
// Cost uses public list pricing. On a Max/Pro subscription the real invoice
// is $0 — treat these numbers as "what this would have cost on the API".
package usage

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/vosamoilenko/claudeme/internal/config"
)

// Price is the $/MTok list price for a model.
type Price struct{ In, Out float64 }

var dateSuffix = regexp.MustCompile(`-\d{8}$`)

// trimModelSuffixes strips the decorations a recorded model string carries
// that are not part of its pricing identity: the context-window marker and
// the -YYYYMMDD release date.
func trimModelSuffixes(model string) string {
	m := strings.TrimSuffix(model, "[1m]")
	return dateSuffix.ReplaceAllString(m, "")
}

// NormalizeModel maps a recorded model string to a pricing key under today's
// prices. Returns "" when the model has no list price (synthetic,
// non-Anthropic). Use PriceAt when the date matters.
func NormalizeModel(model string) string {
	e, ok := EpochAt(Anthropic, "9999-12-31")
	if !ok {
		return ""
	}
	return e.resolve(model)
}

// ProjectsRoot returns the shared transcript directory Claude Code writes to.
func ProjectsRoot() string {
	return filepath.Join(config.SharedDir(), "projects")
}

// ArchiveRoot returns the frozen archive: transcripts a former `claudeme
// archive` command gzipped out of the live tree. Nothing writes here any
// more — reports read it so those sessions still count.
func ArchiveRoot() string {
	return filepath.Join(config.SharedDir(), "archive")
}

// Roots returns every transcript root a report should cover, live first.
// Reports read both so archiving deepens history instead of truncating it.
func Roots() []string {
	return []string{ProjectsRoot(), ArchiveRoot()}
}

// ============ Discovery ============

// Project is a cluster of transcript directories that belong to one codebase.
type Project struct {
	Name     string    // display name (basename of Path)
	Path     string    // real cwd of the cluster root
	Dirs     []string  // transcript directory names, sorted
	Members  []Member  // the distinct cwds that folded into this project
	Files    int       // number of .jsonl transcripts
	Sessions int       // same as Files; one transcript per session file
	Modified time.Time // most recent transcript mtime
}

// Member is one cwd inside a project: the project root itself, a
// subdirectory, or a worktree that resolved back to this checkout.
type Member struct {
	Cwd      string    // real cwd the sessions ran in
	Label    string    // Cwd relative to the project root, for display
	Dirs     []string  // transcript directory names, sorted
	Files    int       // number of .jsonl transcripts
	Sessions int       // same as Files
	Modified time.Time // most recent transcript mtime
}

// memberLabel describes a cwd relative to its project root: "./" for the root
// itself, "wt:<path>" for a worktree that lives outside it, and the relative
// path for a plain subdirectory.
func memberLabel(cwd, root string) string {
	if cwd == root {
		return "./"
	}
	if isUnder(cwd, root) {
		return strings.TrimPrefix(cwd, strings.TrimSuffix(root, "/")+"/")
	}
	if _, after, found := strings.Cut(cwd, worktreesSuffix+string(filepath.Separator)); found {
		return "wt:" + after
	}
	return "wt:" + filepath.Base(cwd)
}

// transcriptDir is one (directory, cwd) pair. A single transcript directory
// can hold sessions from several cwds, so it may produce several of these.
type transcriptDir struct {
	name     string
	cwd      string
	root     string // cwd with linked worktrees resolved to the main checkout
	files    int    // transcripts, including nested subagent ones
	sessions int    // top-level transcripts only; one per session
	modified time.Time
}

// at returns the path clustering works on: the main checkout for a worktree,
// the cwd itself otherwise.
func (d transcriptDir) at() string {
	if d.root != "" {
		return d.root
	}
	return d.cwd
}

// Discover scans the given roots and clusters transcript directories into
// projects.
//
// A directory name appears in every root that holds part of its history — the
// live tree and the archive — so the scans are merged by name before
// clustering. Missing roots are skipped; only all of them missing is an error.
func Discover(roots []string) ([]Project, error) {
	found := map[string][]transcriptDir{}
	var order []string
	read := 0
	var firstErr error

	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		read++
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if _, ok := found[e.Name()]; !ok {
				order = append(order, e.Name())
			}
			found[e.Name()] = append(found[e.Name()],
				scanDir(filepath.Join(root, e.Name()))...)
		}
	}
	if read == 0 {
		return nil, firstErr
	}

	checkout := memoize(mainCheckout)

	var dirs []transcriptDir
	for _, name := range order {
		ds := found[name]
		if len(ds) == 0 {
			ds = []transcriptDir{{}} // empty dir: still a row under --all
		}
		for _, d := range ds {
			d.name = name
			if d.cwd == "" {
				d.cwd = unmangle(name)
			}
			d.root = checkout(d.cwd)
			dirs = append(dirs, d)
		}
	}

	return cluster(dirs, containerFunc(dirs)), nil
}

// memoize caches a pure lookup. mainCheckout shells out to git, and the same
// cwd now arrives once per transcript directory that mentions it.
func memoize(f func(string) string) func(string) string {
	cache := map[string]string{}
	return func(k string) string {
		if v, ok := cache[k]; ok {
			return v
		}
		v := f(k)
		cache[k] = v
		return v
	}
}

// scanDir groups the transcripts in a directory by the cwd they ran in.
//
// The directory name encodes one cwd lossily (path separators become dashes),
// but a directory routinely holds sessions from several: a session resumed
// after a cd, and subagents working in worktrees, both write their own cwd
// into a tree named for the original one. Nested subagent transcripts count
// here for the same reason.
func scanDir(path string) []transcriptDir {
	paths, err := transcripts(path)
	if err != nil {
		return nil
	}
	sort.Strings(paths)

	// The alphabetically first recorded cwd stands in for transcripts that
	// record none, which is what the directory name would have told us.
	cwds := make([]string, len(paths))
	fallback := ""
	for i, p := range paths {
		cwds[i] = findCwd(p)
		if fallback == "" {
			fallback = cwds[i]
		}
	}

	byCwd := map[string]*transcriptDir{}
	var order []string
	for i, p := range paths {
		cwd := cwds[i]
		if cwd == "" {
			cwd = fallback
		}
		d, ok := byCwd[cwd]
		if !ok {
			d = &transcriptDir{cwd: cwd}
			byCwd[cwd] = d
			order = append(order, cwd)
		}
		d.files++
		if filepath.Dir(p) == path {
			d.sessions++ // nested transcripts belong to a session, aren't one
		}
		if info, err := os.Stat(p); err == nil && info.ModTime().After(d.modified) {
			d.modified = info.ModTime()
		}
	}

	out := make([]transcriptDir, 0, len(order))
	for _, cwd := range order {
		out = append(out, *byCwd[cwd])
	}
	return out
}

// openTranscript opens a transcript for reading, decompressing archived ones.
func openTranscript(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(path, gzExt) {
		return f, nil
	}
	zr, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	return gzTranscript{zr, f}, nil
}

// gzTranscript closes the decompressor and the file beneath it.
type gzTranscript struct {
	*gzip.Reader
	f *os.File
}

func (g gzTranscript) Close() error {
	err := g.Reader.Close()
	if cerr := g.f.Close(); err == nil {
		err = cerr
	}
	return err
}

const gzExt = ".gz"

// findCwd returns the first cwd recorded in a transcript, or "".
func findCwd(path string) string {
	f, err := openTranscript(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var entry struct {
			Cwd string `json:"cwd"`
		}
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue
		}
		if entry.Cwd != "" {
			return entry.Cwd
		}
	}
	return ""
}

// unmangle is the fallback when no transcript records a cwd. The directory
// name is the cwd with separators replaced by dashes, which cannot be
// reversed unambiguously, so this only restores the leading slash.
func unmangle(dirName string) string {
	return "/" + strings.TrimPrefix(dirName, "-")
}

// isRepo reports whether a path is a git checkout. Worktrees have a .git
// file rather than a directory, so both count.
func isRepo(path string) bool {
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}

// mainCheckout maps a path inside a linked git worktree to the main checkout
// it belongs to, and returns "" for anything else.
//
// Worktrees are a sibling of the repo, not a descendant — a worktree of
// ~/dev/app typically lives at ~/dev/app-worktrees/<branch> — so no
// ancestor rule can reach them. Git knows the answer: --git-common-dir points
// at the main checkout's .git from anywhere inside any of its worktrees.
func mainCheckout(cwd string) string {
	if cwd == "" {
		return ""
	}
	if fi, err := os.Stat(cwd); err != nil || !fi.IsDir() {
		return siblingCheckout(cwd) // gone from disk: nothing to ask git about
	}
	out, err := exec.Command("git", "-C", cwd,
		"rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return siblingCheckout(cwd)
	}
	gitDir := strings.TrimSpace(string(out))
	if filepath.Base(gitDir) != ".git" {
		return "" // bare repo or an unusual layout: leave the cwd alone
	}
	root := filepath.Dir(gitDir)
	// git reports symlink-resolved paths (/var -> /private/var on macOS),
	// so the cwd has to be resolved too or every path looks like a worktree.
	real := cwd
	if r, err := filepath.EvalSymlinks(cwd); err == nil {
		real = r
	}
	if isUnder(real, root) {
		return "" // already inside its own checkout
	}
	return root
}

// siblingCheckout is the fallback for worktrees git can no longer answer for,
// which is the common case: a worktree is usually deleted long before the
// transcripts of the work done in it. It reads the convention out of the path,
// mapping .../app-worktrees/<anything> back to .../app.
func siblingCheckout(cwd string) string {
	parts := strings.Split(cwd, string(filepath.Separator))
	for i, part := range parts {
		name := strings.TrimSuffix(part, worktreesSuffix)
		if name == part || name == "" {
			continue
		}
		return strings.Join(append(parts[:i:i], name), string(filepath.Separator))
	}
	return ""
}

const worktreesSuffix = "-worktrees"

// containerFunc builds the predicate that separates workspace folders
// (~/Developer, a GitLab group, ~) from actual projects. A path is a container
// when it holds no repo of its own but some other observed directory beneath
// it does — that is what makes ~/Developer/smk-github a folder of projects and
// ~/Developer/audace, whose subdirectories are plain copies, a single project.
//
// Deleted checkouts have no .git to find, so they read as projects and keep
// absorbing their subdirectories.
func containerFunc(dirs []transcriptDir) func(string) bool {
	home, _ := os.UserHomeDir()
	repos := make(map[string]bool, len(dirs))
	for _, d := range dirs {
		repos[d.at()] = isRepo(d.at())
	}
	return func(path string) bool {
		if home != "" && filepath.Clean(path) == filepath.Clean(home) {
			return true
		}
		if repos[path] {
			return false
		}
		for cwd, repo := range repos {
			if repo && cwd != path && isUnder(cwd, path) {
				return true
			}
		}
		return false
	}
}

// cluster assigns each directory to the shallowest non-container cwd at or
// above it, so nested worktrees and subdirectories fold into their project
// root while workspace folders stay separate.
func cluster(dirs []transcriptDir, container func(string) bool) []Project {
	roots := make([]string, 0, len(dirs))
	seen := map[string]bool{}
	for _, d := range dirs {
		if !container(d.at()) && !seen[d.at()] {
			seen[d.at()] = true
			roots = append(roots, d.at())
		}
	}
	// Shallowest first, so the first match is the outermost project root.
	sort.Slice(roots, func(i, j int) bool { return len(roots[i]) < len(roots[j]) })

	byRoot := map[string]*Project{}
	var order []string
	// Several cwds can share a transcript directory; a project must still
	// name it once, or Analyze would read it twice.
	claimed := map[string]bool{}
	claimedByMember := map[string]bool{}
	for _, d := range dirs {
		root := d.at()
		for _, r := range roots {
			if isUnder(d.at(), r) {
				root = r
				break
			}
		}
		p, ok := byRoot[root]
		if !ok {
			p = &Project{Name: filepath.Base(root), Path: root}
			byRoot[root] = p
			order = append(order, root)
		}
		if key := root + "\x00" + d.name; !claimed[key] {
			claimed[key] = true
			p.Dirs = append(p.Dirs, d.name)
		}

		p.Files += d.files
		p.Sessions += d.sessions
		if d.modified.After(p.Modified) {
			p.Modified = d.modified
		}

		m := memberAt(p, d.cwd, root)
		// Same reason as p.Dirs: a directory split across the live tree and
		// the archive arrives once per root, but it is still one directory.
		if key := d.cwd + "\x00" + d.name; !claimedByMember[key] {
			claimedByMember[key] = true
			m.Dirs = append(m.Dirs, d.name)
		}
		m.Files += d.files
		m.Sessions += d.sessions
		if d.modified.After(m.Modified) {
			m.Modified = d.modified
		}
	}

	projects := make([]Project, 0, len(order))
	for _, root := range order {
		p := byRoot[root]
		sort.Strings(p.Dirs)
		for i := range p.Members {
			sort.Strings(p.Members[i].Dirs)
		}
		// The root first, then the rest by descending size: the big
		// worktree matters more than the alphabetically lucky one.
		sort.SliceStable(p.Members, func(i, j int) bool {
			if (p.Members[i].Label == "./") != (p.Members[j].Label == "./") {
				return p.Members[i].Label == "./"
			}
			if p.Members[i].Files != p.Members[j].Files {
				return p.Members[i].Files > p.Members[j].Files
			}
			return p.Members[i].Label < p.Members[j].Label
		})
		projects = append(projects, *p)
	}
	sort.Slice(projects, func(i, j int) bool {
		return projects[i].Modified.After(projects[j].Modified)
	})
	return projects
}

// memberAt returns the member for a cwd, appending one if it is new.
func memberAt(p *Project, cwd, root string) *Member {
	for i := range p.Members {
		if p.Members[i].Cwd == cwd {
			return &p.Members[i]
		}
	}
	p.Members = append(p.Members, Member{Cwd: cwd, Label: memberLabel(cwd, root)})
	return &p.Members[len(p.Members)-1]
}

// isUnder reports whether path is root or lives beneath it.
func isUnder(path, root string) bool {
	if path == root {
		return true
	}
	return strings.HasPrefix(path, strings.TrimSuffix(root, "/")+"/")
}

// Match returns the projects whose name or path contains the substring.
// An exact name match wins outright.
func Match(projects []Project, query string) []Project {
	if query == "" {
		return projects
	}
	q := strings.ToLower(query)
	var exact, partial []Project
	for _, p := range projects {
		switch {
		case strings.EqualFold(p.Name, query):
			exact = append(exact, p)
		case strings.Contains(strings.ToLower(p.Name), q),
			strings.Contains(strings.ToLower(p.Path), q):
			partial = append(partial, p)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return partial
}

// ============ Aggregation ============

// Stats is one row of a breakdown table. Cost is in micro-dollars so the
// aggregation stays integral.
type Stats struct {
	Cost      int64
	Calls     int
	In        int
	Out       int
	CacheRead int

	// CacheWrite is the sum of the two halves below, kept because every
	// display site reads it and one number is what a reader wants. The halves
	// are kept because they bill at different multipliers (2.0 vs 1.25), and
	// without them a stored day can never be re-priced exactly.
	CacheWrite   int
	CacheWrite1h int
	CacheWrite5m int
}

func (s *Stats) add(o Stats) {
	s.Cost += o.Cost
	s.Calls += o.Calls
	s.In += o.In
	s.Out += o.Out
	s.CacheRead += o.CacheRead
	s.CacheWrite += o.CacheWrite
	s.CacheWrite1h += o.CacheWrite1h
	s.CacheWrite5m += o.CacheWrite5m
}

// CwdStats is one cwd's slice of a report: its total, plus the same day and
// skill breakdowns the report keeps globally. Scoping them per cwd is what
// lets `projects` show a per-day and per-skill split for each project without
// re-reading the transcripts once per project.
type CwdStats struct {
	Stats
	Days   map[string]*Stats
	Skills map[string]*Stats
}

func newCwdStats() *CwdStats {
	return &CwdStats{Days: map[string]*Stats{}, Skills: map[string]*Stats{}}
}

// Report is the full breakdown of a set of transcripts.
type Report struct {
	Days     map[string]*Stats
	Models   map[string]*Stats
	Skills   map[string]*Stats
	Sessions map[string]*Stats
	Lanes    map[string]*Stats
	Agents   map[string]*Stats    // subagent spend by agent type
	Cwds     map[string]*CwdStats // spend by the cwd the transcript ran in
	// Crossed breakdowns the durable snapshot needs, keyed day+Sep+model and
	// day+Sep+skill. Report.Models and Report.Skills are whole-report totals;
	// a time series needs them per day, and only this pass sees both at once.
	DayModels map[string]*Stats
	DaySkills map[string]*Stats
	DaySess   map[string]map[string]bool // distinct session ids per day
	Tools     map[string]int
	Unpriced  map[string]int // model strings with no list price, by call count
	Total     Stats
	Files     int
}

func newReport() *Report {
	return &Report{
		Days:      map[string]*Stats{},
		Models:    map[string]*Stats{},
		Skills:    map[string]*Stats{},
		Sessions:  map[string]*Stats{},
		Lanes:     map[string]*Stats{},
		Agents:    map[string]*Stats{},
		Cwds:      map[string]*CwdStats{},
		DayModels: map[string]*Stats{},
		DaySkills: map[string]*Stats{},
		DaySess:   map[string]map[string]bool{},
		Tools:     map[string]int{},
		Unpriced:  map[string]int{},
	}
}

func bump(m map[string]*Stats, key string, row Stats) {
	s, ok := m[key]
	if !ok {
		s = &Stats{}
		m[key] = s
	}
	s.add(row)
}

// bumpCwd records a row against a cwd's total and its nested breakdowns.
func bumpCwd(m map[string]*CwdStats, cwd, day, skill string, row Stats) {
	s, ok := m[cwd]
	if !ok {
		s = newCwdStats()
		m[cwd] = s
	}
	s.add(row)
	bump(s.Days, day, row)
	bump(s.Skills, skill, row)
}

// Merge folds src into dst, summing stats per key. dst is not shared with src,
// so merging several cwds' breakdowns cannot corrupt the report they came from.
func Merge(dst, src map[string]*Stats) {
	for k, v := range src {
		bump(dst, k, *v)
	}
}

// transcript entry, only the fields that matter here.
type entry struct {
	RequestID         string `json:"requestId"`
	UUID              string `json:"uuid"`
	SessionID         string `json:"sessionId"`
	Timestamp         string `json:"timestamp"`
	IsSidechain       bool   `json:"isSidechain"`
	AttributionSkill  string `json:"attributionSkill"`
	AttributionPlugin string `json:"attributionPlugin"`
	Message           struct {
		Model   string `json:"model"`
		Content []struct {
			Type  string `json:"type"`
			Name  string `json:"name"`
			Input struct {
				Skill   string `json:"skill"`
				Command string `json:"command"`
			} `json:"input"`
		} `json:"content"`
		Usage *transcriptUsage `json:"usage"`
	} `json:"message"`
}

// transcriptUsage is one assistant message's usage block. Named rather than
// inline so the token ledger can take one by pointer without restating it.
type transcriptUsage struct {
	InputTokens     int `json:"input_tokens"`
	OutputTokens    int `json:"output_tokens"`
	CacheReadTokens int `json:"cache_read_input_tokens"`
	CacheCreation   struct {
		Ephemeral1h int `json:"ephemeral_1h_input_tokens"`
		Ephemeral5m int `json:"ephemeral_5m_input_tokens"`
	} `json:"cache_creation"`
}

// costMicros returns the cost of one API call in micro-dollars, valued at the
// prices in effect on the day the call happened. A call with no usable
// timestamp falls back to today's epoch.
func costMicros(e *entry, model string) int64 {
	date := "9999-12-31"
	if len(e.Timestamp) >= 10 {
		date = e.Timestamp[:10]
	}
	p, ok := PriceAt(Anthropic, date, model)
	if !ok {
		return 0
	}
	u := e.Message.Usage
	return cost(usageTokens{
		In:           u.InputTokens,
		Out:          u.OutputTokens,
		CacheRead:    u.CacheReadTokens,
		CacheWrite1h: u.CacheCreation.Ephemeral1h,
		CacheWrite5m: u.CacheCreation.Ephemeral5m,
	}, p, MultAt(Anthropic, date))
}

// NoSkill is the bucket for spend that happened with no skill loaded.
const NoSkill = "(no skill)"

// Analyze aggregates every transcript in the given directories.
//
// Records are deduped by requestId: resuming a session replays earlier turns
// into the new transcript, so raw line counts double-count.
//
// Every row is also bucketed under the cwd of the transcript it came from, in
// rep.Cwds. That is what splits a project's cost across its members, and it
// has to happen in this single pass: dedupe is global, so analyzing each cwd's
// files separately would count a replayed record once per cwd it touched.
// Roots that hold no copy of a directory contribute nothing; the global dedupe
// makes reading the same history twice harmless anyway.
func Analyze(roots []string, dirs []string) (*Report, error) {
	rep := newReport()
	seen := map[string]bool{}
	recentSkill := map[string]string{}

	for _, root := range roots {
		for _, d := range dirs {
			dir := filepath.Join(root, d)
			files, err := transcripts(dir)
			if err != nil {
				return nil, err
			}
			sort.Strings(files)
			rep.Files += len(files)

			fallback := ""
			cwds := make([]string, len(files))
			for i, path := range files {
				cwds[i] = findCwd(path)
				if fallback == "" {
					fallback = cwds[i]
				}
			}
			if fallback == "" {
				fallback = unmangle(d)
			}
			for i, path := range files {
				cwd := cwds[i]
				if cwd == "" {
					cwd = fallback
				}
				if err := analyzeFile(path, cwd, rep, seen, recentSkill); err != nil {
					return nil, err
				}
			}
		}
	}
	return rep, nil
}

// transcripts lists every transcript under dir. Session transcripts sit at the
// top level; subagent transcripts live in <session>/subagents/ (nested one
// level deeper again for workflow agents), and their spend appears nowhere
// else, so the walk has to be recursive.
func transcripts(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable subtree: skip, not fatal
		}
		name := strings.TrimSuffix(d.Name(), gzExt) // archived transcripts
		if d.IsDir() || !strings.HasSuffix(name, ".jsonl") {
			return nil
		}
		if name == journalFile {
			return nil // workflow bookkeeping, no API calls
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

const journalFile = "journal.jsonl"

// agentType reads the agent type recorded beside a subagent transcript.
// Returns "" for session transcripts and for subagents with no metadata.
func agentType(path string) string {
	path = strings.TrimSuffix(path, gzExt)
	if !strings.HasPrefix(filepath.Base(path), "agent-") {
		return ""
	}
	data, err := os.ReadFile(strings.TrimSuffix(path, ".jsonl") + ".meta.json")
	if err != nil {
		return ""
	}
	var meta struct {
		AgentType string `json:"agentType"`
	}
	if json.Unmarshal(data, &meta) != nil {
		return ""
	}
	return meta.AgentType
}

const unknownAgent = "(unknown agent)"

func analyzeFile(path, cwd string, rep *Report, seen map[string]bool, recentSkill map[string]string) error {
	f, err := openTranscript(path)
	if err != nil {
		return nil // unreadable transcript: skip, not fatal
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4*1024*1024), 16*1024*1024)
	agent := agentType(path)

	for scanner.Scan() {
		var e entry
		if json.Unmarshal(scanner.Bytes(), &e) != nil {
			continue
		}

		key := e.RequestID
		if key == "" {
			key = e.UUID
		}
		if key != "" {
			if seen[key] {
				continue
			}
			seen[key] = true
		}

		sid := e.SessionID
		if sid == "" {
			sid = filepath.Base(path)
		}

		for _, block := range e.Message.Content {
			if block.Type != "tool_use" {
				continue
			}
			rep.Tools[block.Name]++
			if block.Name == "Skill" || block.Name == "SlashCommand" {
				switch {
				case block.Input.Skill != "":
					recentSkill[sid] = block.Input.Skill
				case block.Input.Command != "":
					recentSkill[sid] = block.Input.Command
				default:
					recentSkill[sid] = block.Name
				}
			}
		}

		if e.Message.Usage == nil {
			continue
		}
		model := NormalizeModel(e.Message.Model)
		if model == "" {
			if e.Message.Model != "" {
				rep.Unpriced[e.Message.Model]++
			}
			continue
		}

		u := e.Message.Usage
		row := Stats{
			Cost:         costMicros(&e, model),
			Calls:        1,
			In:           u.InputTokens,
			Out:          u.OutputTokens,
			CacheRead:    u.CacheReadTokens,
			CacheWrite:   u.CacheCreation.Ephemeral1h + u.CacheCreation.Ephemeral5m,
			CacheWrite1h: u.CacheCreation.Ephemeral1h,
			CacheWrite5m: u.CacheCreation.Ephemeral5m,
		}

		day := "unknown"
		if len(e.Timestamp) >= 10 {
			day = e.Timestamp[:10]
		}
		skill := e.AttributionSkill
		if skill == "" {
			skill = e.AttributionPlugin
		}
		if skill == "" {
			if s, ok := recentSkill[sid]; ok {
				skill = s
			} else {
				skill = NoSkill
			}
		}
		lane := "main"
		if e.IsSidechain || agent != "" {
			lane = "subagent"
			kind := agent
			if kind == "" {
				kind = unknownAgent
			}
			bump(rep.Agents, kind, row)
		}

		bump(rep.Days, day, row)
		bump(rep.Models, model, row)
		bump(rep.Skills, skill, row)
		bump(rep.Sessions, sid, row)
		bump(rep.Lanes, lane, row)
		bumpCwd(rep.Cwds, cwd, day, skill, row)
		bump(rep.DayModels, day+Sep+model, row)
		bump(rep.DaySkills, day+Sep+skill, row)
		if rep.DaySess[day] == nil {
			rep.DaySess[day] = map[string]bool{}
		}
		rep.DaySess[day][sid] = true
		rep.Total.add(row)
	}
	return nil
}

// Row is a single sorted breakdown entry.
type Row struct {
	Key string
	Stats
}

// Rank returns a breakdown sorted by descending cost.
func Rank(m map[string]*Stats) []Row {
	rows := make([]Row, 0, len(m))
	for k, v := range m {
		rows = append(rows, Row{Key: k, Stats: *v})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Cost != rows[j].Cost {
			return rows[i].Cost > rows[j].Cost
		}
		return rows[i].Key < rows[j].Key
	})
	return rows
}

// Window returns the first and last day the report covers, as YYYY-MM-DD.
//
// It is the min and max of Report.Days, which is bumped for every priced call,
// so no second pass is needed. An empty report returns "", "" — callers should
// print nothing rather than an empty range. Days with no timestamp are recorded
// as "unknown" and are not a date; they never bound the window.
func Window(rep *Report) (first, last string) {
	for day := range rep.Days {
		if len(day) != 10 {
			continue
		}
		if first == "" || day < first {
			first = day
		}
		if day > last {
			last = day
		}
	}
	return first, last
}

// RankCounts returns a name/count map sorted by descending count.
func RankCounts(m map[string]int) []Row {
	rows := make([]Row, 0, len(m))
	for k, v := range m {
		rows = append(rows, Row{Key: k, Stats: Stats{Calls: v}})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Calls != rows[j].Calls {
			return rows[i].Calls > rows[j].Calls
		}
		return rows[i].Key < rows[j].Key
	})
	return rows
}
