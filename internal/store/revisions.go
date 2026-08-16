package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/Pzharyuk/harness-memory/internal/types"
)

func nullableJSON(b json.RawMessage) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

func scanRevision(sc interface{ Scan(dest ...any) error }) (types.Revision, error) {
	var r types.Revision
	err := sc.Scan(
		&r.ID,
		&r.EntityType,
		&r.EntityID,
		&r.Before,
		&r.After,
		&r.Harness,
		&r.Reason,
		&r.At,
	)
	return r, err
}

func appendRevision(ctx context.Context, q querier, r types.Revision) (types.Revision, error) {
	if r.EntityType == "" {
		return types.Revision{}, fmt.Errorf("revision entity_type is required")
	}
	if r.EntityID == uuid.Nil {
		return types.Revision{}, fmt.Errorf("revision entity_id is required")
	}
	if r.Harness == "" {
		return types.Revision{}, fmt.Errorf("revision harness is required")
	}
	out, err := scanRevision(q.QueryRow(ctx, `
		INSERT INTO revisions (entity_type, entity_id, before, after, harness, reason)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, entity_type, entity_id, before, after, harness, reason, at
	`, r.EntityType, r.EntityID, nullableJSON(r.Before), nullableJSON(r.After), r.Harness, r.Reason))
	if err != nil {
		return types.Revision{}, fmt.Errorf("append revision: %w", err)
	}
	return out, nil
}

// AppendRevision inserts an append-only before/after snapshot.
func (s *Store) AppendRevision(ctx context.Context, r types.Revision) (types.Revision, error) {
	return appendRevision(ctx, s.Pool, r)
}
