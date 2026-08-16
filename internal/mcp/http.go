package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/Pzharyuk/harness-memory/internal/config"
	"github.com/Pzharyuk/harness-memory/internal/lint"
	"github.com/Pzharyuk/harness-memory/internal/recall"
	"github.com/Pzharyuk/harness-memory/internal/store"
	"github.com/Pzharyuk/harness-memory/internal/types"
)

const (
	recallMaxLines = 200
	recallMaxBytes = 25 * 1024
)

type ctxKey int

const harnessKey ctxKey = 1

type handler struct {
	st  *store.Store
	cfg config.Config
}

// New returns the JSON-RPC MCP handler for POST /mcp.
func New(st *store.Store, cfg config.Config) http.Handler {
	return &handler{st: st, cfg: cfg}
}

// WithHarness stores the authenticated harness name for tool writes.
func WithHarness(ctx context.Context, harness string) context.Context {
	return context.WithValue(ctx, harnessKey, harness)
}

func harnessFrom(ctx context.Context) (string, bool) {
	h, ok := ctx.Value(harnessKey).(string)
	return h, ok && h != ""
}

type tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type indexLine struct {
	ID      uuid.UUID `json:"id"`
	Kind    string    `json:"kind"`
	Title   string    `json:"title"`
	Summary string    `json:"summary"`
	Href    string    `json:"href"`
}

type recallResult struct {
	User    []indexLine      `json:"user"`
	Project []indexLine      `json:"project"`
	Recent  []types.Revision `json:"recent"`
}

