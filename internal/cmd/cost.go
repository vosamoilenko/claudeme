package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/vosamoilenko/claudeme/internal/usage"
)

// Cost values the token ledgers stored per session against the price table in
// effect on the day they were spent. Nothing here re-reads a transcript: the
// tokens are already facts on disk, so the whole command is a table scan.
//
// It is a modelled upper bound, never an invoice. Subscription usage valued at
// API list price answers "how much inference did I do", which is the question
// worth asking; an API user would have cached, batched and switched models
// differently and spent something else entirely.
func Cost() {
	at := ""
	provider := "all"
	by := "model"
	asJSON := false

	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--at":
			if i+1 >= len(args) || !isDate(args[i+1]) {
				fmt.Fprintln(os.Stderr, "--at wants a YYYY-MM-DD date")
				os.Exit(1)
			}
			at = args[i+1]
			i++
		case "--actual":
			at = "" // the default: every day at its own prices
		case "--provider", "-p":
			if i+1 < len(args) {
				provider = args[i+1]
				i++
			}
		case "--by", "-b":
			if i+1 < len(args) {
				by = args[i+1]
				i++
			}
		case "--json":
			asJSON = true
		default:
			fmt.Fprintf(os.Stderr, "unknown flag %q\n", args[i])
			os.Exit(1)
		}
	}

	switch provider {
	case "all", "anthropic", "openai":
	default:
		fmt.Fprintf(os.Stderr, "unknown --provider %q — want anthropic, openai or all\n", provider)
		os.Exit(1)
	}
	switch by {
	case "model", "project", "day", "provider":
	default:
		fmt.Fprintf(os.Stderr, "unknown --by %q — want model, project, day or provider\n", by)
		os.Exit(1)
	}

	rows, cov := collectCost(provider, at, by)
	if asJSON {
		printCostJSON(rows, cov, at)
		return
	}
	printCost(rows, cov, at, provider, by)
}

// costRow is one line of the table.
type costRow struct {
	Key    string `json:"key"`
	Micros int64  `json:"micros"`
	Calls  int    `json:"calls"`
	In     int    `json:"in"`
	Out    int    `json:"out"`
	Cache  int    `json:"cacheRead"`
}

// costCoverage is what the answer does not include. Printed with every total:
// a number that silently omits part of its corpus is worse than one that says
// what it omitted.
type costCoverage struct {
	Sessions      int            `json:"sessions"`
	NoLedger      int            `json:"withoutLedger"`
	CodexLegacy   int            `json:"codexUnrecoverable"`
	CodexNoTokens int            `json:"codexWithoutUsage"`
	Unpriced      map[string]int `json:"unpriced,omitempty"`
	Provisional   []string       `json:"provisionalProviders,omitempty"`
}

func collectCost(provider, at, by string) ([]costRow, costCoverage) {
	acc := map[string]*costRow{}
	cov := costCoverage{Unpriced: map[string]int{}}

	add := func(key string, t *usage.Tokens, project string) {
		var micros int64
		var unpriced []string
		if at != "" {
			micros, unpriced = t.CostAt(at)
		} else {
			micros, unpriced = t.Cost()
		}
		for _, m := range unpriced {
			cov.Unpriced[m]++
		}
		total := t.Total()

		keys := []string{key}
		if by == "model" {
			keys = modelKeys(t)
		}
		if by == "project" {
			keys = []string{project}
		}
		// With several models in one session the money is attributed to the
		// session's dominant model rather than split: splitting would need a
		// per-model cost, which is exactly what a multi-model session cannot
		// give without re-pricing each lane. One key keeps the total honest.
		for _, k := range keys[:1] {
			r, ok := acc[k]
			if !ok {
				r = &costRow{Key: k}
				acc[k] = r
			}
			r.Micros += micros
			r.Calls += total.Calls
			r.In += total.In
			r.Out += total.Out
			r.Cache += total.CacheRead
		}
	}

	if provider == "all" || provider == "anthropic" {
		for _, d := range everyDigest() {
			cov.Sessions++
			if !d.HasTokens() {
				cov.NoLedger++
				continue
			}
			key := d.Date
			if by == "provider" {
				key = string(usage.Anthropic)
			}
			add(key, d.Tokens, d.Project)
		}
	}

	if provider == "all" || provider == "openai" {
		sessions, ccov, err := usage.ReadCodex(usage.CodexRoot())
		if err == nil {
			cov.CodexLegacy = ccov.Legacy
			cov.CodexNoTokens = ccov.NoTokens
			for _, s := range sessions {
				cov.Sessions++
				key := dayOf(s.Started)
				if by == "provider" {
					key = string(usage.OpenAI)
				}
				add(key, s.Tokens, projectOf(s.Cwd))
			}
		}
		if e, ok := usage.EpochAt(usage.OpenAI, "9999-12-31"); ok && e.Provisional {
			cov.Provisional = append(cov.Provisional, string(usage.OpenAI))
		}
	}

	rows := make([]costRow, 0, len(acc))
	for _, r := range acc {
		rows = append(rows, *r)
	}
	sort.Slice(rows, func(i, j int) bool {
		if by == "day" {
			return rows[i].Key < rows[j].Key
		}
		if rows[i].Micros != rows[j].Micros {
			return rows[i].Micros > rows[j].Micros
		}
		return rows[i].Key < rows[j].Key
	})
	return rows, cov
}

