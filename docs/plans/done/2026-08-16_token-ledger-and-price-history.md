# Store tokens as facts and prices as a time series, so cost can be recomputed at any date

## Summary

Today cost is computed once, at scan time, against a single hardcoded price table, and
the token detail needed to redo that arithmetic is partly thrown away. This plan turns
the pipeline into three layers: **tokens are immutable facts**, **prices are a dated
series**, and **cost is derived at query time** as `Σ tokens[date][model] × price_at(date)`.
It then extends the same shape to the OpenAI/Codex corpus so both providers answer the
same question — how much inference did I actually do, valued at the prices in effect then.

Two deadlines force the ordering. Claude Code deletes its own transcripts after ~21 days,
and `claudeme snapshot` is not scheduled anywhere, so the token record froze on 2026-08-04
while the raw source for 08-05 onward is on a rotation clock.

Size: 8 blocks across 4 waves. Touches `internal/usage/usage.go`, `internal/usage/history.go`,
`internal/usage/digest.go`, `internal/usage/digest_merge.go`, `internal/usage/digest_scan.go`,
new `internal/usage/prices.go`, new `internal/usage/tokens.go`, new `internal/usage/codex.go`,
`internal/cmd/usage.go`, `internal/cmd/history.go`, `internal/cmd/heatmap.go`,
new `internal/cmd/cost.go`, new `internal/cmd/snapshot` launchd asset, plus tests
alongside each.

## How user task was understood

> Do two extractions per session — semantic (already done) and meta (tokens and the
> rest). Then record what token prices were each day, so cost over a period is
> `Σ(date_price × tokens)` rather than one flat rate. Anthropic first, OpenAI after.
> I know I'm on a subscription; the point is to know how much inference I did.

Reading, checkable against the code:

- Semantic extraction is done and durable. `claudeme digest` runs daily at 05:00 via
  `com.claudeme.digest`, writing `shared/history/<date>/<project>.json`
  (`internal/usage/digest.go:86-92`). 71 session digests, 2026-07-03 → 2026-08-15.
- Meta extraction **already exists and is already persisted** — in a second, separate
  store I initially missed. `claudeme snapshot` writes `shared/usage-history.json`
  (`internal/usage/history.go:67-69`), holding per-day and per-model `in/out/cacheRead/
  cacheWrite` buckets (`internal/usage/history.go:34-56`). So this plan extends a
  partial implementation rather than starting one.
- The two stores are unrelated: different writers, different schedules, different
  version constants (`digestVersion` `digest.go:16`, `historyVersion` `history.go:29`),
  and confusingly similar names — `DigestRoot()` is `shared/history/` (a directory)
  while `HistoryPath()` is `shared/usage-history.json` (a file).
- Cost is computed at scan time in exactly one place, `costMicros`
  (`internal/usage/usage.go:696-705`), reading the undated `Prices` map
  (`usage.go:29-39`) and three package consts `cache1hMult`/`cache5mMult`/`cacheReadMult`
  (`usage.go:42-46`). `Prices` has only two readers, so the blast radius is small.
- `Stats.CacheWrite` stores the **sum** of 1h and 5m cache-creation tokens
  (`usage.go:879`), but those bill at different multipliers (2.0 vs 1.25). Exact
  recompute from the stored shape is therefore impossible. This is the single most
  important schema defect in the plan.
- `bareAliases` (`usage.go:49-54`) resolves `opus` → `claude-opus-5`. That mapping is
  itself time-dependent — "opus" meant a different model in July — so historical
  correctness requires storing the raw model string, not the resolved one.
  `NormalizeModel` also discards the `-YYYYMMDD` suffix (`dateSuffix`, `usage.go:56`),
  which is the closest thing to a model-version fact the transcripts carry.
- `Pending` (`digest_scan.go:198-217`) gates on *session-id presence* in the digest
  file, not on which fields that record has. Adding a `tokens` field would therefore
  never reach any already-digested session. Token extraction needs its own predicate.
