# Harness Memory — Shared Postgres Memory + LLM Wiki

**Date:** 2026-08-15
**Status:** Approved
**Repo:** https://github.com/Pzharyuk/harness-memory
**License:** MIT

## Purpose

A single Postgres-backed memory service that coding agents (Claude Code, Grok,
Codex, and anything that speaks MCP or HTTP) share. It replaces per-harness
local files (`MEMORY.md`, Grok on-disk memory) with one brain, and compiles
that brain into a Karpathy-style LLM wiki so knowledge accumulates instead of
being re-derived every session.

The server is a shelf. The agent is the librarian. An optional compile worker
can be added later without changing the schema.

This is a public open-source project. People should be able to `brew install`
it, run it with Docker Compose, or deploy the Helm chart. The maintainer's
homelab (k8s, Vault, tunnel) is an example deploy, not the default.

## Goals

- Shared memory across harnesses: a fact Claude saves is recallable by Grok.
- User + project scopes. Session start loads user memory plus the current
  project's memory.
- MCP is the real API. Claude/Grok `MEMORY.md` trees are a **projection**,
  not a second source of truth.
- Tiered writes: auto for small facts; proposals for contradictions,
  supersessions, deletes, and bulk rewrites.
- Karpathy three-layer wiki: immutable sources, LLM-compiled pages, schema
  (a shipped skill). Operations: ingest, query, lint. File good answers back.
- One binary in three runtimes: Homebrew (full local server + Postgres),
  Docker Compose, Kubernetes.
- `memory token create --harness <name>` mints per-harness tokens. Hash
  stored; secret shown once.
- Import existing Claude auto-memory directories on first run.
- Public GitHub project: local dev, CI checks, Dependabot, releases.
- Claude Code and Grok install the same marketplace plugin from
  `Pzharyuk/ai-claude-plugins` (thin MCP client + librarian skill). The
  plugin does **not** reimplement the server.

## Non-Goals (v1)

- In-process LLM / server-side wiki compilation. Agents compile. A worker
  that calls `MEMORY_LLM_URL` is a later optional client of the same APIs.
- Web UI.
- Authentik / OIDC. Tokens only.
- Multi-user / team ACL. One install = one brain.
- Automatic forgetting / Ebbinghaus decay. Explicit supersession only.
  Old rows stay, marked superseded, with a pointer to the replacement.
- Numeric confidence scores. Provenance (source ids, harness, revision
  chain) is the signal.
- pgvector / hybrid search. FTS only. Do **not** add an `embedding` column
  or require `pgvector` in v1 (brew Postgres will not have the extension).
  A later migration can add it when the compile worker ships.
- Two-way file sync. Projection is one-way (DB → files). File edits are
  overwritten on the next project. Import is one-shot.
- Accepting proposals over MCP. Inbox accept/reject is CLI-only.

## Key Decisions

| Decision | Choice | Why |
|---|---|---|
| Job | Memory-first, wiki is compiled view | Pain is split-brain between harnesses; wiki keeps the store compiled |
| Scope | `user` + `project` | Matches Claude today; Grok in another repo still sees infra/preferences |
| Access | MCP + HTTP; files are a projection | Native session-start still works; other agents are first-class |
| Writes | Tiered (auto / proposed / never-auto) | Shared brain makes a bad write expensive; review-gating everything starves the store |
| Compile | Agent librarian now; optional worker later | No API key on the server; same APIs when unattended ingest is needed |
| Language | Go | One static binary for brew / Docker / k8s / GitHub Releases |
| Auth | Per-harness bearer tokens | Revoke Grok without killing Claude; audit names the writer |
| Deploy | Brew **or** Compose **or** Helm | Same `memoryd`; `DATABASE_URL` chooses local vs remote Postgres |
| Brew product | Full local server + `postgresql` dep | `brew services start` is a complete brain |
| Harness attach | Plugin in `ai-claude-plugins` | Same marketplace Claude and Grok already use; HTTP MCP to `memoryd` |
| Search | Postgres FTS | Enough until hundreds of pages; vector reserved |
| History | Append-only revisions; supersede, don't delete | Point-in-time belief; no silent rot from decay |
| Tenant | Single-brain per install | OSS default is personal; team ACL is a later product |

## Architecture

```
Claude / Grok / Codex / `memory` CLI
              │
         MCP + HTTP
         (per-harness token)
              │
           memoryd
              │
          PostgreSQL
              │
     ┌────────┴────────┐
     │                 │
 file projector   compile worker
 (Claude/Grok     (later, optional)
  MEMORY.md)
```

