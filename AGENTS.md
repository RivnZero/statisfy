# Statisfy development guide

This guide applies to the entire repository. It describes the repository as it
actually is — read the code before trusting any summary here. Statisfy is a
cross-platform Go CLI that observes the usage, quotas, limits, sessions,
tokens, costs, plans, providers, and models exposed by the AI coding tools
already configured on a machine. It is an observer, never a controller.

## Product philosophy

> No fake data. No unavailable tools. No empty metrics.

- Normal output shows only integrations that are actually available.
- Unknown values are omitted — never rendered as placeholders.
- Plans, tiers, limits, and multipliers are never guessed.
- Unavailable integrations belong in `statisfy doctor` and `statisfy --all`,
  not in the default dashboard.

## Working agreements

- Be concise and candid. Clearly distinguish verified behavior from
  assumptions.
- Inspect the actual repository before changing it; do not trust chat
  summaries that conflict with the code.
- Finish authorized work end to end, including verification.
- Keep changes focused. Preserve unrelated user work.
- Avoid speculative abstractions. Generalize only when multiple real
  integrations prove the abstraction is useful.
- Never claim a test or build passed unless it was actually executed.
- Use authoritative, current sources when upstream interfaces may have
  changed (Codex app-server protocol, Freebuff API, Claude transcripts).
- Do not push, tag, publish a release, delete remote resources, or perform
  destructive remote actions unless explicitly authorized. Finishing an
  implementation is never automatic permission to release.

## Product boundary

Statisfy is an **observer** of AI coding tools. Do not introduce, by
accident or by convenience:

- automatic provider routing or agent switching
- quota-driven execution (`statisfy recommend`, `statisfy exec`)
- a dynamic plugin runtime
- a universal quota score

Quota systems are not inherently comparable (weekly percent, rolling 5h
limits, session counts, tokens, credits, cost, daily requests). Do not
normalize them into a fake single score.

## Default display contract

Normal `statisfy` output shows available tools and real detected fields
only: no placeholders, no missing metrics, no fake plan/tier. Diagnostics
(`statisfy doctor`, `statisfy --all`) account for every integration. If
`Detect` succeeds but `Fetch` fails, the integration must still appear in
`--all --json` (with its sanitized error under `unavailable`) — never be
silently dropped.

## Observational safety boundary

This is the most important rule in the codebase. Every adapter must be
observational: running `statisfy` must not change the inspected tool's
behavior. `Detect()` and `Fetch()` must not:

- start, terminate, signal, or pause the inspected tool
- attach to its TTY or steal stdin/stdout
- modify account state or authentication (no login/logout, no credential
  refresh or rotation)
- create, finish, or claim sessions
- modify configuration or write into the inspected tool's local state
- mutate server-side session state merely to fetch status

Context cancellation may terminate only processes Statisfy itself spawned
(see `internal/exec/`). It must never terminate an already-running inspected
process.

## Retry rule

Never retry a request that could be state-changing unless idempotency is
proven. A read-looking endpoint is not automatically safe — investigate its
behavior. If the only way to retrieve a metric risks disrupting the user's
active tool, omit the metric. Less information is better than breaking a
user's session.

## Freebuff precedent

Freebuff's session endpoint has a history of interfering with an active
Freebuff workflow. Therefore: do not call that endpoint while a Freebuff
instance is running; make at most one safe request when allowed; never retry
it; a failure returns less/no data instead of affecting Freebuff. Keep
Freebuff-specific networking behavior in the Freebuff adapter/transport —
never in generic core code.

## Secret handling

Credentials must never appear in terminal output, JSON, logs, errors, cache
files, fixtures, release notes, or committed debug files. When an adapter
does not need a credential-bearing field, omit it from the struct entirely
(the parser then can never render or serialize it). Fixtures use synthetic
values only — never real emails, account IDs, API keys, tokens, session IDs,
or personal filesystem paths. If an adapter must read a credential, read only
what is necessary and keep the exposure surface minimal.

## Architecture

