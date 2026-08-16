package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/Pzharyuk/harness-memory/internal/store"
	"github.com/Pzharyuk/harness-memory/internal/types"
)

type contextKey int

// CtxToken is the request context key for the authenticated types.Token.
const CtxToken contextKey = iota

func (s *server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isV1(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		secret, ok := bearerToken(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		tok, err := s.st.Authenticate(r.Context(), secret)
		if err != nil {
			if errors.Is(err, store.ErrUnauthorized) {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		tok.TokenHash = ""
		if isAdminPath(r.URL.Path) && tok.Harness != "admin" {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		ctx := context.WithValue(r.Context(), CtxToken, tok)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func isV1(path string) bool {
	return path == "/v1" || strings.HasPrefix(path, "/v1/")
}

func isAdminPath(path string) bool {
	return strings.HasPrefix(path, "/v1/admin/")
}

func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	typ, rest, ok := strings.Cut(h, " ")
	if !ok || !strings.EqualFold(typ, "Bearer") {
		return "", false
	}
	tok := strings.TrimSpace(rest)
	return tok, tok != ""
}

func tokenFrom(r *http.Request) (types.Token, bool) {
	tok, ok := r.Context().Value(CtxToken).(types.Token)
	return tok, ok
}
