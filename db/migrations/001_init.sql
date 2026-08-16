-- v1 schema. No pgvector and no embedding columns.

CREATE TABLE schema_meta (
    id          integer PRIMARY KEY CHECK (id = 1),
    version     integer NOT NULL,
    instance_id uuid    NOT NULL DEFAULT gen_random_uuid()
);

CREATE TABLE sources (
    id                 uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    scope              text        NOT NULL CHECK (scope IN ('user', 'project')),
    project_slug       text,
    kind               text        NOT NULL CHECK (kind IN ('import', 'file', 'url', 'session')),
    title              text        NOT NULL DEFAULT '',
    body               text        NOT NULL DEFAULT '',
    content_sha256     text        NOT NULL,
    created_at         timestamptz NOT NULL DEFAULT now(),
    created_by_harness text        NOT NULL
);

CREATE UNIQUE INDEX sources_scope_project_sha256_uidx
    ON sources (scope, (coalesce(project_slug, '')), content_sha256);

CREATE TABLE memories (
    id                 uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    scope              text        NOT NULL CHECK (scope IN ('user', 'project')),
    project_slug       text,
    kind               text        NOT NULL CHECK (kind IN ('user', 'feedback', 'project', 'reference')),
    title              text        NOT NULL,
    summary            text        NOT NULL DEFAULT '',
    body               text        NOT NULL DEFAULT '',
    source_id          uuid        REFERENCES sources (id),
    status             text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'superseded')),
    superseded_by      uuid        REFERENCES memories (id),
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    created_by_harness text        NOT NULL,
    updated_by_harness text        NOT NULL,
    search_tsv         tsvector    GENERATED ALWAYS AS (
        to_tsvector('simple', coalesce(summary, '') || ' ' || coalesce(body, ''))
    ) STORED
);

CREATE UNIQUE INDEX memories_active_title_uidx
    ON memories (scope, (coalesce(project_slug, '')), title)
    WHERE status = 'active';

CREATE INDEX memories_search_tsv_idx ON memories USING GIN (search_tsv);

CREATE TABLE wiki_pages (
    id                 uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    scope              text        NOT NULL CHECK (scope IN ('user', 'project')),
    project_slug       text,
    slug               text        NOT NULL,
    title              text        NOT NULL,
    summary            text        NOT NULL DEFAULT '',
    body_markdown      text        NOT NULL DEFAULT '',
    page_type          text        NOT NULL CHECK (page_type IN (
        'entity', 'concept', 'source-summary', 'index', 'log', 'synthesis'
    )),
    status             text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'superseded')),
    superseded_by      uuid        REFERENCES wiki_pages (id),
    source_ids         uuid[]      NOT NULL DEFAULT '{}',
    updated_at         timestamptz NOT NULL DEFAULT now(),
    updated_by_harness text        NOT NULL,
    search_tsv         tsvector    GENERATED ALWAYS AS (
        to_tsvector(
            'simple',
            coalesce(title, '') || ' ' || coalesce(summary, '') || ' ' || coalesce(body_markdown, '')
        )
    ) STORED
);

CREATE UNIQUE INDEX wiki_pages_active_slug_uidx
    ON wiki_pages (scope, (coalesce(project_slug, '')), slug)
    WHERE status = 'active';

CREATE INDEX wiki_pages_search_tsv_idx ON wiki_pages USING GIN (search_tsv);

CREATE TABLE wiki_links (
    from_page uuid NOT NULL REFERENCES wiki_pages (id),
    to_page   uuid NOT NULL REFERENCES wiki_pages (id),
    rel       text NOT NULL CHECK (rel IN ('related', 'uses', 'depends_on', 'supersedes', 'contradicts')),
    PRIMARY KEY (from_page, to_page, rel)
);

CREATE TABLE revisions (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_type text        NOT NULL,
    entity_id   uuid        NOT NULL,
    before      jsonb,
    after       jsonb,
    harness     text        NOT NULL,
    reason      text        NOT NULL DEFAULT '',
    at          timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE proposals (
    id                 uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    action             text        NOT NULL CHECK (action IN (
        'create', 'update', 'supersede', 'delete', 'scope-move'
    )),
    payload            jsonb       NOT NULL,
    reason             text        NOT NULL DEFAULT '',
    status             text        NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'accepted', 'rejected')),
    created_by_harness text        NOT NULL,
    created_at         timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE tokens (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    harness     text        NOT NULL,
    token_hash  text        NOT NULL UNIQUE,
    label       text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    revoked_at  timestamptz
);

CREATE TABLE audit_log (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id text,
    harness    text        NOT NULL,
    action     text        NOT NULL,
    entity     text,
    at         timestamptz NOT NULL DEFAULT now()
);
