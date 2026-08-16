---
name: harness-memory
description: Librarian for the shared harness-memory store. Use at session start, when saving facts, ingesting sources, writing wiki pages, searching, or when the user mentions memory or the inbox.
---

# Harness Memory

The server (`memoryd`) is the shelf. You are the librarian.

MCP tools talk to `POST /mcp` with a Bearer token. You may create proposals. You may never accept them.

## Session start

Call `recall` with the current project slug (the git repo name) before answering.

- User-scope index + this project's index + a short recent-revisions tail.
- One line per memory/page. Fetch a full body with `recall` by `id` or `read_page`.
- If the user names another project, recall that slug instead.

## Save facts

Use `save` for facts, feedback, and references.

| kind | When |
|---|---|
| `user` | Preferences, tooling, infra notes that travel across repos |
| `feedback` | "Don't do X again" / "this approach worked" |
| `project` | Facts about this repo |
| `reference` | Pointers to docs, tickets, URLs |

Auto-writes apply when the title is new or the body is a prefix/equal update. A colliding different body becomes a proposal — do not retry-force it.

**Do not write secrets** (tokens, passwords, API keys, private keys) into memory bodies.

## Ingest then compile

For a source (file, URL, session dump, import):

1. `ingest_source` — immutable raw. Same sha256 + scope + project is idempotent.
2. Read the source and nearby pages (`search`, `read_page`).
3. `write_page` — compiled wiki. New or non-conflicting edits apply. Contradictions become proposals.

Keep `[[wikilinks]]` current in page bodies. The graph is parsed on write.

Page types: `entity`, `concept`, `source-summary`, `index`, `log`, `synthesis`.

## File good answers back

If you produced a durable answer (a diagnosis, a topology, a preference), file it:

- short fact → `save`
- compiled write-up with links → `write_page` (after `ingest_source` if there is a raw source)

Do not leave the only copy in the chat.

## Query

`search` is FTS across memories and pages. Cite page/memory ids in the answer. Worth keeping → file back.

`lint` is read-only (`orphan`, `broken_link`, `stale_source`, `projection_drift`). Fixes are proposals, not silent rewrites.

## Inbox

Never accept inbox. There is no accept tool on MCP.

If a write returns `proposed`, or the user asks about pending changes, tell them to run `memory inbox` and then `memory accept <id>` / `memory reject <id>` with the admin token.

You may `inbox_list` and `inbox_propose`. You may not accept your own proposals.

## Do not

- Write tokens, passwords, or other secrets into bodies.
- Accept or reject proposals.
- Invent facts that are not in recall/search/pages or the current conversation.
- Bulk-rewrite the wiki to "clean it up" — file a proposal and tell the user.
