package api

import (
	"encoding/json"
	"net/http"

	"github.com/Pzharyuk/harness-memory/internal/config"
	"github.com/Pzharyuk/harness-memory/internal/store"
)

// New returns the memoryd HTTP handler.
func New(st *store.Store, cfg config.Config) http.Handler {
	s := &server{st: st, cfg: cfg}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /readyz", s.readyz)
	mux.HandleFunc("POST /v1/admin/tokens", s.createToken)
	mux.HandleFunc("GET /v1/admin/tokens", s.listTokens)
	mux.HandleFunc("POST /v1/admin/tokens/{id}/revoke", s.revokeToken)
	mux.HandleFunc("POST /v1/memories", s.saveMemory)
	mux.HandleFunc("GET /v1/memories/{id}", s.getMemory)
	mux.HandleFunc("POST /v1/recall", s.recall)
	return s.withAuth(mux)
}

type server struct {
	st  *store.Store
	cfg config.Config
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func readJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(dst)
}
