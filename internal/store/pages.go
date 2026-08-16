package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Pzharyuk/harness-memory/internal/types"
	"github.com/Pzharyuk/harness-memory/internal/wikilink"
	"github.com/Pzharyuk/harness-memory/internal/writerules"
)

const pageCols = `
	id, scope, coalesce(project_slug, ''), slug, title, summary, body_markdown,
	page_type, status, superseded_by, source_ids, updated_at, updated_by_harness
`

func scanPage(sc interface{ Scan(dest ...any) error }) (types.Page, error) {
	var p types.Page
	err := sc.Scan(
		&p.ID,
		&p.Scope,
		&p.ProjectSlug,
		&p.Slug,
		&p.Title,
		&p.Summary,
		&p.BodyMarkdown,
		&p.PageType,
		&p.Status,
		&p.SupersededBy,
		&p.SourceIDs,
		&p.UpdatedAt,
		&p.UpdatedByHarness,
	)
	if err != nil {
		return types.Page{}, err
	}
	if p.SourceIDs == nil {
		p.SourceIDs = []uuid.UUID{}
	}
	return p, nil
}

func validatePageWrite(p *types.Page, harness string) error {
	if harness == "" {
		return fmt.Errorf("harness is required")
	}
	p.Slug = wikilink.Slugify(p.Slug)
	if p.Slug == "" {
		p.Slug = wikilink.Slugify(p.Title)
	}
	if p.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	if p.Title == "" {
		return fmt.Errorf("title is required")
	}
	switch p.Scope {
	case types.ScopeUser, types.ScopeProject:
	default:
		return fmt.Errorf("invalid scope %q", p.Scope)
	}
	switch p.PageType {
	case types.PageTypeEntity, types.PageTypeConcept, types.PageTypeSourceSummary,
		types.PageTypeIndex, types.PageTypeLog, types.PageTypeSynthesis:
	default:
		return fmt.Errorf("invalid page_type %q", p.PageType)
	}
	if p.SourceIDs == nil {
		p.SourceIDs = []uuid.UUID{}
	}
	return nil
}

// WritePage looks up the active row by slug, applies writerules.DecidePageWrite,
// and in one transaction either upserts (auto) or opens a proposal (proposed).
// Auto also replaces outbound wiki_links parsed from body_markdown.
func (s *Store) WritePage(ctx context.Context, incoming types.Page, harness string) (types.SaveResult, error) {
	if err := validatePageWrite(&incoming, harness); err != nil {
		return types.SaveResult{}, err
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return types.SaveResult{}, fmt.Errorf("begin write page: %w", err)
	}
	defer tx.Rollback(ctx)

	existing, err := getActivePageBySlug(ctx, tx, incoming.Scope, incoming.ProjectSlug, incoming.Slug, true)
	if err != nil {
		return types.SaveResult{}, err
	}
	d := writerules.DecidePageWrite(existing, incoming, hasContradictsLink(incoming.BodyMarkdown))

	switch d.Path {
	case types.PathAuto:
		page, err := upsertPage(ctx, tx, existing, incoming, harness)
		if err != nil {
			return types.SaveResult{}, err
		}
		if err := replaceOutboundLinks(ctx, tx, page); err != nil {
			return types.SaveResult{}, err
		}
		var before json.RawMessage
		if existing != nil {
			before, err = json.Marshal(existing)
			if err != nil {
				return types.SaveResult{}, fmt.Errorf("marshal revision before: %w", err)
			}
		}
		after, err := json.Marshal(page)
		if err != nil {
			return types.SaveResult{}, fmt.Errorf("marshal revision after: %w", err)
		}
		if _, err := appendRevision(ctx, tx, types.Revision{
			EntityType: "page",
			EntityID:   page.ID,
			Before:     before,
			After:      after,
			Harness:    harness,
			Reason:     d.Reason,
		}); err != nil {
			return types.SaveResult{}, err
		}
		if err := appendAudit(ctx, tx, harness, "page.save", page.ID.String()); err != nil {
			return types.SaveResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return types.SaveResult{}, fmt.Errorf("commit write page: %w", err)
		}
		return types.SaveResult{Status: types.SaveStatusApplied, ID: page.ID}, nil

	case types.PathProposed:
		action := types.ProposalActionCreate
		payloadPage := incoming
		id := incoming.ID
		if existing != nil {
			action = types.ProposalActionUpdate
			payloadPage.ID = existing.ID
			id = existing.ID
		}
		payload, err := json.Marshal(payloadPage)
		if err != nil {
			return types.SaveResult{}, fmt.Errorf("marshal proposal: %w", err)
		}
		p, err := insertProposal(ctx, tx, types.Proposal{
			Action:           action,
			Payload:          payload,
			Reason:           d.Reason,
			Status:           types.ProposalStatusOpen,
			CreatedByHarness: harness,
		})
		if err != nil {
			return types.SaveResult{}, err
		}
		entity := p.ID.String()
		if id != uuid.Nil {
			entity = id.String()
		}
		if err := appendAudit(ctx, tx, harness, "page.propose", entity); err != nil {
			return types.SaveResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return types.SaveResult{}, fmt.Errorf("commit propose page: %w", err)
		}
		pid := p.ID
		return types.SaveResult{
			Status:     types.SaveStatusProposed,
			ID:         id,
			ProposalID: &pid,
		}, nil

	default:
		return types.SaveResult{}, fmt.Errorf("unknown write path %q", d.Path)
	}
}

