package writerules

import (
	"strings"

	"github.com/Pzharyuk/harness-memory/internal/types"
)

// Decision is the result of tiered write rules: auto apply or open a proposal.
type Decision struct {
	Path   types.WritePath
	Reason string
}

// DecideMemorySave chooses auto vs proposed for a memory upsert.
// existing is the active row looked up by title (same scope); nil means create.
// Auto when new, equal body (after TrimSpace), or incoming body extends existing as a prefix.
func DecideMemorySave(existing *types.Memory, incoming types.Memory) Decision {
	if existing == nil {
		return Decision{Path: types.PathAuto, Reason: "new memory"}
	}
	if strings.TrimSpace(existing.Body) == strings.TrimSpace(incoming.Body) {
		return Decision{Path: types.PathAuto, Reason: "equal body"}
	}
	if strings.HasPrefix(incoming.Body, existing.Body) {
		return Decision{Path: types.PathAuto, Reason: "prefix update"}
	}
	return Decision{Path: types.PathProposed, Reason: "body contradicts existing memory"}
}

// DecidePageWrite chooses auto vs proposed for a wiki page write.
// existing is the active page looked up by slug; hasContradictsLink is true if the
// incoming graph would include a contradicts edge (caller-supplied).
// Auto when new without contradicts, equal body (after TrimSpace), or prefix extend.
func DecidePageWrite(existing *types.Page, incoming types.Page, hasContradictsLink bool) Decision {
	if hasContradictsLink {
		return Decision{Path: types.PathProposed, Reason: "contradicts link"}
	}
	if existing == nil {
		return Decision{Path: types.PathAuto, Reason: "new page"}
	}
	if strings.TrimSpace(existing.BodyMarkdown) == strings.TrimSpace(incoming.BodyMarkdown) {
		return Decision{Path: types.PathAuto, Reason: "equal body"}
	}
	if strings.HasPrefix(incoming.BodyMarkdown, existing.BodyMarkdown) {
		return Decision{Path: types.PathAuto, Reason: "prefix update"}
	}
	return Decision{Path: types.PathProposed, Reason: "body contradicts existing page"}
}

// DecideDelete always requires a proposal.
func DecideDelete() Decision {
	return Decision{Path: types.PathProposed, Reason: "delete requires proposal"}
}

// DecideScopeMove always requires a proposal.
func DecideScopeMove() Decision {
	return Decision{Path: types.PathProposed, Reason: "scope-move requires proposal"}
}
