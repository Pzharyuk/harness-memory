package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Pzharyuk/harness-memory/internal/types"
)

func scanProposal(sc interface{ Scan(dest ...any) error }) (types.Proposal, error) {
	var p types.Proposal
	err := sc.Scan(
		&p.ID,
		&p.Action,
		&p.Payload,
		&p.Reason,
		&p.Status,
		&p.CreatedByHarness,
		&p.CreatedAt,
	)
	return p, err
}

func insertProposal(ctx context.Context, q querier, p types.Proposal) (types.Proposal, error) {
	if p.Action == "" {
		return types.Proposal{}, fmt.Errorf("proposal action is required")
	}
	if p.CreatedByHarness == "" {
		return types.Proposal{}, fmt.Errorf("proposal harness is required")
	}
	// Clients (HTTP / MCP) must not insert accepted or rejected rows.
	p.Status = types.ProposalStatusOpen
	payload := p.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	out, err := scanProposal(q.QueryRow(ctx, `
		INSERT INTO proposals (action, payload, reason, status, created_by_harness)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, action, payload, reason, status, created_by_harness, created_at
	`, p.Action, payload, p.Reason, p.Status, p.CreatedByHarness))
	if err != nil {
		return types.Proposal{}, fmt.Errorf("insert proposal: %w", err)
	}
	return out, nil
}

// InsertProposal files an inbox item. Status is always open; client status is ignored.
func (s *Store) InsertProposal(ctx context.Context, p types.Proposal) (types.Proposal, error) {
	return insertProposal(ctx, s.Pool, p)
}

// ListOpenProposals returns inbox items with status open, oldest first.
func (s *Store) ListOpenProposals(ctx context.Context) ([]types.Proposal, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, action, payload, reason, status, created_by_harness, created_at
		FROM proposals
		WHERE status = 'open'
		ORDER BY created_at ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list open proposals: %w", err)
	}
	defer rows.Close()

	out := []types.Proposal{}
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, fmt.Errorf("list open proposals: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list open proposals: %w", err)
	}
	return out, nil
}

func getProposal(ctx context.Context, q querier, id uuid.UUID, forUpdate bool) (types.Proposal, error) {
	sql := `
		SELECT id, action, payload, reason, status, created_by_harness, created_at
		FROM proposals
		WHERE id = $1`
	if forUpdate {
		sql += ` FOR UPDATE`
	}
	p, err := scanProposal(q.QueryRow(ctx, sql, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return types.Proposal{}, fmt.Errorf("proposal not found")
		}
		return types.Proposal{}, fmt.Errorf("get proposal: %w", err)
	}
	return p, nil
}

func setProposalStatus(ctx context.Context, q querier, id uuid.UUID, status types.ProposalStatus) error {
	tag, err := q.Exec(ctx, `
		UPDATE proposals SET status = $2
		WHERE id = $1 AND status = 'open'
	`, id, status)
	if err != nil {
		return fmt.Errorf("set proposal status: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("proposal is not open")
	}
	return nil
}

// AcceptProposal applies an open proposal in one transaction. harness must be admin.
func (s *Store) AcceptProposal(ctx context.Context, id uuid.UUID, harness string) error {
	if harness != "admin" {
		return fmt.Errorf("accept requires admin")
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin accept proposal: %w", err)
	}
	defer tx.Rollback(ctx)

	p, err := getProposal(ctx, tx, id, true)
	if err != nil {
		return err
	}
	if p.Status != types.ProposalStatusOpen {
		return fmt.Errorf("proposal is not open")
	}
	if err := s.applyProposal(ctx, tx, p, harness); err != nil {
		return err
	}
	if err := setProposalStatus(ctx, tx, id, types.ProposalStatusAccepted); err != nil {
		return err
	}
	if err := appendAudit(ctx, tx, harness, "proposal.accept", id.String()); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit accept proposal: %w", err)
	}
	return nil
}