```
cmd/statisfy/      CLI entry point, command dispatch, flags, watch lifecycle
internal/core/     normalized domain model, Adapter contract, orchestration,
                   availability/failure isolation
internal/adapters/ integration-specific detection/parsing/fetching, source
                   semantics, fixtures/tests, registry (adapters compiled in;
                   see internal/adapters/registry.go)
internal/cache/    independent per-adapter file cache
internal/config/   optional TOML configuration
internal/detect/   PATH/filesystem/process detection helpers
internal/exec/     command runner + stdio JSON-RPC client (codex app-server)
internal/render/   terminal dashboard, doctor, sources, JSON
```

## Core boundary

The core knows nothing about provider-specific quirks. Codex JSON-RPC
transport, Freebuff HTTP behavior, Claude transcript parsing, Copilot/OpenCode
SQLite schemas, and Cline local-state shape all belong in adapter- or
transport-scoped code. Do not create a generic abstraction because one adapter
needs it.

## Adapter contract

An adapter implements `core.Adapter` (`ID`, `Name`, `Detect`, `Fetch`) and is
registered in `internal/adapters/registry.go`. `Detect` must be cheap — no
process spawning, no network. `Fetch` honors `context.Context` and returns a
normalized `core.Status` with `Source`/`Stability` metadata. A normal new
integration must not require changes to core or renderers; if it does,
investigate whether the integration is being modeled incorrectly first.

## New adapter workflow

```
research
→ identify the real data source
→ classify provenance/stability
→ verify observational safety
→ create sanitized fixtures
→ parser tests
→ implement adapter
→ register
→ diagnostics/provenance
→ full regression
→ README/support matrix update
```

Do not write the parser before understanding the real source.

## Data source priority

Prefer, in order: documented official API → official structured CLI/API →
stable local structured state → documented local database/state → internal /
community-observed interface → human-formatted output (last resort). Never
convert documented theoretical subscription limits into detected user quota:
documentation saying a plan includes X requests does not mean Statisfy has
observed that account's remaining quota.

## Provenance

Every metric retains source metadata (`official-api`, `official-cli`,
`local-state`, `local-database`, `internal-api`, `derived`) and a stability
label (`documented`, `local`, `internal`, `experimental`). `statisfy sources`
surfaces provenance; the default dashboard stays clean.

## Tool / Provider / Model

These are distinct concepts (Aider → OpenRouter → Claude model). Do not force
provider/model fields onto integrations that cannot detect them. OpenRouter
is a provider integration, not a coding agent.

## Double-counting

Do not add tool-observed activity, provider-wide billing, and account credits
together unless their semantics prove they are independent. The same activity
observed through a client and its provider is not automatically two costs.

## Integration invariants

- **Codex** — structured `codex app-server` JSON-RPC (internal protocol, may
  change). 5x/20x are Pro variants: never render `Plus · 5x` or `Plus · 20x`.
  A multiplier may appear only when the upstream payload explicitly reports it
  AND the plan is Pro; `Pro` alone stays plain `Pro`. Never
  `if plan == "pro" { multiplier = 20 }`.
- **Claude** — plan/account from `~/.claude.json` (`oauthAccount`) and local
  usage from `~/.claude/projects/**/*.jsonl` transcripts are separate sources
  and degrade independently. Transcript-derived tokens/sessions/models are
  local activity, never official Anthropic quota: do not turn them into
  fabricated `5h 70% left` / `weekly 40% left`. The transcript format is
  internal/undocumented — on schema drift, omit or fail safely.
- **OpenRouter** — provider-level; never expose `OPENROUTER_API_KEY`.
- **Cline / Aider** — local/community-observed formats, treated as
  experimental. Aider records no usage unless its analytics log is enabled;
  never invent Aider subscription quota concepts.
- **Droid** — reports only safely detectable local configuration/model; do
  not query the semi-documented backend for usage/credits without explicit
  approval.

## Config

Configuration is optional. Precedence: CLI flags → user config → built-in
defaults. A missing config file means silent defaults; an existing malformed
config prints an actionable diagnostic and falls back to defaults for that
run — never silently pretend it was valid.

## Cache

Each adapter is cached independently (own key, own optional TTL). One expired
adapter must not invalidate unrelated entries. Requirements: global default
TTL, per-adapter TTL overrides, `--refresh` bypass, corruption-safe (corrupt
entries are dropped as misses), cache failure non-fatal, no credentials in
cache.

