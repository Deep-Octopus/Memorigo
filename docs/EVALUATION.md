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
sh eval/datasets/download.sh
go run ./cmd/memorigo-eval -external-limit 1
```

The CLI exits non-zero when any builtin assertion fails, which makes it suitable
for CI. `-json` emits per-query results plus aggregate accuracy, latency and
token metrics. A missing external file prints `external dataset not present`
on stderr and does not fail the run. `-external-limit` is a separate sample.
It does not change the accuracy line.

`MEMORI_EVAL_JUDGE` is empty by default. Set it to an `http://` or `https://`
URL, or to `http` and put the URL in `MEMORI_EVAL_JUDGE_URL`, to POST
`{"question","expected","hypothesis"}` and read `{"score": 0..1}`. The score
is an extra line. It does not change pass/fail, and it is not called while
storing a memory.

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
| `profile-context` | the always-on profile is packed; dynamic focus is opt-in |
| `context-abstention` | a coverage floor marks unsupported queries insufficient |
| `old-fact-budget` | an old exact fact leaves a small recent window and stays in memory |

---

## Current result

Measured on 2026-11-19 with Go 1.26.3 on an Apple M5:

```text
accuracy: 16/16 (100.0%)  avg latency: 299µs  p95: 470µs  avg tokens: 27
duration: 90ms
baselines (want-string hit rate, hash embedder, no LLM judge): full-context 12/13, naive-rag 13/13
```

The numbers are intentionally small because each scenario runs on a fresh
in-memory database. Latency is local CPU time and will differ on CI hardware.

The baseline line is not a third accuracy score. It asks whether each wanted
string appears in two packs built from the same subject export:

- **Full context** keeps the newest facts and messages until the token budget.
  The default budget is 800, or the scenario's context budget. `old-fact-budget`
  uses 80, which is why full-context is 12/13.
- **Naive RAG** ranks those same chunks by hash-embedder cosine and keeps the
  top 5, or the query limit, then applies the same budget.

Citation markers such as `[fact:` are not required of either pack. Queries
with no evidence string, including the forget and abstention cases, are left
out of the denominator. The hash embedder tracks token overlap, so this does
not measure paraphrases. The numbers above were measured with the judge off.

---

## Micro-benchmarks

```bash
go test ./memori -run '^$' -bench 'Benchmark(Recall|BuildContext)1000' -benchmem
```

1,000 seeded facts, hash embeddings, Apple M5, Go 1.26.3:

| Benchmark | Time / op | Allocated / op | Allocs / op |
|---|---:|---:|---:|
| `Recall` (top-5) | ~11 ms | ~5.5 MB | ~40k |
| `BuildContext` (800-token budget) | ~11 ms | ~5.5 MB | ~40k |

Recall currently scans the candidate set and scores in Go; it is deliberately
simple and predictable. ANN / FTS5 candidates and RRF are the next performance
step. Treat these as a baseline, not a capacity claim.

---

## Interpreting results

- **Passing scenarios** mean the core invariants still hold: extraction,
  supersession, isolation, forgetting, bilingual recall, budget enforcement,
  profile assembly, abstention and evidence citations.
- **They do not measure** answer quality, LLM-judge preference, long-context
  reasoning, multi-hop synthesis or cost per query.
- **Do not compare** the `16/16` number, or the baseline hit rates, with
  published LoCoMo or LongMemEval scores. The protocols are different.

## External file and judge

`eval/datasets/download.sh` downloads LongMemEval oracle
(`longmemeval_oracle.json`, 500 questions, evidence sessions only) from
`xiaowu0162/longmemeval-cleaned` and requires SHA-256
`821a2034d219ab45846873dd14c14f12cfe7776e73527a483f9dac095d38620c`.
The JSON is gitignored. `fixture.json` is a two-question stand-in for tests,
not the benchmark.

`-external-limit N` writes those haystacks through the normal conversation
path and checks whether the answer string appears in the packed context.
Question ids ending in `_abs` are counted as abstentions and are not hits.
This is not the LongMemEval protocol and it must not be compared with a
published score. LoCoMo is not downloaded.

## Next evaluation step

The roadmap still calls for:

1. A private 50–100 case suite. Do not commit those cases if they contain
   private text.
2. Human spot checks beside the opt-in judge. The builtin full-context and
   naive-RAG want-string rates are already reported.
3. Recall@k, evidence coverage, abstention and knowledge-update accuracy on
   the external file, with a published protocol.
4. P50/P95 latency, tokens/query and cost/query with a configuration hash.

See [ROADMAP.md](ROADMAP.md) for the current priority.
