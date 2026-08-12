# Security

Statisfy is built to *observe* your AI coding tools, not to touch them. The
single most important rule in the codebase is that running statisfy must never
change the runtime behavior of a tool it inspects — no sessions claimed, no
authentication touched, no processes signaled, no state mutated.

## Reporting a vulnerability

Please report security issues privately via
[GitHub Private Vulnerability Reporting](https://github.com/RivnZero/statisfy/security)
instead of opening a public issue.

When reporting, include:

- the statisfy version (`statisfy version`) and operating system
- the affected integration(s)
- a minimal reproduction, if you have one

Do not paste live credentials or tokens into a report. If you believe a
credential may have leaked, say so without including the value.

I aim to acknowledge reports within a few days and to fix confirmed issues in
a timely release.

## Security properties

These are the behaviors statisfy is designed around, and every change is
expected to preserve them:

- **Observational and read-only.** Fetching status must not start, stop,
  signal, or modify an inspected tool, its sessions, or its authentication.
  Requests that could be state-changing are never retried.
- **Credentials stay where they are.** statisfy never prints, logs, caches,
  or serializes tokens, API keys, or authorization headers. Adapters that must
  read a credential do so with the smallest possible surface and only when the
  integration genuinely requires it.
- **Minimum local state.** When local state is inspected (Claude, OpenCode,
  Copilot, Cline, Aider, Qwen, Droid), only the fields needed for status are
  read — credential-bearing fields are excluded at the struct level.
- **No fabricated data.** Values statisfy cannot detect are omitted, never
  guessed.

## Known risk surface

Some integrations depend on internal or community-observed interfaces
(Codex, Freebuff, Cline, Aider). These can change without notice; statisfy is
designed to degrade gracefully when they do. The
[README](https://github.com/RivnZero/statisfy#supported-integrations) labels
each integration's source stability.
