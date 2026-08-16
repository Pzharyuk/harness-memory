package lint

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/Pzharyuk/harness-memory/internal/store"
	"github.com/Pzharyuk/harness-memory/internal/types"
	"github.com/Pzharyuk/harness-memory/internal/wikilink"
)

const (
	KindOrphan          = "orphan"
	KindBrokenLink      = "broken_link"
	KindStaleSource     = "stale_source"
	KindProjectionDrift = "projection_drift"
)

// Finding is one read-only diagnostic.
type Finding struct {
	Kind    string     `json:"kind"`
	Message string     `json:"message"`
	PageID  *uuid.UUID `json:"page_id,omitempty"`
}

// Options controls optional lint checks.
type Options struct {
	ProjectionDir string
}

type scopeKey struct {
	Scope   types.Scope
	Project string
}

// Run reports diagnostics for active pages. Empty project lints the whole brain.
func Run(ctx context.Context, st *store.Store, project string) ([]Finding, error) {
	return RunWithOptions(ctx, st, project, Options{})
}

// RunWithOptions is Run plus optional projection-dir drift.
func RunWithOptions(ctx context.Context, st *store.Store, project string, opts Options) ([]Finding, error) {
	pages, err := st.ListActivePages(ctx)
	if err != nil {
		return nil, err
	}
	sources, err := listSources(ctx, st)
	if err != nil {
		return nil, err
	}

	bySlug := map[scopeKey]map[string]types.Page{}
	for _, p := range pages {
		k := scopeKey{p.Scope, p.ProjectSlug}
		if bySlug[k] == nil {
			bySlug[k] = map[string]types.Page{}
		}
		bySlug[k][p.Slug] = p
	}

	inbound := map[uuid.UUID]int{}
	outboundOther := map[uuid.UUID]int{}
	findings := []Finding{}

	for i := range pages {
		p := pages[i]
		key := scopeKey{p.Scope, p.ProjectSlug}
		for _, ref := range wikilink.Parse(p.BodyMarkdown) {
			target, ok := bySlug[key][ref.Slug]
			if !ok {
				if matchProject(p, project) {
					id := p.ID
					findings = append(findings, Finding{
						Kind:    KindBrokenLink,
						Message: fmt.Sprintf("broken link [[%s]] on %s", ref.Slug, p.Slug),
						PageID:  &id,
					})
				}
				continue
			}
			if target.ID != p.ID {
				outboundOther[p.ID]++
				inbound[target.ID]++
			}
		}
	}

	for i := range pages {
		p := pages[i]
		if !matchProject(p, project) {
			continue
		}
		if inbound[p.ID] == 0 && outboundOther[p.ID] == 0 {
			id := p.ID
			findings = append(findings, Finding{
				Kind:    KindOrphan,
				Message: fmt.Sprintf("orphan page %s", p.Slug),
				PageID:  &id,
			})
		}
		if stale, msg := staleSource(p, sources); stale {
			id := p.ID
			findings = append(findings, Finding{
				Kind:    KindStaleSource,
				Message: msg,
				PageID:  &id,
			})
		}
	}

	if opts.ProjectionDir != "" {
		mems, err := st.ListActiveMemories(ctx)
		if err != nil {
			return nil, err
		}
		drift, err := projectionDrift(opts.ProjectionDir, mems, pages)
		if err != nil {
			return nil, err
		}
		findings = append(findings, drift...)
	}

	return findings, nil
}

func matchProject(p types.Page, project string) bool {
	if project == "" {
		return true
	}
	return p.Scope == types.ScopeProject && p.ProjectSlug == project
}

func staleSource(p types.Page, sources []types.Source) (bool, string) {
	srcByID := make(map[uuid.UUID]types.Source, len(sources))
	for _, src := range sources {
		srcByID[src.ID] = src
	}
	for _, id := range p.SourceIDs {
		src, ok := srcByID[id]
		if ok && src.CreatedAt.After(p.UpdatedAt) {
			return true, fmt.Sprintf("source newer than page %s", p.Slug)
		}
	}
	for _, src := range sources {
		if src.Scope != p.Scope || src.ProjectSlug != p.ProjectSlug {
			continue
		}
		if wikilink.Slugify(src.Title) == p.Slug && src.CreatedAt.After(p.UpdatedAt) {
			return true, fmt.Sprintf("source newer than page %s", p.Slug)
		}
	}
	return false, ""
}

func projectionDrift(dir string, mems []types.Memory, pages []types.Page) ([]Finding, error) {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat projection dir: %w", err)
	}
	if !info.IsDir() {
		return nil, nil
	}

	expected := map[string]struct{}{"MEMORY.md": {}}
	for _, m := range mems {
		expected[topicFile(string(m.Kind), wikilink.Slugify(m.Title))] = struct{}{}
	}
	for _, p := range pages {
		slug := p.Slug
		if slug == "" {
			slug = wikilink.Slugify(p.Title)
		}
		expected[topicFile(string(p.PageType), slug)] = struct{}{}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read projection dir: %w", err)
	}
	var out []Finding
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if _, ok := expected[name]; ok {
			continue
		}
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		out = append(out, Finding{
			Kind:    KindProjectionDrift,
			Message: fmt.Sprintf("leftover projection file %s", filepath.Base(name)),
		})
	}
	return out, nil
}

func topicFile(kind, slug string) string {
	if slug == "" {
		slug = "untitled"
	}
	return kind + "_" + slug + ".md"
}

func listSources(ctx context.Context, st *store.Store) ([]types.Source, error) {
	rows, err := st.Pool.Query(ctx, `
		SELECT id, scope, coalesce(project_slug, ''), kind, title, body,
		       content_sha256, created_at, created_by_harness
		FROM sources
	`)
	if err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
	}
	defer rows.Close()

	out := []types.Source{}
	for rows.Next() {
		var s types.Source
		if err := rows.Scan(
			&s.ID,
			&s.Scope,
			&s.ProjectSlug,
			&s.Kind,
			&s.Title,
			&s.Body,
			&s.ContentSHA256,
			&s.CreatedAt,
			&s.CreatedByHarness,
		); err != nil {
			return nil, fmt.Errorf("list sources: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
	}
	return out, nil
}