## Watch mode

`statisfy watch` reuses the normal registry/fetch/normalization/cache/
rendering path — no second adapter execution architecture. Lifecycle: periodic
refresh, manual refresh (`r`), clean quit (`q` / Ctrl-C), safe EOF, terminal
restoration on every exit path, bounded/cancellable adapter work, and no
Statisfy-owned goroutine leaks. Do not rely on process destruction as
goroutine cleanup.

## Testing

Baseline before every push/PR:

```bash
gofmt -l .        # must produce no output
go vet ./...
go test ./... -count=1
go build ./...
```

Never claim these passed unless they were actually run.

## No live accounts by default

The normal test suite must not need live AI accounts. Prefer sanitized
fixtures, `t.TempDir()`, temporary HOME/config roots (adapter overrides),
mock HTTP servers, fake JSON-RPC, temporary SQLite databases, and synthetic
credential-shaped values. Do not read the developer's real Claude history,
Codex account, Freebuff session, OpenRouter key, or Copilot DB during unit or
parser tests. Live compatibility testing must be explicit and opt-in.

## Fixture requirements

Adapter parser fixtures should cover, as relevant: valid input, missing
fields, additional future fields, malformed input, empty state, unsupported
shape, safe fake credential fields, and schema drift. Fixtures must not
contain real user data.

## Regression rule

Existing behavior is the regression baseline. If a change breaks an old test:
inspect why, determine whether the behavior genuinely needs to change, and
preserve existing behavior when possible. Document intentional contract
changes explicitly. Do not edit tests merely to make a regression pass.

## Cross-platform

Linux, macOS, and Windows are all real targets. Use platform-aware home/
config/cache paths, filesystem behavior, executable lookup, terminal
handling, and process behavior. Do not assume Unix-only rename semantics,
that HOME always exists, POSIX TTY behavior, or that signals behave
identically on Windows.

## Release process

Inspect `.github/workflows/release.yml` before changing or documenting release
behavior. Current flow: verified implementation → explicit release
authorization → commit/push → semantic `v*` tag → GitHub Actions regression →
cross-platform builds → archives → SHA-256 checksums → GitHub Release (notes
attached via `body_path`). Do not tag, push, or release merely because
implementation is finished — remote publishing requires explicit user
authorization. `make release` builds the same target matrix locally.

## Versioning

Use semantic versioning intentionally. A major stable version (e.g. `v1.0.0`)
represents a deliberate compatibility milestone, not a routine bump.

## Repository hygiene

- Preserve unrelated user changes; avoid destructive Git operations; never
  force-push shared branches without explicit authorization.
- Do not commit binaries, caches, logs, temp files, local databases, or
  credentials. Keep `dist/`, `statisfy`, `statisfy.exe` ignored.
- Preserve the MIT `LICENSE` and third-party licenses: module dependencies
  (modernc.org/sqlite and its transitive deps) keep their own notices.
- Keep fixtures synthetic. Keep the README support matrix synchronized with
  actual behavior; never claim unsupported integrations or capabilities.
- `SPEC.md` is a local-only implementation record (gitignored) — do not
  reference it from committed docs.

## Documentation

The README is user-facing documentation, not an internal bug log. Remove stale
limitations once genuinely fixed; keep unresolved limitations honest. Source
stability must stay visible for integrations that depend on internal APIs,
undocumented schemas, or experimental local state.

## Comments

Comments explain *why*, tradeoffs, external quirks, safety invariants, and
non-obvious lifecycle constraints — not what obvious Go code does. For
example: "Never retry this endpoint: Freebuff may treat the request as
session-affecting." is useful; "Call Fetch to fetch the status." is noise.

## Final handoff

At the end of substantial work, report: files changed, behavior changed,
architecture impact, adapter/data-source changes, provenance/stability
changes, tests added/updated, exact commands actually executed,
cross-platform verification if relevant, known limitations, whether any
live-account testing occurred, and any release/push/tag actions performed.
Do not hide failed verification or claim completion beyond the evidence.