// RejectProposal marks an open proposal rejected. The original entity is unchanged.
func (s *Store) RejectProposal(ctx context.Context, id uuid.UUID, harness string) error {
	if harness == "" {
		return fmt.Errorf("harness is required")
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin reject proposal: %w", err)
	}
	defer tx.Rollback(ctx)

	p, err := getProposal(ctx, tx, id, true)
	if err != nil {
		return err
	}
	if p.Status != types.ProposalStatusOpen {
		return fmt.Errorf("proposal is not open")
	}
	if err := setProposalStatus(ctx, tx, id, types.ProposalStatusRejected); err != nil {
		return err
	}
	if err := appendAudit(ctx, tx, harness, "proposal.reject", p.ID.String()); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit reject proposal: %w", err)
	}
	return nil
}

func (s *Store) applyProposal(ctx context.Context, tx pgx.Tx, p types.Proposal, harness string) error {
	switch p.Action {
	case types.ProposalActionCreate, types.ProposalActionUpdate:
		return s.applyCreateOrUpdate(ctx, tx, p, harness)
	case types.ProposalActionSupersede:
		return s.applySupersede(ctx, tx, p, harness)
	case types.ProposalActionDelete:
		return s.applyDelete(ctx, tx, p, harness)
	case types.ProposalActionScopeMove:
		return s.applyScopeMove(ctx, tx, p, harness)
	default:
		return fmt.Errorf("unknown proposal action %q", p.Action)
	}
}

func (s *Store) applyCreateOrUpdate(ctx context.Context, tx pgx.Tx, p types.Proposal, harness string) error {
	kind, mem, page, err := decodeEntity(p.Payload)
	if err != nil {
		return err
	}
	switch kind {
	case "page":
		if p.Action == types.ProposalActionCreate || page.ID == uuid.Nil {
			return applyPageCreate(ctx, tx, page, harness, p.Reason)
		}
		return applyPageUpdate(ctx, tx, page, harness, p.Reason)
	case "memory":
		if p.Action == types.ProposalActionCreate || mem.ID == uuid.Nil {
			return applyMemoryCreate(ctx, tx, mem, harness, p.Reason)
		}
		return applyMemoryUpdate(ctx, tx, mem, harness, p.Reason)
	default:
		return fmt.Errorf("unknown payload entity")
	}
}

func (s *Store) applySupersede(ctx context.Context, tx pgx.Tx, p types.Proposal, harness string) error {
	var payload struct {
		ID           uuid.UUID  `json:"id"`
		SupersededBy *uuid.UUID `json:"superseded_by"`
	}
	if err := json.Unmarshal(p.Payload, &payload); err != nil {
		return fmt.Errorf("invalid payload")
	}
	if payload.ID == uuid.Nil {
		return fmt.Errorf("payload id is required")
	}
	if payload.SupersededBy == nil || *payload.SupersededBy == uuid.Nil {
		return fmt.Errorf("superseded_by is required")
	}
	kind, err := s.resolveEntityKind(ctx, payload.ID)
	if err != nil {
		return err
	}
	if kind == "page" {
		return supersedePage(ctx, tx, payload.ID, payload.SupersededBy, harness, p.Reason)
	}
	return supersedeMemory(ctx, tx, payload.ID, payload.SupersededBy, harness, p.Reason)
}

func (s *Store) applyDelete(ctx context.Context, tx pgx.Tx, p types.Proposal, harness string) error {
	var payload struct {
		ID uuid.UUID `json:"id"`
	}
	if err := json.Unmarshal(p.Payload, &payload); err != nil {
		return fmt.Errorf("invalid payload")
	}
	if payload.ID == uuid.Nil {
		return fmt.Errorf("payload id is required")
	}
	kind, err := s.resolveEntityKind(ctx, payload.ID)
	if err != nil {
		return err
	}
	if kind == "page" {
		return supersedePage(ctx, tx, payload.ID, nil, harness, p.Reason)
	}
	return supersedeMemory(ctx, tx, payload.ID, nil, harness, p.Reason)
}

