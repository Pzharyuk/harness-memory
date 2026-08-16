package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Pzharyuk/harness-memory/internal/types"
	"github.com/Pzharyuk/harness-memory/internal/wikilink"
)

// replaceOutboundLinks deletes existing edges from fromID and inserts one row
// per parsed wikilink whose slug resolves to an active page in the same scope.
func replaceOutboundLinks(ctx context.Context, q querier, from types.Page) error {
	if _, err := q.Exec(ctx, `DELETE FROM wiki_links WHERE from_page = $1`, from.ID); err != nil {
		return fmt.Errorf("delete outbound links: %w", err)
	}
	for _, ref := range wikilink.Parse(from.BodyMarkdown) {
		if ref.Slug == "" {
			continue
		}
		target, err := getActivePageBySlug(ctx, q, from.Scope, from.ProjectSlug, ref.Slug, false)
		if err != nil {
			return err
		}
		if target == nil {
			continue
		}
		if err := insertLink(ctx, q, from.ID, target.ID, ref.Rel); err != nil {
			return err
		}
	}
	return nil
}

func insertLink(ctx context.Context, q querier, from, to uuid.UUID, rel types.Rel) error {
	_, err := q.Exec(ctx, `
		INSERT INTO wiki_links (from_page, to_page, rel)
		VALUES ($1, $2, $3)
	`, from, to, rel)
	if err != nil {
		return fmt.Errorf("insert link: %w", err)
	}
	return nil
}

func hasContradictsLink(body string) bool {
	for _, ref := range wikilink.Parse(body) {
		if ref.Rel == types.RelContradicts {
			return true
		}
	}
	return false
}