- Codex stores tokens at `payload.info.total_token_usage` on `event_msg` lines with
  `payload.type == "token_count"`, and the value is **cumulative per session**. Summing
  `last_token_usage` double-counts on older CLI versions (verified exactly 2× on
  `~/.codex/sessions/2026/02/25/rollout-2026-02-25T16-50-57-*.jsonl`). The correct read
  is the last non-null `info` in the file.

Assumptions taken without asking — break any that are wrong:

- **Cost is a modelled upper bound, not an invoice.** Valuing subscription usage at
  API list price answers "how much inference", which is the stated goal. It is not what
  an API user would have spent — they would have cached, batched and model-switched
  differently. Every surface must label it as such; `usage.go:4-5` already says so.
- **The per-session digest file is the right home for the token ledger.** It unifies the
  user's two tracks per session and reuses atomic `SaveDigest` (`digest.go:126-139`).
  `usage-history.json` stays as the day-level aggregate.
- **Adding `tokens` is additive, so `digestVersion` stays 1**, per the stated rule at
  `digest.go:14-15`. Presence is detected per-record, mirroring `Digest.Usable()`
  (`digest.go:70-82`).
- **Historical prices are hand-maintained, not scraped.** There is no Anthropic pricing
  API. List prices change perhaps 3-4× a year, so a seed table with `effective_from`
  plus a daily snapshot is enough, and a scraper would be a fragile dependency.
- **Retired OpenAI models still need prices** (o3, gpt-4.1, o4-mini appear in the corpus).
  Where a historical price cannot be sourced, the model is recorded as unpriced rather
  than guessed.

## Open questions

*Q2–Q4 were already answered inline when the plan was written; Q1 stands open by design.*

- [ ] Do we backfill the 18-day semantic gap (2026-07-27 → 08-13)? ~1,400 sessions ×
      ~40s ≈ 16 hours of `codex exec` on gpt-5.6-luna, and it consumes subscription
      quota that may cap mid-run. → Not blocking; token backfill (B0/B3) is cheap,
      deterministic, and independent. Decide after Wave 1.
- [ ] Does `Stats.Cost` stay as a stored cache, or get dropped so cost is always derived?
      → Keep it, renamed to `costAtScan`, because `Rank` (`usage.go:931-943`) and ~11
      display sites read it; derive only in the new cost surface. Revisit if the two
      ever disagree.
- [ ] Should the 455 legacy Codex files (2025/09 → 2026/01, no model, no tokens) be
      counted at all? → No. Record them as a known-unaccountable count so totals are
      honest about their own coverage.
- [ ] Is `cleanupPeriodDays: 3650` in `shared/settings.json` being ignored by Claude
      Code, or overridden per-account? → Observed cutoff is ~21 days regardless. Treat
      retention as hostile; do not rely on fixing it.

## Building blocks

1. **Freeze the at-risk transcripts.** — depends on: none — touches: `ArchiveRoot()`
   contents only, no code. Gzip-copy the transcripts whose days are not yet fully
   captured into the archive root. The ADR `2026-08-14_drop-archive-for-digests.md`
   removed the archive *writer* but deliberately kept `ArchiveRoot()` in `Roots()`, so
   archived `.jsonl.gz` still reads through `openTranscript` (`usage.go:279`) and
   `transcripts` (`usage.go:765-785`) with zero changes. This decouples every later
   block from the rotation deadline and is the only genuinely urgent item.

2. **Schedule `claudeme snapshot`.** — depends on: none — touches:
   `~/Library/LaunchAgents/com.claudeme.snapshot.plist`, `docs/`. The command exists
   (`internal/cmd/history.go:21-98`) and nothing runs it; `usage-history.json` is stamped
   `2026-08-04T14:10:44Z`. Mirror the digest plist, run it now to close the 12-day hole.

