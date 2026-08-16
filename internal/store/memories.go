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
	"github.com/Pzharyuk/harness-memory/internal/writerules"
)

// querier is implemented by *pgxpool.Pool and pgx.Tx.
type querier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

const memoryCols = `
	id, scope, coalesce(project_slug, ''), kind, title, summary, body,
	source_id, status, superseded_by, created_at, updated_at,
	created_by_harness, updated_by_harness
`

func scanMemory(sc interface{ Scan(dest ...any) error }) (types.Memory, error) {
	var m types.Memory
	err := sc.Scan(
		&m.ID,
		&m.Scope,
		&m.ProjectSlug,
		&m.Kind,
		&m.Title,
		&m.Summary,
		&m.Body,
		&m.SourceID,
		&m.Status,
		&m.SupersededBy,
		&m.CreatedAt,
		&m.UpdatedAt,
		&m.CreatedByHarness,
		&m.UpdatedByHarness,
	)
	return m, err
}

func projectArg(project string) any {
	if project == "" {
		return nil
	}
	return project
}

func validateMemoryWrite(m types.Memory, harness string) error {
	if harness == "" {
		return fmt.Errorf("harness is required")
	}
	if m.Title == "" {
		return fmt.Errorf("title is required")
	}
	switch m.Scope {
	case types.ScopeUser, types.ScopeProject:
	default:
		return fmt.Errorf("invalid scope %q", m.Scope)
	}
	switch m.Kind {
	case types.MemoryKindUser, types.MemoryKindFeedback, types.MemoryKindProject, types.MemoryKindReference:
	default:
		return fmt.Errorf("invalid kind %q", m.Kind)
	}
	return nil
}

// SaveMemory looks up the active row by title, applies writerules.DecideMemorySave,
// and in one transaction either upserts (auto) or opens a proposal (proposed).
func (s *Store) SaveMemory(ctx context.Context, incoming types.Memory, harness string) (types.SaveResult, error) {
	if err := validateMemoryWrite(incoming, harness); err != nil {
		return types.SaveResult{}, err
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return types.SaveResult{}, fmt.Errorf("begin save memory: %w", err)
	}
	defer tx.Rollback(ctx)

	existing, err := getActiveMemoryByTitle(ctx, tx, incoming.Scope, incoming.ProjectSlug, incoming.Title, true)
	if err != nil {
		return types.SaveResult{}, err
	}
	d := writerules.DecideMemorySave(existing, incoming)

	switch d.Path {
	case types.PathAuto:
		mem, err := upsertMemory(ctx, tx, existing, incoming, harness)
		if err != nil {
			return types.SaveResult{}, err
		}
		var before json.RawMessage
		if existing != nil {
			before, err = json.Marshal(existing)
			if err != nil {
				return types.SaveResult{}, fmt.Errorf("marshal revision before: %w", err)
			}
		}
		after, err := json.Marshal(mem)
		if err != nil {
			return types.SaveResult{}, fmt.Errorf("marshal revision after: %w", err)
		}
		if _, err := appendRevision(ctx, tx, types.Revision{
			EntityType: "memory",
			EntityID:   mem.ID,
			Before:     before,
			After:      after,
			Harness:    harness,
			Reason:     d.Reason,
		}); err != nil {
			return types.SaveResult{}, err
		}
		if err := appendAudit(ctx, tx, harness, "memory.save", mem.ID.String()); err != nil {
			return types.SaveResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return types.SaveResult{}, fmt.Errorf("commit save memory: %w", err)
		}
		return types.SaveResult{Status: types.SaveStatusApplied, ID: mem.ID}, nil

	case types.PathProposed:
		action := types.ProposalActionCreate
		payloadMem := incoming
		id := incoming.ID
		if existing != nil {
			action = types.ProposalActionUpdate
			payloadMem.ID = existing.ID
			id = existing.ID
		}
		payload, err := json.Marshal(payloadMem)
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
		if err := appendAudit(ctx, tx, harness, "memory.propose", entity); err != nil {
			return types.SaveResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return types.SaveResult{}, fmt.Errorf("commit propose memory: %w", err)
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

func upsertMemory(ctx context.Context, q querier, existing *types.Memory, incoming types.Memory, harness string) (types.Memory, error) {
	if existing == nil {
		return insertMemory(ctx, q, incoming, harness)
	}
	return updateMemory(ctx, q, existing.ID, incoming, harness)
}

func insertMemory(ctx context.Context, q querier, incoming types.Memory, harness string) (types.Memory, error) {
	m, err := scanMemory(q.QueryRow(ctx, `
		INSERT INTO memories (
			scope, project_slug, kind, title, summary, body, source_id,
			created_by_harness, updated_by_harness
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
		RETURNING `+memoryCols, incoming.Scope, projectArg(incoming.ProjectSlug), incoming.Kind,
		incoming.Title, incoming.Summary, incoming.Body, incoming.SourceID, harness))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return types.Memory{}, fmt.Errorf("active memory already exists for title")
		}
		return types.Memory{}, fmt.Errorf("insert memory: %w", err)
	}
	return m, nil
}

func updateMemory(ctx context.Context, q querier, id uuid.UUID, incoming types.Memory, harness string) (types.Memory, error) {
	m, err := scanMemory(q.QueryRow(ctx, `
		UPDATE memories SET
			kind = $2,
			summary = $3,
			body = $4,
			source_id = $5,
			updated_at = now(),
			updated_by_harness = $6
		WHERE id = $1 AND status = 'active'
		RETURNING `+memoryCols, id, incoming.Kind, incoming.Summary, incoming.Body, incoming.SourceID, harness))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return types.Memory{}, fmt.Errorf("memory not found")
		}
		return types.Memory{}, fmt.Errorf("update memory: %w", err)
	}
	return m, nil
}