func upsertPage(ctx context.Context, q querier, existing *types.Page, incoming types.Page, harness string) (types.Page, error) {
	if existing == nil {
		return insertPage(ctx, q, incoming, harness)
	}
	return updatePage(ctx, q, existing.ID, incoming, harness)
}

func insertPage(ctx context.Context, q querier, incoming types.Page, harness string) (types.Page, error) {
	p, err := scanPage(q.QueryRow(ctx, `
		INSERT INTO wiki_pages (
			scope, project_slug, slug, title, summary, body_markdown, page_type,
			source_ids, updated_by_harness
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING `+pageCols, incoming.Scope, projectArg(incoming.ProjectSlug), incoming.Slug,
		incoming.Title, incoming.Summary, incoming.BodyMarkdown, incoming.PageType,
		incoming.SourceIDs, harness))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return types.Page{}, fmt.Errorf("active page already exists for slug")
		}
		return types.Page{}, fmt.Errorf("insert page: %w", err)
	}
	return p, nil
}

func updatePage(ctx context.Context, q querier, id uuid.UUID, incoming types.Page, harness string) (types.Page, error) {
	p, err := scanPage(q.QueryRow(ctx, `
		UPDATE wiki_pages SET
			title = $2,
			summary = $3,
			body_markdown = $4,
			page_type = $5,
			source_ids = $6,
			updated_at = now(),
			updated_by_harness = $7
		WHERE id = $1 AND status = 'active'
		RETURNING `+pageCols, id, incoming.Title, incoming.Summary, incoming.BodyMarkdown,
		incoming.PageType, incoming.SourceIDs, harness))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return types.Page{}, fmt.Errorf("page not found")
		}
		return types.Page{}, fmt.Errorf("update page: %w", err)
	}
	return p, nil
}

func getActivePageBySlug(ctx context.Context, q querier, scope types.Scope, project, slug string, forUpdate bool) (*types.Page, error) {
	sql := `SELECT ` + pageCols + `
		FROM wiki_pages
		WHERE status = 'active'
		  AND scope = $1
		  AND coalesce(project_slug, '') = $2
		  AND slug = $3`
	if forUpdate {
		sql += ` FOR UPDATE`
	}
	p, err := scanPage(q.QueryRow(ctx, sql, scope, project, slug))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get active page by slug: %w", err)
	}
	return &p, nil
}

// GetPage returns a wiki page by id (any status).
func (s *Store) GetPage(ctx context.Context, id uuid.UUID) (types.Page, error) {
	p, err := scanPage(s.Pool.QueryRow(ctx, `SELECT `+pageCols+` FROM wiki_pages WHERE id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return types.Page{}, fmt.Errorf("page not found")
		}
		return types.Page{}, fmt.Errorf("get page: %w", err)
	}
	return p, nil
}

// GetActivePageBySlug returns the active page for (scope, project, slug).
// Missing row is (nil, nil).
func (s *Store) GetActivePageBySlug(ctx context.Context, scope types.Scope, project, slug string) (*types.Page, error) {
	return getActivePageBySlug(ctx, s.Pool, scope, project, slug, false)
}