func toolDefs() []tool {
	return []tool{
		{
			Name:        "recall",
			Description: "Session brief: user + project index, optional query, optional id for full body",
			InputSchema: objectSchema(map[string]any{
				"project": strProp("project slug"),
				"query":   strProp("optional FTS query"),
				"id":      strProp("memory id for full body"),
			}, nil),
		},
		{
			Name:        "save",
			Description: "Auto-write a memory (tiered rules apply)",
			InputSchema: objectSchema(map[string]any{
				"scope":        strProp("user or project"),
				"project_slug": strProp("project slug"),
				"kind":         strProp("user, feedback, project, or reference"),
				"title":        strProp("title"),
				"summary":      strProp("one-line summary"),
				"body":         strProp("body"),
				"source_id":    strProp("optional source id"),
			}, []string{"scope", "kind", "title"}),
		},
		{
			Name:        "search",
			Description: "FTS across memories and wiki pages",
			InputSchema: objectSchema(map[string]any{
				"q":       strProp("search query"),
				"project": strProp("optional project slug"),
				"scope":   strProp("optional user or project"),
			}, []string{"q"}),
		},
		{
			Name:        "ingest_source",
			Description: "Store an immutable raw source",
			InputSchema: objectSchema(map[string]any{
				"scope":        strProp("user or project"),
				"project_slug": strProp("project slug"),
				"kind":         strProp("import, file, url, or session"),
				"title":        strProp("title"),
				"body":         strProp("raw body"),
			}, []string{"scope", "kind"}),
		},
		{
			Name:        "read_page",
			Description: "Read a wiki page by id",
			InputSchema: objectSchema(map[string]any{
				"id": strProp("page id"),
			}, []string{"id"}),
		},
		{
			Name:        "write_page",
			Description: "Write a wiki page (tiered rules apply)",
			InputSchema: objectSchema(map[string]any{
				"scope":         strProp("user or project"),
				"project_slug":  strProp("project slug"),
				"slug":          strProp("page slug"),
				"title":         strProp("title"),
				"summary":       strProp("summary"),
				"body_markdown": strProp("markdown body"),
				"page_type":     strProp("entity, concept, source-summary, index, log, or synthesis"),
				"source_ids":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			}, []string{"scope", "title", "page_type"}),
		},
		{
			Name:        "lint",
			Description: "Read-only diagnostics",
			InputSchema: objectSchema(map[string]any{
				"project": strProp("optional project slug"),
			}, nil),
		},
		{
			Name:        "inbox_list",
			Description: "List open proposals",
			InputSchema: objectSchema(map[string]any{}, nil),
		},
		{
			Name:        "inbox_propose",
			Description: "File a proposal",
			InputSchema: objectSchema(map[string]any{
				"action":  strProp("create, update, supersede, delete, or scope-move"),
				"payload": map[string]any{"type": "object"},
				"reason":  strProp("reason"),
			}, []string{"action"}),
		},
	}
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func (h *handler) callTool(ctx context.Context, name, harness string, args json.RawMessage) (string, error) {
	switch name {
	case "recall":
		return h.toolRecall(ctx, args)
	case "save":
		return h.toolSave(ctx, harness, args)
	case "search":
		return h.toolSearch(ctx, args)
	case "ingest_source":
		return h.toolIngestSource(ctx, harness, args)
	case "read_page":
		return h.toolReadPage(ctx, args)
	case "write_page":
		return h.toolWritePage(ctx, harness, args)
	case "lint":
		return h.toolLint(ctx, args)
	case "inbox_list":
		return h.toolInboxList(ctx)
	case "inbox_propose":
		return h.toolInboxPropose(ctx, harness, args)
	default:
		return "", fmt.Errorf("unknown tool %q", name)
	}
}

func (h *handler) toolSave(ctx context.Context, harness string, args json.RawMessage) (string, error) {
	var mem types.Memory
	if err := json.Unmarshal(args, &mem); err != nil {
		return "", fmt.Errorf("invalid arguments")
	}
	res, err := h.st.SaveMemory(ctx, mem, harness)
	if err != nil {
		return "", err
	}
	return toolJSON(res)
}

func (h *handler) toolRecall(ctx context.Context, args json.RawMessage) (string, error) {
	var req struct {
		Project string    `json:"project"`
		Query   string    `json:"query"`
		ID      uuid.UUID `json:"id"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &req); err != nil && err != io.EOF {
			return "", fmt.Errorf("invalid arguments")
		}
	}
	if req.ID != uuid.Nil {
		mem, err := h.st.GetMemory(ctx, req.ID)
		if err != nil {
			return "", err
		}
		return toolJSON(mem)
	}

	var userLines, projectLines []indexLine
	if req.Query != "" {
		userHits, err := h.st.SearchScoped(ctx, req.Query, types.ScopeUser, "", recallMaxLines)
		if err != nil {
			return "", err
		}
		projectHits, err := h.st.SearchScoped(ctx, req.Query, types.ScopeProject, req.Project, recallMaxLines)
		if err != nil {
			return "", err
		}
		userLines = h.hitLines(userHits)
		projectLines = h.hitLines(projectHits)
	} else {
		user, err := h.st.ListMemories(ctx, types.ScopeUser, "")
		if err != nil {
			return "", err
		}
		project, err := h.st.ListMemories(ctx, types.ScopeProject, req.Project)
		if err != nil {
			return "", err
		}
		userLines = h.memoryLines(user)
		projectLines = h.memoryLines(project)
	}
	return toolJSON(recallResult{
		User:    budgetIndex(userLines),
		Project: budgetIndex(projectLines),
		Recent:  []types.Revision{},
	})
}

func (h *handler) toolSearch(ctx context.Context, args json.RawMessage) (string, error) {
	var req struct {
		Q       string       `json:"q"`
		Project string       `json:"project"`
		Scope   *types.Scope `json:"scope"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return "", fmt.Errorf("invalid arguments")
	}
	if strings.TrimSpace(req.Q) == "" {
		return "", fmt.Errorf("q is required")
	}
	if req.Scope != nil && *req.Scope != types.ScopeUser && *req.Scope != types.ScopeProject {
		return "", fmt.Errorf("invalid scope")
	}
	hits, err := h.st.Search(ctx, req.Q, req.Scope, req.Project, 0)
	if err != nil {
		return "", err
	}
	return toolJSON(hits)
}

func (h *handler) toolIngestSource(ctx context.Context, harness string, args json.RawMessage) (string, error) {
	var src types.Source
	if err := json.Unmarshal(args, &src); err != nil {
		return "", fmt.Errorf("invalid arguments")
	}
	src.CreatedByHarness = harness
	out, created, err := h.st.IngestSource(ctx, src)
	if err != nil {
		return "", err
	}
	return toolJSON(map[string]any{"source": out, "created": created})
}

func (h *handler) toolReadPage(ctx context.Context, args json.RawMessage) (string, error) {
	var req struct {
		ID uuid.UUID `json:"id"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return "", fmt.Errorf("invalid arguments")
	}
	if req.ID == uuid.Nil {
		return "", fmt.Errorf("id is required")
	}
	page, err := h.st.GetPage(ctx, req.ID)
	if err != nil {
		return "", err
	}
	return toolJSON(page)
}

func (h *handler) toolWritePage(ctx context.Context, harness string, args json.RawMessage) (string, error) {
	var page types.Page
	if err := json.Unmarshal(args, &page); err != nil {
		return "", fmt.Errorf("invalid arguments")
	}
	res, err := h.st.WritePage(ctx, page, harness)
	if err != nil {
		return "", err
	}
	return toolJSON(res)
}

func (h *handler) toolLint(ctx context.Context, args json.RawMessage) (string, error) {
	var req struct {
		Project string `json:"project"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &req); err != nil && err != io.EOF {
			return "", fmt.Errorf("invalid arguments")
		}
	}
	findings, err := lint.RunWithOptions(ctx, h.st, req.Project, lint.Options{
		ProjectionDir: h.cfg.ProjectionDir,
	})
	if err != nil {
		return "", err
	}
	if findings == nil {
		findings = []lint.Finding{}
	}
	return toolJSON(map[string]any{"findings": findings})
}

