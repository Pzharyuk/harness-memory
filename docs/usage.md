# Usage

See [install.md](install.md) first. This assumes `memoryd` is up and you have
an admin `MEMORY_TOKEN` (from `memory init`) plus per-harness tokens.

## Tokens

| Token | Who | What it can do |
|---|---|---|
| `admin` | you / CLI | mint/revoke tokens, accept/reject inbox |
| `claude`, `grok`, … | that harness only | recall, save, search, wiki, propose — **not** accept |

```sh
export MEMORY_URL=https://memory.onit.systems   # or http://127.0.0.1:8741
export MEMORY_TOKEN='<admin token>'

memory token list
memory token create --harness grok
memory token revoke --id <uuid>
memory status
```

Bootstrap (`POST /v1/admin/bootstrap` / `memory init`) works **once**, while
the tokens table is empty. After that it returns 409.

## CLI

```sh
memory status
memory inbox
memory accept <id>
memory reject <id>
memory lint
memory import claude --path ~/.claude/projects/<encoded>/memory --dry-run
memory import claude --path ~/.claude/projects/<encoded>/memory
memory project --out /tmp/memory-projection
memory mcp                          # stdio proxy → $MEMORY_URL/mcp
```

`memoryd` reads **env** (`MEMORY_DATABASE_URL`, `MEMORY_LISTEN`), not
`config.toml`. The CLI reads `~/.config/harness-memory/config.toml`; env
overrides.

## Plugin / MCP

Tools on `POST {MEMORY_URL}/mcp` with `Authorization: Bearer <harness token>`:

- `recall` — session brief (user + current project)
- `save` — auto-write a fact
- `search` — full-text search
- `ingest_source` — immutable raw source
- `read_page` / `write_page` — wiki
- `lint` — read-only diagnostics
- `inbox_list` / `inbox_propose`

There is **no** `inbox_accept` on MCP. Tell the human to run `memory inbox`.

## How agents should work

From [`skills/harness-memory/SKILL.md`](../skills/harness-memory/SKILL.md):

1. Session start: `recall` with the current repo name as project.
2. `save` small facts and corrections.
3. `ingest_source` then `write_page` for sources; keep `[[wikilinks]]`.
4. File good answers back as pages or memories.
5. Never write secrets (tokens, passwords) into memory bodies.

## Troubleshooting

| Symptom | What to check |
|---|---|
| Connection refused | Local: `brew services start harness-memory`. Cluster: pod/ingress. |
| Cloudflare Access / 403 at the edge | Cluster is church-IP only (`97.120.177.5`). Home is blocked. |
| 401 Unauthorized | Wrong `MEMORY_TOKEN` or `MEMORY_URL`. |
| `memory init` 409 | Admin already exists; use the saved admin token. |
| Plugin empty | `memoryd` down, or plugin still has no `MEMORY_TOKEN`. |

## Spec

Design: [docs/superpowers/specs/2026-08-15-harness-memory-design.md](superpowers/specs/2026-08-15-harness-memory-design.md)
