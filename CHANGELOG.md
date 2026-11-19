# Changelog

All notable changes to Memorigo are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- **Builtin retrieval baselines**: `memorigo-eval` now reports a want-string hit rate for a recent-window full-context pack and a top-k hash-cosine naive RAG pack. Citation markup is not required of either baseline. The suite has 16 scenarios. `old-fact-budget` is the case where an 80-token recent window drops an older exact fact that memory and naive top-k still return. There is no LLM judge and no LoCoMo or LongMemEval data in the repo.
- **MCP tool scopes, quota and audit**: `-scopes` / `MEMORI_MCP_SCOPES` is a static allow-list (`read`, `write`, `all`, or tool names). Empty allows every tool. Tools outside the list are not advertised. `-quota` / `MEMORI_MCP_QUOTA` caps tool calls per minute in this process (0 is unlimited, max 100000). `-audit` / `MEMORI_MCP_AUDIT_LOG` appends JSONL with tool, entity, outcome and duration. The log does not contain the query or the memory text. A log write failure does not undo the memory operation. This is not OAuth and the quota is not shared across processes.
- **JSON HTTP API** (`cmd/memorigo-api`, package `api`): `/healthz` plus `/v1/recall`, `/v1/remember`, `/v1/forget`, `/v1/restore`, `/v1/context`, `/v1/facts`, `/v1/profile`, `/v1/stats`, `/v1/export`, `/v1/maintain` and `DELETE /v1/subject`. Bearer auth is required unless `-allow-anonymous` is set. `-readonly` blocks writes and still allows a maintain dry run. Calls are serialized per process because attribution is instance-wide.
- **Subject export, deletion and opt-in PII redaction**: `Export` writes a version-1 JSON document of every fact (any status) and message for the current entity. `ExportOptions.Redact` masks the copy only. `DeleteSubject` hard-deletes that entity's facts, messages, sessions and knowledge-graph edges, then drops graph vocabulary that nothing else references. A second delete is a no-op. The agent process row stays. `MEMORI_PII_REDACT=true` masks emails, phone numbers and Luhn-valid card numbers before they are stored. This is not cryptographic shredding and it does not claim to find every personal identifier.
- **Local embeddings and optional rerank**: `MEMORI_EMBEDDING_PROVIDER=ollama` calls a running Ollama server at `POST /api/embed` (default model `nomic-embed-text`, default URL `http://127.0.0.1:11434`). A configured dimension that does not match the response is rejected. `MEMORI_RERANK=http` sends the top `MEMORI_RERANK_TOP_N` facts (default 20) to a Jina/Cohere-compatible rerank endpoint. The default remains `off`. A rerank failure keeps the hybrid order. This does not download models and does not use ONNX or cgo.
- **Reversible forgetting**: `Maintain` expires active facts that have not been seen within a caller-supplied TTL. Facts mentioned at least `ProtectTimes` times are kept. `DryRun` lists the same ids without writing. `Restore` reopens an expired fact. `Forget`, slot supersession, and a non-positive TTL are unchanged. Automatic extraction does not resurrect an expired fact; an explicit `Remember` of the same content does. There is no background summarizer.
- **Temporal validity**: facts record `valid_from`, `valid_to`, `source_message_id`, `confidence` and `asserted_by`. Superseding or forgetting a fact closes its validity window instead of erasing provenance. Facts asserted by the assistant or by inference stay out of recall and the profile until `MEMORI_RECALL_INCLUDE_INFERRED=true`. `RememberWith` sets that provenance explicitly.
- **Tenant scope**: `SetScope(org, app)` (or `MEMORI_ORG_ID` / `MEMORI_APP_ID`) isolates memory. The same entity id in two organizations cannot read each other's facts. Empty scope keeps the previous single-tenant key.
- **Lexical candidate index**: facts store the same tokens the ranker uses. SQLite queries them with FTS5, PostgreSQL with a GIN `tsvector`, and MongoDB with a multikey token index. Recall unions those hits with the recent scan window, so an exact name or Chinese phrase is still found after it falls outside `MEMORI_RECALL_SCAN_LIMIT`. Dense search stays a bounded in-process scan; `modernc.org/sqlite` cannot load `vec0`.
- **Reciprocal rank fusion** (`MEMORI_RECALL_FUSION=rrf`): an optional alternative to linear hybrid weights. A fact that is best on one signal (typically an exact name or identifier) is no longer buried by a mediocre dense score. `MEMORI_RECALL_FUSION=weighted` restores the previous ranking. Scores stay in `[0, 1]`, so `MinScore` means the same thing in both modes.
- **Always-on profile** (`Memori.Profile`): a small, query-independent
  `<memory_profile>` built from active facts and grouped into Identity,
  Preferences, Health, Skills and optional dynamic sections. Static facts are
  on by default; recent focus/opinion facts require `IncludeDynamic` and are
  limited to 90 days.
- **Context Engine v2** (`BuildContext`): three channels (profile, ranked facts,
  raw evidence) share one token budget. Adds MMR diversity selection over a
  4× candidate pool, cross-channel deduplication, coverage/abstention metadata
  (`Coverage`, `TopScore`, `InsufficientEvidence`, `MinCoverage`), a raw-evidence
  budget reserve, and lost-in-the-middle render ordering.
