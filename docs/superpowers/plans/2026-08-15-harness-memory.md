# Harness Memory Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a Go `memoryd` + `memory` CLI on Postgres so Claude, Grok, and other harnesses share one user+project memory store with a Karpathy wiki layer, MCP/HTTP access, file projection, Claude import, and a thin plugin in `ai-claude-plugins`.

**Architecture:** One `memoryd` process owns Postgres. HTTP `/v1` and streamable HTTP MCP `/mcp` share the same handlers. The `memory` CLI talks to `memoryd` with an admin token. Agents compile wiki pages via tools; the server enforces write rules (auto vs proposal). A one-way projector writes Claude/Grok `MEMORY.md`. No in-process LLM. The marketplace plugin is HTTP MCP + skills only.

**Tech Stack:** Go 1.24, `pgx/v5`, stdlib `net/http`, `golang.org/x/crypto/argon2`, JSON-RPC 2.0 MCP at `POST /mcp`, Postgres 16 FTS, Docker Compose, Helm, GoReleaser, GitHub Actions, Dependabot.

**Spec:** `docs/superpowers/specs/2026-08-15-harness-memory-design.md`

## Global Constraints

- Go 1.24. Module path: `github.com/Pzharyuk/harness-memory`.
- Default listen: `127.0.0.1:8741`. Binding `0.0.0.0` is opt-in.
- Compose Postgres published as `55432:5432` (avoid colliding with local 5432).
- Env: `MEMORY_DATABASE_URL`, `MEMORY_TOKEN`, `MEMORY_URL`, `MEMORY_LISTEN`, `MEMORY_PROJECTION_DIR`.
- Config file: `~/.config/harness-memory/config.toml`.
- Tokens: argon2id hashes only; plaintext shown once.
- No `pgvector`, no `embedding` column, no LLM client, no web UI, no OIDC.
- One install = one brain. Scopes: `user` and `project` only.
- MCP must not expose `inbox_accept` or token admin.
- Projection is one-way (DB → files). Import is one-shot.
- Tests: `go test ./...`. Integration tests skip if `MEMORY_TEST_DATABASE_URL` is unset; CI always sets it.
- Conventional commits after every task. No secrets in git.

---

## File Structure

```
harness-memory/
  cmd/memoryd/main.go
  cmd/memory/main.go
  internal/
    types/types.go           shared enums + structs
    config/config.go         env + toml
    auth/argon.go            hash / verify
    writerules/rules.go      pure decide(auto|proposed)
    store/
      db.go                  pool + migrate
      tokens.go
      memories.go
      sources.go
      pages.go
      links.go
      proposals.go
      revisions.go
      audit.go
      search.go
    wikilink/parse.go        [[wikilinks]] → slugs
    api/
      server.go              mux, auth middleware
      handlers.go            /v1 + health
    mcp/
      http.go                streamable HTTP /mcp
      stdio.go               memory mcp proxy
    project/project.go       MEMORY.md projector
    importclaude/import.go
    lint/lint.go
  db/migrations/001_init.sql
  deploy/compose/docker-compose.yml
  deploy/chart/              Chart.yaml, values.yaml, templates/
  Formula/harness-memory.rb
  skills/harness-memory/SKILL.md
  testdata/claude-memory/    fixture for import tests
  .github/workflows/ci.yml
  .github/dependabot.yml
  .goreleaser.yaml
  Dockerfile
  Makefile
  go.mod
  CONTRIBUTING.md SECURITY.md CODE_OF_CONDUCT.md CHANGELOG.md .env.example

ai-claude-plugins/harness-memory/   (Task 16, other repo)
  .claude-plugin/plugin.json
  .mcp.json
  skills/configure/SKILL.md
  skills/harness-memory/SKILL.md
  README.md
```

---

### Task 1: Repo skeleton and CI

**Files:**
- Create: `go.mod`, `Makefile`, `.env.example`, `CONTRIBUTING.md`, `SECURITY.md`, `CODE_OF_CONDUCT.md`, `CHANGELOG.md`, `Dockerfile`, `.dockerignore`, `deploy/compose/docker-compose.yml`, `deploy/chart/Chart.yaml`, `deploy/chart/values.yaml`, `deploy/chart/templates/deployment.yaml`, `deploy/chart/templates/service.yaml`, `deploy/chart/templates/_helpers.tpl`, `.github/workflows/ci.yml`, `.github/dependabot.yml`, `internal/config/config.go`, `internal/config/config_test.go`, `cmd/memoryd/main.go`, `cmd/memory/main.go`

**Interfaces:**
- Consumes: nothing
- Produces: `config.Config` struct and `config.Load() (Config, error)`

- [ ] **Step 1: Write the failing config test**

`internal/config/config_test.go`:

```go
package config

import (
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("MEMORY_DATABASE_URL", "")
	t.Setenv("MEMORY_LISTEN", "")
	t.Setenv("HOME", t.TempDir())
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != "127.0.0.1:8741" {
		t.Fatalf("listen=%q", c.Listen)
	}
}

func TestLoadEnvOverrides(t *testing.T) {
	t.Setenv("MEMORY_DATABASE_URL", "postgres://x")
	t.Setenv("MEMORY_LISTEN", "0.0.0.0:9000")
	t.Setenv("MEMORY_URL", "http://memory.example")
	t.Setenv("MEMORY_TOKEN", "tok")
	t.Setenv("HOME", t.TempDir())
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.DatabaseURL != "postgres://x" || c.Listen != "0.0.0.0:9000" || c.URL != "http://memory.example" || c.Token != "tok" {
		t.Fatalf("%+v", c)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -count=1`
