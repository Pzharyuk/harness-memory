package api

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Pzharyuk/harness-memory/internal/auth"
	"github.com/Pzharyuk/harness-memory/internal/lint"
	"github.com/Pzharyuk/harness-memory/internal/recall"
	"github.com/Pzharyuk/harness-memory/internal/types"
)

const (
	recallMaxLines = 200
	recallMaxBytes = 25 * 1024
)

// IndexLine is one recall index entry.
type IndexLine struct {
	ID      uuid.UUID `json:"id"`
	Kind    string    `json:"kind"`
	Title   string    `json:"title"`
	Summary string    `json:"summary"`
	Href    string    `json:"href"`
}

type recallResponse struct {
	User    []IndexLine      `json:"user"`
	Project []IndexLine      `json:"project"`
	Recent  []types.Revision `json:"recent"`
}

// tokenView is types.Token without TokenHash so list/JSON cannot leak hashes.
type tokenView struct {
	ID         uuid.UUID  `json:"id"`
	Harness    string     `json:"harness"`
	Label      string     `json:"label"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

func publicToken(t types.Token) tokenView {
	return tokenView{
		ID:         t.ID,
		Harness:    t.Harness,
		Label:      t.Label,
		CreatedAt:  t.CreatedAt,
		LastUsedAt: t.LastUsedAt,
		RevokedAt:  t.RevokedAt,
	}
}

func (s *server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) readyz(w http.ResponseWriter, r *http.Request) {
	var n int
	if err := s.st.Pool.QueryRow(r.Context(), "select 1").Scan(&n); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]bool{"ok": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) bootstrap(w http.ResponseWriter, r *http.Request) {
	toks, err := s.st.ListTokens(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if len(toks) > 0 {
		writeError(w, http.StatusConflict, "already bootstrapped")
		return
	}
	secret, err := randomSecret()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hash, err := auth.Hash(secret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	tok, err := s.st.CreateToken(r.Context(), "admin", "bootstrap", hash)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":        tok.ID,
		"harness":   tok.Harness,
		"plaintext": secret,
	})
}

func (s *server) createToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Harness string `json:"harness"`
		Label   string `json:"label"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Harness == "" {
		writeError(w, http.StatusBadRequest, "harness is required")
		return
	}
	secret, err := randomSecret()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hash, err := auth.Hash(secret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	tok, err := s.st.CreateToken(r.Context(), req.Harness, req.Label, hash)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":        tok.ID,
		"harness":   tok.Harness,
		"plaintext": secret,
	})
}

