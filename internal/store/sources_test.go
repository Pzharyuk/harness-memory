package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/google/uuid"

	"github.com/Pzharyuk/harness-memory/internal/types"
)

func TestIngestSourceIdempotentSameBody(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	in := types.Source{
		Scope:            types.ScopeUser,
		Kind:             types.SourceKindImport,
		Title:            "notes",
		Body:             "hello immutable world",
		CreatedByHarness: "grok",
	}

	first, created, err := st.IngestSource(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("first ingest created=false want true")
	}
	if first.ID == uuid.Nil {
		t.Fatal("empty id")
	}
	wantSHA := sha256Hex(in.Body)
	if first.ContentSHA256 != wantSHA {
		t.Fatalf("sha=%q want %q", first.ContentSHA256, wantSHA)
	}
	if first.Body != in.Body || first.Title != in.Title {
		t.Fatalf("first=%+v", first)
	}
	if first.CreatedByHarness != "grok" {
		t.Fatalf("harness=%q", first.CreatedByHarness)
	}

	second, created, err := st.IngestSource(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("second ingest created=true want false")
	}
	if second.ID != first.ID {
		t.Fatalf("id=%s want %s", second.ID, first.ID)
	}
	if second.ContentSHA256 != first.ContentSHA256 {
		t.Fatalf("sha changed: %q vs %q", second.ContentSHA256, first.ContentSHA256)
	}

	got, err := st.GetSource(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != first.ID || got.Body != in.Body {
		t.Fatalf("GetSource=%+v", got)
	}
}

func TestIngestSourceComputesSHAWhenOmitted(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	body := "sha me please"
	in := types.Source{
		Scope:            types.ScopeProject,
		ProjectSlug:      "demo",
		Kind:             types.SourceKindFile,
		Title:            "f",
		Body:             body,
		CreatedByHarness: "claude",
	}
	got, created, err := st.IngestSource(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected create")
	}
	if got.ContentSHA256 != sha256Hex(body) {
		t.Fatalf("sha=%q", got.ContentSHA256)
	}
	if got.ProjectSlug != "demo" || got.Scope != types.ScopeProject {
		t.Fatalf("got=%+v", got)
	}
}

func sha256Hex(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}
