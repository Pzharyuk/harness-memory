package wikilink

import (
	"regexp"
	"strings"

	"github.com/Pzharyuk/harness-memory/internal/types"
)

// Ref is one parsed [[wikilink]] target.
type Ref struct {
	Slug string
	Rel  types.Rel
}

var wikiLinkRe = regexp.MustCompile(`\[\[([^\]]+)\]\]`)

// Parse extracts [[wikilinks]] from body.
// [[Foo]] → slug foo rel related; [[uses:Bar]] → rel uses slug bar.
// Slug: lowercase, spaces → '-', strip non [a-z0-9-]. Empty slugs are dropped.
// Duplicate (slug, rel) pairs keep the first occurrence.
func Parse(body string) []Ref {
	matches := wikiLinkRe.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	out := make([]Ref, 0, len(matches))
	for _, m := range matches {
		ref, ok := parseInner(m[1])
		if !ok {
			continue
		}
		key := string(ref.Rel) + "\x00" + ref.Slug
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	return out
}

// Slugify lowercases, turns spaces into '-', and strips characters outside [a-z0-9-].
func Slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			b.WriteByte(c)
		}
	}
	return b.String()
}

func parseInner(inner string) (Ref, bool) {
	inner = strings.TrimSpace(inner)
	if inner == "" {
		return Ref{}, false
	}
	rel := types.RelRelated
	target := inner
	if left, right, ok := strings.Cut(inner, ":"); ok {
		if r, valid := parseRel(strings.TrimSpace(left)); valid {
			rel = r
			target = strings.TrimSpace(right)
		}
	}
	slug := Slugify(target)
	if slug == "" {
		return Ref{}, false
	}
	return Ref{Slug: slug, Rel: rel}, true
}

func parseRel(s string) (types.Rel, bool) {
	r := types.Rel(strings.ToLower(s))
	switch r {
	case types.RelRelated, types.RelUses, types.RelDependsOn, types.RelSupersedes, types.RelContradicts:
		return r, true
	default:
		return "", false
	}
}
