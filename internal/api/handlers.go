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
	"github.com/Pzharyuk/harness-memory/internal/types"
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
	writeJSON(w, http.StatusOK, recallResponse{
		User:    indexLines(s, user),
		Project: indexLines(s, project),
		Recent:  []types.Revision{},
	})
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

func (s *server) memoryHref(id uuid.UUID) string {
	path := "/v1/memories/" + id.String()
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
