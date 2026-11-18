# Security policy

## Supported versions

Security fixes are provided for the latest released minor version.

## Reporting a vulnerability

Please do not open a public issue for security problems. Report them privately
to the repository maintainer through GitHub's private vulnerability reporting
(Security → Advisories → Report a vulnerability), or by contacting the
maintainer listed on the repository profile.

Include:

- affected version/commit,
- a minimal reproduction,
- the impact (for example, cross-entity memory disclosure or prompt injection),
- any suggested mitigation.

You will receive an acknowledgement as soon as possible. Please allow time for
a fix and coordinated disclosure before publishing details.

## Deployment notes

- MCP HTTP requires a Bearer token by default. Use a long random token, TLS (or
  a TLS-terminating reverse proxy), and a trusted network.
- `-allow-anonymous` is intended only for a trusted loopback interface.
- SQLite database files contain conversation memory in plaintext. Protect file
  permissions, backups and volume snapshots accordingly.
- Memory is untrusted data. Clients should never execute instructions found in
  recalled memory; Memorigo frames it as data and the MCP tool descriptions say
  so explicitly.
- The LLM extraction prompt tells the model not to store secrets, but this is a
  prompt-level guard, not a redaction layer. Redact credentials and regulated
  data before writing them to memory.
- MCP HTTP rejects cross-site browser requests; non-browser MCP clients are
  allowed. A TLS reverse proxy may preserve a public `Host` header.
- Use a dedicated `entity_id` per end user. The MCP server's default entity is
  shared by every client using that process.
- Pin embedding model and dimension; call `Reindex` after changing either.