`memoryd` is the only process that talks to Postgres. It serves HTTP and
MCP over streamable HTTP on the same listener. `memory mcp` is a stdio
proxy for harnesses that cannot speak HTTP MCP; it forwards to `memoryd`.
Same binary:

- **Homebrew:** `depends_on postgresql`, `brew services start harness-memory`,
  then `memory token create --harness grok`.
- **Compose:** `memoryd` + official Postgres image.
- **Kubernetes:** Helm chart, secrets from Vault/External Secrets (example),
  ingress/tunnel. Example hostname for the maintainer: `memory.onit.systems`.

`DATABASE_URL` selects the database. A laptop can run its own store or point
at the cluster.

### Karpathy mapping

| Layer | In this system |
|---|---|
| Raw sources | `sources` table — immutable |
| Wiki | `wiki_pages` + `wiki_links` + generated index/log |
| Schema | `skills/harness-memory/SKILL.md` (and CLAUDE.md / AGENTS.md pointers) |

Operations: **ingest**, **query**, **lint**. Good answers are filed back as
pages or memories.

## Data Model

One install = one brain. No multi-user ACL in v1.

### Scopes

| Scope | Key | Example |
|---|---|---|
| `user` | (none) | “use pipenv”, Vault token in Keychain |
| `project` | slug (`live-translator-node`) | broadcast scaling, admin password model |

Optional `project_remote` (git URL) so a rename still matches.

### Tables

**`sources`** — immutable raw.

- `id` (uuid), `scope`, `project_slug`, `kind` (`import` / `file` / `url` / `session`)
- `title`, `body`, `content_sha256`
- `created_at`, `created_by_harness`
- Never updated. Re-ingest of the same sha256 + scope + project is
  idempotent and returns the existing row.

**`memories`** — Claude-style facts (auto-write path).

- `id`, `scope`, `project_slug`, `kind` (`user` / `feedback` / `project` / `reference`)
- `title`, `summary` (one line for the index), `body`
- `source_id` (nullable)
- `status` (`active` / `superseded`), `superseded_by`
- `created_at`, `updated_at`, `created_by_harness`, `updated_by_harness`
- Upsert key for auto-save: `(scope, project_slug, title)` among `active` rows.

**`wiki_pages`** — compiled layer.

- `id`, `scope`, `project_slug`, `slug` (`vault-ha-topology`)
- `title`, `summary`, `body_markdown`
- `page_type` (`entity` / `concept` / `source-summary` / `index` / `log` / `synthesis`)
- `status` (`active` / `superseded`), `superseded_by`
- `source_ids[]`
- `updated_at`, `updated_by_harness`
- Unique among active rows: `(scope, project_slug, slug)`.

**`wiki_links`**

- `from_page`, `to_page`, `rel` (`related` / `uses` / `depends_on` / `supersedes` / `contradicts`)
- Parsed from `[[wikilinks]]` on write. The graph is data, not only markdown.

**`revisions`**

- `entity_type`, `entity_id`, `before` jsonb, `after` jsonb, `harness`, `reason`, `at`
- Every memory/page write appends a row. Deletes are status changes to
  `superseded` (or a delete proposal), not SQL `DELETE`.

**`proposals`** — inbox.

- `action` (`create` / `update` / `supersede` / `delete` / `scope-move`)
- `payload` jsonb, `reason`
- `status` (`open` / `accepted` / `rejected`)
- `created_by_harness`
- Accept applies the payload in one transaction and writes a revision.

**`tokens`**

- `harness` (`claude` / `grok` / `codex` / `admin` / …)
- `token_hash`, `label`
- `created_at`, `last_used_at`, `revoked_at`
- Secret shown once by `memory token create --harness <name>`.
- Admin token minted by `memory init` (or first boot) for CLI.

**`audit_log`**

- `request_id`, `harness`, `action`, `entity`, `at`
- Read/write/search. Query text is **not** stored by default.

**`schema_meta`**

- migrations version, instance id.

**Reserved for later (not in v1 schema):** `embedding` columns + `pgvector`.
v1 migrations must not require the extension so stock Homebrew Postgres works.

### Write rules (enforced in `memoryd`)

| Action | Path |
|---|---|
| New/update memory (fact, feedback, reference) | **auto** |
| New wiki page, or update that does not conflict | **auto** |
| Update that contradicts or supersedes | **proposal** |
| Delete, scope move, bulk lint rewrite | **proposal** |
| Token create/revoke | CLI + admin token only |

