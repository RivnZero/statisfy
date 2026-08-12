# statisfy

**English** | [فارسی](README.fa.md)

Statisfy gives you one place to check how much usage you have left across the
AI coding tools you actually use — plans, quotas, rate limits, session limits,
tokens, and costs, in one command.

Run it and see only the tools that are actually set up on your machine:

```text
$ statisfy
AI CODING
────────────────────────────────────────────

Codex       Plus
Weekly        ██████████  96% left  ·  reset 5d 14h

Claude      Pro
Today       1.2M tokens
Sessions    5
OpenCode
Today         294.8K tokens
Cost          $0.00
Sessions      4 sessions

Freebuff    Limited
DeepSeek      1 / 6 sessions
MiMo          1 / 6 sessions
GLM           6 sessions
```

No fake data. No unavailable tools. No empty metrics.

Statisfy only shows values it can actually detect. If a tool isn't installed
or configured, it stays out of the way. If a plan, tier, or usage number
can't be determined, it's omitted — never guessed. That's the whole product
philosophy, and it's also why `statisfy doctor` exists: it's the one place
that explains *why* something isn't showing up.

## Why

If you use more than one AI coding tool, you probably check a few different
CLIs or dashboards to see where you stand. Statisfy puts that in one place
and tells you where each number came from (`statisfy sources`), so you can
trust what you're seeing.

## Installation

```bash
go install statisfy/cmd/statisfy@latest   # or: go build ./cmd/statisfy
```

## Commands

| Command | Description |
|---|---|
| `statisfy` | Dashboard of available integrations |
| `statisfy <tool>` | Detail for one tool (`codex`, `claude`, `opencode`, `freebuff`, `gemini`, `copilot`, `qwen`, `openrouter`, `cline`, `aider`, `droid`) |
| `statisfy --json` | Stable machine-readable normalized JSON (no ANSI, no secrets) |
| `statisfy --refresh` | Ignore the cache and fetch fresh data |
| `statisfy --all` | Include integrations that are not currently available |
| `statisfy doctor` | Diagnose installation/configuration/authentication problems |
| `statisfy sources` | Show where each detected value comes from (provenance + stability) |
| `statisfy watch` | Live refreshing dashboard (`q` quits, `r` refreshes) |
| `statisfy version` | Print version metadata |

## Supported integrations

| Tool | Detection | Data shown | Source | Stability |
|---|---|---|---|---|
| Codex | `codex` on PATH + `~/.codex/auth.json` | Plan, rate-limit windows (5h/weekly/daily), percent used, reset time, multiplier when the API explicitly reports one (Pro only) | `codex app-server` (official API) | internal |
| Claude | `claude` on PATH or `~/.claude` state | Plan (Free/Pro/Max/Business/Enterprise), account, today's tokens/sessions/models from local transcripts, usage windows when a credentials file is present | local state (account + transcripts) + official API | local |
| Gemini CLI | `gemini` on PATH + active account in `~/.gemini/google_accounts.json` | Active account/plan when the local account state exposes it | local state | local |
| Copilot CLI | `copilot` on PATH + `~/.copilot/data.db` | Session/token usage (nano-AIU) from the local SQLite store | local SQLite | local |
| Qwen Code | `qwen` on PATH + `~/.qwen/settings.json` | Provider/Model relationship from settings | local state | local |
| OpenRouter | `OPENROUTER_API_KEY` env var | Account usage, monthly/daily usage, credit limit, limit remaining | openrouter.ai `/api/v1/key` (official API) | documented |
| Cline | VS Code-family `globalStorage/saoudrizwan.claude-dev` state | Today's tasks, tokens, cost from `state/taskHistory.json` | local state | experimental |
| Aider | `aider` on PATH + `~/.aider/analytics.jsonl` | Today's requests, tokens, cost, model from the analytics log | local state (opt-in log) | experimental |
| Droid | `droid` on PATH + `~/.factory/settings.json` | Configured model (usage is never queried from the backend) | local state | documented |
| OpenCode | `opencode.db` in `~/.local/share/opencode` | Today's sessions, tokens, cost | local SQLite | local |
| Freebuff | credentials in `~/.config/manicode/credentials.json` | Per-model session limits, GLM promo, message quotas (only when no Freebuff instance is running) | codebuff.com API | internal |

### Support matrix

Only capabilities actually implemented and fixture-tested are marked.

| Tool | Plan | Tier/Multiplier | Usage | Reset | Sessions | Tokens | Cost | Provider/Model | Source stability |
|---|---|---|---|---|---|---|---|---|---|
| Codex | ✓ | – | ✓ | ✓ | – | – | – | – | internal |
| Claude | ✓ | – | ✓ | ✓ | ✓ | ✓ | – | ✓ | local |
| Gemini CLI | ✓* | – | – | – | – | – | – | – | local |
| Copilot CLI | ✓* | – | ✓ | – | – | ✓ | – | – | local |
| Qwen Code | – | – | – | – | – | – | – | ✓ | local |
| OpenRouter | – | – | ✓ | ✓ | – | – | ✓ | – | documented |
| Cline | – | – | – | – | ✓ | ✓ | ✓ | – | experimental |
| Aider | – | – | – | – | ✓ | ✓ | ✓ | ✓ | experimental |
| Droid | – | – | – | – | – | – | – | ✓ (model) | documented |
| OpenCode | – | – | – | – | ✓ | ✓ | ✓ | – | local |
| Freebuff | ✓ | – | ✓ | – | ✓ | – | – | ✓ | internal |

\* shown only when the local account state actually reports it.

### What is never shown

- **Codex multipliers** — 5x and 20x are Pro tier variants. statisfy shows
  one only when the Codex API explicitly reports it on a Pro plan, and never
  attaches one to Plus or any other plan.
