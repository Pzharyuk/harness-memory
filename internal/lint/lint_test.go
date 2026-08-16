package lint

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Pzharyuk/harness-memory/internal/store"
	"github.com/Pzharyuk/harness-memory/internal/types"
)

func openTest(t *testing.T) *store.Store {
	t.Helper()
	store.LockTestDB(t)
	url := os.Getenv("MEMORY_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("MEMORY_TEST_DATABASE_URL unset")
	}
	st, err := store.Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_, err := st.Pool.Exec(ctx, `
			TRUNCATE TABLE
				audit_log,
				tokens,
				proposals,
				revisions,
				wiki_links,
				wiki_pages,
				memories,
				sources
			CASCADE
		`)
		if err != nil {
			t.Errorf("truncate: %v", err)
		}
		st.Pool.Close()
	})
	return st
}

func hasKind(findings []Finding, kind string) bool {
	for _, f := range findings {
		if f.Kind == kind {
			return true
		}
	}
	return false
}

func TestBrokenLink(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	res, err := st.WritePage(ctx, types.Page{
		Scope:        types.ScopeUser,
		Slug:         "lonely",
		Title:        "Lonely",
		BodyMarkdown: "see [[missing]]",
		PageType:     types.PageTypeEntity,
	}, "grok")
	if err != nil {
		t.Fatal(err)
	}

	findings, err := Run(ctx, st, "")
	if err != nil {
		t.Fatal(err)
	}
	if !hasKind(findings, KindBrokenLink) {
		t.Fatalf("missing broken_link: %+v", findings)
	}
	found := false
	for _, f := range findings {
		if f.Kind == KindBrokenLink && f.PageID != nil && *f.PageID == res.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("broken_link not attached to page %s: %+v", res.ID, findings)
	}
}

func TestOrphan(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	res, err := st.WritePage(ctx, types.Page{
		Scope:        types.ScopeUser,
		Slug:         "island",
		Title:        "Island",
		BodyMarkdown: "just me [[Island]]",
		PageType:     types.PageTypeEntity,
	}, "grok")
	if err != nil {
		t.Fatal(err)
	}

	findings, err := Run(ctx, st, "")
	if err != nil {
		t.Fatal(err)
	}
	if !hasKind(findings, KindOrphan) {
		t.Fatalf("missing orphan: %+v", findings)
	}
	found := false
	for _, f := range findings {
		if f.Kind == KindOrphan && f.PageID != nil && *f.PageID == res.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("orphan not attached to page %s: %+v", res.ID, findings)
	}
}

func TestStaleSource(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	pageRes, err := st.WritePage(ctx, types.Page{
		Scope:        types.ScopeUser,
		Slug:         "notes",
		Title:        "Notes",
		BodyMarkdown: "compiled notes",
		PageType:     types.PageTypeSourceSummary,
	}, "grok")
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := st.IngestSource(ctx, types.Source{
		Scope:            types.ScopeUser,
		Kind:             types.SourceKindFile,
		Title:            "Notes",
		Body:             "newer raw source",
		CreatedByHarness: "grok",
	}); err != nil {
		t.Fatal(err)
	}

	findings, err := Run(ctx, st, "")
	if err != nil {
		t.Fatal(err)
	}
	if !hasKind(findings, KindStaleSource) {
		t.Fatalf("missing stale_source: %+v", findings)
	}
	found := false
	for _, f := range findings {
		if f.Kind == KindStaleSource && f.PageID != nil && *f.PageID == pageRes.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("stale_source not attached to page %s: %+v", pageRes.ID, findings)
	}
}

func TestProjectionDriftLeftover(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	if _, err := st.SaveMemory(ctx, types.Memory{
		Scope:   types.ScopeUser,
		Kind:    types.MemoryKindUser,
		Title:   "pipenv",
		Summary: "use pipenv",
		Body:    "use pipenv",
	}, "grok"); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	leftover := filepath.Join(dir, "feedback_stale-topic.md")
	if err := os.WriteFile(leftover, []byte("left behind"), 0o600); err != nil {
		t.Fatal(err)
	}

	findings, err := RunWithOptions(ctx, st, "", Options{ProjectionDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !hasKind(findings, KindProjectionDrift) {
		t.Fatalf("missing projection_drift: %+v", findings)
	}
}