func (s *server) listTokens(w http.ResponseWriter, r *http.Request) {
	toks, err := s.st.ListTokens(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]tokenView, 0, len(toks))
	for _, t := range toks {
		out = append(out, publicToken(t))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) revokeToken(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.st.RevokeToken(r.Context(), id); err != nil {
		if strings.Contains(err.Error(), "token not found") {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) saveMemory(w http.ResponseWriter, r *http.Request) {
	tok, ok := tokenFrom(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var mem types.Memory
	if err := readJSON(r, &mem); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	res, err := s.st.SaveMemory(r.Context(), mem, tok.Harness)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *server) getMemory(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	mem, err := s.st.GetMemory(r.Context(), id)
	if err != nil {
		if strings.Contains(err.Error(), "memory not found") {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, mem)
}

type ingestSourceResponse struct {
	Source  types.Source `json:"source"`
	Created bool         `json:"created"`
}

func (s *server) ingestSource(w http.ResponseWriter, r *http.Request) {
	tok, ok := tokenFrom(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var src types.Source
	if err := readJSON(r, &src); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	src.CreatedByHarness = tok.Harness
	out, created, err := s.st.IngestSource(r.Context(), src)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ingestSourceResponse{Source: out, Created: created})
}

func (s *server) getSource(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	src, err := s.st.GetSource(r.Context(), id)
	if err != nil {
		if strings.Contains(err.Error(), "source not found") {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, src)
}

func (s *server) writePage(w http.ResponseWriter, r *http.Request) {
	tok, ok := tokenFrom(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var page types.Page
	if err := readJSON(r, &page); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	res, err := s.st.WritePage(r.Context(), page, tok.Harness)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *server) getPage(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	page, err := s.st.GetPage(r.Context(), id)
	if err != nil {
		if strings.Contains(err.Error(), "page not found") {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *server) recall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Project string    `json:"project"`
		Query   string    `json:"query"`
		ID      uuid.UUID `json:"id"`
	}
	if err := readJSON(r, &req); err != nil && err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.ID != uuid.Nil {
		mem, err := s.st.GetMemory(r.Context(), req.ID)
		if err != nil {
			if strings.Contains(err.Error(), "memory not found") {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, http.StatusOK, mem)
		return
	}

	var userLines, projectLines []IndexLine
	if req.Query != "" {
		userHits, err := s.st.SearchScoped(r.Context(), req.Query, types.ScopeUser, "", recallMaxLines)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		projectHits, err := s.st.SearchScoped(r.Context(), req.Query, types.ScopeProject, req.Project, recallMaxLines)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		userLines = hitLines(s, userHits)
		projectLines = hitLines(s, projectHits)
	} else {
		user, err := s.st.ListMemories(r.Context(), types.ScopeUser, "")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		project, err := s.st.ListMemories(r.Context(), types.ScopeProject, req.Project)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		userLines = indexLines(s, user)
		projectLines = indexLines(s, project)
	}
	writeJSON(w, http.StatusOK, recallResponse{
		User:    budgetIndex(userLines),
		Project: budgetIndex(projectLines),
		Recent:  []types.Revision{},
	})
}

func (s *server) lint(w http.ResponseWriter, r *http.Request) {
	findings, err := lint.RunWithOptions(r.Context(), s.st, r.URL.Query().Get("project"), lint.Options{
		ProjectionDir: s.cfg.ProjectionDir,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if findings == nil {
		findings = []lint.Finding{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"findings": findings})
}

func (s *server) listInbox(w http.ResponseWriter, r *http.Request) {
	ps, err := s.st.ListOpenProposals(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, ps)
}

func (s *server) proposeInbox(w http.ResponseWriter, r *http.Request) {
	tok, ok := tokenFrom(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var p types.Proposal
	if err := readJSON(r, &p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	p.CreatedByHarness = tok.Harness
	out, err := s.st.InsertProposal(r.Context(), p)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) acceptInbox(w http.ResponseWriter, r *http.Request) {
	tok, ok := tokenFrom(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.st.AcceptProposal(r.Context(), id, tok.Harness); err != nil {
		writeProposalErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) rejectInbox(w http.ResponseWriter, r *http.Request) {
	tok, ok := tokenFrom(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.st.RejectProposal(r.Context(), id, tok.Harness); err != nil {
		writeProposalErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func writeProposalErr(w http.ResponseWriter, err error) {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "not found"):
		writeError(w, http.StatusNotFound, "not found")
	case strings.Contains(msg, "not open"):
		writeError(w, http.StatusConflict, msg)
	case strings.Contains(msg, "admin"):
		writeError(w, http.StatusForbidden, "forbidden")
	default:
		writeError(w, http.StatusBadRequest, msg)
	}
}

func (s *server) search(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Q       string       `json:"q"`
		Project string       `json:"project"`
		Scope   *types.Scope `json:"scope"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if strings.TrimSpace(req.Q) == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}
	if req.Scope != nil && *req.Scope != types.ScopeUser && *req.Scope != types.ScopeProject {
		writeError(w, http.StatusBadRequest, "invalid scope")
		return
	}
	hits, err := s.st.Search(r.Context(), req.Q, req.Scope, req.Project, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, hits)
}

func indexLines(s *server, mems []types.Memory) []IndexLine {
	out := make([]IndexLine, 0, len(mems))
	for _, m := range mems {
		out = append(out, IndexLine{
			ID:      m.ID,
			Kind:    string(m.Kind),
			Title:   m.Title,
			Summary: m.Summary,
			Href:    s.memoryHref(m.ID),
		})
	}
	return out
}

func hitLines(s *server, hits []types.SearchHit) []IndexLine {
	out := make([]IndexLine, 0, len(hits))
	for _, h := range hits {
		href := s.memoryHref(h.ID)
		if h.Kind == "page" {
			href = s.pageHref(h.ID)
		}
		out = append(out, IndexLine{
			ID:      h.ID,
			Kind:    h.Kind,
			Title:   h.Title,
			Summary: h.Summary,
			Href:    href,
		})
	}
	return out
}

func formatIndexLine(l IndexLine) string {
	return l.Kind + " " + l.Title + " — " + l.Summary
}

func budgetIndex(lines []IndexLine) []IndexLine {
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
	out := make([]IndexLine, 0, n+1)
	if n > 0 {
		out = append(out, lines[:n]...)
	}
	if overflow {
		out = append(out, IndexLine{Title: recall.Overflow, Summary: recall.Overflow})
	}
	return out
}

func (s *server) memoryHref(id uuid.UUID) string {
	return s.apiHref("/v1/memories/" + id.String())
}

func (s *server) pageHref(id uuid.UUID) string {
	return s.apiHref("/v1/pages/" + id.String())
}

func (s *server) apiHref(path string) string {
	if s.cfg.URL == "" {
		return path
	}
	return strings.TrimRight(s.cfg.URL, "/") + path
}

func randomSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
