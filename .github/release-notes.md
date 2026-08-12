## What's in v0.0.1

The first public release of Statisfy.

Statisfy gives you one place to check how much usage you have left across the
AI coding tools already configured on your machine — quotas, sessions, tokens,
costs, and reset windows, in one command.

Statisfy only shows data it can actually detect. If a tool isn't installed or
configured, it stays out of the way. Missing metrics are omitted, never
invented.

### Supported integrations

- Codex
- Claude
- OpenCode
- Freebuff
- Gemini CLI
- GitHub Copilot CLI
- Qwen Code
- OpenRouter (provider)
- Cline
- Aider
- Factory Droid

### Also included

- `--json` machine-readable output
- Per-adapter caching with `--refresh` to bypass
- `statisfy watch` live dashboard
- `statisfy sources` provenance reporting
- `statisfy doctor` diagnostics
- Optional TOML configuration
- Cross-platform builds (macOS, Linux, Windows)

Some integrations depend on internal or community-observed interfaces
(Codex, Freebuff, Cline, Aider). Those may need adjustments as upstream tools
change — Statisfy degrades gracefully rather than guessing.

## Installing

```bash
go install github.com/RivnZero/statisfy/cmd/statisfy@latest
```

Or grab a binary for your platform from the release assets below.
