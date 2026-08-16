package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Pzharyuk/harness-memory/internal/api"
	"github.com/Pzharyuk/harness-memory/internal/auth"
	"github.com/Pzharyuk/harness-memory/internal/config"
	"github.com/Pzharyuk/harness-memory/internal/mcp"
	"github.com/Pzharyuk/harness-memory/internal/store"
	"github.com/Pzharyuk/harness-memory/internal/types"
)

func openTest(t *testing.T) *store.Store {
	t.Helper()
	url := os.Getenv("MEMORY_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("MEMORY_TEST_DATABASE_URL unset")
	}
	st, err := store.Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_, err := st.Pool.Exec(ctx, `
			TRUNCATE TABLE
				audit_log,
				tokens,
				proposals,
				revisions,
				wiki_links,
				wiki_pages,
				memories,
				sources
			CASCADE
		`)
		if err != nil {
			t.Errorf("truncate: %v", err)
		}
		st.Pool.Close()
	})
	return st
}

type testEnv struct {
	t     *testing.T
	st    *store.Store
	srv   *httptest.Server
	admin string
	grok  string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	st := openTest(t)
	ctx := context.Background()

	const adminSecret = "admin-secret"
	const grokSecret = "grok-secret"
	adminHash, err := auth.Hash(adminSecret)
	if err != nil {
		t.Fatal(err)
	}
	grokHash, err := auth.Hash(grokSecret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateToken(ctx, "admin", "test-admin", adminHash); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateToken(ctx, "grok", "test-grok", grokHash); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(api.New(st, config.Config{}))
	t.Cleanup(srv.Close)
	return &testEnv{t: t, st: st, srv: srv, admin: adminSecret, grok: grokSecret}
}

func (e *testEnv) rpc(id int, method, token string, params any) (int, []byte) {
	e.t.Helper()
	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		body["params"] = params
	}
	raw, err := json.Marshal(body)
	if err != nil {
		e.t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, e.srv.URL+"/mcp", bytes.NewReader(raw))
	if err != nil {
		e.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	got, err := io.ReadAll(res.Body)
	if err != nil {
		e.t.Fatal(err)
	}
	return res.StatusCode, got
}

func TestMCPUnauthorized(t *testing.T) {
	e := newTestEnv(t)
	status, body := e.rpc(1, "initialize", "", map[string]any{"protocolVersion": "2025-03-26"})
	if status != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", status, body)
	}
}

func TestToolsListHasSaveNotInboxAccept(t *testing.T) {
	e := newTestEnv(t)
	status, body := e.rpc(1, "initialize", e.grok, map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "test", "version": "0"},
	})
	if status != http.StatusOK {
		t.Fatalf("initialize status=%d body=%s", status, body)
	}
	var initResp struct {
		JSONRPC string `json:"jsonrpc"`
		Error   *struct {
			Message string `json:"message"`
		} `json:"error"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &initResp); err != nil {
		t.Fatalf("initialize json: %v body=%s", err, body)
	}
	if initResp.JSONRPC != "2.0" {
		t.Fatalf("jsonrpc=%q", initResp.JSONRPC)
	}
	if initResp.Error != nil {
		t.Fatalf("initialize error: %s", initResp.Error.Message)
	}
	if len(initResp.Result) == 0 {
		t.Fatalf("initialize missing result: %s", body)
	}

	status, body = e.rpc(2, "tools/list", e.grok, map[string]any{})
	if status != http.StatusOK {
		t.Fatalf("tools/list status=%d body=%s", status, body)
	}
	var listResp struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &listResp); err != nil {
		t.Fatalf("tools/list json: %v body=%s", err, body)
	}
	if listResp.Error != nil {
		t.Fatalf("tools/list error: %s", listResp.Error.Message)
	}

	names := map[string]bool{}
	for _, tool := range listResp.Result.Tools {
		names[tool.Name] = true
	}
	if !names["save"] {
		t.Fatalf("tools/list missing save: %s", body)
	}
	if names["inbox_accept"] {
		t.Fatalf("tools/list must not contain inbox_accept: %s", body)
	}
	for _, forbid := range []string{"token_create", "token_list", "token_revoke", "create_token"} {
		if names[forbid] {
			t.Fatalf("tools/list must not contain %s: %s", forbid, body)
		}
	}
}

func TestSaveThenRecallReturnsFact(t *testing.T) {
	e := newTestEnv(t)
	status, body := e.rpc(1, "tools/call", e.grok, map[string]any{
		"name": "save",
		"arguments": types.Memory{
			Scope:   types.ScopeUser,
			Kind:    types.MemoryKindUser,
			Title:   "pipenv",
			Summary: "use pipenv",
			Body:    "use pipenv",
		},
	})
	if status != http.StatusOK {
		t.Fatalf("save status=%d body=%s", status, body)
	}
	if strings.Contains(string(body), `"error"`) && !strings.Contains(string(body), `"isError":false`) {
		var errResp struct {
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
			Result struct {
				IsError bool `json:"isError"`
			} `json:"result"`
		}
		if err := json.Unmarshal(body, &errResp); err == nil {
			if errResp.Error != nil {
				t.Fatalf("save rpc error: %s", errResp.Error.Message)
			}
			if errResp.Result.IsError {
				t.Fatalf("save isError: %s", body)
			}
		}
	}

	status, body = e.rpc(2, "tools/call", e.grok, map[string]any{
		"name":      "recall",
		"arguments": map[string]any{"project": ""},
	})
	if status != http.StatusOK {
		t.Fatalf("recall status=%d body=%s", status, body)
	}
	if !strings.Contains(string(body), "use pipenv") || !strings.Contains(string(body), "pipenv") {
		t.Fatalf("recall missing saved fact: %s", body)
	}
}

func TestStdioProxyForwardsBearer(t *testing.T) {
	var gotAuth, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`))
	}))
	t.Cleanup(srv.Close)

	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n")
	var out bytes.Buffer
	if err := mcp.Proxy(context.Background(), in, &out, srv.URL, "test-token", srv.Client()); err != nil {
		t.Fatalf("proxy: %v", err)
	}
	if gotPath != "/mcp" {
		t.Fatalf("path=%q want /mcp", gotPath)
	}
	if gotAuth != "Bearer test-token" {
		t.Fatalf("auth=%q", gotAuth)
	}
	if !strings.Contains(gotBody, `"method":"initialize"`) {
		t.Fatalf("forwarded body=%s", gotBody)
	}
	if !strings.Contains(out.String(), `"ok":true`) {
		t.Fatalf("stdout=%s", out.String())
	}
}
