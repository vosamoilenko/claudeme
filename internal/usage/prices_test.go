package usage

import "testing"

// The regression lock the plan asks for: routing cost through the epoch table
// must return exactly what the old flat `Prices` map plus package-level
// multipliers returned. Numbers below are the pre-change constants, written
// out literally so a future edit to the seed epoch has to break this test on
// purpose.
func TestCostAtDateReproducesCurrentNumbers(t *testing.T) {
	const (
		oldOpusIn, oldOpusOut = 5.0, 25.0
		old1h, old5m, oldRead = 2.0, 1.25, 0.1
	)
	u := usageTokens{In: 12_000, Out: 3_400, CacheRead: 980_000, CacheWrite1h: 55_000, CacheWrite5m: 210_000}

	want := int64((float64(u.In)*oldOpusIn +
		float64(u.CacheWrite1h)*oldOpusIn*old1h +
		float64(u.CacheWrite5m)*oldOpusIn*old5m +
		float64(u.CacheRead)*oldOpusIn*oldRead +
		float64(u.Out)*oldOpusOut) / 1e6 * 1e6)

	for _, date := range []string{"2026-08-18", "2026-01-01", "2000-01-01"} {
		p, ok := PriceAt(Anthropic, date, "claude-opus-5")
		if !ok {
			t.Fatalf("%s: claude-opus-5 has no price", date)
		}
		if got := cost(u, p, MultAt(Anthropic, date)); got != want {
			t.Fatalf("%s: cost = %d micros, want %d", date, got, want)
		}
	}
}

func TestPriceAtResolvesAliasesAndSuffixes(t *testing.T) {
	opus, _ := PriceAt(Anthropic, "2026-08-18", "claude-opus-5")
	cases := []string{"opus", "claude-opus-5", "claude-opus-5[1m]", "claude-opus-5-20260101"}
	for _, m := range cases {
		got, ok := PriceAt(Anthropic, "2026-08-18", m)
		if !ok || got != opus {
			t.Fatalf("PriceAt(%q) = %v, %v — want %v", m, got, ok, opus)
		}
	}

	// Unpriced and free are different facts.
	if _, ok := PriceAt(Anthropic, "2026-08-18", "some-local-llama"); ok {
		t.Fatal("an unknown model must report no price, not a zero one")
	}
	if _, ok := PriceAt(Anthropic, "2026-08-18", ""); ok {
		t.Fatal("an empty model must report no price")
	}
}

// Providers are priced from separate tables: an OpenAI model must not resolve
// against the Anthropic epoch, and vice versa.
func TestPriceAtIsProviderScoped(t *testing.T) {
	if _, ok := PriceAt(Anthropic, "2026-08-18", "gpt-5.6-luna"); ok {
		t.Fatal("an OpenAI model must not price against the Anthropic table")
	}
	if _, ok := PriceAt(OpenAI, "2026-08-18", "claude-opus-5"); ok {
		t.Fatal("an Anthropic model must not price against the OpenAI table")
	}
	if _, ok := PriceAt(OpenAI, "2026-08-18", "gpt-5.6-luna"); !ok {
		t.Fatal("gpt-5.6-luna is the digest model and must be priced")
	}
	if _, ok := PriceAt("nobody", "2026-08-18", "x"); ok {
		t.Fatal("an unknown provider must report no price")
	}
}

// A date before the oldest epoch must still resolve: the seed is dated
// 2000-01-01 precisely so nothing in the corpus falls off the front.
func TestEpochAtCoversTheWholeCorpus(t *testing.T) {
	for _, date := range []string{"2025-09-01", "2026-02-25", "2026-08-18"} {
		for _, p := range []Provider{Anthropic, OpenAI} {
			e, ok := EpochAt(p, date)
			if !ok {
				t.Fatalf("%s/%s: no epoch", p, date)
			}
			if e.From > date {
				t.Fatalf("%s/%s: epoch %s starts after the date it priced", p, date, e.From)
			}
		}
	}
	if got := len(Epochs()); got < 2 {
		t.Fatalf("want at least one epoch per provider, got %d", got)
	}
}

// NormalizeModel is the scan-time path and must keep meaning "today".
func TestNormalizeModelStillResolvesToday(t *testing.T) {
	cases := map[string]string{
		"opus":                    "claude-opus-5",
		"claude-sonnet-5[1m]":     "claude-sonnet-5",
		"claude-haiku-4-5-202510": "", // not a -YYYYMMDD suffix, so no match
		"gpt-5.6-luna":            "",
		"":                        "",
	}
	for in, want := range cases {
		if got := NormalizeModel(in); got != want {
			t.Fatalf("NormalizeModel(%q) = %q, want %q", in, got, want)
		}
	}
}

// The daily record is an audit trail, so an unchanged day must not churn its
// timestamp — otherwise the day a price actually moved is invisible.
func TestRecordPricesIsANoOpWhenNothingChanged(t *testing.T) {
	root := t.TempDir()

	wrote, err := RecordPrices(root, "2026-08-18", "2026-08-18T05:00:00Z")
	if err != nil || !wrote {
		t.Fatalf("first write: %v, %v", wrote, err)
	}
	wrote, err = RecordPrices(root, "2026-08-18", "2026-08-18T17:00:00Z")
	if err != nil || wrote {
		t.Fatalf("second write should be a no-op: %v, %v", wrote, err)
	}

	rec, ok, err := LoadPriceRecord(root, "2026-08-18")
	if err != nil || !ok {
		t.Fatalf("load: %v, %v", ok, err)
	}
	if rec.Written != "2026-08-18T05:00:00Z" {
		t.Fatalf("timestamp churned on a no-op: %q", rec.Written)
	}
	if len(rec.Epochs) != 2 {
		t.Fatalf("want one epoch per provider, got %d", len(rec.Epochs))
	}
	// The record must be self-contained: a reader with no Go binary can price
	// a day from it alone.
	for _, e := range rec.Epochs {
		if len(e.Table) == 0 || e.Mult.CacheRead == 0 {
			t.Fatalf("epoch %s/%s is not self-contained: %+v", e.Provider, e.From, e)
		}
	}

	if _, ok, err := LoadPriceRecord(root, "2026-08-17"); ok || err != nil {
		t.Fatalf("an unrecorded date must report absent, got %v / %v", ok, err)
	}
}