func (s *Store) applyScopeMove(ctx context.Context, tx pgx.Tx, p types.Proposal, harness string) error {
	var payload struct {
		ID          uuid.UUID   `json:"id"`
		Scope       types.Scope `json:"scope"`
		ProjectSlug string      `json:"project_slug"`
	}
	if err := json.Unmarshal(p.Payload, &payload); err != nil {
		return fmt.Errorf("invalid payload")
	}
	if payload.ID == uuid.Nil {
		return fmt.Errorf("payload id is required")
	}
	if payload.Scope != types.ScopeUser && payload.Scope != types.ScopeProject {
		return fmt.Errorf("invalid scope %q", payload.Scope)
	}
	kind, err := s.resolveEntityKind(ctx, payload.ID)
	if err != nil {
		return err
	}
	if kind == "page" {
		return movePageScope(ctx, tx, payload.ID, payload.Scope, payload.ProjectSlug, harness, p.Reason)
	}
	return moveMemoryScope(ctx, tx, payload.ID, payload.Scope, payload.ProjectSlug, harness, p.Reason)
}

func (s *Store) resolveEntityKind(ctx context.Context, id uuid.UUID) (string, error) {
	if _, err := s.GetMemory(ctx, id); err == nil {
		return "memory", nil
	} else if !strings.Contains(err.Error(), "memory not found") {
		return "", err
	}
	if _, err := s.GetPage(ctx, id); err == nil {
		return "page", nil
	} else if !strings.Contains(err.Error(), "page not found") {
		return "", err
	}
	return "", fmt.Errorf("entity not found")
}

func decodeEntity(raw json.RawMessage) (string, types.Memory, types.Page, error) {
	var peek struct {
		PageType     string `json:"page_type"`
		BodyMarkdown string `json:"body_markdown"`
		Slug         string `json:"slug"`
		Kind         string `json:"kind"`
		Title        string `json:"title"`
		Body         string `json:"body"`
		ID           uuid.UUID
	}
	if err := json.Unmarshal(raw, &peek); err != nil {
		return "", types.Memory{}, types.Page{}, fmt.Errorf("invalid payload")
	}
	if peek.PageType != "" || peek.BodyMarkdown != "" || peek.Slug != "" {
		var page types.Page
		if err := json.Unmarshal(raw, &page); err != nil {
			return "", types.Memory{}, types.Page{}, fmt.Errorf("invalid page payload")
		}
		return "page", types.Memory{}, page, nil
	}
	var mem types.Memory
	if err := json.Unmarshal(raw, &mem); err != nil {
		return "", types.Memory{}, types.Page{}, fmt.Errorf("invalid memory payload")
	}
	if mem.ID != uuid.Nil || mem.Title != "" || mem.Kind != "" || mem.Body != "" {
		return "memory", mem, types.Page{}, nil
	}
	return "", types.Memory{}, types.Page{}, fmt.Errorf("unknown payload entity")
}

func applyMemoryCreate(ctx context.Context, q querier, incoming types.Memory, harness, reason string) error {
	if err := validateMemoryWrite(incoming, harness); err != nil {
		return err
	}
	mem, err := insertMemory(ctx, q, incoming, harness)
	if err != nil {
		return err
	}
	after, err := json.Marshal(mem)
	if err != nil {
		return fmt.Errorf("marshal revision after: %w", err)
	}
	_, err = appendRevision(ctx, q, types.Revision{
		EntityType: "memory",
		EntityID:   mem.ID,
		After:      after,
		Harness:    harness,
		Reason:     reason,
	})
	return err
}

