package wikilink

import (
	"testing"

	"github.com/Pzharyuk/harness-memory/internal/types"
)

func TestParseVaultHAAndDependsOn(t *testing.T) {
	refs := Parse("see [[Vault HA]] and [[depends_on:Postgres]]")
	if len(refs) != 2 {
		t.Fatalf("len=%d want 2 refs=%v", len(refs), refs)
	}
	if refs[0].Slug != "vault-ha" || refs[0].Rel != types.RelRelated {
		t.Fatalf("ref0=%+v want slug=vault-ha rel=related", refs[0])
	}
	if refs[1].Slug != "postgres" || refs[1].Rel != types.RelDependsOn {
		t.Fatalf("ref1=%+v want slug=postgres rel=depends_on", refs[1])
	}
}

func TestParseBareAndTypedRel(t *testing.T) {
	refs := Parse("[[Foo]] then [[uses:Bar]]")
	if len(refs) != 2 {
		t.Fatalf("len=%d want 2 refs=%v", len(refs), refs)
	}
	if refs[0].Slug != "foo" || refs[0].Rel != types.RelRelated {
		t.Fatalf("ref0=%+v want slug=foo rel=related", refs[0])
	}
	if refs[1].Slug != "bar" || refs[1].Rel != types.RelUses {
		t.Fatalf("ref1=%+v want slug=bar rel=uses", refs[1])
	}
}

func TestParseEmptyAndJunk(t *testing.T) {
	if refs := Parse(""); len(refs) != 0 {
		t.Fatalf("empty body refs=%v", refs)
	}
	if refs := Parse("no links here"); len(refs) != 0 {
		t.Fatalf("no-link refs=%v", refs)
	}
	if refs := Parse("[[!!!]]"); len(refs) != 0 {
		t.Fatalf("stripped-empty refs=%v", refs)
	}
}