3. **Split `cacheWrite` into 1h and 5m.** — depends on: 1 — touches:
   `internal/usage/usage.go` (`Stats:565-581`, row build `873-880`),
   `internal/usage/history.go` (`Bucket:34-41`, `toBucket:72-81`, `addBucket:155-167`),
   `internal/cmd/usage.go`, `internal/cmd/history.go`. Keep the old `cacheWrite` field
   populated as the sum so existing readers and stored days keep working; add
   `cacheWrite1h`/`cacheWrite5m` alongside. Without this, exact historical recompute is
   impossible, because the two halves bill at 2.0× and 1.25×. Days ≤ 2026-07-26 have
   already lost this detail permanently — their transcripts are gone — so those days can
   only ever be recomputed approximately, and must be flagged as such.

4. **Date-versioned price table.** — depends on: none — touches: new
   `internal/usage/prices.go`, `internal/usage/usage.go:29-73,696-705`. Introduce
   `PriceEpoch{From string; Table map[string]Price; Mult Multipliers; Aliases map[string]string}`
   and `PriceAt(date, model) (Price, bool)`. The cache multipliers and `bareAliases` move
   *into* the epoch — both are price policy and both are time-dependent. Keep `Prices` as
   a view onto the newest epoch so existing call sites compile unchanged. Re-signature
   `costMicros(u usageTokens, p Price, m Multipliers) int64`; it stays the only site where
   money arithmetic happens. Seed the table with today's values marked
   `from: "2000-01-01"` so current behaviour is bit-identical, then add real epochs.

5. **Per-session token ledger.** — depends on: 3, 4 — touches: new
   `internal/usage/tokens.go`, `internal/usage/digest.go`, `internal/usage/digest_merge.go`,
   `internal/cmd/digest.go`. Add `SessionTokens(path) (*Tokens, error)` reusing the
   unexported `entry` (`usage.go:665-693`), `openTranscript` and the `requestId`-else-`uuid`
   dedupe rule (`usage.go:828-837`), splitting main-loop from sidechain via `isSidechain`.
   Add `PutTokens(root, *Tokens)` writing a `tokens` field into the existing per-session
   record, with its **own** pending predicate (`HasTokens()`) rather than reusing
   `Pending` — extraction is deterministic and model-free, so it can and should run over
   every session on every pass, including ones whose summary failed and ones digested
   before this field existed. Store the **raw** model string.

6. **Daily price snapshot.** — depends on: 4 — touches: `internal/cmd/snapshot`,
   `shared/prices/<date>.json`. Write the epoch in effect each day. Almost always a
   no-op write; the value is a dated audit record independent of the Go binary, so a
   later correction to the seed table cannot silently rewrite history.

7. **Codex ingestion.** — depends on: 4, 5 — touches: new `internal/usage/codex.go`.
   Parse `~/.codex/sessions/<YYYY>/<MM>/<DD>/rollout-*.jsonl` into the same `Tokens`
   shape. Rules the parser must honour, all verified against the corpus: take the **last
   non-null** `payload.info.total_token_usage` (never sum `last_token_usage`);
   `cached_input_tokens` is a **subset** of `input_tokens`, so billable uncached input is
   the difference; `reasoning_output_tokens` is a subset of `output_tokens`; model comes
   from the nearest preceding `turn_context.payload.model` and is safely session-level
   (only 2 files in 2,509 switch mid-session); `cache_write_input_tokens` is absent in
   older files and defaults to 0. Skip the 455 legacy files and report the count.
   Price epochs gain a provider dimension for the 15 OpenAI model strings.

8. **Query surface.** — depends on: 5, 6, 7 — touches: new `internal/cmd/cost.go`,
   `internal/cmd/history.go`, `internal/cmd/heatmap.go`. `claudeme cost` with
   `--at <date>` (value all tokens at one date's prices), `--actual` (value each day at
   its own prices, the default), `--provider anthropic|openai|all`, `--by model|project|day`.
   Output states the coverage caveat and lists unpriced/unaccountable sessions rather
   than silently dropping them.