// modelKeys is a session's models, biggest first, so attributing to keys[0]
// picks the model that did most of the work.
func modelKeys(t *usage.Tokens) []string {
	type row struct {
		model string
		size  int
	}
	var rs []row
	for _, m := range append(append([]*usage.ModelTokens{}, t.Models...), t.Sidechain...) {
		rs = append(rs, row{m.Model, m.In + m.Out + m.CacheRead})
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i].size > rs[j].size })
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.model)
	}
	if len(out) == 0 {
		return []string{"(no model)"}
	}
	return out
}

// everyDigest walks the history tree and yields every stored session record.
func everyDigest() []*usage.Digest {
	root := usage.DigestRoot()
	days, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []*usage.Digest
	for _, day := range days {
		if !day.IsDir() {
			continue
		}
		files, err := os.ReadDir(root + "/" + day.Name())
		if err != nil {
			continue
		}
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".json") {
				continue
			}
			df, err := usage.LoadDigest(root + "/" + day.Name() + "/" + f.Name())
			if err != nil {
				continue
			}
			for _, id := range df.Ids() {
				out = append(out, df.Sessions[id])
			}
		}
	}
	return out
}

func dayOf(ts string) string {
	if len(ts) < 10 {
		return "unknown"
	}
	return ts[:10]
}

// projectOf names a Codex session's project from its cwd. A bare home
// directory is not a project.
func projectOf(cwd string) string {
	if cwd == "" || cwd == "/" {
		return "(no project)"
	}
	if home, err := os.UserHomeDir(); err == nil && cwd == home {
		return "(no project)"
	}
	parts := strings.Split(strings.TrimSuffix(cwd, "/"), "/")
	return parts[len(parts)-1]
}

func printCost(rows []costRow, cov costCoverage, at, provider, by string) {
	basis := "each day at its own prices"
	if at != "" {
		basis = "everything at " + at + " prices"
	}
	fmt.Printf("%s %s\n", HeaderStyle.Render("Cost"),
		DimStyle.Render(fmt.Sprintf("%s, by %s, %s", provider, by, basis)))
	fmt.Println()

	var total int64
	var calls int
	for _, r := range rows {
		total += r.Micros
		calls += r.Calls
		fmt.Printf("  %s  %s  %s\n",
			pad(HeaderStyle.Render(money(r.Micros)), 12),
			pad(NameStyle.Render(truncateUTF8(r.Key, 28)), 28),
			DimStyle.Render(fmt.Sprintf("%s calls, %s in, %s out, %s cached",
				comma(r.Calls), comma(r.In), comma(r.Out), comma(r.Cache))))
	}

	fmt.Println()
	fmt.Printf("%s  %s over %s calls\n",
		HeaderStyle.Render("TOTAL"), HeaderStyle.Render(money(total)), comma(calls))
	printCoverage(cov)
}

func printCoverage(cov costCoverage) {
	fmt.Println(DimStyle.Render(fmt.Sprintf("  %d session%s counted", cov.Sessions, plural(cov.Sessions))))
	if cov.NoLedger > 0 {
		// Mostly sessions whose transcript Claude Code has already deleted:
		// the digest outlived its source, and tokens can no longer be
		// counted from it. A run only helps for the ones still on disk.
		fmt.Println(DimStyle.Render(fmt.Sprintf(
			"  %d digested session%s carry no ledger — transcript gone, or not yet extracted (claudeme digest --tokens-only)",
			cov.NoLedger, plural(cov.NoLedger))))
	}
	if cov.CodexNoTokens > 0 {
		fmt.Println(DimStyle.Render(fmt.Sprintf(
			"  %d Codex session%s recorded no usage events", cov.CodexNoTokens, plural(cov.CodexNoTokens))))
	}
	if cov.CodexLegacy > 0 {
		fmt.Println(DimStyle.Render(fmt.Sprintf(
			"  %d Codex session%s predate usage reporting — unrecoverable, not zero",
			cov.CodexLegacy, plural(cov.CodexLegacy))))
	}
	if len(cov.Unpriced) > 0 {
		var names []string
		for m, n := range cov.Unpriced {
			names = append(names, fmt.Sprintf("%s (%d)", m, n))
		}
		sort.Strings(names)
		fmt.Println(WarnStyle.Render("  unpriced, excluded from the total: " + strings.Join(names, ", ")))
	}
	for _, p := range cov.Provisional {
		fmt.Println(WarnStyle.Render("  " + p + " prices are provisional — not checked against an invoice"))
	}
	fmt.Println(DimStyle.Render("  list price on subscription usage: an upper bound, not a bill"))
}

func printCostJSON(rows []costRow, cov costCoverage, at string) {
	out := struct {
		At       string       `json:"at,omitempty"`
		Rows     []costRow    `json:"rows"`
		Coverage costCoverage `json:"coverage"`
	}{At: at, Rows: rows, Coverage: cov}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
