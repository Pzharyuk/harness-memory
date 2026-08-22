# Install

Click-through setup with copy buttons: [interactive docs](index.html).

Three ways to run the store, then a client (plugin or CLI) that talks to it.
Pick **one** store. Do not run Homebrew Postgres and the cluster Postgres at
the same time for the same brain.

| Path | When |
|---|---|
| [Homebrew (local brain)](#homebrew-local-brain) | Single Mac, Postgres on the Mac |
| [Homebrew (CLI only, cluster brain)](#homebrew-cli-only-cluster-brain) | Shared `memoryd` in Kubernetes; brew only installs `memory` / `memoryd` |
| [Kubernetes](#kubernetes) | The cluster pod + in-cluster Postgres (this is the central brain) |
| [Plugin](#plugin-claude--grok) | How Claude Code and Grok attach |

## Homebrew (local brain)

Needs [Homebrew](https://brew.sh). Installs `memoryd`, `memory`, and
PostgreSQL 16.

```sh
brew tap Pzharyuk/tools
brew install harness-memory

brew services start postgresql@16
createdb memory
brew services start harness-memory

memory init
memory token create --harness claude
memory token create --harness grok
```

`memory init` prints the **admin** token once. Export it for later CLI
admin commands:

```sh
export MEMORY_TOKEN='<admin token>'
```

Give each harness its own token from `memory token create`, not the admin
token.

`memoryd` listens on `127.0.0.1:8741`.

Check:

```sh
memory status
curl -sS http://127.0.0.1:8741/healthz
```

Logs: `$(brew --prefix)/var/log/memoryd.log`

## Homebrew (CLI only, cluster brain)

Same formula, **do not start local Postgres or `harness-memory`**. The
cluster pod is the store. You must be on the **church office IP**
(`97.120.177.5`); Cloudflare Access denies other IPs.

```sh
brew tap Pzharyuk/tools
brew install harness-memory

export MEMORY_URL=https://memory.onit.systems
```

First boot (tokens table empty) — prints admin token once:

```sh
memory init
export MEMORY_TOKEN='<admin token>'
memory token create --harness grok
memory token create --harness claude
```

Do **not** run `brew services start harness-memory` or `postgresql@16` on
this path.

## Kubernetes

GitOps chart in `hgwa-k8s-gitops` (`charts/harness-memory`): `memoryd` pod
+ Postgres StatefulSet, Vault secret `secret/harness-memory/app`,
TunnelIngress `memory.onit.systems`, Access bypass **church IP only**.

Before the first healthy sync:

```sh
PW="$(openssl rand -base64 24)"
vault kv put secret/harness-memory/app \
  POSTGRES_USER=harness \
  POSTGRES_DB=harness \
  POSTGRES_PASSWORD="$PW" \
  DATABASE_URL="postgres://harness:${PW}@postgres:5432/harness?sslmode=disable"
```

Then use the [CLI-only brew path](#homebrew-cli-only-cluster-brain) against
`https://memory.onit.systems`.

## Plugin (Claude / Grok)

`memoryd` must already be reachable.

```
/plugin marketplace add Pzharyuk/ai-claude-plugins
/plugin install harness-memory@ai-claude-plugins
```

Grok:

```
grok plugin marketplace add Pzharyuk/ai-claude-plugins
grok plugin install harness-memory --trust
```

| Variable | Required | Default |
|---|---|---|
| `MEMORY_URL` | no | `http://127.0.0.1:8741` (use `https://memory.onit.systems` for the cluster) |
| `MEMORY_TOKEN` | yes | per-harness token from `memory token create --harness <name>` |

## Docker Compose

Local Postgres only (port `55432`):

```sh
docker compose -f deploy/compose/docker-compose.yml up -d
export MEMORY_DATABASE_URL=postgres://memory:memory@127.0.0.1:55432/memory?sslmode=disable
make build
MEMORY_DATABASE_URL="$MEMORY_DATABASE_URL" ./bin/memoryd
```