func getActiveMemoryByTitle(ctx context.Context, q querier, scope types.Scope, project, title string, forUpdate bool) (*types.Memory, error) {
	sql := `SELECT ` + memoryCols + `
		FROM memories
		WHERE status = 'active'
		  AND scope = $1
		  AND coalesce(project_slug, '') = $2
		  AND title = $3`
	if forUpdate {
		sql += ` FOR UPDATE`
	}
	m, err := scanMemory(q.QueryRow(ctx, sql, scope, project, title))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get active memory by title: %w", err)
	}
	return &m, nil
}

// GetMemory returns a memory by id (any status).
func (s *Store) GetMemory(ctx context.Context, id uuid.UUID) (types.Memory, error) {
	m, err := scanMemory(s.Pool.QueryRow(ctx, `SELECT `+memoryCols+` FROM memories WHERE id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return types.Memory{}, fmt.Errorf("memory not found")
		}
		return types.Memory{}, fmt.Errorf("get memory: %w", err)
	}
	return m, nil
}

// GetActiveMemoryByTitle returns the active memory for (scope, project, title).
// Missing row is (nil, nil).
func (s *Store) GetActiveMemoryByTitle(ctx context.Context, scope types.Scope, project, title string) (*types.Memory, error) {
	return getActiveMemoryByTitle(ctx, s.Pool, scope, project, title, false)
}

// ListMemories returns active memories for a scope and project ("" for user).
func (s *Store) ListMemories(ctx context.Context, scope types.Scope, project string) ([]types.Memory, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+memoryCols+`
		FROM memories
		WHERE status = 'active'
		  AND scope = $1
		  AND coalesce(project_slug, '') = $2
		ORDER BY updated_at DESC, title ASC, id ASC`, scope, project)
	if err != nil {
		return nil, fmt.Errorf("list memories: %w", err)
	}
	defer rows.Close()

	out := []types.Memory{}
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return nil, fmt.Errorf("list memories: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list memories: %w", err)
	}
	return out, nil
}
