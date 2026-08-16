package store

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Pzharyuk/harness-memory/internal/types"
)

func conflictProposal(t *testing.T, st *Store) (memID, proposalID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	first := types.Memory{
		Scope:   types.ScopeUser,
		Kind:    types.MemoryKindUser,
		Title:   "pipenv",
		Summary: "use pipenv",
		Body:    "use pipenv",
	}
	res, err := st.SaveMemory(ctx, first, "grok")
	if err != nil {
		t.Fatal(err)
	}
	conflict := first
	conflict.Body = "use poetry"
	proposed, err := st.SaveMemory(ctx, conflict, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if proposed.ProposalID == nil {
		t.Fatal("missing proposal id")
	}
	return res.ID, *proposed.ProposalID
}

func TestListOpenProposals(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	open, err := st.InsertProposal(ctx, types.Proposal{
		Action:           types.ProposalActionUpdate,
		Payload:          json.RawMessage(`{"title":"open"}`),
		Reason:           "keep",
		CreatedByHarness: "grok",
	})
	if err != nil {
		t.Fatal(err)
	}
	closed, err := st.InsertProposal(ctx, types.Proposal{
		Action:           types.ProposalActionDelete,
		Payload:          json.RawMessage(`{"title":"closed"}`),
		Reason:           "drop",
		CreatedByHarness: "grok",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RejectProposal(ctx, closed.ID, "admin"); err != nil {
		t.Fatal(err)
	}

	got, err := st.ListOpenProposals(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != open.ID {
		t.Fatalf("open=%+v want only %s", got, open.ID)
	}
	if got[0].ID == closed.ID {
		t.Fatal("rejected proposal listed as open")
	}
}

func TestInsertProposalIgnoresClientStatus(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	got, err := st.InsertProposal(ctx, types.Proposal{
		Action:           types.ProposalActionUpdate,
		Payload:          json.RawMessage(`{"title":"sneak"}`),
		Reason:           "client tried accepted",
		Status:           types.ProposalStatusAccepted,
		CreatedByHarness: "grok",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != types.ProposalStatusOpen {
		t.Fatalf("status=%q want open", got.Status)
	}
	open, err := st.ListOpenProposals(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0].ID != got.ID {
		t.Fatalf("open=%+v want only %s", open, got.ID)
	}
}

func TestAcceptProposalUpdatesBody(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	_, pid := conflictProposal(t, st)

	if err := st.AcceptProposal(ctx, pid, "admin"); err != nil {
		t.Fatal(err)
	}

	got, err := st.GetActiveMemoryByTitle(ctx, types.ScopeUser, "", "pipenv")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("memory missing")
	}
	if got.Body != "use poetry" {
		t.Fatalf("body=%q want use poetry", got.Body)
	}

	open, err := st.ListOpenProposals(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 0 {
		t.Fatalf("open proposals=%d want 0", len(open))
	}
}

func TestRejectProposalLeavesOriginal(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	_, pid := conflictProposal(t, st)

	if err := st.RejectProposal(ctx, pid, "admin"); err != nil {
		t.Fatal(err)
	}

	got, err := st.GetActiveMemoryByTitle(ctx, types.ScopeUser, "", "pipenv")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("memory missing")
	}
	if got.Body != "use pipenv" {
		t.Fatalf("body=%q want original unchanged", got.Body)
	}

	open, err := st.ListOpenProposals(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 0 {
		t.Fatalf("open proposals=%d want 0", len(open))
	}
}

func TestAcceptProposalRequiresAdmin(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	_, pid := conflictProposal(t, st)

	err := st.AcceptProposal(ctx, pid, "grok")
	if err == nil {
		t.Fatal("accept as grok succeeded")
	}
	if !strings.Contains(err.Error(), "admin") {
		t.Fatalf("err=%v want admin", err)
	}

	got, err := st.GetActiveMemoryByTitle(ctx, types.ScopeUser, "", "pipenv")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Body != "use pipenv" {
		t.Fatalf("body changed on forbidden accept: %+v", got)
	}
}

func TestAcceptDeleteSupersedesMemory(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	res, err := st.SaveMemory(ctx, types.Memory{
		Scope:   types.ScopeUser,
		Kind:    types.MemoryKindUser,
		Title:   "forget-me",
		Summary: "old",
		Body:    "old",
	}, "grok")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{"id": res.ID})
	if err != nil {
		t.Fatal(err)
	}
	p, err := st.InsertProposal(ctx, types.Proposal{
		Action:           types.ProposalActionDelete,
		Payload:          payload,
		Reason:           "no longer true",
		CreatedByHarness: "grok",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AcceptProposal(ctx, p.ID, "admin"); err != nil {
		t.Fatal(err)
	}

	got, err := st.GetMemory(ctx, res.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != types.StatusSuperseded {
		t.Fatalf("status=%q want superseded", got.Status)
	}
	active, err := st.GetActiveMemoryByTitle(ctx, types.ScopeUser, "", "forget-me")
	if err != nil {
		t.Fatal(err)
	}
	if active != nil {
		t.Fatalf("active row still present: %+v", active)
	}
}

func TestAcceptScopeMoveMemory(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	res, err := st.SaveMemory(ctx, types.Memory{
		Scope:   types.ScopeUser,
		Kind:    types.MemoryKindUser,
		Title:   "moved",
		Summary: "user fact",
		Body:    "user fact",
	}, "grok")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{
		"id":           res.ID,
		"scope":        types.ScopeProject,
		"project_slug": "demo",
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := st.InsertProposal(ctx, types.Proposal{
		Action:           types.ProposalActionScopeMove,
		Payload:          payload,
		Reason:           "belongs to demo",
		CreatedByHarness: "grok",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AcceptProposal(ctx, p.ID, "admin"); err != nil {
		t.Fatal(err)
	}

	got, err := st.GetMemory(ctx, res.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Scope != types.ScopeProject || got.ProjectSlug != "demo" {
		t.Fatalf("scope=%q project=%q", got.Scope, got.ProjectSlug)
	}
	if got.Status != types.StatusActive {
		t.Fatalf("status=%q", got.Status)
	}
}

func TestAcceptSupersedeMemory(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	oldRes, err := st.SaveMemory(ctx, types.Memory{
		Scope:   types.ScopeUser,
		Kind:    types.MemoryKindUser,
		Title:   "old-name",
		Summary: "old",
		Body:    "old",
	}, "grok")
	if err != nil {
		t.Fatal(err)
	}
	newRes, err := st.SaveMemory(ctx, types.Memory{
		Scope:   types.ScopeUser,
		Kind:    types.MemoryKindUser,
		Title:   "new-name",
		Summary: "new",
		Body:    "new",
	}, "grok")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{
		"id":            oldRes.ID,
		"superseded_by": newRes.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := st.InsertProposal(ctx, types.Proposal{
		Action:           types.ProposalActionSupersede,
		Payload:          payload,
		Reason:           "renamed",
		CreatedByHarness: "grok",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AcceptProposal(ctx, p.ID, "admin"); err != nil {
		t.Fatal(err)
	}

	got, err := st.GetMemory(ctx, oldRes.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != types.StatusSuperseded {
		t.Fatalf("status=%q want superseded", got.Status)
	}
	if got.SupersededBy == nil || *got.SupersededBy != newRes.ID {
		t.Fatalf("superseded_by=%v want %s", got.SupersededBy, newRes.ID)
	}
}

func TestAcceptPageUpdate(t *testing.T) {
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
	proposed, err := st.WritePage(ctx, conflict, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if proposed.ProposalID == nil {
		t.Fatal("missing proposal id")
	}
	if err := st.AcceptProposal(ctx, *proposed.ProposalID, "admin"); err != nil {
		t.Fatal(err)
	}

	got, err := st.GetActivePageBySlug(ctx, types.ScopeUser, "", "vault-ha")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("page missing")
	}
	if got.BodyMarkdown != "Vault is a single node." {
		t.Fatalf("body=%q", got.BodyMarkdown)
	}
}
