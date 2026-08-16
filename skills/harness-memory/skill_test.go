package skilltest

import (
	"os"
	"strings"
	"testing"
)

func TestSkillContainsRequiredGuidance(t *testing.T) {
	raw, err := os.ReadFile("SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, want := range []string{"recall", "save", "write_page", "memory inbox"} {
		if !strings.Contains(body, want) {
			t.Errorf("SKILL.md missing %q", want)
		}
	}
}
