package store

import (
	"context"
	"testing"

	"github.com/Pzharyuk/harness-memory/internal/types"
)

func TestSearchPipenvRanksFirst(t *testing.T) {
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
	if _, err := st.SaveMemory(ctx, types.Memory{
		Scope:   types.ScopeUser,
		Kind:    types.MemoryKindUser,
		Title:   "vault raft",
		Summary: "vault raft quorum",
		Body:    "vault raft",
	}, "grok"); err != nil {
		t.Fatal(err)
	}

	hits, err := st.Search(ctx, "pipenv", nil, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("no hits")
	}
	if hits[0].Title != "pipenv" {
		t.Fatalf("first title=%q want pipenv (hits=%+v)", hits[0].Title, hits)
	}
}