func (h *handler) toolInboxList(ctx context.Context) (string, error) {
	ps, err := h.st.ListOpenProposals(ctx)
	if err != nil {
		return "", err
	}
	return toolJSON(ps)
}

func (h *handler) toolInboxPropose(ctx context.Context, harness string, args json.RawMessage) (string, error) {
	var p types.Proposal
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid arguments")
	}
	p.CreatedByHarness = harness
	out, err := h.st.InsertProposal(ctx, p)
	if err != nil {
		return "", err
	}
	return toolJSON(out)
}

func (h *handler) memoryLines(mems []types.Memory) []indexLine {
	out := make([]indexLine, 0, len(mems))
	for _, m := range mems {
		out = append(out, indexLine{
			ID:      m.ID,
			Kind:    string(m.Kind),
			Title:   m.Title,
			Summary: m.Summary,
			Href:    h.apiHref("/v1/memories/" + m.ID.String()),
		})
	}
	return out
}

func (h *handler) hitLines(hits []types.SearchHit) []indexLine {
	out := make([]indexLine, 0, len(hits))
	for _, hit := range hits {
		href := h.apiHref("/v1/memories/" + hit.ID.String())
		if hit.Kind == "page" {
			href = h.apiHref("/v1/pages/" + hit.ID.String())
		}
		out = append(out, indexLine{
			ID:      hit.ID,
			Kind:    hit.Kind,
			Title:   hit.Title,
			Summary: hit.Summary,
			Href:    href,
		})
	}
	return out
}

func (h *handler) apiHref(path string) string {
	if h.cfg.URL == "" {
		return path
	}
	return strings.TrimRight(h.cfg.URL, "/") + path
}

func formatIndexLine(l indexLine) string {
	return l.Kind + " " + l.Title + " — " + l.Summary
}

func budgetIndex(lines []indexLine) []indexLine {
	rendered := make([]string, len(lines))
	for i, l := range lines {
		rendered[i] = formatIndexLine(l)
	}
	kept := recall.Budget(rendered, recallMaxLines, recallMaxBytes)
	n := len(kept)
	overflow := n > 0 && kept[n-1] == recall.Overflow
	if overflow {
		n--
	}
	out := make([]indexLine, 0, n+1)
	if n > 0 {
		out = append(out, lines[:n]...)
	}
	if overflow {
		out = append(out, indexLine{Title: recall.Overflow, Summary: recall.Overflow})
	}
	return out
}
