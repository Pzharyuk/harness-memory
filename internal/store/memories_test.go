package store

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/Pzharyuk/harness-memory/internal/types"
)

func TestMemoryFirstSaveApplied(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	incoming := types.Memory{
		Scope:   types.ScopeUser,
		Kind:    types.MemoryKindUser,
		Title:   "pipenv",
		Summary: "use pipenv",
		Body:    "use pipenv",
	}
	res, err := st.SaveMemory(ctx, incoming, "grok")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != types.SaveStatusApplied {
		t.Fatalf("status=%q want applied", res.Status)
	}
	if res.ID == uuid.Nil {
		t.Fatal("empty id")
	}
	if res.ProposalID != nil {
		t.Fatal("proposal id on applied save")
	}

	got, err := st.GetActiveMemoryByTitle(ctx, types.ScopeUser, "", "pipenv")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("GetActiveMemoryByTitle returned nil")
	}
	if got.ID != res.ID {
		t.Fatalf("id=%s want %s", got.ID, res.ID)
	}
	if got.Body != "use pipenv" || got.Title != "pipenv" {
		t.Fatalf("got=%+v", got)
	}
	if got.CreatedByHarness != "grok" || got.UpdatedByHarness != "grok" {
		t.Fatalf("harness created=%q updated=%q", got.CreatedByHarness, got.UpdatedByHarness)
	}
	if got.Status != types.StatusActive {
		t.Fatalf("status=%q", got.Status)
	}

	if n := countRevisions(t, st, got.ID); n != 1 {
		t.Fatalf("revisions=%d want 1", n)
	}
}

func TestMemoryConflictProposed(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	first := types.Memory{
		Scope:   types.ScopeUser,
		Kind:    types.MemoryKindUser,
		Title:   "pipenv",
		Summary: "use pipenv",
		Body:    "use pipenv",
	}
	if _, err := st.SaveMemory(ctx, first, "grok"); err != nil {
		t.Fatal(err)
	}

	conflict := first
	conflict.Body = "use poetry"
	res, err := st.SaveMemory(ctx, conflict, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != types.SaveStatusProposed {
		t.Fatalf("status=%q want proposed", res.Status)
	}
	if res.ProposalID == nil || *res.ProposalID == uuid.Nil {
		t.Fatal("missing proposal id")
	}

	got, err := st.GetActiveMemoryByTitle(ctx, types.ScopeUser, "", "pipenv")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("original memory missing")
	}
	if got.Body != "use pipenv" {
		t.Fatalf("body=%q want original unchanged", got.Body)
	}

	if n := countOpenProposals(t, st); n != 1 {
		t.Fatalf("open proposals=%d want 1", n)
	}
	if n := countRevisions(t, st, got.ID); n != 1 {
		t.Fatalf("revisions=%d want 1 (no extra on propose)", n)
	}
}

func TestMemoryPrefixApplied(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	first := types.Memory{
		Scope:   types.ScopeUser,
		Kind:    types.MemoryKindUser,
		Title:   "pipenv",
		Summary: "use pipenv",
		Body:    "use pipenv",
	}
	res1, err := st.SaveMemory(ctx, first, "grok")
	if err != nil {
		t.Fatal(err)
	}

	prefix := first
	prefix.Body = "use pipenv\nalways pin"
	prefix.Summary = "use pipenv; pin"
	res2, err := st.SaveMemory(ctx, prefix, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if res2.Status != types.SaveStatusApplied {
		t.Fatalf("status=%q want applied", res2.Status)
	}
	if res2.ID != res1.ID {
		t.Fatalf("id=%s want %s", res2.ID, res1.ID)
	}

	got, err := st.GetActiveMemoryByTitle(ctx, types.ScopeUser, "", "pipenv")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("memory missing")
	}
	if got.Body != "use pipenv\nalways pin" {
		t.Fatalf("body=%q", got.Body)
	}
	if got.UpdatedByHarness != "claude" {
		t.Fatalf("updated_by=%q", got.UpdatedByHarness)
	}
	if n := countRevisions(t, st, got.ID); n != 2 {
		t.Fatalf("revisions=%d want 2", n)
	}
}

func countRevisions(t *testing.T, st *Store, entityID uuid.UUID) int {
	t.Helper()
	var n int
	err := st.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM revisions WHERE entity_id = $1`, entityID).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func countOpenProposals(t *testing.T, st *Store) int {
	t.Helper()
	var n int
	err := st.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM proposals WHERE status = 'open'`).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