Waves: 1,2 → 3,4 → 5,6 → 7 → 8

## Blockers

- **Transcript rotation, ~1 day of headroom.** Oldest surviving transcript mtime is
  2026-07-27T10:48; today is 2026-08-16. Day 07-27 ages out imminently and the whole
  1,421-file window is gone by ~2026-09-03. Block 1 must land before anything else.
- **Days ≤ 2026-07-26 cannot be made exact.** Their transcripts are already deleted and
  `usage-history.json` merged their `cacheWrite`. Approximate-only, permanently.
- **Subscription quota.** Any semantic backfill runs through `codex exec` and
  `summarize.sh` already treats a cap rejection as a distinct failure mode (exit 10).
  Token blocks have no such exposure — they are pure parsing.

## Steps

- [x] 1. Freeze at-risk transcripts into the archive root
- [x] 2. Schedule and run `claudeme snapshot`
- [x] 3. Split `cacheWrite` into 1h/5m through `Stats`, `Bucket`, snapshot and printers
- [x] 4. Date-versioned price table with epoch-scoped multipliers and aliases
- [x] 5. Per-session token ledger with its own pending predicate
- [x] 6. Daily price snapshot
- [x] 7. Codex ingestion
- [x] 8. `claudeme cost` query surface

## Verification

### Automated

- `go test ./...` — all packages pass.
- `TestCostAtDateReproducesCurrentNumbers` — `costMicros` under the newest epoch returns
  the same micros as the pre-change `Prices` map for a fixed fixture. Regression lock so
  Block 4 provably changes nothing on day one.
- `TestCacheWriteSplitSumsToLegacyField` — `cacheWrite1h + cacheWrite5m == cacheWrite`
  for every fixture, mirroring the existing invariant style at `history_test.go:68`.
- `TestSessionTokensDedupesReplayedRequests` — one `requestId` across two files counts once.
- `TestPutTokensPreservesExistingSummary` — `PutDigest` then `PutTokens`; summary
  survives, `version` still 1.
- `TestPutTokensIsIdempotent` — twice, one record, same numbers.
- `TestCodexTakesLastCumulativeNotSum` — fixture with duplicated `token_count` events
  yields the single total, not double.
- `TestCodexCachedInputIsSubset` — billable input is `input - cached`, never their sum.

Fixtures follow house convention: no `testdata/`, `fmt.Sprintf`'d JSONL into `t.TempDir()`,
extending `writeTranscript` (`digest_test.go:157-179`).

### Manual

- `ls ~/.config/claudeme/shared/archive | wc -l` — non-trivial count after Block 1.
- `claudeme snapshot && claudeme history | tail -3` — last row is today, not 2026-08-04.
- `claudeme cost --at 2026-08-01 && claudeme cost --at 2026-01-01` — same tokens, two
  totals; the difference is exactly the price delta.
- `claudeme cost --actual --by model` — totals reconcile with `claudeme history` for any
  day whose prices have not changed since it was scanned.
- `claudeme cost --provider openai --by model` — 15 models, with the legacy-file count
  reported as unaccountable rather than zero.

## Out of scope

- Scraping live pricing from any vendor page. The table is hand-maintained.
- Backfilling the 18-day semantic digest gap — tracked as an open question, decided
  separately, and independent of every token block here.
- Changing `schema.json`, `distill.py`, or the summary shape.
- Fixing Claude Code's own transcript retention. Treated as hostile and worked around.
- Reconciling against real invoices. Nothing here claims to be a bill.

## Amendments

- 2026-08-18 — all 8 blocks shipped in one session. See
  [`architecture/2026-08-18_token-ledger-and-prices.md`](../../architecture/2026-08-18_token-ledger-and-prices.md)
  for the resulting structure and
  [`commands/2026-08-18_token-ledger-ops.md`](../../commands/2026-08-18_token-ledger-ops.md)
  for how to run it.

