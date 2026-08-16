package importclaude

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/Pzharyuk/harness-memory/internal/store"
	"github.com/Pzharyuk/harness-memory/internal/types"
	"github.com/Pzharyuk/harness-memory/internal/wikilink"
)

const (
	// Harness is the created_by / updated_by value for imported rows.
	Harness = "import"

	sourceSummaryBytes = 2000
)

// Item is one topic file from a Claude MEMORY.md index.
type Item struct {
	File    string
	Kind    types.MemoryKind
	Title   string
	Summary string
	Body    string
}

// Plan is a parsed Claude auto-memory directory.
type Plan struct {
	Dir      string
	Index    string
	Memories []Item
}

var indexLineRe = regexp.MustCompile(`^\s*-\s*\[([^\]]+)\]\(([^)]+)\)(?:\s+[—–-]+\s*(.*))?\s*$`)

// Parse reads MEMORY.md and the topic files it links to.
func Parse(dir string) (Plan, error) {
	if dir == "" {
		return Plan{}, fmt.Errorf("dir is required")
	}
	indexPath := filepath.Join(dir, "MEMORY.md")
	raw, err := os.ReadFile(indexPath)
	if err != nil {
		return Plan{}, fmt.Errorf("read MEMORY.md: %w", err)
	}
	plan := Plan{Dir: dir, Index: string(raw)}
	for i, line := range strings.Split(plan.Index, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		m := indexLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		title := strings.TrimSpace(m[1])
		rel := strings.TrimSpace(m[2])
		summary := strings.TrimSpace(m[3])
		if title == "" || rel == "" {
			return Plan{}, fmt.Errorf("MEMORY.md:%d: empty title or path", i+1)
		}
		topic, err := resolveTopic(dir, rel)
		if err != nil {
			return Plan{}, fmt.Errorf("MEMORY.md:%d: %w", i+1, err)
		}
		body, err := os.ReadFile(topic)
		if err != nil {
			return Plan{}, fmt.Errorf("read %s: %w", rel, err)
		}
		plan.Memories = append(plan.Memories, Item{
			File:    filepath.Base(rel),
			Kind:    kindFromFilename(rel),
			Title:   title,
			Summary: summary,
			Body:    string(body),
		})
	}
	return plan, nil
}

// Apply writes one import source, a memory per topic file, and a source-summary
// page when a body is larger than 2000 bytes. Rows are attributed to harness.
func Apply(ctx context.Context, st *store.Store, plan Plan, harness string) error {
	if st == nil {
		return fmt.Errorf("store is required")
	}
	if harness == "" {
		harness = Harness
	}

	src, _, err := st.IngestSource(ctx, types.Source{
		Scope:            types.ScopeUser,
		Kind:             types.SourceKindImport,
		Title:            sourceTitle(plan),
		Body:             sourceBody(plan),
		CreatedByHarness: harness,
	})
	if err != nil {
		return fmt.Errorf("ingest source: %w", err)
	}
	srcID := src.ID

	for _, item := range plan.Memories {
		mem := types.Memory{
			Scope:    types.ScopeUser,
			Kind:     item.Kind,
			Title:    item.Title,
			Summary:  item.Summary,
			Body:     item.Body,
			SourceID: &srcID,
		}
		if _, err := st.SaveMemory(ctx, mem, harness); err != nil {
			return fmt.Errorf("save %q: %w", item.Title, err)
		}
		if len(item.Body) <= sourceSummaryBytes {
			continue
		}
		page := types.Page{
			Scope:        types.ScopeUser,
			Slug:         wikilink.Slugify(item.Title),
			Title:        item.Title,
			Summary:      item.Summary,
			BodyMarkdown: item.Body,
			PageType:     types.PageTypeSourceSummary,
			SourceIDs:    []uuid.UUID{srcID},
		}
		if _, err := st.WritePage(ctx, page, harness); err != nil {
			return fmt.Errorf("write page %q: %w", item.Title, err)
		}
	}
	return nil
}

func sourceTitle(plan Plan) string {
	if plan.Dir != "" {
		return "Claude import " + filepath.Base(plan.Dir)
	}
	return "Claude import"
}

func sourceBody(plan Plan) string {
	var b strings.Builder
	b.WriteString(plan.Index)
	for _, item := range plan.Memories {
		b.WriteString("\n# ")
		b.WriteString(item.File)
		b.WriteByte('\n')
		b.WriteString(item.Body)
	}
	return b.String()
}

func kindFromFilename(name string) types.MemoryKind {
	base := filepath.Base(name)
	switch {
	case strings.HasPrefix(base, "feedback_"):
		return types.MemoryKindFeedback
	case strings.HasPrefix(base, "project_"):
		return types.MemoryKindProject
	case strings.HasPrefix(base, "reference_"):
		return types.MemoryKindReference
	case strings.HasPrefix(base, "user_"):
		return types.MemoryKindUser
	default:
		return types.MemoryKindProject
	}
}

func resolveTopic(dir, rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) {
		return "", fmt.Errorf("invalid topic path %q", rel)
	}
	clean := filepath.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid topic path %q", rel)
	}
	return filepath.Join(dir, clean), nil
}
