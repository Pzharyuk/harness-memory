package api

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

	"github.com/Pzharyuk/harness-memory/internal/auth"
	"github.com/Pzharyuk/harness-memory/internal/config"
	"github.com/Pzharyuk/harness-memory/internal/store"
	"github.com/Pzharyuk/harness-memory/internal/types"
)

func openTest(t *testing.T) *store.Store {
	t.Helper()
	store.LockTestDB(t)
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

	srv := httptest.NewServer(New(st, config.Config{}))
	t.Cleanup(srv.Close)
	return &testEnv{t: t, st: st, srv: srv, admin: adminSecret, grok: grokSecret}
}

func (e *testEnv) do(method, path, token, body string) *http.Response {
	e.t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, e.srv.URL+path, rdr)
	if err != nil {
		e.t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	return res
}

func readBody(t *testing.T, res *http.Response) []byte {
	t.Helper()
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestHealthzNoAuth(t *testing.T) {
	e := newTestEnv(t)
	res := e.do(http.MethodGet, "/healthz", "", "")
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.StatusCode, body)
	}
	var got struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("json: %v body=%s", err, body)
	}
	if !got.OK {
		t.Fatalf("ok=false body=%s", body)
	}
}

func TestRecallUnauthorized(t *testing.T) {
	e := newTestEnv(t)
	res := e.do(http.MethodPost, "/v1/recall", "", `{}`)
	body := readBody(t, res)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", res.StatusCode, body)
	}
	var got struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("json: %v body=%s", err, body)
	}
	if got.Error != "unauthorized" {
		t.Fatalf("error=%q want unauthorized", got.Error)
	}
}

func TestGrokSaveAndRecall(t *testing.T) {
	e := newTestEnv(t)
	mem := types.Memory{
		Scope:   types.ScopeUser,
		Kind:    types.MemoryKindUser,
		Title:   "pipenv",
		Summary: "use pipenv",
		Body:    "use pipenv",
	}
	raw, err := json.Marshal(mem)
	if err != nil {
		t.Fatal(err)
	}
	res := e.do(http.MethodPost, "/v1/memories", e.grok, string(raw))
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("save status=%d body=%s", res.StatusCode, body)
	}
	var saved types.SaveResult
	if err := json.Unmarshal(body, &saved); err != nil {
		t.Fatalf("save json: %v body=%s", err, body)
	}
	if saved.Status != types.SaveStatusApplied {
		t.Fatalf("status=%q want applied", saved.Status)
	}

	res = e.do(http.MethodPost, "/v1/recall", e.grok, `{"project":""}`)
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("recall status=%d body=%s", res.StatusCode, body)
	}
	var rec struct {
		User    []IndexLine `json:"user"`
		Project []IndexLine `json:"project"`
	}
	if err := json.Unmarshal(body, &rec); err != nil {
		t.Fatalf("recall json: %v body=%s", err, body)
	}
	found := false
	for _, line := range rec.User {
		if line.Summary == "use pipenv" && line.Title == "pipenv" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("recall missing summary: %+v", rec.User)
	}
}

func TestRecallQueryEmptyProjectDoesNotListOtherProjects(t *testing.T) {
	e := newTestEnv(t)
	for _, mem := range []types.Memory{
		{Scope: types.ScopeUser, Kind: types.MemoryKindUser, Title: "vault-user", Summary: "user vault", Body: "vault user fact"},
		{Scope: types.ScopeProject, ProjectSlug: "other", Kind: types.MemoryKindProject, Title: "vault-other", Summary: "other vault", Body: "vault project fact"},
	} {
		raw, err := json.Marshal(mem)
		if err != nil {
			t.Fatal(err)
		}
		res := e.do(http.MethodPost, "/v1/memories", e.grok, string(raw))
		body := readBody(t, res)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("save %q status=%d body=%s", mem.Title, res.StatusCode, body)
		}
	}

	res := e.do(http.MethodPost, "/v1/recall", e.grok, `{"query":"vault"}`)
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("recall status=%d body=%s", res.StatusCode, body)
	}
	var rec struct {
		User    []IndexLine `json:"user"`
		Project []IndexLine `json:"project"`
	}
	if err := json.Unmarshal(body, &rec); err != nil {
		t.Fatalf("recall json: %v body=%s", err, body)
	}
	foundUser := false
	for _, line := range rec.User {
		if line.Title == "vault-user" {
			foundUser = true
		}
	}
	if !foundUser {
		t.Fatalf("user recall missing vault-user: %+v", rec.User)
	}
	for _, line := range rec.Project {
		if line.Title == "vault-other" {
			t.Fatalf("empty project leaked other project's memory: %+v", rec.Project)
		}
	}
}

