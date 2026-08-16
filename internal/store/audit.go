package store

import (
	"context"
	"fmt"
)

func appendAudit(ctx context.Context, q querier, harness, action, entity string) error {
	if harness == "" {
		return fmt.Errorf("audit harness is required")
	}
	if action == "" {
		return fmt.Errorf("audit action is required")
	}
	var entityArg any
	if entity != "" {
		entityArg = entity
	}
	_, err := q.Exec(ctx, `
		INSERT INTO audit_log (harness, action, entity)
		VALUES ($1, $2, $3)
	`, harness, action, entityArg)
	if err != nil {
		return fmt.Errorf("append audit: %w", err)
	}
	return nil
}

// AppendAudit records a read/write/search event. Query text is not stored.
func (s *Store) AppendAudit(ctx context.Context, harness, action, entity string) error {
	return appendAudit(ctx, s.Pool, harness, action, entity)
}
