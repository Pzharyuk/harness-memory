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

func TestMCPProxiesStdio(t *testing.T) {
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`))
	}))
	t.Cleanup(srv.Close)

	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n")
	var out bytes.Buffer
	env := Env{
		Stdin:  in,
		Stdout: &out,
		Stderr: io.Discard,
		Getenv: func(key string) string {
			switch key {
			case "MEMORY_URL":
				return srv.URL
			case "MEMORY_TOKEN":
				return "cli-token"
			default:
				return ""
			}
		},
		HTTP: srv.Client(),
	}
	code := Run([]string{"mcp"}, env)
	if code != 0 {
		t.Fatalf("exit=%d out=%q", code, out.String())
	}
	if gotPath != "/mcp" {
		t.Fatalf("path=%q want /mcp", gotPath)
	}
	if gotAuth != "Bearer cli-token" {
		t.Fatalf("auth=%q", gotAuth)
	}
	if !strings.Contains(out.String(), `"ok":true`) {
		t.Fatalf("stdout=%s", out.String())
	}
}

func TestImportClaudeDryRun(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "claude-memory")
	var out bytes.Buffer
	env := Env{
		Stdout: &out,
		Stderr: io.Discard,
		Getenv: func(string) string { return "" },
	}
	code := Run([]string{"import", "claude", "--path", path, "--dry-run"}, env)
	if code != 0 {
		t.Fatalf("exit=%d out=%q", code, out.String())
	}
	text := out.String()
	if !strings.Contains(text, "Python tooling preferences") {
		t.Fatalf("dry-run missing title: %q", text)
	}
	if !strings.Contains(text, "feedback") {
		t.Fatalf("dry-run missing kind: %q", text)
	}
}

func TestInboxAcceptRejectLintHTTP(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/inbox":
			_, _ = w.Write([]byte(`[{"id":"11111111-1111-1111-1111-111111111111","action":"update","reason":"conflict","status":"open","created_by_harness":"grok"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/admin/inbox/11111111-1111-1111-1111-111111111111/accept":
			_, _ = w.Write([]byte(`{"ok":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/admin/inbox/11111111-1111-1111-1111-111111111111/reject":
			_, _ = w.Write([]byte(`{"ok":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/lint":
			_, _ = w.Write([]byte(`{"findings":[{"kind":"broken_link","message":"[[missing]]"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	envFor := func(out *bytes.Buffer) Env {
		return Env{
			Stdout: out,
			Stderr: io.Discard,
			Getenv: func(key string) string {
				switch key {
				case "MEMORY_URL":
					return srv.URL
				case "MEMORY_TOKEN":
					return "admin-token"
				default:
					return ""
				}
			},
			HTTP: srv.Client(),
		}
	}

	var inboxOut bytes.Buffer
	if code := Run([]string{"inbox"}, envFor(&inboxOut)); code != 0 {
		t.Fatalf("inbox exit=%d out=%q", code, inboxOut.String())
	}
	if !strings.Contains(inboxOut.String(), "11111111-1111-1111-1111-111111111111") {
		t.Fatalf("inbox missing id: %q", inboxOut.String())
	}

	var acceptOut bytes.Buffer
	if code := Run([]string{"accept", "11111111-1111-1111-1111-111111111111"}, envFor(&acceptOut)); code != 0 {
		t.Fatalf("accept exit=%d out=%q", code, acceptOut.String())
	}

	var rejectOut bytes.Buffer
	if code := Run([]string{"reject", "11111111-1111-1111-1111-111111111111"}, envFor(&rejectOut)); code != 0 {
		t.Fatalf("reject exit=%d out=%q", code, rejectOut.String())
	}

	var lintOut bytes.Buffer
	if code := Run([]string{"lint"}, envFor(&lintOut)); code != 0 {
		t.Fatalf("lint exit=%d out=%q", code, lintOut.String())
	}
	if !strings.Contains(lintOut.String(), "broken_link") {
		t.Fatalf("lint missing finding: %q", lintOut.String())
	}

	want := []string{
		"GET /v1/inbox",
		"POST /v1/admin/inbox/11111111-1111-1111-1111-111111111111/accept",
		"POST /v1/admin/inbox/11111111-1111-1111-1111-111111111111/reject",
		"GET /v1/lint",
	}
	if len(got) != len(want) {
		t.Fatalf("calls=%v want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("call[%d]=%q want %q", i, got[i], w)
		}
	}
}