### Where the corpus contradicted the plan

Measured, not assumed — a read-only sweep of all 2,586 rollouts ran before block 7:

- **Block 1 was already 93% done.** 1,878 of 2,016 live transcripts were already frozen
  by an earlier session. Only 133 were missing and 5 stale. The urgency was real but the
  work was not — and rotation had not yet bitten: 2026-07-27 transcripts were still on
  disk at 22 days.
- **Codex corpus is 2,586 files, not 2,509.**
- **Legacy files are 527, not 455.** The plan's 455 is exactly the true-legacy
  bare-header format (2025/09/02 → 2026/02/01, so also six days past the stated range);
  72 more files carry a modern `session_meta` but emit no `turn_context` and no
  `token_count` at all. A parser keying on format alone would have silently mis-bucketed
  those 72.
- **`last_token_usage` double-counts at 1.9987×, not exactly 2×** — the CLI emits each
  `token_count` event twice on affected versions, but a few events are unpaired. The
  conclusion is unchanged and stronger: never sum it, and totals are strictly monotonic
  in every one of the 2,586 files, so the last value is also the max.
- **`payload.info` can be null** — not mentioned in the plan, and a crash for a parser
  that dereferences it.
- **`cache_write_input_tokens` is a subset of `input_tokens`**, not an addition. The plan
  said only that it defaults to 0 when absent. Billing it separately would have
  double-charged 1,984 events.
- **15 OpenAI model strings, and they are not the ones the plan implied.** The corpus
  holds `gpt-5.3-codex` (413 files) and `gpt-5.1-codex-mini` (392) as the second and
  third most common — neither appears in the plan. Two of the 15 are not billable OpenAI
  models at all: `sonnet` (an Anthropic model reached through Codex, already counted on
  the other side) and `codex-auto-review` (a harness label).

### Deviations from the plan as written

- **The OpenAI price table is `Provisional`.** The plan assumed retired-model prices could
  be sourced; they could not be, for any of the gpt-5.x names. Rather than guess silently,
  the epoch carries a `Provisional` flag and every surface that spends it prints a
  warning. **This needs real numbers before any OpenAI figure means anything.**
- **`Unpriced` added to the epoch**, listing models it knows about and deliberately does
  not price. Without it, `<synthetic>` and `sonnet`-through-Codex are indistinguishable
  from a model the table has simply never seen.
- **`MergeHistory` gained an arm.** Its "unchanged day" fast path would have prevented the
  1h/5m split from ever reaching a day whose money had not moved — i.e. almost every day
  worth backfilling. Now a rescan that gains recomputability overwrites; a rescan that
  reads lower still does not.
- **Block 5's "own pending predicate" was already half-built.** The metrics work earlier
  the same day generalised `Pending` into `pendingBy`; `PendingTokens` is three lines on
  top of it.
- **`--tokens-only` mirrors `--metrics-only`** rather than being folded into the daily
  run only. The daily run does both.
- **`Stats.Cost` kept as-is**, not renamed to `costAtScan` as Q2 proposed. Eleven display
  sites read it, the rename buys nothing, and `claudeme cost` derives independently.

### Verified results

- `go test ./...` green, including the two regression locks the plan specified.
- 2,016/2,016 transcripts frozen (467 MB archive).
- 1,312 token ledgers extracted in 9 s, 0 failures.
- `claudeme snapshot` scheduled (03:00 / 15:00) and run: 41 days on record, 2 added,
  21 updated. **23 days now carry the 1h/5m split** (2026-07-27 → 2026-08-18); the 18
  older days are permanently approximate, exactly as the plan predicted.
- `claudeme cost --by provider`: $5,177.84 Anthropic over 57,710 calls, $134.97 OpenAI
  over 1,709 — the latter against a provisional table.
