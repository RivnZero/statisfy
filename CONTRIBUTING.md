# Contributing

Thanks for considering a contribution to statisfy. The project is small on
purpose — a handful of packages, no plugin runtime, everything compiled in.
That keeps it fast and reviewable.

## Development setup

Requirements: Go 1.26+ (see `go.mod`).

```bash
git clone https://github.com/RivnZero/statisfy
cd statisfy

go build ./...    # builds everything
go test ./...     # runs the full suite
```

The standard loop before every push or PR:

```bash
gofmt -l .        # must be empty
go vet ./...
go test ./... -count=1
go build ./...
```

The test suite needs no live accounts: every parser is exercised through
sanitized fixtures under `internal/adapters/fixtures/`. If you touch an
adapter, run its tests with `-count=1` and make sure the whole suite still
passes.

## Project layout

```
cmd/statisfy/          CLI entry point, flags, watch mode
internal/core/         domain model, Adapter contract, orchestration
internal/adapters/     one package per integration, plus fixtures and tests
internal/cache/        per-adapter file cache
internal/config/       optional TOML config
internal/detect/       PATH / filesystem / process helpers
internal/exec/         command runner + JSON-RPC stdio client (codex)
internal/render/       terminal dashboard, doctor, sources, JSON
```

## The most important rule

**Adapters are observational.** Fetching status must never change the tool
being inspected — its account, its sessions, its authentication, or its
runtime behavior. Never retry a request that could be state-changing. If an
endpoint's side effects can't be proven safe while the tool is running, don't
call it while the tool is running. Less data is always better than a broken
user session.

## Adding an integration

Every integration is an adapter implementing `core.Adapter` (`ID`, `Name`,
`Detect`, `Fetch`). Before writing code, figure out where the *real* data
comes from — prefer, in order: a documented official API, structured CLI
output, stable local state, a documented local database, and only last,
human-formatted output. Never derive quotas from documentation about
theoretical limits.

An adapter lands with:

1. A sanitized fixture in `internal/adapters/fixtures/` covering valid data,
   missing fields, malformed input, empty state, and (where relevant) a fake
   credential field proving it is never surfaced.
2. Parser tests that use those fixtures — no live accounts.
3. `Detect` with a structured `ReasonKind` when unavailable, and
   `Source`/`Stability` metadata on the Status.
4. Registration in `internal/adapters/registry.go`.
5. A row in the README integration table and support matrix, with an honest
   stability label (`documented`, `local`, `internal`, `experimental`).


## Behavior changes

Existing behavior is a regression baseline. If a change breaks an existing
test, investigate whether the behavior genuinely needs to change — and if it
does, say so explicitly in the PR description. Don't edit tests to make a
regression pass.

## Commits and PRs

- Keep commits focused; the history should read like a story.
- Open a PR against `main`. The CI workflow runs the suite on Linux, macOS,
  and Windows, plus a cross-compile check.
- Fill out the PR template, especially the checklist and the "notes for
  reviewers" section for anything involving internal interfaces or stability
  tradeoffs.
