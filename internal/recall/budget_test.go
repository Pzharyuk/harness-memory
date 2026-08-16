package recall

import (
	"strings"
	"testing"
)

func TestBudgetMaxLines(t *testing.T) {
	lines := make([]string, 201)
	for i := range lines {
		lines[i] = "x\n"
	}
	got := Budget(lines, 200, 25*1024)
	if len(got) != 201 {
		t.Fatalf("len=%d want 201 (200 + overflow)", len(got))
	}
	for i := 0; i < 200; i++ {
		if got[i] != "x\n" {
			t.Fatalf("line %d = %q want %q", i, got[i], "x\n")
		}
	}
	if got[200] != Overflow {
		t.Fatalf("overflow=%q want %q", got[200], Overflow)
	}
}

func TestBudgetMaxBytes(t *testing.T) {
	const maxBytes = 25 * 1024
	lines := []string{strings.Repeat("a", maxBytes+1)}
	got := Budget(lines, 200, maxBytes)
	if len(got) == 0 {
		t.Fatal("empty result")
	}
	if got[len(got)-1] != Overflow {
		t.Fatalf("missing overflow: %#v", got)
	}
	n := 0
	for _, s := range got[:len(got)-1] {
		n += len(s)
	}
	if n > maxBytes {
		t.Fatalf("kept %d bytes want <= %d", n, maxBytes)
	}
}