- Credentials, tokens, or authorization headers in any output.

## Security model

- **Observational by design**: statisfy never changes the runtime behavior of
  the tools it inspects. It reads their state read-only, never starts/kills
  their processes, never touches their authentication, and never retries
  requests that could be state-changing.
- statisfy **reads** existing CLI state with the smallest possible surface and
  never modifies other CLIs' credentials.
- Codex: statisfy talks to `codex app-server`, which authenticates itself using
  the user's own `~/.codex/auth.json`. statisfy never reads that token.
- Freebuff: the auth token is read from the manicode credentials file and sent
  only in the `Authorization` header of the session endpoint. **While a
  Freebuff instance is running, statisfy does not call the session endpoint at
  all** (it shows only locally-known state), so it can never interfere with an
  active session.
- Nothing is logged, printed, or serialized unless it is already public.

## JSON mode

```bash
statisfy --json
```

```json
{
  "version": 1,
  "generated_at": "2026-08-12T21:35:41+03:30",
  "tools": [
    {
      "id": "codex",
      "name": "Codex",
      "available": true,
      "installed": true,
      "authenticated": true,
      "plan": "Plus",
      "source": "official-api",
      "limits": [
        {
          "kind": "weekly",
          "label": "Weekly",
          "percent_used": 4,
          "unit": "percent",
          "reset_at": "2026-08-18T11:36:14+03:30",
          "source": "official-api"
        }
      ]
    }
  ]
}
```

Unknown values are omitted (`used`/`total` only appear when actually detected).
With `--all`, unavailable integrations appear under `unavailable` with a
`reason`.

## Configuration (optional)

statisfy works with no config file at all. To override defaults, create
`config.toml` in the platform config directory (or pass `--config PATH`):

- Linux: `~/.config/statisfy/config.toml`
- macOS: `~/Library/Application Support/statisfy/config.toml`
- Windows: `%APPDATA%\statisfy\config.toml`

```toml
cache_ttl = "5m"
timeout = "8s"
watch_interval = "10s"

[adapters.freebuff]
enabled = false

[adapters.gemini]
cache_ttl = "2m"
```

Precedence: **CLI flags > config > built-in defaults** (`--refresh` always
bypasses the cache). Adapters can be disabled individually and `cache_ttl` can
be overridden per adapter.

- A **missing** config file is not an error.
- A config file that **exists but is invalid** (bad TOML, bad duration) is
  never silently accepted: statisfy prints an actionable diagnostic naming the
  file to stderr and falls back to built-in defaults for that run.
- Unknown fields are tolerated and ignored deliberately.

## Caching

Each adapter is cached independently for a short TTL (60s by default). Use
`--refresh` to bypass. Corrupt or stale cache files are ignored safely and
never break normal operation.

## doctor

`statisfy doctor` shows every integration — installed/authenticated/configured
state plus the reason when something is missing. Diagnostic output only.

## Architecture

```
cmd/statisfy/          CLI entry point, flags, dispatch, watch mode
internal/core/         domain model, Adapter contract, registry, orchestration
internal/adapters/     codex, claude, gemini, copilot, qwen, openrouter,
                       cline, aider, droid, opencode, freebuff (+ fixtures/tests)
internal/cache/        per-adapter file cache (TTL, corrupt-safe)
internal/config/       optional TOML config (platform-aware)
internal/detect/       PATH and filesystem helpers, process liveness probe
internal/exec/         command runner + JSON-RPC stdio client (codex app-server)
internal/render/       terminal dashboard, doctor view, sources view, JSON renderer
```

Adding a new integration = implement `core.Adapter`, register it, add
fixtures/tests. The core and renderers never change.

## Known limitations / stability

- **Codex** (`codex app-server` JSON-RPC) and **Freebuff** (`codebuff.com`
  session endpoint) rely on internal interfaces that can change without
  notice. When they change, statisfy degrades gracefully (tool disappears or
  shows only local state) rather than crashing.
- Freebuff's backend can be slow/flaky from some networks. The session request
  is made at most once per run and is **never retried** (a retry could repeat a
  side effect); a failed request simply shows no Freebuff data that run.
- Freebuff live session metrics are only fetched when no Freebuff instance is
  running; while one is active, statisfy shows presence only (see Security
  model). This is deliberate: safety over data completeness.
- **Cline** and **Aider** read internal/local formats (`taskHistory.json`, the
  analytics log) that can shift across tool versions; statisfy then degrades
  gracefully (missing/unparseable state → tool unavailable → hidden).
- **Claude** transcripts under `~/.claude/projects` are an internal,
  undocumented format; if the schema shifts, statisfy stops seeing usage
  rather than showing wrong numbers. Official usage windows are only shown
  when an OAuth token file exists on disk — on Windows that token lives in
  the OS credential vault, so windows are omitted there, but the transcript
  metrics work on every platform.
- **Aider** records no usage by default — only with `aider --analytics-log
  ~/.aider/analytics.jsonl` does it persist the JSONL log statisfy reads.
- **Droid** usage/credits live behind the semi-documented `api.factory.ai`
  backend; statisfy never queries it with the user's live token, so only the
  locally configured model is reported.
- OpenRouter is a provider integration: it reads the key from the
  `OPENROUTER_API_KEY` environment variable (never from files, never printed),
  shows account-level usage/limits, and is hidden when no key is set.

## Development

```bash
go build ./... && go vet ./... && go test ./...
```

Tests cover parsing, normalization, rendering, availability filtering, cache
behavior, and every adapter parser using sanitized fixtures — no live accounts
needed. See [CONTRIBUTING.md](CONTRIBUTING.md) for the adapter contract.

## License

[MIT](LICENSE) — © 2026 RivnZero
