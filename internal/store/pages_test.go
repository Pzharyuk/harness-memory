package store

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/Pzharyuk/harness-memory/internal/types"
)

func TestPageNewApplied(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	incoming := types.Page{
		Scope:        types.ScopeUser,
		Slug:         "vault-ha",
		Title:        "Vault HA",
		Summary:      "Vault high availability",
		BodyMarkdown: "Vault runs in HA.",
		PageType:     types.PageTypeEntity,
	}
	res, err := st.WritePage(ctx, incoming, "grok")
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
		t.Fatal("proposal id on applied write")
	}

	got, err := st.GetActivePageBySlug(ctx, types.ScopeUser, "", "vault-ha")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("GetActivePageBySlug returned nil")
	}
	if got.ID != res.ID {
		t.Fatalf("id=%s want %s", got.ID, res.ID)
	}
	if got.BodyMarkdown != "Vault runs in HA." || got.Title != "Vault HA" {
		t.Fatalf("got=%+v", got)
	}
	if got.UpdatedByHarness != "grok" {
		t.Fatalf("updated_by=%q", got.UpdatedByHarness)
	}
	if got.Status != types.StatusActive {
		t.Fatalf("status=%q", got.Status)
	}

	byID, err := st.GetPage(ctx, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if byID.ID != got.ID || byID.Slug != "vault-ha" {
		t.Fatalf("GetPage=%+v", byID)
	}

	if n := countRevisions(t, st, got.ID); n != 1 {
		t.Fatalf("revisions=%d want 1", n)
	}
}

func TestPageConflictProposed(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	first := types.Page{
		Scope:        types.ScopeUser,
		Slug:         "vault-ha",
		Title:        "Vault HA",
		Summary:      "old",
		BodyMarkdown: "Vault runs in HA.",
		PageType:     types.PageTypeEntity,
	}
	if _, err := st.WritePage(ctx, first, "grok"); err != nil {
		t.Fatal(err)
	}

	conflict := first
	conflict.BodyMarkdown = "Vault is a single node."
	res, err := st.WritePage(ctx, conflict, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != types.SaveStatusProposed {
		t.Fatalf("status=%q want proposed", res.Status)
	}
	if res.ProposalID == nil || *res.ProposalID == uuid.Nil {
		t.Fatal("missing proposal id")
	}

	got, err := st.GetActivePageBySlug(ctx, types.ScopeUser, "", "vault-ha")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("original page missing")
	}
	if got.BodyMarkdown != "Vault runs in HA." {
		t.Fatalf("body=%q want original unchanged", got.BodyMarkdown)
	}

	if n := countOpenProposals(t, st); n != 1 {
		t.Fatalf("open proposals=%d want 1", n)
	}
	if n := countRevisions(t, st, got.ID); n != 1 {
		t.Fatalf("revisions=%d want 1 (no extra on propose)", n)
	}
}

func TestPagePrefixApplied(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	first := types.Page{
		Scope:        types.ScopeUser,
		Slug:         "vault-ha",
		Title:        "Vault HA",
		Summary:      "Vault HA",
		BodyMarkdown: "Vault runs in HA.",
		PageType:     types.PageTypeEntity,
	}
	res1, err := st.WritePage(ctx, first, "grok")
	if err != nil {
		t.Fatal(err)
	}

	prefix := first
	prefix.BodyMarkdown = "Vault runs in HA.\nAlso pin the Raft peers."
	prefix.Summary = "Vault HA; pin Raft"
	res2, err := st.WritePage(ctx, prefix, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if res2.Status != types.SaveStatusApplied {
		t.Fatalf("status=%q want applied", res2.Status)
	}
	if res2.ID != res1.ID {
		t.Fatalf("id=%s want %s", res2.ID, res1.ID)
	}

	got, err := st.GetActivePageBySlug(ctx, types.ScopeUser, "", "vault-ha")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("page missing")
	}
	if got.BodyMarkdown != "Vault runs in HA.\nAlso pin the Raft peers." {
		t.Fatalf("body=%q", got.BodyMarkdown)
	}
	if got.UpdatedByHarness != "claude" {
		t.Fatalf("updated_by=%q", got.UpdatedByHarness)
	}
	if n := countRevisions(t, st, got.ID); n != 2 {
		t.Fatalf("revisions=%d want 2", n)
	}
}

func TestPageLinksPopulated(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	pg, err := st.WritePage(ctx, types.Page{
		Scope:        types.ScopeUser,
		Slug:         "postgres",
		Title:        "Postgres",
		BodyMarkdown: "The database.",
		PageType:     types.PageTypeEntity,
	}, "grok")
	if err != nil {
		t.Fatal(err)
	}

	vault, err := st.WritePage(ctx, types.Page{
		Scope:        types.ScopeUser,
		Slug:         "vault-ha",
		Title:        "Vault HA",
		BodyMarkdown: "see [[Vault HA]] and [[depends_on:Postgres]]",
		PageType:     types.PageTypeEntity,
	}, "grok")
	if err != nil {
		t.Fatal(err)
	}
	if vault.Status != types.SaveStatusApplied {
		t.Fatalf("status=%q want applied", vault.Status)
	}

	links := listOutboundLinks(t, st, vault.ID)
	if len(links) != 2 {
		t.Fatalf("links=%d want 2: %+v", len(links), links)
	}

	var sawSelf, sawDepends bool
	for _, l := range links {
		if l.FromPage != vault.ID {
			t.Fatalf("from=%s want %s", l.FromPage, vault.ID)
		}
		switch {
		case l.ToPage == vault.ID && l.Rel == types.RelRelated:
			sawSelf = true
		case l.ToPage == pg.ID && l.Rel == types.RelDependsOn:
			sawDepends = true
		default:
			t.Fatalf("unexpected link %+v", l)
		}
	}
	if !sawSelf || !sawDepends {
		t.Fatalf("missing edges self=%v depends=%v links=%+v", sawSelf, sawDepends, links)
	}
}

func listOutboundLinks(t *testing.T, st *Store, from uuid.UUID) []types.Link {
	t.Helper()
	rows, err := st.Pool.Query(context.Background(),
		`SELECT from_page, to_page, rel FROM wiki_links WHERE from_page = $1 ORDER BY rel, to_page`, from)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []types.Link
	for rows.Next() {
		var l types.Link
		if err := rows.Scan(&l.FromPage, &l.ToPage, &l.Rel); err != nil {
			t.Fatal(err)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