Contradiction = same slug with a different body, or an existing
`contradicts` link, or an active memory with the same title in the same
scope whose body is not a prefix/equal update.

## Data Flow

### Session start

1. Harness starts; MCP connects with its token.
2. Client calls `recall` with `{ project }` (optional query).
3. `memoryd` returns user-scope index + this project's index + a short
   recent-revisions tail.
4. Projector refreshes `MEMORY.md` + topic files so native auto-memory
   load still works if the harness reads files.

Pushed index stays under Claude's 200-line / 25KB habit: one line per
memory/page. Full body via `recall` by id or `read_page`.

### Auto-save

Agent calls `save`. Server upserts by `(scope, project_slug, title)` or id,
writes a revision + audit row. If the title/slug collides with a
*different* body → reject auto, open a proposal, return
`{ status: proposed, id }`. Projector dirties the index.

### Ingest → wiki

`ingest_source` stores an immutable row. The **agent** reads the source and
existing pages (`search` / `read_page`), then `write_page`. New or
non-conflicting edits apply. Contradictions become proposals. `[[wikilinks]]`
are parsed into `wiki_links`. Index and log pages are updated last (or
generated from live rows).

The later compile worker uses these same APIs with its own token.

### Query and file-back

`search` (FTS, scope-filtered) → agent reads hits → answers with citations
(page/memory ids). Worth keeping → `write_page` or `save`.

### Lint

`memory lint` / MCP `lint` is **read-only** in v1:

- orphan pages (no inbound links)
- broken links
- newer source vs older page on the same slug
- memories never recalled (if recall events are counted; otherwise skip)
- index/projection drift

Fixes are proposals. The only auto lint fix allowed is mechanical: rewrite
a broken slug that still exists under a new slug. No silent bulk rewrite.

### Inbox

`memory inbox` lists open proposals. `memory accept <id>` / `reject <id>`
applies or drops them in one transaction.

Agents may create proposals. They may not accept their own. No
`--i-mean-it` on MCP. CLI accept requires the admin token.

### Failure behavior

| Case | Behavior |
|---|---|
| Bad/revoked token | 401, no leak of whether the harness name exists |
| Postgres down | MCP/HTTP fail loud; projector leaves last files in place |
| Partial wiki write | transaction rolls back; no half-updated links |
| Projection write fails | DB commit still succeeds; `memory project` retries; health shows drift |
| Duplicate ingest (same sha256 + scope + project) | idempotent, return existing source |
| Index would exceed 25KB | truncate **projection** with a “use search” line; full data stays in DB |

## Components and Interfaces

### Binaries

| Binary | Role |
|---|---|
| `memoryd` | HTTP + MCP (streamable HTTP on the same listener). Only process that writes the DB. |
| `memory` | CLI. Talks to `memoryd` with the admin token. `memory mcp` is stdio→HTTP for harnesses that cannot speak HTTP MCP. |

Brew installs both and a service plist for `memoryd`. Compose/k8s run
`memoryd`; CLI is local (brew or `go install`).

### MCP tools

| Tool | Does |
|---|---|
| `recall` | Session brief: user + project index, optional query, optional id for full body |
| `save` | Auto-write a memory (tiered rules apply) |
| `search` | FTS across memories + wiki pages, scope-filtered |
| `ingest_source` | Store immutable raw source |
| `read_page` / `write_page` | Wiki page I/O |
| `lint` | Read-only diagnostics |
| `inbox_list` | Open proposals |
| `inbox_propose` | Explicitly file a proposal |

No `inbox_accept` on MCP. No token admin on MCP.

### HTTP

Same handlers as MCP:

- `GET /healthz` `GET /readyz`
- `/v1/recall` `/v1/memories` `/v1/search` `/v1/sources` `/v1/pages`
- `/v1/lint` `/v1/inbox`
- `/v1/admin/tokens` — admin token only

Auth: `Authorization: Bearer <token>`.

### CLI

```
memory init                         # config + wait for DB + mint admin token
memory token create --harness grok
memory token list | revoke
memory import claude --path ~/.claude/projects/.../memory
memory project --out <dir>          # write MEMORY.md projection
memory inbox
memory accept <id> | reject <id>
memory lint
memory status
memory mcp                          # stdio MCP proxy → memoryd
memoryd                             # daemon (also: memory serve)
```

Config: `~/.config/harness-memory/config.toml` (`database_url`, `listen`,
`projection_dir`). Env overrides: `MEMORY_DATABASE_URL`, `MEMORY_TOKEN`,
`MEMORY_URL`.

