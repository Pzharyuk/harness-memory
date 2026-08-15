# harness-memory

Shared Postgres memory for coding agents, with a [Karpathy LLM wiki](https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f) as the compiled view.

Point Claude Code, Grok, Codex, or anything that speaks MCP at one store. A fact one harness saves is recallable by the others.

**Status:** design complete, implementation not started. See the [design spec](docs/superpowers/specs/2026-08-15-harness-memory-design.md).

## What it will be

- `memoryd` — HTTP + MCP server on Postgres (user + project scopes)
- `memory` — CLI (`init`, `token create`, `import`, `inbox`, `lint`)
- One-way projection to Claude/Grok `MEMORY.md` so native session-start still works
- Install via **Homebrew** (full local server + Postgres), **Docker Compose**, or **Helm**
- Attach Claude Code or Grok via the **`harness-memory` plugin** on [`Pzharyuk/ai-claude-plugins`](https://github.com/Pzharyuk/ai-claude-plugins) (same marketplace both harnesses already use)

## License

MIT
