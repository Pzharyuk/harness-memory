package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/Pzharyuk/harness-memory/internal/types"
)

const defaultSearchLimit = 20
const maxSearchLimit = 200

// Search runs FTS over active memories and wiki pages using plainto_tsquery
// ('simple') and ranks with ts_rank. nil scope or empty project means no filter.
func (s *Store) Search(ctx context.Context, q string, scope *types.Scope, project string, limit int) ([]types.SearchHit, error) {
	if strings.TrimSpace(q) == "" {
		return []types.SearchHit{}, nil
	}
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}

	var scopeArg any
	if scope != nil && *scope != "" {
		scopeArg = string(*scope)
	}

	rows, err := s.Pool.Query(ctx, `
		WITH q AS (SELECT plainto_tsquery('simple', $1) AS tsq)
		SELECT kind, id, title, summary, rank FROM (
			SELECT 'memory'::text AS kind, m.id, m.title, m.summary,
			       ts_rank(m.search_tsv, q.tsq) AS rank
			FROM memories m, q
			WHERE m.status = 'active'
			  AND m.search_tsv @@ q.tsq
			  AND ($2::text IS NULL OR m.scope = $2)
			  AND ($3::text = '' OR coalesce(m.project_slug, '') = $3)
			UNION ALL
			SELECT 'page', p.id, p.title, p.summary,
			       ts_rank(p.search_tsv, q.tsq)
			FROM wiki_pages p, q
			WHERE p.status = 'active'
			  AND p.search_tsv @@ q.tsq
			  AND ($2::text IS NULL OR p.scope = $2)
			  AND ($3::text = '' OR coalesce(p.project_slug, '') = $3)
		) hits
		ORDER BY rank DESC, title ASC, id ASC
		LIMIT $4
	`, q, scopeArg, project, limit)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	defer rows.Close()

	out := []types.SearchHit{}
	for rows.Next() {
		var h types.SearchHit
		if err := rows.Scan(&h.Kind, &h.ID, &h.Title, &h.Summary, &h.Rank); err != nil {
			return nil, fmt.Errorf("search: %w", err)
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	return out, nil
}