func applyMemoryUpdate(ctx context.Context, q querier, incoming types.Memory, harness, reason string) error {
	if err := validateMemoryWrite(incoming, harness); err != nil {
		return err
	}
	existing, err := getMemoryByID(ctx, q, incoming.ID)
	if err != nil {
		return err
	}
	before, err := json.Marshal(existing)
	if err != nil {
		return fmt.Errorf("marshal revision before: %w", err)
	}
	mem, err := updateMemory(ctx, q, incoming.ID, incoming, harness)
	if err != nil {
		return err
	}
	after, err := json.Marshal(mem)
	if err != nil {
		return fmt.Errorf("marshal revision after: %w", err)
	}
	_, err = appendRevision(ctx, q, types.Revision{
		EntityType: "memory",
		EntityID:   mem.ID,
		Before:     before,
		After:      after,
		Harness:    harness,
		Reason:     reason,
	})
	return err
}

func applyPageCreate(ctx context.Context, q querier, incoming types.Page, harness, reason string) error {
	if err := validatePageWrite(&incoming, harness); err != nil {
		return err
	}
	page, err := insertPage(ctx, q, incoming, harness)
	if err != nil {
		return err
	}
	if err := replaceOutboundLinks(ctx, q, page); err != nil {
		return err
	}
	after, err := json.Marshal(page)
	if err != nil {
		return fmt.Errorf("marshal revision after: %w", err)
	}
	_, err = appendRevision(ctx, q, types.Revision{
		EntityType: "page",
		EntityID:   page.ID,
		After:      after,
		Harness:    harness,
		Reason:     reason,
	})
	return err
}

func applyPageUpdate(ctx context.Context, q querier, incoming types.Page, harness, reason string) error {
	if err := validatePageWrite(&incoming, harness); err != nil {
		return err
	}
	existing, err := getPageByID(ctx, q, incoming.ID)
	if err != nil {
		return err
	}
	before, err := json.Marshal(existing)
	if err != nil {
		return fmt.Errorf("marshal revision before: %w", err)
	}
	page, err := updatePage(ctx, q, incoming.ID, incoming, harness)
	if err != nil {
		return err
	}
	if err := replaceOutboundLinks(ctx, q, page); err != nil {
		return err
	}
	after, err := json.Marshal(page)
	if err != nil {
		return fmt.Errorf("marshal revision after: %w", err)
	}
	_, err = appendRevision(ctx, q, types.Revision{
		EntityType: "page",
		EntityID:   page.ID,
		Before:     before,
		After:      after,
		Harness:    harness,
		Reason:     reason,
	})
	return err
}

func supersedeMemory(ctx context.Context, q querier, id uuid.UUID, by *uuid.UUID, harness, reason string) error {
	existing, err := getMemoryByID(ctx, q, id)
	if err != nil {
		return err
	}
	before, err := json.Marshal(existing)
	if err != nil {
		return fmt.Errorf("marshal revision before: %w", err)
	}
	mem, err := scanMemory(q.QueryRow(ctx, `
		UPDATE memories SET
			status = 'superseded',
			superseded_by = $2,
			updated_at = now(),
			updated_by_harness = $3
		WHERE id = $1 AND status = 'active'
		RETURNING `+memoryCols, id, by, harness))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("memory not found")
		}
		return fmt.Errorf("supersede memory: %w", err)
	}
	after, err := json.Marshal(mem)
	if err != nil {
		return fmt.Errorf("marshal revision after: %w", err)
	}
	_, err = appendRevision(ctx, q, types.Revision{
		EntityType: "memory",
		EntityID:   mem.ID,
		Before:     before,
		After:      after,
		Harness:    harness,
		Reason:     reason,
	})
	return err
}

