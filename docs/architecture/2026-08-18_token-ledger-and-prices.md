# Token Ledger And Price History

Cost used to be computed once, at scan time, against one undated price table, and the
token detail needed to redo that arithmetic was partly thrown away. It is now three
layers:

| Layer | Where it lives | Property |
| --- | --- | --- |
| **Tokens are facts** | `tokens` on each session record in `history/<date>/<project>.json`; day aggregates in `usage-history.json` | counted, never valued |
| **Prices are a dated series** | `internal/usage/prices.go`, plus a daily copy in `shared/prices/<date>.json` | hand-maintained, versioned by `effective_from` |
| **Cost is derived** | `claudeme cost` | `Σ tokens[day][model] × price_at(day)` |

Correcting a price therefore re-values history instead of contradicting a stored number.

## Price epochs

`PriceEpoch{From, Provider, Table, Mult, Aliases, Unpriced, Provisional}` is the whole
pricing policy in effect from one date. The cache multipliers and the bare-name aliases
live *inside* it, because both are price policy and both are time-dependent — "opus"
resolved to a different model in July than it does now.

`PriceAt(provider, date, model)` is the only way to get a price; `cost()` in `prices.go`
is the only place money arithmetic happens. Both are provider-scoped, so an OpenAI model
can never resolve against the Anthropic table.

Seeded with one epoch per provider dated `2000-01-01`, so every date in the corpus
resolves and the change was bit-identical on day one. `TestCostAtDateReproducesCurrentNumbers`
locks that: it writes out the pre-change constants literally, so a future edit to the seed
has to break the test on purpose. Real historical epochs get appended as they are sourced.

Two honesty markers on an epoch:

- `Unpriced` — models the epoch knows about and deliberately refuses to price.
  `<synthetic>` (Claude Code's placeholder for a turn nobody billed), `sonnet` reached
  *through* Codex (already counted on the Anthropic side), `codex-auto-review` (a harness
  label). Distinct from a model never seen: one is a decision, the other a gap.
- `Provisional` — the table has not been checked against an invoice. **The OpenAI table
  is provisional.** Every surface that spends it says so.

## Cache writes bill at two rates

`Stats.CacheWrite` stored the *sum* of 1h and 5m cache-creation tokens, which bill at
2.0× and 1.25× of the input price. Exact recompute from that shape is impossible, so
`CacheWrite1h`/`CacheWrite5m` are carried alongside it — the sum is kept because every
display site wants one number.

`Bucket.Recomputable()` is how a stored day says which it is. An unsplit day with
non-zero cache writes predates the split; an absent pair means **unknown, not zero**.

Days ≤ 2026-07-26 are approximate permanently — their transcripts were already deleted
when the split shipped. Verified 2026-08-18: 23 of 41 recorded days carry the split
(2026-07-27 → 2026-08-18), 18 never can.

`MergeHistory` has an arm for this: a rescan whose money is unchanged still overwrites a
stored day when the fresh one is recomputable and the stored one is not. Without it the
split would only ever reach days that also changed. It never lets a *lower* rescan
through — that is retention, not news.

## The per-session ledger

`SessionTokens(path)` parses one transcript into `Tokens`: per-model rows split into
main-loop and sidechain lanes, plus a per-day view so a session crossing midnight is
valued at each day's prices. It dedupes by `requestId` (falling back to `uuid`), because
resuming a session replays earlier turns into the new transcript.

**The raw model string is stored, never the resolved one.** Resolving at write time would
bake today's answer into a fact about the past; the alias is resolved at read time by the
epoch that was in effect.

`PendingTokens` is its own predicate, alongside `Pending` (no summary) and
`PendingMetrics` (no metrics) — all three through one `pendingBy` helper. Extraction is
deterministic and model-free, so a session whose summary failed, or which was digested
before the field existed, is still a candidate.

## Codex ingestion

`internal/usage/codex.go` reduces `~/.codex/sessions/**/rollout-*.jsonl` to the same
`Tokens` shape. Every rule was measured against all 2,586 files, not assumed:

- `payload.info.total_token_usage` is **cumulative per session** and strictly monotonic
  in every file. Summing `last_token_usage` double-counts on the CLI versions that emit
  each event twice (measured 1.9987× on a 2026-02-25 session, ~54 files affected).
  **Take the last non-null cumulative total.**
- `payload.info` can be `null`.
- `cached_input_tokens` ⊂ `input_tokens`, proven by `total == input + output` on
  9,303/9,303 events. Billable uncached input is the difference.
- `reasoning_output_tokens` ⊂ `output_tokens`. Do not add it.
- `cache_write_input_tokens` is optional (1,984/9,303) and also inside `input_tokens`, so
  it is recorded and never billed twice.
- Model is the nearest preceding `turn_context.payload.model`, always resolvable. Only 2
  files switch model mid-session; since totals are cumulative, the session is attributed
  to the model in effect at its end.
- Session start is `session_meta.payload.timestamp` (UTC) — **not** the envelope
  timestamp (lags) and **not** the filename (local time).
- cwd: `turn_context.payload.cwd`, else `session_meta.payload.cwd`, else the
  `<cwd>` in the legacy `<environment_context>` block (present in 527/527 legacy files).

Coverage as measured 2026-08-18: 2,586 files → 1,709 with a ledger, 350 with a model but
no usage events, **527 legacy** with neither (the plan said 455; that is the true-legacy
format subset, and 72 more modern-format files emit no `turn_context` or `token_count`).

## What the numbers are

**List price applied to subscription usage: an upper bound, never a bill.** It answers
"how much inference did I do", which is the question worth asking. An API user would have
cached, batched and switched models differently.

`claudeme cost` therefore prints its own coverage with every total — sessions with no
ledger, Codex sessions that predate usage reporting, and deliberately unpriced models are
reported rather than counted as zero.

`cost` totals will read *lower* than `claudeme history` for the same range. That is
correct: `history` recorded days while their transcripts existed, and the ledger can only
exist for sessions whose transcripts survive. Measured 2026-08-18: history $6,574.95 over
78,461 calls, cost $5,177.84 over 57,710 calls, the gap being 409 digested sessions whose
transcripts Claude Code has since deleted.

## Amendments

- 2026-08-18 — created with the 8 blocks of
  [`plans/done/2026-08-16_token-ledger-and-price-history.md`](../plans/done/2026-08-16_token-ledger-and-price-history.md).
