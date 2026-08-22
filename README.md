# harness-memory

Shared Postgres memory for coding agents, with a [Karpathy LLM wiki](https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f) as the compiled view.

Point Claude Code, Grok, Codex, or anything that speaks MCP at **one** store. A fact one harness saves is recallable by the others.

**Interactive docs (click through, copy commands):** [docs/index.html](docs/index.html)

Also: [Install](docs/install.md) · [Usage](docs/usage.md) · [Design spec](docs/superpowers/specs/2026-08-15-harness-memory-design.md)

---

## Pick one brain

Do **not** run Homebrew Postgres and the cluster Postgres as two brains.

| Brain | Who talks to it | Where you must be |
|---|---|---|
| **Cluster** (this is the shared one) | `https://memory.onit.systems` | Church office IP `97.120.177.5` |
| **Local Homebrew** | `http://127.0.0.1:8741` | This Mac |

---

## 1. Install the CLI (Homebrew)

```sh
brew tap Pzharyuk/tools
brew update
brew untap Pzharyuk/tools
brew tap Pzharyuk/tools
brew install harness-memory
```

If checksum errors persist: `brew cleanup -s harness-memory` then `brew install harness-memory`.

This installs `memory` (CLI) and `memoryd` (daemon). For the **cluster** brain, do **not** start local Postgres or `brew services start harness-memory`.

---

## 2. Cluster brain (shared)

Cloudflare Access allows **church IP only**. Home is blocked at the edge.

### Vault (once, if External Secrets is still empty)

```sh
PW="$(openssl rand -base64 24)"
vault kv put secret/harness-memory/app \
  POSTGRES_USER=harness \
  POSTGRES_DB=harness \
  POSTGRES_PASSWORD="$PW" \
  DATABASE_URL="postgres://harness:${PW}@postgres:5432/harness?sslmode=disable"
```

Path must be exactly `secret/harness-memory/app` (same shape as `secret/guest-docs/app`). Then:

```sh
kubectl annotate externalsecret harness-memory-secrets -n harness-memory \
  force-sync="$(date +%s)" --overwrite
kubectl get pods -n harness-memory
```

GitOps chart: `hgwa-k8s-gitops/charts/harness-memory` (Postgres StatefulSet + `memoryd` pod + TunnelIngress). Image: `ghcr.io/pzharyuk/harness-memory`.

### Tokens (church IP)

`MEMORY_TOKEN is required` on `memory token create` means you skipped `memory init`. Init does **not** need a token.

```sh
export MEMORY_URL=https://memory.onit.systems

memory init                          # once — prints the ADMIN token; save it
export MEMORY_TOKEN='<admin token>'  # admin only, for CLI
memory token create --harness grok
memory token create --harness claude
```

| Token | Who | Use |
|---|---|---|
| admin | you / `memory` CLI | mint tokens, `memory inbox` / accept / reject |
| grok / claude | that harness | plugin `MEMORY_TOKEN` — **not** the admin token |

If `memory init` returns **409**, admin already exists. Reuse the saved admin token.

---

## 3. Attach Grok and Claude

The plugin is **not** installed until you install it. It lives in marketplace **ai-claude-plugins**, not xAI Official. Refresh the marketplace first (local clones go stale).

### Grok

```sh
grok plugin marketplace add Pzharyuk/ai-claude-plugins
grok plugin marketplace update ai-claude-plugins
grok plugin install harness-memory --trust
```

Or UI: `/marketplace` → source **ai-claude-plugins** → `r` refresh → install **harness-memory**.

Env when prompted:

```
MEMORY_URL=https://memory.onit.systems
MEMORY_TOKEN=<grok token from memory token create --harness grok>
```

### Claude Code

```
/plugin marketplace add Pzharyuk/ai-claude-plugins
/plugin install harness-memory@ai-claude-plugins
```

If it still does not list: the marketplace clone was stale. Refresh with `r` on the Marketplace tab, or:

```sh
git -C ~/.claude/plugins/marketplaces/ai-claude-plugins pull
```

Same env, with the **claude** harness token. Press `r` on the Plugins tab or start a new session.

The plugin is HTTP MCP only (`POST ${MEMORY_URL}/mcp`). No Node `server/`.

---

## 4. First session

Open Grok or Claude in a project. The agent should `recall` user + project memory, `save` facts, and file wiki pages. A fact saved in one harness is recallable in the other.

Optional import of existing Claude auto-memory:

```sh
memory import claude --path ~/.claude/projects/<encoded-cwd>/memory --dry-run
memory import claude --path ~/.claude/projects/<encoded-cwd>/memory
```

---

## Local Homebrew brain (optional, not shared)

Only if you are **not** using the cluster:

```sh
brew services start postgresql@16
createdb memory
brew services start harness-memory
memory init
export MEMORY_TOKEN='<admin token>'
memory token create --harness claude
```

`memoryd` listens on `127.0.0.1:8741`. Logs: `$(brew --prefix)/var/log/memoryd.log`. Plugin `MEMORY_URL` stays at the default `http://127.0.0.1:8741`.

---

## CLI cheatsheet

```sh
export MEMORY_URL=https://memory.onit.systems   # or http://127.0.0.1:8741
export MEMORY_TOKEN='<admin token>'

memory status
memory token list
memory token create --harness grok
memory inbox
memory accept <id>
memory reject <id>
memory lint
memory mcp                    # stdio proxy if a harness cannot speak HTTP MCP
```

`memoryd` reads **env** (`MEMORY_DATABASE_URL`, `MEMORY_LISTEN`), not `config.toml`. The CLI reads `~/.config/harness-memory/config.toml`; env overrides.

MCP tools: `recall`, `save`, `search`, `ingest_source`, `read_page`, `write_page`, `lint`, `inbox_list`, `inbox_propose`. **No** `inbox_accept` on MCP.

---

## Troubleshooting

| Symptom | Fix |
|---|---|
| `MEMORY_TOKEN is required` on `token create` | Run `memory init` first; export the **admin** token; then `token create`. |
| Plugin install asks for `MEMORY_TOKEN` | That is required. Use the **harness** token, not admin. |
| Plugin not in the list | Refresh marketplace (`grok plugin marketplace update ai-claude-plugins` or Marketplace tab `r`). Look under **ai-claude-plugins**, not xAI Official. Not on Plugins tab until installed. |
| Homebrew `sha256 :no_check` / checksum | `brew untap Pzharyuk/tools && brew tap Pzharyuk/tools && brew cleanup -s harness-memory && brew install harness-memory` |
| Access / 403 at `memory.onit.systems` | Church IP `97.120.177.5` only. |
| `memory init` 409 | Admin already minted; use the saved admin token. |
| 401 | Wrong token or `MEMORY_URL`. |
| Pods `Secret does not exist` | Vault path must be `secret/harness-memory/app`. |
| `wait-postgres` / no response | Postgres not ready yet; check `kubectl get pods -n harness-memory`. |

```sh
kubectl get pods,externalsecret -n harness-memory
kubectl logs -n harness-memory deploy/memoryd
```

---

## What it is

- **`memoryd`** — HTTP + MCP server on Postgres (user + project scopes).
- **`memory`** — CLI.
- One-way projection to Claude/Grok `MEMORY.md`.
- Librarian skill: [`skills/harness-memory/SKILL.md`](skills/harness-memory/SKILL.md).

License: MIT