func TestGrokAdminForbidden(t *testing.T) {
	e := newTestEnv(t)
	res := e.do(http.MethodPost, "/v1/admin/tokens", e.grok, `{"harness":"claude","label":"nope"}`)
	body := readBody(t, res)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", res.StatusCode, body)
	}
}

func TestBootstrapOnceThenConflict(t *testing.T) {
	st := openTest(t)
	srv := httptest.NewServer(New(st, config.Config{}))
	t.Cleanup(srv.Close)
	e := &testEnv{t: t, st: st, srv: srv}

	res := e.do(http.MethodPost, "/v1/admin/bootstrap", "", "")
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("first bootstrap status=%d body=%s", res.StatusCode, body)
	}
	var got struct {
		ID        string `json:"id"`
		Harness   string `json:"harness"`
		Plaintext string `json:"plaintext"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("json: %v body=%s", err, body)
	}
	if got.ID == "" || got.Harness != "admin" || got.Plaintext == "" {
		t.Fatalf("bootstrap=%+v", got)
	}
	if bytes.Contains(body, []byte("token_hash")) {
		t.Fatal("bootstrap leaked token_hash")
	}

	res = e.do(http.MethodPost, "/v1/admin/bootstrap", "", "")
	body = readBody(t, res)
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("second bootstrap status=%d body=%s", res.StatusCode, body)
	}

	res = e.do(http.MethodGet, "/v1/admin/tokens", got.Plaintext, "")
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("bootstrapped admin list status=%d body=%s", res.StatusCode, body)
	}
}

func TestIngestSourceIdempotentHTTP(t *testing.T) {
	e := newTestEnv(t)
	payload := `{"scope":"user","kind":"import","title":"notes","body":"hello source"}`

	res := e.do(http.MethodPost, "/v1/sources", e.grok, payload)
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("first ingest status=%d body=%s", res.StatusCode, body)
	}
	var first struct {
		Source  types.Source `json:"source"`
		Created bool         `json:"created"`
	}
	if err := json.Unmarshal(body, &first); err != nil {
		t.Fatalf("json: %v body=%s", err, body)
	}
	if !first.Created {
		t.Fatal("first created=false")
	}
	if first.Source.ID.String() == "00000000-0000-0000-0000-000000000000" {
		t.Fatal("empty source id")
	}
	if first.Source.CreatedByHarness != "grok" {
		t.Fatalf("harness=%q want grok", first.Source.CreatedByHarness)
	}
	if first.Source.ContentSHA256 == "" {
		t.Fatal("missing content_sha256")
	}

	res = e.do(http.MethodPost, "/v1/sources", e.grok, payload)
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("second ingest status=%d body=%s", res.StatusCode, body)
	}
	var second struct {
		Source  types.Source `json:"source"`
		Created bool         `json:"created"`
	}
	if err := json.Unmarshal(body, &second); err != nil {
		t.Fatalf("json: %v body=%s", err, body)
	}
	if second.Created {
		t.Fatal("second created=true want false")
	}
	if second.Source.ID != first.Source.ID {
		t.Fatalf("id=%s want %s", second.Source.ID, first.Source.ID)
	}

	res = e.do(http.MethodGet, "/v1/sources/"+first.Source.ID.String(), e.grok, "")
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("get status=%d body=%s", res.StatusCode, body)
	}
	var got types.Source
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("get json: %v body=%s", err, body)
	}
	if got.ID != first.Source.ID || got.Body != "hello source" {
		t.Fatalf("get=%+v", got)
	}
}

func TestWriteAndGetPageHTTP(t *testing.T) {
	e := newTestEnv(t)
	page := types.Page{
		Scope:        types.ScopeUser,
		Slug:         "vault-ha",
		Title:        "Vault HA",
		Summary:      "Vault high availability",
		BodyMarkdown: "Vault runs in HA.",
		PageType:     types.PageTypeEntity,
	}
	raw, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	res := e.do(http.MethodPost, "/v1/pages", e.grok, string(raw))
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("write status=%d body=%s", res.StatusCode, body)
	}
	var saved types.SaveResult
	if err := json.Unmarshal(body, &saved); err != nil {
		t.Fatalf("write json: %v body=%s", err, body)
	}
	if saved.Status != types.SaveStatusApplied {
		t.Fatalf("status=%q want applied", saved.Status)
	}
	if saved.ID.String() == "00000000-0000-0000-0000-000000000000" {
		t.Fatal("empty id")
	}

	res = e.do(http.MethodGet, "/v1/pages/"+saved.ID.String(), e.grok, "")
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("get status=%d body=%s", res.StatusCode, body)
	}
	var got types.Page
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("get json: %v body=%s", err, body)
	}
	if got.ID != saved.ID || got.Slug != "vault-ha" || got.BodyMarkdown != "Vault runs in HA." {
		t.Fatalf("get=%+v", got)
	}
	if got.UpdatedByHarness != "grok" {
		t.Fatalf("harness=%q want grok", got.UpdatedByHarness)
	}
}

func TestSearchPipenvRanksFirstHTTP(t *testing.T) {
	e := newTestEnv(t)
	for _, mem := range []types.Memory{
		{Scope: types.ScopeUser, Kind: types.MemoryKindUser, Title: "pipenv", Summary: "use pipenv", Body: "use pipenv"},
		{Scope: types.ScopeUser, Kind: types.MemoryKindUser, Title: "vault raft", Summary: "vault raft quorum", Body: "vault raft"},
	} {
		raw, err := json.Marshal(mem)
		if err != nil {
			t.Fatal(err)
		}
		res := e.do(http.MethodPost, "/v1/memories", e.grok, string(raw))
		body := readBody(t, res)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("save %q status=%d body=%s", mem.Title, res.StatusCode, body)
		}
	}

	res := e.do(http.MethodPost, "/v1/search", e.grok, `{"q":"pipenv"}`)
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("search status=%d body=%s", res.StatusCode, body)
	}
	var hits []types.SearchHit
	if err := json.Unmarshal(body, &hits); err != nil {
		t.Fatalf("search json: %v body=%s", err, body)
	}
	if len(hits) == 0 {
		t.Fatal("no hits")
	}
	if hits[0].Title != "pipenv" {
		t.Fatalf("first title=%q want pipenv hits=%+v", hits[0].Title, hits)
	}
}

func TestAdminMintToken(t *testing.T) {
	e := newTestEnv(t)
	res := e.do(http.MethodPost, "/v1/admin/tokens", e.admin, `{"harness":"claude","label":"dev"}`)
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.StatusCode, body)
	}
	var got struct {
		ID        string `json:"id"`
		Harness   string `json:"harness"`
		Plaintext string `json:"plaintext"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("json: %v body=%s", err, body)
	}
	if got.ID == "" || got.Harness != "claude" || got.Plaintext == "" {
		t.Fatalf("mint=%+v", got)
	}
	if bytes.Contains(body, []byte("token_hash")) {
		t.Fatal("create response leaked token_hash")
	}

	// minted plaintext authenticates
	res = e.do(http.MethodPost, "/v1/recall", got.Plaintext, `{"project":""}`)
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("minted token recall status=%d body=%s", res.StatusCode, body)
	}

	res = e.do(http.MethodGet, "/v1/admin/tokens", e.admin, "")
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d body=%s", res.StatusCode, body)
	}
	if bytes.Contains(body, []byte("token_hash")) {
		t.Fatal("list leaked token_hash")
	}
}

