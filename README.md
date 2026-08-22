# harness-memory

Shared Postgres memory for coding agents, with a [Karpathy LLM wiki](https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f) as the compiled view.

Point Claude Code, Grok, Codex, or anything that speaks MCP at one store. A fact one harness saves is recallable by the others.

**Read these first**

- [Install](docs/install.md) — Homebrew, Kubernetes, plugin
- [Usage](docs/usage.md) — tokens, CLI, MCP, troubleshooting
- [Design spec](docs/superpowers/specs/2026-08-15-harness-memory-design.md)

## Quick start (Homebrew)

```sh
brew tap Pzharyuk/tools
brew install harness-memory
brew services start postgresql@16
createdb memory
brew services start harness-memory
memory init
memory token create --harness claude
```

For the **cluster** brain instead (church IP only):

```sh
brew tap Pzharyuk/tools
brew install harness-memory
export MEMORY_URL=https://memory.onit.systems
memory init
memory token create --harness grok
```

Do not start local Postgres/`harness-memory` if you are using the cluster.

## What it is

- `memoryd` — HTTP + MCP server on Postgres (user + project scopes). Default listen `127.0.0.1:8741`. Reads **env** (`MEMORY_DATABASE_URL`, `MEMORY_LISTEN`), not `config.toml`.
- `memory` — CLI (`init`, `token create`, `import`, `inbox`, `lint`, `project`, `mcp`). Reads `~/.config/harness-memory/config.toml`; env overrides (`MEMORY_URL`, `MEMORY_TOKEN`, `MEMORY_DATABASE_URL`, …).
- MCP at `POST /mcp` with `Authorization: Bearer <token>`.
- One-way projection to Claude/Grok `MEMORY.md` so native session-start still works.
- Librarian skill: [`skills/harness-memory/SKILL.md`](skills/harness-memory/SKILL.md).

## Install

Full steps: [docs/install.md](docs/install.md).

### Homebrew

See the [quick start](#quick-start-homebrew). Formula: [`Formula/harness-memory.rb`](Formula/harness-memory.rb), published on [`Pzharyuk/tools`](https://github.com/Pzharyuk/homebrew-tools).

### Docker Compose

Compose publishes Postgres `55432:5432` (user / password / database `memory`):

```
docker compose -f deploy/compose/docker-compose.yml up -d
export MEMORY_DATABASE_URL=postgres://memory:memory@127.0.0.1:55432/memory?sslmode=disable
make build
MEMORY_DATABASE_URL="$MEMORY_DATABASE_URL" ./bin/memoryd
```

Or `docker build -t harness-memory .` and run the image with the same env. Binding `0.0.0.0` / `:8741` is opt-in (used in-cluster).

### Kubernetes (shared / central brain)

This is the path for one Postgres that every harness talks to. The chart can run **Postgres + `memoryd` in the same release**, or only `memoryd` against an existing DSN.

```
# Secret with key database-url (and POSTGRES_PASSWORD if postgres.enabled)
kubectl create secret generic harness-memory \
  --from-literal=database-url='postgres://harness:PASSWORD@postgres:5432/harness?sslmode=disable' \
  --from-literal=POSTGRES_PASSWORD='PASSWORD'

helm install harness-memory deploy/chart \
  --set postgres.enabled=true \
  --set postgres.storageClass=your-storage-class \
  --set image.tag=latest
```

`memoryd` applies schema migrations on boot. Point Claude/Grok at the Service (or ingress) with `MEMORY_URL` and a per-harness `MEMORY_TOKEN`. Do **not** also run Homebrew Postgres if this cluster DB is the source of truth.

Image: `ghcr.io/pzharyuk/harness-memory` (built on push to `main`). In-cluster listen is `:8741` (all interfaces).

### Helm (bring-your-own Postgres)

```
helm install harness-memory deploy/chart
```

Create a Secret named `harness-memory` with key `database-url` (Postgres DSN) first.

### Plugin (Claude / Grok)

The marketplace plugin lives in [`Pzharyuk/ai-claude-plugins`](https://github.com/Pzharyuk/ai-claude-plugins):

```
/plugin marketplace add Pzharyuk/ai-claude-plugins
/plugin install harness-memory@ai-claude-plugins
```

Grok:

```
grok plugin marketplace add Pzharyuk/ai-claude-plugins
grok plugin install harness-memory --trust
```

The plugin is a thin HTTP MCP client. **`memoryd` is still required.** Set `MEMORY_TOKEN` (and `MEMORY_URL` if not `http://127.0.0.1:8741`).

For a cluster `memoryd`, set `MEMORY_URL` to that base URL (MCP is `POST {MEMORY_URL}/mcp`). Or use `memory mcp` as a stdio proxy to localhost.

## First run

`memoryd` must be up (`brew services start harness-memory`, or run `./bin/memoryd` against compose Postgres).

```
memory init
memory token create --harness claude
```

`memory init` writes `~/.config/harness-memory/config.toml`, waits for `/readyz`, and prints the **admin** token once. Export it as `MEMORY_TOKEN`, then mint a per-harness token (`--harness grok`, `codex`, …). Give each harness its own token, not the admin token.

MCP: `POST /mcp` with `Authorization: Bearer <token>`.

## Skill

Agents follow [`skills/harness-memory/SKILL.md`](skills/harness-memory/SKILL.md):

- Session start: `recall` with the current project slug (repo name)
- `save` facts, feedback, references
- `ingest_source` then `write_page` for sources; keep `[[wikilinks]]` current
- File good answers back
- Never accept inbox; tell the user to run `memory inbox`
- Do not write secrets (tokens, passwords) into memory bodies

## License

MIT
