package types

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Scope string

const (
	ScopeUser    Scope = "user"
	ScopeProject Scope = "project"
)

type MemoryKind string

const (
	MemoryKindUser      MemoryKind = "user"
	MemoryKindFeedback  MemoryKind = "feedback"
	MemoryKindProject   MemoryKind = "project"
	MemoryKindReference MemoryKind = "reference"
)

type Status string

const (
	StatusActive     Status = "active"
	StatusSuperseded Status = "superseded"
)

type PageType string

const (
	PageTypeEntity        PageType = "entity"
	PageTypeConcept       PageType = "concept"
	PageTypeSourceSummary PageType = "source-summary"
	PageTypeIndex         PageType = "index"
	PageTypeLog           PageType = "log"
	PageTypeSynthesis     PageType = "synthesis"
)

type Rel string

const (
	RelRelated     Rel = "related"
	RelUses        Rel = "uses"
	RelDependsOn   Rel = "depends_on"
	RelSupersedes  Rel = "supersedes"
	RelContradicts Rel = "contradicts"
)

type SourceKind string

const (
	SourceKindImport  SourceKind = "import"
	SourceKindFile    SourceKind = "file"
	SourceKindURL     SourceKind = "url"
	SourceKindSession SourceKind = "session"
)

type ProposalAction string

const (
	ProposalActionCreate    ProposalAction = "create"
	ProposalActionUpdate    ProposalAction = "update"
	ProposalActionSupersede ProposalAction = "supersede"
	ProposalActionDelete    ProposalAction = "delete"
	ProposalActionScopeMove ProposalAction = "scope-move"
)

type ProposalStatus string

const (
	ProposalStatusOpen     ProposalStatus = "open"
	ProposalStatusAccepted ProposalStatus = "accepted"
	ProposalStatusRejected ProposalStatus = "rejected"
)

type WritePath string

const (
	PathAuto     WritePath = "auto"
	PathProposed WritePath = "proposed"
)

// Memory is a Claude-style fact on the auto-write path.
// ProjectSlug is "" for user scope.
type Memory struct {
	ID               uuid.UUID  `json:"id"`
	Scope            Scope      `json:"scope"`
	ProjectSlug      string     `json:"project_slug"`
	Kind             MemoryKind `json:"kind"`
	Title            string     `json:"title"`
	Summary          string     `json:"summary"`
	Body             string     `json:"body"`
	SourceID         *uuid.UUID `json:"source_id,omitempty"`
	Status           Status     `json:"status"`
	SupersededBy     *uuid.UUID `json:"superseded_by,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	CreatedByHarness string     `json:"created_by_harness"`
	UpdatedByHarness string     `json:"updated_by_harness"`
}

// Source is an immutable raw ingest record.
// ProjectSlug is "" for user scope.
type Source struct {
	ID               uuid.UUID  `json:"id"`
	Scope            Scope      `json:"scope"`
	ProjectSlug      string     `json:"project_slug"`
	Kind             SourceKind `json:"kind"`
	Title            string     `json:"title"`
	Body             string     `json:"body"`
	ContentSHA256    string     `json:"content_sha256"`
	CreatedAt        time.Time  `json:"created_at"`
	CreatedByHarness string     `json:"created_by_harness"`
}

// Page is a compiled wiki page.
// ProjectSlug is "" for user scope.
type Page struct {
	ID               uuid.UUID   `json:"id"`
	Scope            Scope       `json:"scope"`
	ProjectSlug      string      `json:"project_slug"`
	Slug             string      `json:"slug"`
	Title            string      `json:"title"`
	Summary          string      `json:"summary"`
	BodyMarkdown     string      `json:"body_markdown"`
	PageType         PageType    `json:"page_type"`
	Status           Status      `json:"status"`
	SupersededBy     *uuid.UUID  `json:"superseded_by,omitempty"`
	SourceIDs        []uuid.UUID `json:"source_ids,omitempty"`
	UpdatedAt        time.Time   `json:"updated_at"`
	UpdatedByHarness string      `json:"updated_by_harness"`
}

// Link is a directed edge between wiki pages.
type Link struct {
	FromPage uuid.UUID `json:"from_page"`
	ToPage   uuid.UUID `json:"to_page"`
	Rel      Rel       `json:"rel"`
}

// Proposal is an inbox item for review-gated writes.
type Proposal struct {
	ID               uuid.UUID       `json:"id"`
	Action           ProposalAction  `json:"action"`
	Payload          json.RawMessage `json:"payload"`
	Reason           string          `json:"reason"`
	Status           ProposalStatus  `json:"status"`
	CreatedByHarness string          `json:"created_by_harness"`
	CreatedAt        time.Time       `json:"created_at"`
}

// Token is a per-harness API credential (hash only; plaintext shown once).
type Token struct {
	ID         uuid.UUID  `json:"id"`
	Harness    string     `json:"harness"`
	TokenHash  string     `json:"token_hash,omitempty"`
	Label      string     `json:"label"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

// Revision is an append-only before/after snapshot for an entity write.
type Revision struct {
	ID         uuid.UUID       `json:"id"`
	EntityType string          `json:"entity_type"`
	EntityID   uuid.UUID       `json:"entity_id"`
	Before     json.RawMessage `json:"before,omitempty"`
	After      json.RawMessage `json:"after,omitempty"`
	Harness    string          `json:"harness"`
	Reason     string          `json:"reason"`
	At         time.Time       `json:"at"`
}

// SaveResult is returned by tiered write paths.
// Status is "applied" or "proposed".
type SaveResult struct {
	Status     string     `json:"status"`
	ID         uuid.UUID  `json:"id"`
	ProposalID *uuid.UUID `json:"proposal_id,omitempty"`
}

const (
	SaveStatusApplied  = "applied"
	SaveStatusProposed = "proposed"
)

// SearchHit is one FTS match from memories or wiki pages.
type SearchHit struct {
	Kind    string    `json:"kind"`
	ID      uuid.UUID `json:"id"`
	Title   string    `json:"title"`
	Summary string    `json:"summary"`
	Rank    float64   `json:"rank"`
}