Expected: FAIL — package does not exist / `Load` undefined.

- [ ] **Step 3: Implement config + skeleton**

```bash
go mod init github.com/Pzharyuk/harness-memory
```

`internal/config/config.go`:

```go
package config

import (
	"os"
	"path/filepath"
)

type Config struct {
	DatabaseURL   string
	Listen        string
	URL           string
	Token         string
	ProjectionDir string
	ConfigPath    string
}

func Load() (Config, error) {
	home, _ := os.UserHomeDir()
	c := Config{
		Listen:        "127.0.0.1:8741",
		URL:           "http://127.0.0.1:8741",
		ConfigPath:    filepath.Join(home, ".config", "harness-memory", "config.toml"),
		ProjectionDir: filepath.Join(home, ".local", "share", "harness-memory", "projection"),
	}
	if v := os.Getenv("MEMORY_DATABASE_URL"); v != "" {
		c.DatabaseURL = v
	}
	if v := os.Getenv("MEMORY_LISTEN"); v != "" {
		c.Listen = v
	}
	if v := os.Getenv("MEMORY_URL"); v != "" {
		c.URL = v
	}
	if v := os.Getenv("MEMORY_TOKEN"); v != "" {
		c.Token = v
	}
	if v := os.Getenv("MEMORY_PROJECTION_DIR"); v != "" {
		c.ProjectionDir = v
	}
	return c, nil
}
```

`cmd/memoryd/main.go` and `cmd/memory/main.go` should `fmt.Fprintln(os.Stderr, "not implemented")` and `os.Exit(2)` for now.

`Makefile`:

```makefile
.PHONY: test lint build dev
test:
	go test ./...
lint:
	golangci-lint run
build:
	go build -o bin/memoryd ./cmd/memoryd
	go build -o bin/memory ./cmd/memory
dev:
	docker compose -f deploy/compose/docker-compose.yml up --build
```

Compose: official `postgres:16`, user/pass/db `memory`, port `55432:5432`, healthcheck. `memoryd` service can wait until Task 7; for now compose may be Postgres-only.

CI (`ci.yml`): on pull_request and push to main — `gofmt -l`, `go vet`, `go test ./...`, `go build` matrix (`linux/darwin` × `amd64/arm64`, compile-only via `GOOS`/`GOARCH`), `docker build`, `helm lint deploy/chart`. Postgres service:

```yaml
services:
  postgres:
    image: postgres:16
    env:
      POSTGRES_USER: memory
      POSTGRES_PASSWORD: memory
      POSTGRES_DB: memory_test
    ports: ["5432:5432"]
    options: >-
      --health-cmd "pg_isready -U memory -d memory_test"
      --health-interval 5s --health-retries 10
env:
  MEMORY_TEST_DATABASE_URL: postgres://memory:memory@localhost:5432/memory_test?sslmode=disable
```

Dependabot weekly: `gomod`, `github-actions`, `docker`. Group patch/minor.

Helm chart: name `harness-memory`, image placeholder, containerPort 8741, env from secret keys `database-url` and (optional) nothing else. Default `listen` in-cluster may be `:8741` (all interfaces) via values — document that this is opt-in vs brew's localhost.

Dockerfile: multi-stage `golang:1.24` → distroless/static, entrypoint `memoryd`.

Write CONTRIBUTING (Go 1.24, `make test`, `MEMORY_TEST_DATABASE_URL`, how to add a migration), SECURITY (private reports to repo Security tab), CODE_OF_CONDUCT (Contributor Covenant 2.1), CHANGELOG (`## Unreleased`), `.env.example`.

- [ ] **Step 4: Run tests and CI-equivalent locally**

Run: `gofmt -l . && go vet ./... && go test ./... && go build ./cmd/memoryd ./cmd/memory && helm lint deploy/chart`
Expected: PASS (helm lint OK, tests pass).

- [ ] **Step 5: Commit**

```bash
git add go.mod Makefile .env.example CONTRIBUTING.md SECURITY.md CODE_OF_CONDUCT.md CHANGELOG.md Dockerfile .dockerignore deploy .github internal/config cmd
git commit -m "chore: scaffold Go module, CI, Dependabot, and compose"
```

---

### Task 2: Types and argon2id tokens

**Files:**
- Create: `internal/types/types.go`, `internal/auth/argon.go`, `internal/auth/argon_test.go`

**Interfaces:**
- Consumes: nothing
- Produces:
  - `types.Scope`, `types.MemoryKind`, `types.Status`, `types.PageType`, `types.Rel`, `types.SourceKind`, `types.ProposalAction`, `types.ProposalStatus`, `types.WritePath` (`auto` | `proposed`)
  - structs: `types.Memory`, `types.Source`, `types.Page`, `types.Link`, `types.Proposal`, `types.Token`, `types.Revision`, `types.SaveResult` (`Status string`, `ID uuid.UUID`, `ProposalID *uuid.UUID`)
  - `auth.Hash(plaintext string) (string, error)`
  - `auth.Verify(hash, plaintext string) bool`

