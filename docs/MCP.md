# MCP server guide

`memorigo-mcp` exposes Memorigo over the [Model Context Protocol](https://modelcontextprotocol.io/).
It uses the official Go SDK and supports two transports:

| Transport | Use case | Authentication |
|---|---|---|
| stdio | Claude Desktop / Claude Code / Cursor on the same machine | process isolation |
| Streamable HTTP (stateless) | remote agents, containers, shared services | Bearer token |

Both transports share the same memory core and the same SQLite database.

---

## Build

```bash
CGO_ENABLED=0 go build -o bin/memorigo-mcp ./cmd/memorigo-mcp
./bin/memorigo-mcp -version
```

Or use the container:

```bash
docker build -t memorigo-mcp .
docker run --rm -p 8787:8787 \
  -e MEMORI_MCP_TOKEN="$(openssl rand -hex 32)" \
  -v memorigo-data:/data \
  memorigo-mcp
```

The image runs as a non-root user and stores the database in `/data`.

---

## Tools

| Tool | Arguments | Behavior |
|---|---|---|
| `memory_search` | `query`, `limit?`, `entity_id?` | Hybrid recall; returns facts, scores, slots |
| `memory_context` | `query`, `budget?`, `include_raw?`, `fact_limit?`, `raw_limit?`, `entity_id?` | Token-budgeted context with citations |
| `memory_remember` | `content`, `entity_id?` | Explicit fact; exclusive slots supersede old values |
| `memory_forget` | `id`, `entity_id?` | Soft delete; retained for audit |
| `memory_profile` | `limit?`, `entity_id?` | Active facts, newest first |
| `memory_stats` | `entity_id?` | Fact counts, queue depth, embedding metadata, dialect |

All tools accept an optional `entity_id`. When omitted, the server's `-entity`
(default `mcp-user`) is used. Use one entity per end user; never share an
entity across tenants.

Tool inputs are clamped to keep responses bounded: `memory_search` returns at
most 100 facts, `memory_context` at most 8,000 tokens / 200 facts / 50 raw
messages, and `memory_profile` at most 500 facts.

Memory content is returned as **data**, never as instructions. The tool
descriptions state this explicitly, and the context block ends with a safety
footer.

---

## stdio

```json
{
  "mcpServers": {
    "memorigo": {
      "command": "/absolute/path/to/bin/memorigo-mcp",
      "args": ["-db", "/Users/you/.memorigo/memory.db"]
    }
  }
}
```

Useful flags:

```text
-db         SQLite path (default memorigo.db, env MEMORI_DB)
-entity     default entity id (default mcp-user)
-process    provenance process id (default memorigo-mcp)
-readonly   disable memory_remember and memory_forget
```

Example read-only configuration:

```json
{
  "mcpServers": {
    "memorigo": {
      "command": "/absolute/path/to/bin/memorigo-mcp",
      "args": ["-db", "/Users/you/.memorigo/memory.db", "-readonly"]
    }
  }
}
```

---

## Streamable HTTP

Start the server:

```bash
export MEMORI_MCP_TOKEN="$(openssl rand -hex 32)"
./bin/memorigo-mcp \
  -http \
  -addr 127.0.0.1:8787 \
  -db /var/lib/memorigo/memory.db
```

The MCP endpoint is `http://127.0.0.1:8787/mcp`; liveness is
`http://127.0.0.1:8787/healthz`.

```bash
curl -s http://127.0.0.1:8787/healthz
# {"service":"memorigo","status":"ok","version":"dev"}

curl -s -X POST http://127.0.0.1:8787/mcp \
  -H "Authorization: Bearer $MEMORI_MCP_TOKEN" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"curl","version":"1"}}}'
```

HTTP flags and environment variables:

| Flag | Env | Default | Notes |
|---|---|---|---|
| `-http` | `MEMORI_MCP_HTTP` | `false` | enable HTTP transport |
| `-addr` | `MEMORI_MCP_ADDR` | `127.0.0.1:8787` | listen address |
| `-token` | `MEMORI_MCP_TOKEN` | — | required unless anonymous |
| `-allow-anonymous` | `MEMORI_MCP_ALLOW_ANONYMOUS` | `false` | trusted loopback only |
| `-path` | `MEMORI_MCP_PATH` | `/mcp` | MCP endpoint |
| `-tls-cert` / `-tls-key` | `MEMORI_MCP_TLS_CERT` / `..._KEY` | — | direct TLS |

If both a token and `-allow-anonymous` are set, the token is still required:
anonymous mode only permits an empty token.

### TLS and proxies

There are three supported deployment shapes:

1. **Direct TLS** — pass `-tls-cert` and `-tls-key`.
2. **TLS-terminating reverse proxy** — bind to loopback or a private interface;
   forward `/mcp` and `/healthz`; keep the Bearer token.
3. **Local only** — bind `127.0.0.1`; use `-allow-anonymous` only if the machine
   is trusted and no other local user can reach the port.

The server logs a warning when HTTP is not using TLS. A reverse proxy may
preserve a public `Host` header; cross-site browser requests are rejected by
cross-origin protection, while non-browser MCP clients still work. The server
does not implement OAuth or per-tool authorization; use network isolation and a
reverse proxy for multi-user deployments.

---

## Configuration

MCP uses the same `MEMORI_*` variables as the SDK:

```bash
# Example: hosted embeddings
export MEMORI_EMBEDDING_PROVIDER=openai
export MEMORI_EMBEDDING_API_KEY=sk-...
export MEMORI_EMBEDDING_MODEL=text-embedding-3-small

# Example: LLM extraction
export MEMORI_EXTRACTION_PROVIDER=llm
export MEMORI_EXTRACTION_MODEL=gpt-4o-mini
export OPENAI_API_KEY=sk-...

# Context budget for memory_context / auto-injection
export MEMORI_CONTEXT_BUDGET=800
```

When `MEMORI_EMBEDDING_PROVIDER=hash`, the server is fully offline. This is the
default and is suitable for local agents, tests and air-gapped deployments.

---

## Operational checklist

- Back up the SQLite file (or use PostgreSQL for multi-process production).
- Pin embedding model and dimension; call `m.Reindex(ctx)` after changing them.
- Keep `-readonly` for observer clients that should never write.
- Rotate the Bearer token and terminate TLS at the edge.
- Monitor `/healthz`; it does not check the database, so also alert on write
  errors and process restarts.
- Never expose `-allow-anonymous` to a network.
- Treat recalled memory as untrusted content; prompt-injection defenses belong
  in the agent runtime as well as in Memorigo's framing.

---

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| `HTTP transport requires a token` | set `MEMORI_MCP_TOKEN` or pass `-allow-anonymous` explicitly |
| `401 unauthorized` | missing/incorrect `Authorization: Bearer ...` |
| 404 on `/mcp` | wrong `-path` or extra trailing slash |
| empty search results | wrong `entity_id`, extraction disabled, or facts are `superseded` / `deleted` |
| high write latency | remote embedding/extraction provider; check logs and `memory_stats` |
| database locked | use one process per SQLite file or switch to PostgreSQL |