`memory token create` works against a remote `MEMORY_URL` when the admin
token is present (cluster ops from a laptop).

### Skill

`skills/harness-memory/SKILL.md` in this repo is the canonical Karpathy
schema: when to `save` vs `write_page`, how to ingest, how to lint, how
to file-back. The marketplace plugin vendors a copy (see below).

### Plugin (Claude + Grok marketplace)

Harnesses do not hand-edit MCP JSON as the primary path. They install a
plugin from the existing marketplace repo
[`Pzharyuk/ai-claude-plugins`](https://github.com/Pzharyuk/ai-claude-plugins),
which both Claude Code and Grok already consume.

```
/plugin marketplace add Pzharyuk/ai-claude-plugins
/plugin install harness-memory@ai-claude-plugins
```

Grok:

```
grok plugin marketplace add Pzharyuk/ai-claude-plugins
grok plugin install harness-memory --trust
```

**The plugin is a client, not a second server.** Follow the
`ai-business-tools` pattern (HTTP MCP), not the Node-wrapper pattern
used by Vault/Proxmox.

| Piece | Lives in | Role |
|---|---|---|
| `memoryd` | `harness-memory` | The store |
| Plugin `harness-memory/` | `ai-claude-plugins` | `.mcp.json` + skills + marketplace entry |
| Canonical skill | `harness-memory/skills/` | Source of truth; copied into the plugin |

Plugin layout (same conventions as the other marketplace plugins):

```
ai-claude-plugins/harness-memory/
  .claude-plugin/plugin.json
  .mcp.json
  skills/configure/SKILL.md
  skills/harness-memory/SKILL.md    # vendored from this repo
  README.md
```

`.mcp.json` points at a running `memoryd`:

```json
{
  "mcpServers": {
    "harness-memory": {
      "type": "http",
      "url": "${MEMORY_URL}/mcp",
      "headers": {
        "Authorization": "Bearer ${MEMORY_TOKEN}"
      }
    }
  }
}
```

`plugin.json` `setup.env`:

| Var | Required | Default | Meaning |
|---|---|---|---|
| `MEMORY_URL` | no | `http://127.0.0.1:8741` | `memoryd` base URL (local brew or cluster) |
| `MEMORY_TOKEN` | yes (secret) | — | Per-harness token from `memory token create --harness <name>` |

`skills/configure` walks: is `memoryd` up? create a token for this
harness; write `MEMORY_URL` / `MEMORY_TOKEN` into plugin env /
`~/.mcp.json`; optional projection dir.

`memoryd` must expose MCP at `POST /mcp` (streamable HTTP) on the same
listener as `/v1`. If a harness cannot speak HTTP MCP, `memory mcp`
stdio remains the escape hatch (`command: memory`, `args: ["mcp"]`).

**Do not** add a Node MCP server that re-wraps the HTTP API. Two
implementations of `save`/`recall` will drift.

When the skill in `harness-memory` changes, the plugin copy is updated
in the same implementation slice (or a tiny sync script in CI later).
v1: copy by hand and note the source commit in the plugin README.

Marketplace: add an entry to
`ai-claude-plugins/.claude-plugin/marketplace.json` and a row on that
repo's README. Update the marketplace description to mention shared
agent memory, not only infra plugins.

### Repo layout

```
harness-memory/
  cmd/memoryd/
  cmd/memory/
  internal/          api, store, search, project, import, auth, mcp
  db/migrations/
  deploy/compose/
  deploy/chart/
  Formula/harness-memory.rb   # also published to Pzharyuk/tools
  skills/harness-memory/
  docs/superpowers/specs/     # this spec
  .github/workflows/
  .github/dependabot.yml
```

Go module: `github.com/Pzharyuk/harness-memory`.

## Install (what strangers see)

| Path | Command |
|---|---|
| Homebrew | `brew tap Pzharyuk/tools && brew install harness-memory` then `brew services start harness-memory` |
| Docker | `docker compose up -d` (Postgres + `memoryd`) |
| Kubernetes | Helm chart in `deploy/chart` |
| Release binary | GitHub Releases via GoReleaser (darwin/linux × amd64/arm64) |
| Claude / Grok | `/plugin install harness-memory@ai-claude-plugins` (server still required) |

First-run: `memory init` → `memory token create --harness <name>` →
install the plugin and set `MEMORY_TOKEN` (and `MEMORY_URL` if not
localhost).

README is written for that path. `deploy/` holds the maintainer's cluster
example (Vault, ingress) as *an* example.

## Local Development

```
make dev          # compose: Postgres + memoryd (live reload)
make test         # unit + integration
make lint         # golangci-lint
make build        # memoryd + memory
```

`CONTRIBUTING.md`: Go version, `make dev`, how to run one test, how to add
a migration. `.env.example` only — no secrets in git.

Integration tests use a Postgres service (CI) or testcontainers (local).
No LLM in tests. The skill markdown is reviewed, not executed in CI.

## GitHub: CI, Dependabot, Releases

Public repo. PRs must pass CI before merge.

**Checks on every PR**

- `gofmt` / `go vet`
- `golangci-lint`
- unit tests
- integration tests against a Postgres service container
- `go build` matrix: linux/darwin × amd64/arm64 (compile only)
- Docker image build (smoke optional)
- `helm lint` on `deploy/chart`

**Release (git tag)**

- GoReleaser: binaries, checksums, SBOM, GitHub Release
- Docker image push
- Formula bump instructions (or tap commit) for `Pzharyuk/tools`

**Dependabot** (`.github/dependabot.yml`, weekly)

- `gomod`
- `github-actions`
- `docker`

Grouped patch/minor PRs.

**Also from day one:** `LICENSE` (MIT), `README`, `CONTRIBUTING`,
`SECURITY.md`, `CODE_OF_CONDUCT`, issue/PR templates, `CHANGELOG`.

No DCO required in v1. CodeQL can be added later; not a v1 gate.

## Security

- Tokens are random, stored as argon2id hashes. Plaintext shown once.
- Admin token required for token mint/revoke, inbox accept/reject, import.
- Harness tokens can read/write memory and wiki per write rules; they
  cannot mint tokens or accept proposals.
- Projection directories must not be world-readable if they contain
  infra notes. Default `0600` / `0700`.
- Import of Claude memory is local-to-local; it does not upload to a third
  party. Cluster deploy is the operator's choice.
- `SECURITY.md` documents private vulnerability reports.

This store will hold infra topology and operational notes. The default
listen address for brew is `127.0.0.1`. Binding `0.0.0.0` is opt-in.
Cluster ingress must be authenticated (tokens); do not expose `/v1`
without TLS and a token.

## Testing Bar

- Unit: write rules, token hash, FTS query builder, `[[wikilink]]` parse,
  projection size cap, Claude `MEMORY.md` import parser.
- Integration: real Postgres — save/recall, conflict → proposal, accept
  proposal, ingest idempotency, projection drift, token revoke → 401.
- CI must run all of the above. A red integration test blocks merge.

## Migration from Claude Auto-Memory

`memory import claude --path <dir>`:

1. Read `MEMORY.md` as the index.
2. Create one `source` of kind `import` for the directory snapshot.
3. Each topic file → a `memory` (kind inferred from filename prefix
   `feedback_` / `project_` / `reference_` / `user_`) and, if the body is
   long, a wiki page of type `source-summary` linked to that memory.
4. All imported rows attributed to harness `import`.
5. Dry-run flag prints the plan and writes nothing.

Default path discovery: `~/.claude/projects/<encoded-cwd>/memory/`.

Grok on-disk import is a fast-follow if the format is stable; not a v1
blocker.

## Open Questions

None that block v1. Defaults:

- Example cluster host: `memory.onit.systems` (maintainer only).
- Brew tap: existing `Pzharyuk/tools`.
- Formula name: `harness-memory`.
- Default listen: `127.0.0.1:8741`.

Change these in review if needed.

## Implementation Notes (for the later plan)

Build in this order so each step is usable:

1. Repo skeleton: Go module, Makefile, LICENSE, CI, Dependabot, Compose,
   empty Helm chart, CONTRIBUTING.
2. Store + migrations + write rules + tokens.
3. HTTP API + `memory` CLI (`init`, `token`, `status`).
4. MCP tools wrapping the same handlers.
5. Search (FTS) + `recall` index budget.
6. Projector + Claude import.
7. Wiki pages, links, lint, inbox accept/reject.
8. Skill + README install story + GoReleaser + formula.
9. Marketplace plugin in `ai-claude-plugins` (HTTP `.mcp.json`, configure
   skill, vendored librarian skill, marketplace.json + README row).

Do not ship a compile worker in the first implementation plan. The
plugin ships in the same overall plan but is a separate PR in the
plugins repo, after `memoryd` serves `/mcp`.