- [ ] **Step 1: Write the failing argon test**

```go
package auth

import "testing"

func TestHashVerify(t *testing.T) {
	h, err := Hash("secret-token")
	if err != nil {
		t.Fatal(err)
	}
	if h == "secret-token" {
		t.Fatal("hash stored plaintext")
	}
	if !Verify(h, "secret-token") {
		t.Fatal("verify failed")
	}
	if Verify(h, "wrong") {
		t.Fatal("wrong password accepted")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/ -count=1`
Expected: FAIL — `Hash` undefined.

- [ ] **Step 3: Implement types + argon**

Use `golang.org/x/crypto/argon2` + `crypto/rand` + `encoding/base64`. Encode as `argon2id$v=19$m=65536,t=1,p=4$<salt>$<key>`. `Verify` is constant-time on the derived key (`subtle.ConstantTimeCompare`).

Put every enum and struct from the spec into `internal/types/types.go` using `github.com/google/uuid`. `Memory.ProjectSlug` is `""` for user scope. `SaveResult.Status` is `"applied"` or `"proposed"`.

```bash
go get golang.org/x/crypto@latest github.com/google/uuid@latest
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/auth/ ./internal/types/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/types internal/auth go.mod go.sum
git commit -m "feat: add domain types and argon2id token hashing"
```

---

### Task 3: Write rules (pure)

**Files:**
- Create: `internal/writerules/rules.go`, `internal/writerules/rules_test.go`

**Interfaces:**
- Consumes: `types.Memory`, `types.Page`, `types.WritePath`
- Produces:
  - `writerules.Decision` `{ Path types.WritePath; Reason string }`
  - `writerules.DecideMemorySave(existing *types.Memory, incoming types.Memory) Decision`
  - `writerules.DecidePageWrite(existing *types.Page, incoming types.Page, hasContradictsLink bool) Decision`
  - `writerules.DecideDelete() Decision` always `proposed`
  - `writerules.DecideScopeMove() Decision` always `proposed`

Contradiction for memories: `existing != nil` and bodies are not equal and incoming body is not a prefix of existing (or vice versa for equal-update). Same title is implied by the caller looking up existing. Empty existing → auto.

Contradiction for pages: existing active page same slug and body differs, or `hasContradictsLink`.

- [ ] **Step 1: Write the failing tests**

```go
package writerules

import (
	"testing"

	"github.com/Pzharyuk/harness-memory/internal/types"
)

func TestNewMemoryIsAuto(t *testing.T) {
	d := DecideMemorySave(nil, types.Memory{Title: "pipenv", Body: "use pipenv"})
	if d.Path != types.PathAuto {
		t.Fatalf("%+v", d)
	}
}

func TestSameBodyIsAuto(t *testing.T) {
	ex := &types.Memory{Title: "pipenv", Body: "use pipenv"}
	d := DecideMemorySave(ex, types.Memory{Title: "pipenv", Body: "use pipenv"})
	if d.Path != types.PathAuto {
		t.Fatalf("%+v", d)
	}
}

func TestDifferentBodyIsProposed(t *testing.T) {
	ex := &types.Memory{Title: "pipenv", Body: "use pipenv"}
	d := DecideMemorySave(ex, types.Memory{Title: "pipenv", Body: "use poetry"})
	if d.Path != types.PathProposed {
		t.Fatalf("%+v", d)
	}
}

func TestPrefixUpdateIsAuto(t *testing.T) {
	ex := &types.Memory{Title: "pipenv", Body: "use pipenv"}
	d := DecideMemorySave(ex, types.Memory{Title: "pipenv", Body: "use pipenv\nalways pin"})
	if d.Path != types.PathAuto {
		t.Fatalf("%+v", d)
	}
}

func TestDeleteIsProposed(t *testing.T) {
	if DecideDelete().Path != types.PathProposed {
		t.Fatal("delete must be proposed")
	}
}

func TestPageConflictProposed(t *testing.T) {
	ex := &types.Page{Slug: "vault", BodyMarkdown: "old"}
	d := DecidePageWrite(ex, types.Page{Slug: "vault", BodyMarkdown: "new"}, false)
	if d.Path != types.PathProposed {
		t.Fatalf("%+v", d)
	}
}

func TestPageContradictsLinkProposed(t *testing.T) {
	d := DecidePageWrite(nil, types.Page{Slug: "vault", BodyMarkdown: "x"}, true)
	if d.Path != types.PathProposed {
		t.Fatalf("%+v", d)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/writerules/ -count=1`
Expected: FAIL — functions undefined.

- [ ] **Step 3: Implement rules**

