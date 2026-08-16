package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Pzharyuk/harness-memory/internal/recall"
	"github.com/Pzharyuk/harness-memory/internal/types"
	"github.com/Pzharyuk/harness-memory/internal/wikilink"
)

const (
	maxIndexLines = 200
	maxIndexBytes = 25 * 1024
)

// Write projects memories and pages into dir as MEMORY.md plus topic files.
// Index lines are budgeted together (200 lines / 25KB). File mode 0600, dir 0700.
func Write(dir string, memories []types.Memory, pages []types.Page) error {
	if dir == "" {
		return fmt.Errorf("dir is required")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("chmod %s: %w", dir, err)
	}

	var lines []string
	var files []topicFile
	for _, m := range memories {
		name := topicName(string(m.Kind), wikilink.Slugify(m.Title))
		lines = append(lines, indexLine(m.Title, name, m.Summary))
		files = append(files, topicFile{name: name, body: m.Body})
	}
	for _, p := range pages {
		slug := p.Slug
		if slug == "" {
			slug = wikilink.Slugify(p.Title)
		}
		name := topicName(string(p.PageType), slug)
		lines = append(lines, indexLine(p.Title, name, p.Summary))
		files = append(files, topicFile{name: name, body: p.BodyMarkdown})
	}

	kept := recall.Budget(lines, maxIndexLines, maxIndexBytes)
	index := ""
	if len(kept) > 0 {
		index = strings.Join(kept, "\n") + "\n"
	}
	if err := writeFile(filepath.Join(dir, "MEMORY.md"), index); err != nil {
		return err
	}
	for _, f := range files {
		if err := writeFile(filepath.Join(dir, f.name), f.body); err != nil {
			return err
		}
	}
	return nil
}

type topicFile struct {
	name string
	body string
}

func topicName(kind, slug string) string {
	if slug == "" {
		slug = "untitled"
	}
	return kind + "_" + slug + ".md"
}

func indexLine(title, filename, summary string) string {
	line := "- [" + title + "](" + filename + ")"
	if summary != "" {
		line += " — " + summary
	}
	return line
}

func writeFile(path, body string) error {
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return nil
}
