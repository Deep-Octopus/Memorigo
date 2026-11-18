# Evaluation

Memorigo ships a deterministic regression harness that runs without API keys,
network access or downloaded datasets. It is designed to catch behavioral
regressions in the memory loop; it is **not** a substitute for external quality
benchmarks such as LoCoMo or LongMemEval.

---

## Run

```bash
go run ./cmd/memorigo-eval
go run ./cmd/memorigo-eval -json
go run ./cmd/memorigo-eval -filter context
make eval
```

The CLI exits non-zero when any assertion fails, which makes it suitable for CI.
`-json` emits per-query results plus aggregate accuracy, latency and token
metrics.

---

## Environment

- In-memory SQLite (`modernc.org/sqlite`, pure Go).
- Offline feature-hash embeddings (256 dimensions).
- Heuristic extraction unless the scenario disables it.
- No LLM, no network, no external services.

Because of the offline embedder, results measure behavioral correctness and
local overhead, not semantic quality of a production embedding model.

---

## Scenarios

| Scenario | What it proves |
|---|---|
| `basic-preference` | a stated preference is extracted and recalled |
| `preference-update` | a new exclusive-slot value supersedes the old one |
| `relocation` | a new home city replaces `profile.location` |
| `session-continuity` | facts survive `NewSession()` for the same entity |
| `forget` | soft-deleted facts are excluded from recall |
| `noise-resistance` | unrelated frequent facts do not outrank the relevant one |
| `chinese-recall` | Chinese extraction and recall work end to end |
| `entity-isolation` | switching attribution never leaks another entity's facts |
| `token-budget` | context fits the budget and emits `fact:` citations |
| `raw-evidence` | with extraction off, raw messages fill the evidence channel |
| `cross-session-evidence` | raw messages are retrievable across sessions |

---

## Current result

Measured on 2026-11-18 with Go 1.26.3 on an Apple M5:

```text
accuracy: 11/11 (100.0%)  avg latency: 134µs  p95: 215µs  avg tokens: 17
duration: 49ms
```

The numbers are intentionally small because each scenario runs on a fresh
in-memory database. Latency is local CPU time and will differ on CI hardware.

---

## Micro-benchmarks

```bash
go test ./memori -run '^$' -bench 'Benchmark(Recall|BuildContext)1000' -benchmem
```

1,000 seeded facts, hash embeddings, Apple M5, Go 1.26.3:

| Benchmark | Time / op | Allocated / op | Allocs / op |
|---|---:|---:|---:|
| `Recall` (top-5) | ~11 ms | ~5.5 MB | ~40k |
| `BuildContext` (800-token budget) | ~10 ms | ~5.5 MB | ~40k |

Recall currently scans the candidate set and scores in Go; it is deliberately
simple and predictable. ANN / FTS5 candidates and RRF are the next performance
step. Treat these as a baseline, not a capacity claim.

---

## Interpreting results

- **Passing scenarios** mean the core invariants still hold: extraction,
  supersession, isolation, forgetting, bilingual recall, budget enforcement and
  evidence citations.
- **They do not measure** answer quality, LLM-judge preference, long-context
  reasoning, multi-hop synthesis or cost per query.
- **Do not compare** the `11/11` number with published LoCoMo or LongMemEval
  scores. The protocols are different.

## Next evaluation step

The roadmap calls for:

1. LongMemEval dev + a private 50–100 case suite.
2. Full-context and naive-RAG baselines under the same token budget.
3. LLM-judge plus human spot checks.
4. Recall@k, evidence coverage, abstention and knowledge-update accuracy.
5. P50/P95 latency, tokens/query and cost/query with a configuration hash.

See [ROADMAP.md](ROADMAP.md) for the current priority.