Prefix check: `strings.HasPrefix(incoming.Body, existing.Body)` or bodies equal after `strings.TrimSpace`. Delete/scope-move return `{Path: types.PathProposed, Reason: "..."}`.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/writerules/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/writerules
git commit -m "feat: enforce tiered write rules in a pure package"
```

---

### Task 4: Migrations and store bootstrap

**Files:**
- Create: `db/migrations/001_init.sql`, `internal/store/db.go`, `internal/store/db_test.go`, `internal/store/testutil_test.go`

**Interfaces:**
- Consumes: `config.Config.DatabaseURL`
- Produces: `store.Store` `{ Pool *pgxpool.Pool }`, `store.Open(ctx, databaseURL string) (*Store, error)` (opens pool, applies migrations, upserts `schema_meta`)

- [ ] **Step 1: Write the failing migrate test**

```go
func TestOpenAppliesMigrations(t *testing.T) {
	st := openTest(t)
	var v int
	if err := st.Pool.QueryRow(context.Background(), `select version from schema_meta`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v < 1 {
		t.Fatalf("version=%d", v)
	}
	for _, table := range []string{"sources", "memories", "wiki_pages", "wiki_links", "revisions", "proposals", "tokens", "audit_log", "schema_meta"} {
		var n int
		q := `select count(*) from information_schema.tables where table_name=$1`
		if err := st.Pool.QueryRow(context.Background(), q, table).Scan(&n); err != nil || n != 1 {
			t.Fatalf("missing table %s: %v n=%d", table, err, n)
		}
	}
}
```

`openTest` reads `MEMORY_TEST_DATABASE_URL`, `t.Skip` if empty, `Open`s, and `t.Cleanup` truncates all tables except `schema_meta`.

- [ ] **Step 2: Run test to verify it fails**

Run: `MEMORY_TEST_DATABASE_URL=postgres://memory:memory@localhost:55432/memory?sslmode=disable go test ./internal/store/ -count=1`
Expected: FAIL — `Open` undefined. Start compose postgres first if needed: `docker compose -f deploy/compose/docker-compose.yml up -d`.

- [ ] **Step 3: Write SQL + Open**

`001_init.sql` must create all tables from the spec. Constraints:

- `sources`: unique `(scope, coalesce(project_slug,''), content_sha256)`
- `memories`: unique index on `(scope, coalesce(project_slug,''), title)` where `status='active'`
- `wiki_pages`: unique index on `(scope, coalesce(project_slug,''), slug)` where `status='active'`
- `wiki_links`: PK `(from_page, to_page, rel)` FKs to `wiki_pages(id)`
- `tokens`: `token_hash` unique
- FTS: `memories.search_tsv tsvector` generated from `coalesce(summary,'') || ' ' || coalesce(body,'')`, GIN index. Same on `wiki_pages` from title/summary/body_markdown.
- **No** vector types.

Embed migrations with `//go:embed ../../db/migrations/*.sql` and apply in filename order. Record version in `schema_meta` (single row).

- [ ] **Step 4: Run tests**

Run: `MEMORY_TEST_DATABASE_URL=... go test ./internal/store/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add db/migrations internal/store
git commit -m "feat: add Postgres schema and store bootstrap"
```

---

### Task 5: Token store

**Files:**
- Create: `internal/store/tokens.go`, `internal/store/tokens_test.go`
- Modify: `internal/store/db.go` if helpers needed

**Interfaces:**
- Consumes: `auth.Hash`, `auth.Verify`, `types.Token`
- Produces:
  - `(*Store).CreateToken(ctx, harness, label, plaintext string) (types.Token, error)` — hashes internally if you pass plaintext; **better:** `CreateToken(ctx, harness, label, hash string) (types.Token, error)`
  - `(*Store).Authenticate(ctx, plaintext string) (types.Token, error)` — returns `store.ErrUnauthorized` if missing/revoked/mismatch
  - `(*Store).ListTokens(ctx) ([]types.Token, error)` — never returns hashes in logs; struct may include hash (API layer strips it)
  - `(*Store).RevokeToken(ctx, id uuid.UUID) error`
  - `(*Store).TouchToken(ctx, id uuid.UUID) error`

- [ ] **Step 1: Write failing tests**

Cover: create + authenticate success; wrong secret → `ErrUnauthorized`; revoke → authenticate fails; two harnesses independent.

- [ ] **Step 2: Run to verify fail**

Run: `MEMORY_TEST_DATABASE_URL=... go test ./internal/store/ -run Token -count=1`
Expected: FAIL — methods undefined.

- [ ] **Step 3: Implement tokens.go**

On authenticate, load all non-revoked hashes is wrong (timing). Load is by scanning non-revoked tokens and `auth.Verify` each is OK for v1 (dozens of tokens). Update `last_used_at` on success.

- [ ] **Step 4: Run tests**

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/tokens.go internal/store/tokens_test.go
git commit -m "feat: store and authenticate per-harness tokens"
```

---

### Task 6: Memories, revisions, audit, proposals

**Files:**
- Create: `internal/store/memories.go`, `internal/store/revisions.go`, `internal/store/audit.go`, `internal/store/proposals.go`, `internal/store/memories_test.go`

**Interfaces:**
- Consumes: `writerules.DecideMemorySave`, `types.Memory`, `types.SaveResult`
- Produces:
  - `(*Store).SaveMemory(ctx, incoming types.Memory, harness string) (types.SaveResult, error)`
  - `(*Store).GetMemory(ctx, id uuid.UUID) (types.Memory, error)`
  - `(*Store).GetActiveMemoryByTitle(ctx, scope types.Scope, project, title string) (*types.Memory, error)` — nil, nil if missing
  - `(*Store).ListMemories(ctx, scope types.Scope, project string) ([]types.Memory, error)` — active only
  - `(*Store).InsertProposal(ctx, p types.Proposal) (types.Proposal, error)`
  - `(*Store).AppendRevision(...)` and `(*Store).AppendAudit(...)` used internally

`SaveMemory` in one transaction: look up existing by title; `DecideMemorySave`; if auto, upsert + revision + audit; if proposed, insert proposal (`action=update` or `create`) + audit, do not change the active row. Return `SaveResult`.

- [ ] **Step 1: Write failing tests**

1. First save applied; `GetActiveMemoryByTitle` finds it; one revision.
2. Same title + different body → status `proposed`, original body unchanged, one open proposal.
3. Same title + prefix body → applied, body updated.

- [ ] **Step 2: Run to verify fail**

Run: `MEMORY_TEST_DATABASE_URL=... go test ./internal/store/ -run Memory -count=1`
Expected: FAIL.

- [ ] **Step 3: Implement**

- [ ] **Step 4: Run tests**

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/memories.go internal/store/revisions.go internal/store/audit.go internal/store/proposals.go internal/store/memories_test.go
git commit -m "feat: save memories with revisions and conflict proposals"
```

---

### Task 7: HTTP API (health, tokens admin, memories, recall)

**Files:**
- Create: `internal/api/server.go`, `internal/api/handlers.go`, `internal/api/auth.go`, `internal/api/api_test.go`
- Modify: `cmd/memoryd/main.go`

**Interfaces:**
- Consumes: `store.Store`, `config.Config`
- Produces: `api.New(st *store.Store, cfg config.Config) http.Handler`
  - `GET /healthz` → 200 `{"ok":true}`
  - `GET /readyz` → 200 if `select 1` works else 503
  - Auth: `Authorization: Bearer <token>` required on `/v1/*`. Missing/bad → 401 `{"error":"unauthorized"}` (do not say whether harness exists).
  - Context key `api.CtxToken` → `types.Token`
  - `POST /v1/admin/tokens` `{harness,label}` → `{id,harness,plaintext}` once — **admin token only** (`token.Harness == "admin"`) else 403
  - `GET /v1/admin/tokens` list (no hashes) — admin only
  - `POST /v1/admin/tokens/{id}/revoke` — admin only
  - `POST /v1/memories` body `types.Memory` JSON (server sets harness from token) → `SaveResult`
  - `GET /v1/memories/{id}`
  - `POST /v1/recall` `{project?, query?, id?}` → `{user: []IndexLine, project: []IndexLine, recent: []Revision}` or full memory if `id` set
  - `IndexLine` `{id, kind, title, summary, href}`

- [ ] **Step 1: Write failing httptest**

Use `openTest` store, create admin + grok tokens, `httptest.NewServer(api.New(...))`.

Tests:
- `/healthz` 200 without auth
- `/v1/recall` without token → 401
- grok token `POST /v1/memories` then `POST /v1/recall` `{project:""}` includes the summary
- grok token `POST /v1/admin/tokens` → 403
- admin token can mint a token and receive plaintext

- [ ] **Step 2: Run to verify fail**

Run: `MEMORY_TEST_DATABASE_URL=... go test ./internal/api/ -count=1`
Expected: FAIL.

- [ ] **Step 3: Implement mux**

Go 1.22 `http.NewServeMux`. JSON helpers. `cmd/memoryd/main.go`: `config.Load()`, require `DatabaseURL`, `store.Open`, `ListenAndServe(cfg.Listen, api.New(...))`.

Recall without `id`: list user memories + project memories as one-line summaries. Cap later in Task 11.

- [ ] **Step 4: Run tests + build**

Run: `MEMORY_TEST_DATABASE_URL=... go test ./internal/api/ ./cmd/memoryd/ -count=1 && go build -o bin/memoryd ./cmd/memoryd`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api cmd/memoryd
git commit -m "feat: serve health, admin tokens, memories, and recall over HTTP"
```

---

### Task 8: CLI — init, token, status

**Files:**
- Create: `internal/cli/cli.go`, `internal/cli/cli_test.go`
- Modify: `cmd/memory/main.go`

**Interfaces:**
- Consumes: `config.Config`, HTTP `/v1`
- Produces: cobra-free stdlib `flag` + subcommands:
  - `memory init` — mkdir config dir, write `config.toml` (`database_url`, `listen`, `projection_dir`), wait for `/readyz`, if no admin token exists call a **bootstrap** path
  - `memory token create --harness grok [--label ...]`
  - `memory token list`
  - `memory token revoke --id <uuid>`
  - `memory status` — prints URL, ready, token harness if `MEMORY_TOKEN` set
  - `memory serve` — exec same as `memoryd` (optional: call `memoryd` main via shared `serve.Run`)

**Bootstrap problem:** first admin token cannot use `/v1/admin/tokens` (no token yet). Add `POST /v1/admin/bootstrap` that succeeds **only if** `tokens` is empty, mints harness=`admin`, returns plaintext. After that, 409. Cover this in `internal/api` tests in this task.

- [ ] **Step 1: Write failing tests**

API: bootstrap once works, second bootstrap 409.

CLI: `cli.Run([]string{"status"}, env)` against `httptest` server returns exit 0 and contains `ok`. Use a fake HTTP client interface if easier: `type Client struct { BaseURL, Token string; HTTP *http.Client }`.

- [ ] **Step 2: Run to verify fail**

- [ ] **Step 3: Implement CLI + bootstrap**

Write config toml with `github.com/pelletier/go-toml/v2` or a 10-line encoder (keys only). `memory init` prints the admin token once.

- [ ] **Step 4: Run tests**

- [ ] **Step 5: Commit**

```bash
git add internal/cli internal/api cmd/memory
git commit -m "feat: add memory CLI init, token, and status"
```

---

### Task 9: Sources (immutable ingest)

**Files:**
- Create: `internal/store/sources.go`, `internal/store/sources_test.go`
- Modify: `internal/api/handlers.go`, `internal/api/api_test.go`

**Interfaces:**
- Produces:
  - `(*Store).IngestSource(ctx, s types.Source) (types.Source, created bool, error)`
  - `(*Store).GetSource(ctx, id uuid.UUID) (types.Source, error)`
  - `POST /v1/sources` `GET /v1/sources/{id}`

Idempotent on `(scope, project_slug, sha256)`. Compute sha256 of body if client omitted it.

- [ ] **Step 1: Write failing tests** — two ingest same body → same id, `created=false` second time.
- [ ] **Step 2: Run to verify fail**
- [ ] **Step 3: Implement**
- [ ] **Step 4: Run tests**
- [ ] **Step 5: Commit**

```bash
git add internal/store/sources.go internal/store/sources_test.go internal/api
git commit -m "feat: ingest immutable sources with sha256 idempotency"
```

---

### Task 10: Wiki pages and wikilinks

**Files:**
- Create: `internal/wikilink/parse.go`, `internal/wikilink/parse_test.go`, `internal/store/pages.go`, `internal/store/links.go`, `internal/store/pages_test.go`
- Modify: `internal/api/handlers.go`

**Interfaces:**
- Produces:
  - `wikilink.Parse(body string) []wikilink.Ref` where `Ref {Slug string; Rel types.Rel}` — `[[Foo]]` → slug `foo` rel `related`; `[[uses:Bar]]` → rel `uses` slug `bar`. Slug: lowercase, spaces → `-`, strip non `[a-z0-9-]`.
  - `(*Store).WritePage(ctx, incoming types.Page, harness string) (types.SaveResult, error)` — uses `DecidePageWrite`; on auto, upsert page, replace outbound links from parse, revision+audit; on proposed, proposal only
  - `(*Store).GetPage(ctx, id uuid.UUID)` `GetActivePageBySlug(ctx, scope, project, slug)`
  - `POST /v1/pages` `GET /v1/pages/{id}`

- [ ] **Step 1: Write failing parse + page tests**

Parse: `"see [[Vault HA]] and [[depends_on:Postgres]]"` → slugs `vault-ha`, `postgres` rels related/depends_on.

Page: new page applied; rewrite with different body → proposed; rewrite adding only a sentence prefix → applied; links table populated.

- [ ] **Step 2: Run to verify fail**
- [ ] **Step 3: Implement**
- [ ] **Step 4: Run tests**
- [ ] **Step 5: Commit**

```bash
git add internal/wikilink internal/store/pages.go internal/store/links.go internal/store/pages_test.go internal/api
git commit -m "feat: write wiki pages and parse wikilinks into the graph"
```

---

### Task 11: FTS search and recall budget

**Files:**
- Create: `internal/store/search.go`, `internal/store/search_test.go`, `internal/recall/budget.go`, `internal/recall/budget_test.go`
- Modify: `internal/api/handlers.go`

**Interfaces:**
- Produces:
  - `(*Store).Search(ctx, q string, scope *types.Scope, project string, limit int) ([]types.SearchHit, error)`
  - `types.SearchHit {Kind string; ID uuid.UUID; Title, Summary string; Rank float64}`
  - `POST /v1/search` `{q, project?, scope?}`
  - `recall.Budget(lines []string, maxLines int, maxBytes int) []string` — first 200 lines or 25KB, whichever first; if truncated append `… use search for more`

Recall handler must run `Budget` on the rendered index lines.

- [ ] **Step 1: Write failing tests**

Search: ingest two memories "pipenv" and "vault raft", query `pipenv` returns pipenv first.

Budget: 201 lines of `"x\n"` → 200 + overflow line; 25KB+ of `'a'` → truncated.

- [ ] **Step 2: Run to verify fail**
- [ ] **Step 3: Implement** using `to_tsquery('simple', ...)` / `plainto_tsquery` and `ts_rank`.
- [ ] **Step 4: Run tests**
- [ ] **Step 5: Commit**

```bash
git add internal/store/search.go internal/store/search_test.go internal/recall internal/api
git commit -m "feat: add FTS search and recall index budget"
```

---

### Task 12: MCP HTTP + stdio proxy

**Files:**
- Create: `internal/mcp/http.go`, `internal/mcp/stdio.go`, `internal/mcp/mcp_test.go`
- Modify: `internal/api/server.go`, `internal/cli/cli.go`

**Interfaces:**
- Produces: MCP tools exactly: `recall`, `save`, `search`, `ingest_source`, `read_page`, `write_page`, `lint` (stub empty findings until Task 14), `inbox_list`, `inbox_propose`.
- **Forbidden:** `inbox_accept`, token admin tools.
- `POST /mcp` streamable HTTP on the same `memoryd` listener. Auth: same Bearer as `/v1`.
- `memory mcp` stdio client that forwards to `cfg.URL + "/mcp"` with `cfg.Token`.

Implement a minimal JSON-RPC 2.0 POST handler at `/mcp` (`initialize`, `tools/list`, `tools/call`, `notifications/initialized`) in `internal/mcp/jsonrpc.go`. Do **not** invent a second tool schema. Tool handlers call the same `store` methods as HTTP, not HTTP-loopback (except the stdio proxy). A later swap to `github.com/modelcontextprotocol/go-sdk` is allowed only if tests stay green and the tool list is unchanged.

- [ ] **Step 1: Write failing tests**

httptest: initialize + tools/list contains `save` and does **not** contain `inbox_accept`. `tools/call` `save` then `recall` returns the fact. Request without Bearer → 401.

- [ ] **Step 2: Run to verify fail**
- [ ] **Step 3: Implement**
- [ ] **Step 4: Run tests**
- [ ] **Step 5: Commit**

```bash
git add internal/mcp internal/api internal/cli go.mod go.sum
git commit -m "feat: expose MCP tools over HTTP and stdio proxy"
```

---

### Task 13: Projector and Claude import

**Files:**
- Create: `internal/project/project.go`, `internal/project/project_test.go`, `internal/importclaude/import.go`, `internal/importclaude/import_test.go`, `testdata/claude-memory/MEMORY.md`, `testdata/claude-memory/feedback_python_tooling.md`, `testdata/claude-memory/project_vault.md`
- Modify: `internal/cli/cli.go`

**Interfaces:**
- Produces:
  - `project.Write(dir string, memories []types.Memory, pages []types.Page) error` — `MEMORY.md` index (one markdown link line per item, run through `recall.Budget`), topic files `{kind}_{slug}.md` with body. File mode `0600`, dir `0700`. Overwrites existing.
  - `importclaude.Parse(dir string) (importclaude.Plan, error)`
  - `importclaude.Apply(ctx, st *store.Store, plan Plan, harness string) error` — harness `"import"`; one `Source` kind `import`; each topic file → `Memory` (kind from prefix `feedback_`/`project_`/`reference_`/`user_`, else `project`); if body > 2000 bytes also write a `source-summary` page
  - CLI: `memory project --out <dir>`, `memory import claude --path <dir> [--dry-run]`

- [ ] **Step 1: Write failing tests**

Projector: two memories → `MEMORY.md` contains both titles; topic file exists; budget overflow adds the search line.

Import parse of `testdata/claude-memory`: plan has 2 memories, kinds feedback+project. Dry-run apply not needed in parse test. Apply test uses `openTest`, then `GetActiveMemoryByTitle` finds "Python tooling preferences".

Fixture `MEMORY.md`:

```markdown
- [Python tooling preferences](feedback_python_tooling.md) — use pipenv
- [Vault](project_vault.md) — raft quorum
```

- [ ] **Step 2: Run to verify fail**
- [ ] **Step 3: Implement**
- [ ] **Step 4: Run tests**
- [ ] **Step 5: Commit**

```bash
git add internal/project internal/importclaude testdata internal/cli
git commit -m "feat: project MEMORY.md and import Claude auto-memory"
```

---

### Task 14: Lint and inbox accept/reject

**Files:**
- Create: `internal/lint/lint.go`, `internal/lint/lint_test.go`
- Modify: `internal/store/proposals.go`, `internal/store/proposals_test.go`, `internal/api/handlers.go`, `internal/mcp/http.go`, `internal/cli/cli.go`

**Interfaces:**
- Produces:
  - `lint.Run(ctx, st *store.Store, project string) ([]lint.Finding, error)`
  - `Finding {Kind string; Message string; PageID *uuid.UUID}` kinds: `orphan`, `broken_link`, `stale_source`, `projection_drift` (optional if dir configured)
  - `GET /v1/lint?project=`
  - MCP `lint` returns findings (still read-only)
  - `(*Store).ListOpenProposals(ctx) ([]types.Proposal, error)`
  - `(*Store).AcceptProposal(ctx, id uuid.UUID, harness string) error` — **caller must be admin**; apply payload in one tx
  - `(*Store).RejectProposal(ctx, id uuid.UUID, harness string) error`
  - `GET /v1/inbox` `POST /v1/inbox` (propose) for harness tokens
  - `POST /v1/admin/inbox/{id}/accept` `.../reject` admin only — **do not** add these to MCP
  - CLI: `memory inbox`, `memory accept <id>`, `memory reject <id>`, `memory lint`

Accept apply:
- `create`/`update` memory or page from payload JSON
- `supersede` sets old status superseded + superseded_by
- `delete` sets superseded (no SQL delete)
- `scope-move` updates scope/project_slug

- [ ] **Step 1: Write failing tests**

Lint: page with `[[missing]]` → `broken_link`; page with no inbound and no outbound to others (except its own) → `orphan`.

Inbox: save conflicting memory → proposal; accept as admin → body updated; reject leaves original.

MCP tools/list still has no accept.

- [ ] **Step 2: Run to verify fail**
- [ ] **Step 3: Implement**
- [ ] **Step 4: Run tests**
- [ ] **Step 5: Commit**

```bash
git add internal/lint internal/store/proposals.go internal/store/proposals_test.go internal/api internal/mcp internal/cli
git commit -m "feat: add lint diagnostics and admin inbox accept/reject"
```

---

### Task 15: Skill, README, GoReleaser, formula

**Files:**
- Create: `skills/harness-memory/SKILL.md`, `.goreleaser.yaml`, `Formula/harness-memory.rb`
- Modify: `README.md`, `CHANGELOG.md`, `cmd/memoryd` if needed for `brew services`

**Interfaces:**
- Produces: user-facing install story. Formula is a **template** pointing at GitHub release tarball placeholders (`url` + `sha256` filled on first tag). `brew services` runs `memoryd`. `depends_on "postgresql@16"`. Caveats: `memory init` then `memory token create --harness claude`.

Skill must tell the agent:
- Session start: call `recall` with the current project slug (repo name)
- `save` for facts/feedback/references
- `ingest_source` then `write_page` for sources; update `[[wikilinks]]`
- file good answers back
- never accept inbox; tell the user to `memory inbox`
- do not write secrets (tokens, passwords) into memory bodies

GoReleaser: builds `memoryd` and `memory` for darwin/linux amd64/arm64, checksums, changelog. SBOM if supported (`sboms` section); if the GoReleaser version lacks it, checksums only — do not block.

README: brew / compose / helm / plugin install; first-run; link spec. Remove “implementation not started”.

- [ ] **Step 1: Write a skill lint test** (optional string contains checks)

`skills/harness-memory/skill_test.go` in package `skilltest`: read SKILL.md, require it contains `recall`, `save`, `write_page`, `memory inbox`.

- [ ] **Step 2: Run to verify fail** (file missing)
- [ ] **Step 3: Write skill, README, formula, goreleaser**
- [ ] **Step 4: Run `go test ./...` and `goreleaser check` if installed**
- [ ] **Step 5: Commit**

```bash
git add skills README.md CHANGELOG.md .goreleaser.yaml Formula
git commit -m "docs: add librarian skill, install README, and brew formula"
```

---

### Task 16: Marketplace plugin in `ai-claude-plugins`

**Files (other repo `../ai-claude-plugins` or `https://github.com/Pzharyuk/ai-claude-plugins`):**
- Create: `harness-memory/.claude-plugin/plugin.json`, `harness-memory/.mcp.json`, `harness-memory/skills/configure/SKILL.md`, `harness-memory/skills/harness-memory/SKILL.md`, `harness-memory/README.md`
- Modify: `.claude-plugin/marketplace.json`, `README.md`

**Interfaces:**
- Consumes: `memoryd` `POST /mcp` + `MEMORY_URL` + `MEMORY_TOKEN`
- Produces: installable plugin `harness-memory@ai-claude-plugins`

`.mcp.json` (HTTP, same shape as `ai-business-tools`):

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

`plugin.json` `setup.env`: `MEMORY_URL` default `http://127.0.0.1:8741`, `MEMORY_TOKEN` required secret.

Configure skill: check `memory status`; if down, tell user to `brew services start harness-memory` or set `MEMORY_URL`; run `memory token create --harness claude` (or grok); persist env like Vault's configure skill (`~/.mcp.json`).

Vendor `skills/harness-memory/SKILL.md` from harness-memory (copy; note source commit SHA in plugin README). **No Node `server/`.**

Marketplace entry + README table row. Update marketplace metadata description to include shared agent memory.

- [ ] **Step 1: Add plugin files; keep JSON valid**

Validate: `python3 -m json.tool harness-memory/.mcp.json` and `plugin.json` and marketplace.json.

- [ ] **Step 2: Confirm marketplace.json lists `harness-memory` with `source: ./harness-memory`**
- [ ] **Step 3: Commit in `ai-claude-plugins`**

```bash
git add harness-memory .claude-plugin/marketplace.json README.md
git commit -m "feat: add harness-memory plugin (HTTP MCP client + skills)"
```

Do not merge this before Task 12 is on `harness-memory` main (`/mcp` exists).

---

## Self-review (spec coverage)

| Spec requirement | Task |
|---|---|
| User + project scopes | 2, 6, 7 |
| MCP + HTTP, files are projection | 7, 12, 13 |
| Tiered writes | 3, 6, 10, 14 |
| Agent librarian, no in-process LLM | 12, 15 (no worker task) |
| Go single binary, brew/compose/helm | 1, 8, 15 |
| Per-harness tokens, argon2id, shown once | 2, 5, 7, 8 |
| Import Claude memory | 13 |
| CI + Dependabot | 1 |
| Plugin in ai-claude-plugins, no Node rewrap | 16 |
| FTS, no pgvector | 4, 11 |
| Revisions, supersede not delete | 6, 14 |
| Lint read-only, accept CLI/admin only | 14 |
| Recall 200 lines / 25KB | 11 |
| `memory mcp` stdio | 12 |
| `memory token create` | 8 |
| Default listen 127.0.0.1:8741 | 1 |
| Failure: 401, tx rollback, idempotent ingest | 5, 6, 9 |
| Skill schema | 15 |
| GoReleaser + formula | 15 |

No compile worker. No OIDC. No web UI.
