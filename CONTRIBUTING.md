# Contributing to Memorigo

Thanks for helping make Go agent memory better. This project values small,
reviewable changes, deterministic tests and honest benchmarks.

## Development

Requirements: Go 1.25+.

```bash
make check       # gofmt + vet + build + tests + eval
make race        # race detector
make eval        # deterministic evaluation report
```

Run the examples:

```bash
go run ./examples/sqlite
go run ./examples/postgres     # POSTGRES_DSN required
go run ./examples/mongodb      # MONGODB_URI required
```

## Pull requests

- Keep changes focused; explain the user-visible behavior and the failure mode
  being fixed.
- Add or update tests. New recall/storage behavior should get an acceptance
  test or an `eval` scenario.
- Run `gofmt`, `go vet` and `go test -race ./...` before opening a PR.
- Do not commit API keys, database files, generated coverage or binaries.
- If a change affects recall quality or latency, include a before/after
  measurement and the exact command used.

## Design constraints

- Prefer the existing `storage.Repos` abstraction over provider-specific code in
  the core.
- Do not put LLM calls on the synchronous write path.
- Never treat injected memory as user-authored text.
- Keep the default build pure Go (`CGO_ENABLED=0`).
- Do not add a dependency unless it removes more complexity than it introduces.

## Commit messages

Use a short imperative subject. Conventional prefixes (`feat:`, `fix:`,
`docs:`, `test:`) are welcome but not required.

## Security

Please report vulnerabilities privately as described in [SECURITY.md](SECURITY.md).
