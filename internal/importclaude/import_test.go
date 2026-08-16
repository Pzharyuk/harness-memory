package importclaude

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pzharyuk/harness-memory/internal/store"
	"github.com/Pzharyuk/harness-memory/internal/types"
)

func fixtureDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join("..", "..", "testdata", "claude-memory")
	if _, err := os.Stat(filepath.Join(dir, "MEMORY.md")); err != nil {
		t.Fatalf("fixture %s: %v", dir, err)
	}
	return dir
}

func openTest(t *testing.T) *store.Store {
	t.Helper()
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

func TestParseClaudeMemory(t *testing.T) {
	plan, err := Parse(fixtureDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Memories) != 2 {
		t.Fatalf("memories=%d want 2", len(plan.Memories))
	}
	if plan.Memories[0].Kind != types.MemoryKindFeedback {
		t.Fatalf("first kind=%q want feedback", plan.Memories[0].Kind)
	}
	if plan.Memories[0].Title != "Python tooling preferences" {
		t.Fatalf("first title=%q", plan.Memories[0].Title)
	}
	if plan.Memories[1].Kind != types.MemoryKindProject {
		t.Fatalf("second kind=%q want project", plan.Memories[1].Kind)
	}
	if plan.Memories[1].Title != "Vault" {
		t.Fatalf("second title=%q", plan.Memories[1].Title)
	}
}

func TestApplyImportsMemoryByTitle(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	plan, err := Parse(fixtureDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(ctx, st, plan, "import"); err != nil {
		t.Fatal(err)
	}

	got, err := st.GetActiveMemoryByTitle(ctx, types.ScopeUser, "", "Python tooling preferences")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("GetActiveMemoryByTitle returned nil")
	}
	if got.Kind != types.MemoryKindFeedback {
		t.Fatalf("kind=%q want feedback", got.Kind)
	}
	if got.CreatedByHarness != "import" {
		t.Fatalf("harness=%q want import", got.CreatedByHarness)
	}
	if !strings.Contains(got.Body, "pipenv") {
		t.Fatalf("body=%q", got.Body)
	}
}

func TestApplyWritesSourceSummaryForLongBody(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	body := strings.Repeat("x", 2001)
	plan := Plan{Memories: []Item{{
		File:    "project_long_note.md",
		Kind:    types.MemoryKindProject,
		Title:   "Long note",
		Summary: "big",
		Body:    body,
	}}}
	if err := Apply(ctx, st, plan, "import"); err != nil {
		t.Fatal(err)
	}
	page, err := st.GetActivePageBySlug(ctx, types.ScopeUser, "", "long-note")
	if err != nil {
		t.Fatal(err)
	}
	if page == nil {
		t.Fatal("missing source-summary page")
	}
	if page.PageType != types.PageTypeSourceSummary {
		t.Fatalf("page_type=%q", page.PageType)
	}
	if page.UpdatedByHarness != "import" {
		t.Fatalf("harness=%q want import", page.UpdatedByHarness)
	}
}
