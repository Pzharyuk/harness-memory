package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pzharyuk/harness-memory/internal/recall"
	"github.com/Pzharyuk/harness-memory/internal/types"
)

func TestWriteIndexContainsTitlesAndTopicFile(t *testing.T) {
	dir := t.TempDir()
	mems := []types.Memory{
		{
			Kind:    types.MemoryKindFeedback,
			Title:   "Python tooling preferences",
			Summary: "use pipenv",
			Body:    "use pipenv for python",
		},
		{
			Kind:    types.MemoryKindProject,
			Title:   "Vault",
			Summary: "raft quorum",
			Body:    "raft quorum for vault",
		},
	}
	if err := Write(dir, mems, nil); err != nil {
		t.Fatal(err)
	}

	index, err := os.ReadFile(filepath.Join(dir, "MEMORY.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(index)
	if !strings.Contains(text, "Python tooling preferences") {
		t.Fatalf("MEMORY.md missing first title:\n%s", text)
	}
	if !strings.Contains(text, "Vault") {
		t.Fatalf("MEMORY.md missing second title:\n%s", text)
	}

	topic := filepath.Join(dir, "feedback_python-tooling-preferences.md")
	body, err := os.ReadFile(topic)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "use pipenv for python" {
		t.Fatalf("topic body=%q", body)
	}

	st, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode=%o want 0700", st.Mode().Perm())
	}
	st, err = os.Stat(filepath.Join(dir, "MEMORY.md"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("MEMORY.md mode=%o want 0600", st.Mode().Perm())
	}
	st, err = os.Stat(topic)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("topic mode=%o want 0600", st.Mode().Perm())
	}
}

func TestWriteBudgetOverflowAddsSearchLine(t *testing.T) {
	dir := t.TempDir()
	mems := make([]types.Memory, 201)
	for i := range mems {
		mems[i] = types.Memory{
			Kind:    types.MemoryKindUser,
			Title:   fmt.Sprintf("Fact %03d", i),
			Summary: "s",
			Body:    "b",
		}
	}
	if err := Write(dir, mems, nil); err != nil {
		t.Fatal(err)
	}
	index, err := os.ReadFile(filepath.Join(dir, "MEMORY.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(index)
	if !strings.Contains(text, recall.Overflow) {
		t.Fatalf("MEMORY.md missing overflow line:\n%s", text)
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if lines[len(lines)-1] != recall.Overflow {
		t.Fatalf("last line=%q want %q", lines[len(lines)-1], recall.Overflow)
	}
}
