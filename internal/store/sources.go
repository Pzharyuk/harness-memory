package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Pzharyuk/harness-memory/internal/types"
)

const sourceCols = `
	id, scope, coalesce(project_slug, ''), kind, title, body,
	content_sha256, created_at, created_by_harness
`

func scanSource(sc interface{ Scan(dest ...any) error }) (types.Source, error) {
	var s types.Source
	err := sc.Scan(
		&s.ID,
		&s.Scope,
		&s.ProjectSlug,
		&s.Kind,
		&s.Title,
		&s.Body,
		&s.ContentSHA256,
		&s.CreatedAt,
		&s.CreatedByHarness,
	)
	return s, err
}

func validateSourceIngest(s types.Source) error {
	if s.CreatedByHarness == "" {
		return fmt.Errorf("harness is required")
	}
	switch s.Scope {
	case types.ScopeUser, types.ScopeProject:
	default:
		return fmt.Errorf("invalid scope %q", s.Scope)
	}
	switch s.Kind {
	case types.SourceKindImport, types.SourceKindFile, types.SourceKindURL, types.SourceKindSession:
	default:
		return fmt.Errorf("invalid kind %q", s.Kind)
	}
	return nil
}

func contentSHA256(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// IngestSource inserts an immutable source. Idempotent on (scope, project_slug, sha256).
// ContentSHA256 is computed from body when omitted.
func (s *Store) IngestSource(ctx context.Context, in types.Source) (types.Source, bool, error) {
	if err := validateSourceIngest(in); err != nil {
		return types.Source{}, false, err
	}
	if in.ContentSHA256 == "" {
		in.ContentSHA256 = contentSHA256(in.Body)
	}

	existing, err := getSourceByKey(ctx, s.Pool, in.Scope, in.ProjectSlug, in.ContentSHA256)
	if err != nil {
		return types.Source{}, false, err
	}
	if existing != nil {
		return *existing, false, nil
	}

	out, err := insertSource(ctx, s.Pool, in)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			existing, err := getSourceByKey(ctx, s.Pool, in.Scope, in.ProjectSlug, in.ContentSHA256)
			if err != nil {
				return types.Source{}, false, err
			}
			if existing != nil {
				return *existing, false, nil
			}
		}
		return types.Source{}, false, err
	}
	return out, true, nil
}

func insertSource(ctx context.Context, q querier, in types.Source) (types.Source, error) {
	out, err := scanSource(q.QueryRow(ctx, `
		INSERT INTO sources (
			scope, project_slug, kind, title, body, content_sha256, created_by_harness
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+sourceCols,
		in.Scope, projectArg(in.ProjectSlug), in.Kind, in.Title, in.Body,
		in.ContentSHA256, in.CreatedByHarness,
	))
	if err != nil {
		return types.Source{}, fmt.Errorf("insert source: %w", err)
	}
	return out, nil
}

func getSourceByKey(ctx context.Context, q querier, scope types.Scope, project, sha string) (*types.Source, error) {
	out, err := scanSource(q.QueryRow(ctx, `
		SELECT `+sourceCols+`
		FROM sources
		WHERE scope = $1
		  AND coalesce(project_slug, '') = $2
		  AND content_sha256 = $3
	`, scope, project, sha))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get source by key: %w", err)
	}
	return &out, nil
}

// GetSource returns a source by id.
func (s *Store) GetSource(ctx context.Context, id uuid.UUID) (types.Source, error) {
	out, err := scanSource(s.Pool.QueryRow(ctx, `
		SELECT `+sourceCols+` FROM sources WHERE id = $1
	`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return types.Source{}, fmt.Errorf("source not found")
		}
		return types.Source{}, fmt.Errorf("get source: %w", err)
	}
	return out, nil
}
