package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/readyz" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	var out bytes.Buffer
	env := Env{
		Stdout: &out,
		Stderr: io.Discard,
		Getenv: func(key string) string {
			if key == "MEMORY_URL" {
				return srv.URL
			}
			return ""
		},
		HTTP: srv.Client(),
	}
	code := Run([]string{"status"}, env)
	if code != 0 {
		t.Fatalf("exit=%d out=%q", code, out.String())
	}
	if !strings.Contains(out.String(), "ok") {
		t.Fatalf("output %q does not contain ok", out.String())
	}
}

func TestInitWritesConfigAndPrintsToken(t *testing.T) {
	const secret = "bootstrap-secret"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/readyz":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/admin/bootstrap":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"id":        "11111111-1111-1111-1111-111111111111",
				"harness":   "admin",
				"plaintext": secret,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	home := t.TempDir()
	var out bytes.Buffer
	env := Env{
		Stdout: &out,
		Stderr: io.Discard,
		Getenv: func(key string) string {
			switch key {
			case "MEMORY_URL":
				return srv.URL
			case "MEMORY_DATABASE_URL":
				return "postgres://memory:memory@127.0.0.1:55432/memory?sslmode=disable"
			case "HOME":
				return home
			default:
				return ""
			}
		},
		HTTP: srv.Client(),
	}
	code := Run([]string{"init"}, env)
	if code != 0 {
		t.Fatalf("exit=%d out=%q", code, out.String())
	}
	if !strings.Contains(out.String(), secret) {
		t.Fatalf("init did not print admin token: %q", out.String())
	}

	cfgPath := filepath.Join(home, ".config", "harness-memory", "config.toml")
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{"database_url", "listen", "projection_dir", "postgres://memory:memory@127.0.0.1:55432/memory?sslmode=disable"} {
		if !strings.Contains(text, want) {
			t.Fatalf("config missing %q:\n%s", want, text)
		}
	}
}
