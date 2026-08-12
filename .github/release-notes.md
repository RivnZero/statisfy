### Changed

- Claude can now read usage straight from Claude Code's local transcripts
  (`~/.claude/projects/**/*.jsonl`): today's tokens, sessions, and the model
  used — no account token required, and it works on Windows too.
- Claude plan and local usage are collected independently, so partial data
  (plan only, or usage only) is shown instead of hiding the whole tool.
- Corrected Codex tier handling: 5x and 20x are Pro variants. They are only
  shown when the Codex API explicitly reports them on a Pro plan, and they can
  never appear next to Plus.
- Removed the outdated "no Claude usage on Windows" limitation from the README
  and refreshed the support/source matrix.
- Added sanitized fixture coverage for Claude transcripts and Codex
  plan/tier combinations.

The test suite runs entirely on fixtures and temp directories — no live
account queries are involved.