func supersedePage(ctx context.Context, q querier, id uuid.UUID, by *uuid.UUID, harness, reason string) error {
	existing, err := getPageByID(ctx, q, id)
	if err != nil {
		return err
	}
	before, err := json.Marshal(existing)
	if err != nil {
		return fmt.Errorf("marshal revision before: %w", err)
	}
	page, err := scanPage(q.QueryRow(ctx, `
		UPDATE wiki_pages SET
			status = 'superseded',
			superseded_by = $2,
			updated_at = now(),
			updated_by_harness = $3
		WHERE id = $1 AND status = 'active'
		RETURNING `+pageCols, id, by, harness))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("page not found")
		}
		return fmt.Errorf("supersede page: %w", err)
	}
	after, err := json.Marshal(page)
	if err != nil {
		return fmt.Errorf("marshal revision after: %w", err)
	}
	_, err = appendRevision(ctx, q, types.Revision{
		EntityType: "page",
		EntityID:   page.ID,
		Before:     before,
		After:      after,
		Harness:    harness,
		Reason:     reason,
	})
	return err
}

func moveMemoryScope(ctx context.Context, q querier, id uuid.UUID, scope types.Scope, project, harness, reason string) error {
	existing, err := getMemoryByID(ctx, q, id)
	if err != nil {
		return err
	}
	before, err := json.Marshal(existing)
	if err != nil {
		return fmt.Errorf("marshal revision before: %w", err)
	}
	mem, err := scanMemory(q.QueryRow(ctx, `
		UPDATE memories SET
			scope = $2,
			project_slug = $3,
			updated_at = now(),
			updated_by_harness = $4
		WHERE id = $1 AND status = 'active'
		RETURNING `+memoryCols, id, scope, projectArg(project), harness))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("memory not found")
		}
		return fmt.Errorf("scope-move memory: %w", err)
	}
	after, err := json.Marshal(mem)
	if err != nil {
		return fmt.Errorf("marshal revision after: %w", err)
	}
	_, err = appendRevision(ctx, q, types.Revision{
		EntityType: "memory",
		EntityID:   mem.ID,
		Before:     before,
		After:      after,
		Harness:    harness,
		Reason:     reason,
	})
	return err
}

func movePageScope(ctx context.Context, q querier, id uuid.UUID, scope types.Scope, project, harness, reason string) error {
	existing, err := getPageByID(ctx, q, id)
	if err != nil {
		return err
	}
	before, err := json.Marshal(existing)
	if err != nil {
		return fmt.Errorf("marshal revision before: %w", err)
	}
	page, err := scanPage(q.QueryRow(ctx, `
		UPDATE wiki_pages SET
			scope = $2,
			project_slug = $3,
			updated_at = now(),
			updated_by_harness = $4
		WHERE id = $1 AND status = 'active'
		RETURNING `+pageCols, id, scope, projectArg(project), harness))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("page not found")
		}
		return fmt.Errorf("scope-move page: %w", err)
	}
	if err := replaceOutboundLinks(ctx, q, page); err != nil {
		return err
	}
	after, err := json.Marshal(page)
	if err != nil {
		return fmt.Errorf("marshal revision after: %w", err)
	}
	_, err = appendRevision(ctx, q, types.Revision{
		EntityType: "page",
		EntityID:   page.ID,
		Before:     before,
		After:      after,
		Harness:    harness,
		Reason:     reason,
	})
	return err
}

func getMemoryByID(ctx context.Context, q querier, id uuid.UUID) (types.Memory, error) {
	m, err := scanMemory(q.QueryRow(ctx, `SELECT `+memoryCols+` FROM memories WHERE id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return types.Memory{}, fmt.Errorf("memory not found")
		}
		return types.Memory{}, fmt.Errorf("get memory: %w", err)
	}
	return m, nil
}

func getPageByID(ctx context.Context, q querier, id uuid.UUID) (types.Page, error) {
	p, err := scanPage(q.QueryRow(ctx, `SELECT `+pageCols+` FROM wiki_pages WHERE id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return types.Page{}, fmt.Errorf("page not found")
		}
		return types.Page{}, fmt.Errorf("get page: %w", err)
	}
	return p, nil
}
