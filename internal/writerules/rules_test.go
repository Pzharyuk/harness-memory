package writerules

import (
	"testing"

	"github.com/Pzharyuk/harness-memory/internal/types"
)

func TestNewMemoryIsAuto(t *testing.T) {
	d := DecideMemorySave(nil, types.Memory{Title: "pipenv", Body: "use pipenv"})
	if d.Path != types.PathAuto {
		t.Fatalf("%+v", d)
	}
}

func TestSameBodyIsAuto(t *testing.T) {
	ex := &types.Memory{Title: "pipenv", Body: "use pipenv"}
	d := DecideMemorySave(ex, types.Memory{Title: "pipenv", Body: "use pipenv"})
	if d.Path != types.PathAuto {
		t.Fatalf("%+v", d)
	}
}

func TestDifferentBodyIsProposed(t *testing.T) {
	ex := &types.Memory{Title: "pipenv", Body: "use pipenv"}
	d := DecideMemorySave(ex, types.Memory{Title: "pipenv", Body: "use poetry"})
	if d.Path != types.PathProposed {
		t.Fatalf("%+v", d)
	}
}

func TestPrefixUpdateIsAuto(t *testing.T) {
	ex := &types.Memory{Title: "pipenv", Body: "use pipenv"}
	d := DecideMemorySave(ex, types.Memory{Title: "pipenv", Body: "use pipenv\nalways pin"})
	if d.Path != types.PathAuto {
		t.Fatalf("%+v", d)
	}
}

func TestEmptyExistingBodyIsProposed(t *testing.T) {
	ex := &types.Memory{Title: "pipenv", Body: ""}
	d := DecideMemorySave(ex, types.Memory{Title: "pipenv", Body: "use poetry"})
	if d.Path != types.PathProposed {
		t.Fatalf("empty existing body must not prefix-match: %+v", d)
	}
}

func TestEmptyExistingPageBodyIsProposed(t *testing.T) {
	ex := &types.Page{Slug: "vault", BodyMarkdown: ""}
	d := DecidePageWrite(ex, types.Page{Slug: "vault", BodyMarkdown: "new"}, false)
	if d.Path != types.PathProposed {
		t.Fatalf("empty existing page body must not prefix-match: %+v", d)
	}
}

func TestDeleteIsProposed(t *testing.T) {
	if DecideDelete().Path != types.PathProposed {
		t.Fatal("delete must be proposed")
	}
}

func TestPageConflictProposed(t *testing.T) {
	ex := &types.Page{Slug: "vault", BodyMarkdown: "old"}
	d := DecidePageWrite(ex, types.Page{Slug: "vault", BodyMarkdown: "new"}, false)
	if d.Path != types.PathProposed {
		t.Fatalf("%+v", d)
	}
}

func TestPageContradictsLinkProposed(t *testing.T) {
	d := DecidePageWrite(nil, types.Page{Slug: "vault", BodyMarkdown: "x"}, true)
	if d.Path != types.PathProposed {
		t.Fatalf("%+v", d)
	}
}