- **Profile-aware auto-injection**: `{{profile}}` is now part of the default
  memory prompt; older custom templates that omit it still receive the profile
  before `{{facts}}`.
- **MCP**: `memory_context` includes the profile by default and exposes
  profile/MMR/raw-reserve/coverage controls; `memory_profile` returns grouped
  sections plus the rendered profile text.
- **Evaluation**: two new deterministic scenarios (`profile-context`,
  `context-abstention`), now 15 total; `ContextCheck` can assert profile content,
  dynamic-profile gating and abstention.
- Configuration: `MEMORI_PROFILE_BUDGET` (200), `MEMORI_PROFILE_LIMIT` (40) and
  `MEMORI_PROFILE_DYNAMIC` (false).
- Research note `docs/MEMORY-LANDSCAPE-2026-11.md`, including the evidence for
  profile-first and context-v2 priorities and the deferred fact-metadata work.

### Changed

- `DefaultMemoryPrompt` now contains both `{{profile}}` and `{{facts}}`.
- `BuildContext` defaults to MMR (`MMRLambda=0.7`) and edge-weighted fact
  ordering; both can be disabled for the previous pure-score behavior.
- Profile facts are included in `ContextPackage.Citations`.

## [1.0.0] - 2026-11-18

The first production-oriented release. Memorigo is now an end-to-end memory
layer rather than a collection of storage helpers.

### Added

- **Token-budgeted context assembly** (`BuildContext`): facts and raw
  conversation evidence share one budget; stable `fact:<id>` / `msg:<id>`
  citations; cross-session evidence for the same entity.
- **Automatic injection uses the context engine**, so injected tokens are
  bounded by `MEMORI_CONTEXT_BUDGET` (default 800).
- **Deterministic evaluation harness** (`eval/`, `memorigo-eval`): 11
  scenarios covering recall, supersession, relocation, session continuity,
  forgetting, noise, Chinese recall, entity isolation, token budgets, raw
  evidence and cross-session evidence.
- **MCP server** (`mcpserver/`, `memorigo-mcp`) with six tools:
  `memory_search`, `memory_context`, `memory_remember`, `memory_forget`,
  `memory_profile`, `memory_stats`.
- **MCP transports**: stdio for local clients and stateless Streamable HTTP
  with Bearer authentication and `/healthz`.
- **Release automation**: cross-platform archives, checksums and GitHub
  releases on `v*` tags; a multi-stage Docker image for the HTTP server.
- **Benchmarks** for recall and context assembly over 1,000 facts.
- `-version` output and link-time build metadata.

### Changed

- Module path is `github.com/Deep-Octopus/Memorigo`.
- Go 1.25+ is required by the official MCP Go SDK; the default build remains
  pure Go (`CGO_ENABLED=0`).
- Recall BM25 now tracks only query-term frequencies and uses a bounded fact-token cache, reducing allocations on repeated queries.
- Writes are idempotent across complete-history replays; response messages are
  not duplicated in conversation history.
- Empty queries skip embedding work and return recent facts.

### Fixed

- Streaming OpenAI-compatible responses persist exactly once and no longer
  truncate when the upstream error channel closes early.
- Attribution errors are surfaced through `Err()` / `TryAttribution` instead of
  panicking.
- Semantic merge only applies within the same slot.
- Embedding model/dimension changes are handled through `Reindex` without
  mixing vector spaces.
- Replaying a conversation no longer enqueues a no-op augmentation job or
  inflates `num_times`; automatic extraction can no longer resurrect a
  superseded or forgotten fact. Explicit `Remember` still reactivates.
- Only the unseen suffix of a growing chat history is sent to extraction, so a
  fact stated once is not re-counted on every turn.
- Embedding dimension detection is race-free under concurrent `EmbedText` /
  `EmbedTexts` calls.
- Dedup keys preserve one-character values, so facts such as
  "favorite number is 7" and "favorite number is 8" remain distinct.
- Fact reconciliation is serialized per entity, preventing interleaved
  exclusive-slot writes from leaving zero or multiple active values.
- Reindex converges when an endpoint returns a different dimension than the
  embedder initially reported.
- Writer transactions no longer populate the process-wide id cache before
  commit, so a rolled-back transaction cannot poison later writes; SQL
  get-or-create uses `ON CONFLICT DO NOTHING` to keep PostgreSQL transactions
  usable after a concurrent unique race.
- Lazy OpenAI-compatible client access is synchronized.
- Streamable HTTP accepts a TLS reverse proxy's public Host header while
  retaining cross-origin browser protection.

### Security

- MCP HTTP is token-protected by default; anonymous mode must be explicit.
- MCP tool inputs are bounded (search 100 facts, context 8,000 tokens / 200
  facts / 50 raw messages, profile 500 facts) to keep responses predictable.
- MCP responses mark memory text as untrusted data, not instructions.
- Memory blocks are always framed as data. The LLM extraction prompt tells the
  model not to store secrets, but this is a prompt-level guard, not a
  redaction layer; redact regulated data upstream.