- `--at 2026-01-01` and `--actual` agree to the cent, which is the invariant that holds
  while only one epoch exists and the test that will catch a second epoch being added
  without updating the day-aware path.
- `cost` reads below `history` ($5,177.84 vs $6,574.95) by exactly the 409 digested
  sessions whose transcripts are already deleted. Reported as coverage, not hidden.

### Follow-on, not in this plan

- **`history.jsonl` ingestion (`digest --prompts-only`).** The plan never looked at
  Claude Code's prompt history. It holds every prompt ever typed with a timestamp,
  project and session id, and is **never rotated** — 4,970 sessions back to 2026-02-10,
  against 1,327 surviving transcripts and 1,665 digests. Backfilling it added **3,442
  sessions that had no record at all** and five months of history no other source
  retains. It carries no model and no tokens, so it cannot extend `claudeme cost`; it
  answers when a session ran and on what. See
  [`architecture/2026-08-18_session-digest.md`](../../architecture/2026-08-18_session-digest.md).

  This also forced `IndexDigests`: project names come from surviving transcripts, so a
  session whose transcript is gone can resolve to a different name than its record was
  filed under. Writing blind produced 3,483 records instead of 3,442 — 41 sessions split
  in two. The index pins each session to where it already lives.

### Left open

- **Q1 (18-day semantic backfill) is untouched**, as the plan directed — independent of
  every token block, and ~16 h of `codex exec`.
- **Real OpenAI list prices.** Until then, treat `claudeme cost --provider openai` as an
  order of magnitude.
- **Historical Anthropic epochs.** The seed dates everything to 2000-01-01, so today's
  prices are applied to all of history. The machinery to fix this is in place; the
  numbers are not.

### Amendment 2026-08-18 — distill.py's token counts are superseded, and were wrong

The metrics backfill (`docs/plans/2026-08-18_persist-distill-metrics.md`) persisted
`distill.py --metrics` verbatim, which includes its own `tokens` block. Once block 5
landed, every record carried **two contradictory token counts**, and the wrong one sat
beside the timings that make the rest of `metrics` worth having.

`distill.py` sums the usage block of every assistant record. It has no dedupe rule, while
`SessionTokens` uses the requestId-else-uuid rule `usage.go` has always used. Measured on
session `1b8df218` (mitarbeiterportal, 2026-08-17) — 123 assistant records carrying usage,
**44 of them repeat a requestId**:

| | distill.py | ledger | ratio |
| --- | ---: | ---: | ---: |
| output | 67,883 | 38,080 | 1.78× |
| input | 246 | 158 | 1.56× |
| cache read | 12,352,931 | 8,033,269 | 1.54× |
| cache write | 251,836 | 141,982 | 1.77× |

`cache_hit_rate` is a ratio of two inflated numbers and `subagent_output_tokens` is summed
the same way, so all three fields go.

Resolution — `internal/usage/metrics.go`:

- `StripNaiveTokens` drops those three fields from a `--metrics` payload, leaving the
  timings, branches and shape counts untouched. Unknown shapes pass through unchanged.
- It is applied in `PutDigest`, the single write funnel, so no future write can reintroduce
  them regardless of caller.
- `PruneNaiveTokens` sweeps records written before this. It reads and writes only what is
  on disk — no transcript reopened, no model called — and is idempotent. The
  `--metrics-only` pass runs it on the way in and reports the tally.

Verified on the real corpus: **1,312 records across 132 files cleaned in one pass**, second
pass reports nothing, 1,315 records with metrics and 0 still carrying the naive counts.
`1b8df218` keeps `started`, `ended` and `branches: ["feat/DP-3282", "sandbox"]`, and its
only token record is now the ledger's `out: 38,080`.

Note for anyone reading old numbers: **every token figure derived from `metrics.tokens`
before 2026-08-18 is inflated by roughly 1.5–1.8×.** Nothing in the Go tree ever read
those fields — `usage.go` and `cost` have always counted independently — so no stored cost
or usage report is affected.