func TestLintBrokenLinkHTTP(t *testing.T) {
	e := newTestEnv(t)
	page := types.Page{
		Scope:        types.ScopeUser,
		Slug:         "lonely",
		Title:        "Lonely",
		BodyMarkdown: "see [[missing]]",
		PageType:     types.PageTypeEntity,
	}
	raw, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	res := e.do(http.MethodPost, "/v1/pages", e.grok, string(raw))
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("write status=%d body=%s", res.StatusCode, body)
	}

	res = e.do(http.MethodGet, "/v1/lint", e.grok, "")
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("lint status=%d body=%s", res.StatusCode, body)
	}
	var got struct {
		Findings []struct {
			Kind string `json:"kind"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("json: %v body=%s", err, body)
	}
	found := false
	for _, f := range got.Findings {
		if f.Kind == "broken_link" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing broken_link: %s", body)
	}
}

func TestInboxProposeAndList(t *testing.T) {
	e := newTestEnv(t)
	res := e.do(http.MethodPost, "/v1/inbox", e.grok, `{"action":"delete","payload":{"title":"x"},"reason":"cleanup"}`)
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("propose status=%d body=%s", res.StatusCode, body)
	}
	var created types.Proposal
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("json: %v body=%s", err, body)
	}
	if created.ID.String() == "00000000-0000-0000-0000-000000000000" {
		t.Fatal("empty proposal id")
	}
	if created.CreatedByHarness != "grok" {
		t.Fatalf("harness=%q want grok", created.CreatedByHarness)
	}

	res = e.do(http.MethodGet, "/v1/inbox", e.grok, "")
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d body=%s", res.StatusCode, body)
	}
	var listed []types.Proposal
	if err := json.Unmarshal(body, &listed); err != nil {
		t.Fatalf("list json: %v body=%s", err, body)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("listed=%+v", listed)
	}
}

func TestAcceptForbiddenForGrok(t *testing.T) {
	e := newTestEnv(t)
	mem := types.Memory{
		Scope:   types.ScopeUser,
		Kind:    types.MemoryKindUser,
		Title:   "pipenv",
		Summary: "use pipenv",
		Body:    "use pipenv",
	}
	raw, err := json.Marshal(mem)
	if err != nil {
		t.Fatal(err)
	}
	res := e.do(http.MethodPost, "/v1/memories", e.grok, string(raw))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("save status=%d body=%s", res.StatusCode, readBody(t, res))
	}
	readBody(t, res)

	mem.Body = "use poetry"
	raw, err = json.Marshal(mem)
	if err != nil {
		t.Fatal(err)
	}
	res = e.do(http.MethodPost, "/v1/memories", e.grok, string(raw))
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("conflict status=%d body=%s", res.StatusCode, body)
	}
	var proposed types.SaveResult
	if err := json.Unmarshal(body, &proposed); err != nil {
		t.Fatal(err)
	}
	if proposed.ProposalID == nil {
		t.Fatal("missing proposal id")
	}

	res = e.do(http.MethodPost, "/v1/admin/inbox/"+proposed.ProposalID.String()+"/accept", e.grok, `{}`)
	body = readBody(t, res)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", res.StatusCode, body)
	}
}

func TestAcceptAdminUpdatesBodyHTTP(t *testing.T) {
	e := newTestEnv(t)
	mem := types.Memory{
		Scope:   types.ScopeUser,
		Kind:    types.MemoryKindUser,
		Title:   "pipenv",
		Summary: "use pipenv",
		Body:    "use pipenv",
	}
	raw, err := json.Marshal(mem)
	if err != nil {
		t.Fatal(err)
	}
	res := e.do(http.MethodPost, "/v1/memories", e.grok, string(raw))
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("save status=%d body=%s", res.StatusCode, body)
	}

	mem.Body = "use poetry"
	raw, err = json.Marshal(mem)
	if err != nil {
		t.Fatal(err)
	}
	res = e.do(http.MethodPost, "/v1/memories", e.grok, string(raw))
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("conflict status=%d body=%s", res.StatusCode, body)
	}
	var proposed types.SaveResult
	if err := json.Unmarshal(body, &proposed); err != nil {
		t.Fatal(err)
	}
	if proposed.ProposalID == nil {
		t.Fatal("missing proposal id")
	}

	res = e.do(http.MethodPost, "/v1/admin/inbox/"+proposed.ProposalID.String()+"/accept", e.admin, `{}`)
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("accept status=%d body=%s", res.StatusCode, body)
	}

	res = e.do(http.MethodGet, "/v1/memories/"+proposed.ID.String(), e.grok, "")
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("get status=%d body=%s", res.StatusCode, body)
	}
	var got types.Memory
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Body != "use poetry" {
		t.Fatalf("body=%q want use poetry", got.Body)
	}
}

func TestRejectLeavesOriginalHTTP(t *testing.T) {
	e := newTestEnv(t)
	mem := types.Memory{
		Scope:   types.ScopeUser,
		Kind:    types.MemoryKindUser,
		Title:   "pipenv",
		Summary: "use pipenv",
		Body:    "use pipenv",
	}
	raw, err := json.Marshal(mem)
	if err != nil {
		t.Fatal(err)
	}
	res := e.do(http.MethodPost, "/v1/memories", e.grok, string(raw))
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("save status=%d body=%s", res.StatusCode, body)
	}
	var saved types.SaveResult
	if err := json.Unmarshal(body, &saved); err != nil {
		t.Fatal(err)
	}

	mem.Body = "use poetry"
	raw, err = json.Marshal(mem)
	if err != nil {
		t.Fatal(err)
	}
	res = e.do(http.MethodPost, "/v1/memories", e.grok, string(raw))
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("conflict status=%d body=%s", res.StatusCode, body)
	}
	var proposed types.SaveResult
	if err := json.Unmarshal(body, &proposed); err != nil {
		t.Fatal(err)
	}
	if proposed.ProposalID == nil {
		t.Fatal("missing proposal id")
	}

	res = e.do(http.MethodPost, "/v1/admin/inbox/"+proposed.ProposalID.String()+"/reject", e.admin, `{}`)
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("reject status=%d body=%s", res.StatusCode, body)
	}

	res = e.do(http.MethodGet, "/v1/memories/"+saved.ID.String(), e.grok, "")
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("get status=%d body=%s", res.StatusCode, body)
	}
	var got types.Memory
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Body != "use pipenv" {
		t.Fatalf("body=%q want original", got.Body)
	}
}
