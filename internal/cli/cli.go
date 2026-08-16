package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Pzharyuk/harness-memory/internal/api"
	"github.com/Pzharyuk/harness-memory/internal/config"
	"github.com/Pzharyuk/harness-memory/internal/importclaude"
	"github.com/Pzharyuk/harness-memory/internal/mcp"
	"github.com/Pzharyuk/harness-memory/internal/project"
	"github.com/Pzharyuk/harness-memory/internal/store"
)

const (
	defaultListen = "127.0.0.1:8741"
	defaultURL    = "http://127.0.0.1:8741"
	readyPoll     = 200 * time.Millisecond
	readyWait     = 30 * time.Second
	httpTimeout   = 10 * time.Second
)

const usage = `usage: memory <command> [flags]

commands:
  init              write config, wait for /readyz, mint admin token
  token create      mint a harness token (--harness, --label)
  token list        list tokens
  token revoke      revoke a token (--id)
  status            show URL, readiness, and token
  serve             run the memoryd HTTP server
  mcp               stdio MCP proxy to memoryd
  project           write MEMORY.md projection (--out)
  import claude     import a Claude auto-memory dir (--path, --dry-run)
  inbox             list open proposals
  accept            accept a proposal (<id>)
  reject            reject a proposal (<id>)
  lint              read-only diagnostics (--project)
`

// Env is the process environment for Run. Zero values use os and http defaults.
type Env struct {
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
	Getenv    func(string) string
	HTTP      *http.Client
	ReadyWait time.Duration
	Sleep     func(time.Duration)
}

// Client is an HTTP client for memoryd.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

type apiError struct {
	Status int
	Msg    string
}

func (e *apiError) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	return http.StatusText(e.Status)
}

type tokenMint struct {
	ID        string `json:"id"`
	Harness   string `json:"harness"`
	Plaintext string `json:"plaintext"`
}

type tokenRow struct {
	ID        string  `json:"id"`
	Harness   string  `json:"harness"`
	Label     string  `json:"label"`
	RevokedAt *string `json:"revoked_at"`
}

// Run dispatches a cobra-free CLI. args do not include the program name.
func Run(args []string, env Env) int {
	env = env.withDefaults()
	if len(args) == 0 {
		fmt.Fprint(env.Stderr, usage)
		return 2
	}
	switch args[0] {
	case "init":
		return runInit(args[1:], env)
	case "token":
		return runToken(args[1:], env)
	case "status":
		return runStatus(args[1:], env)
	case "serve":
		return runServe(args[1:], env)
	case "mcp":
		return runMCP(args[1:], env)
	case "project":
		return runProject(args[1:], env)
	case "import":
		return runImport(args[1:], env)
	case "inbox":
		return runInbox(args[1:], env)
	case "accept":
		return runAccept(args[1:], env)
	case "reject":
		return runReject(args[1:], env)
	case "lint":
		return runLint(args[1:], env)
	case "help", "-h", "--help":
		fmt.Fprint(env.Stdout, usage)
		return 0
	default:
		fmt.Fprintf(env.Stderr, "unknown command: %s\n", args[0])
		fmt.Fprint(env.Stderr, usage)
		return 2
	}
}

func (e Env) withDefaults() Env {
	if e.Stdin == nil {
		e.Stdin = os.Stdin
	}
	if e.Stdout == nil {
		e.Stdout = os.Stdout
	}
	if e.Stderr == nil {
		e.Stderr = os.Stderr
	}
	if e.Getenv == nil {
		e.Getenv = os.Getenv
	}
	if e.HTTP == nil {
		e.HTTP = &http.Client{Timeout: httpTimeout}
	}
	if e.ReadyWait <= 0 {
		e.ReadyWait = readyWait
	}
	if e.Sleep == nil {
		e.Sleep = time.Sleep
	}
	return e
}

func (e Env) getenv(key string) string {
	if e.Getenv == nil {
		return os.Getenv(key)
	}
	return e.Getenv(key)
}

func loadCfg(env Env) config.Config {
	home := env.getenv("HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	c := config.Config{
		Listen:        defaultListen,
		URL:           defaultURL,
		ConfigPath:    filepath.Join(home, ".config", "harness-memory", "config.toml"),
		ProjectionDir: filepath.Join(home, ".local", "share", "harness-memory", "projection"),
	}
	if raw, err := os.ReadFile(c.ConfigPath); err == nil {
		applyTOML(raw, &c)
	}
	if v := env.getenv("MEMORY_DATABASE_URL"); v != "" {
		c.DatabaseURL = v
	}
	if v := env.getenv("MEMORY_LISTEN"); v != "" {
		c.Listen = v
	}
	if v := env.getenv("MEMORY_URL"); v != "" {
		c.URL = v
	}
	if v := env.getenv("MEMORY_TOKEN"); v != "" {
		c.Token = v
	}
	if v := env.getenv("MEMORY_PROJECTION_DIR"); v != "" {
		c.ProjectionDir = v
	}
	return c
}

func applyTOML(raw []byte, c *config.Config) {
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if u, err := strconv.Unquote(v); err == nil {
			v = u
		}
		switch k {
		case "database_url":
			c.DatabaseURL = v
		case "listen":
			c.Listen = v
		case "projection_dir":
			c.ProjectionDir = v
		}
	}
}

func encodeTOML(c config.Config) string {
	var b strings.Builder
	fmt.Fprintf(&b, "database_url = %q\n", c.DatabaseURL)
	fmt.Fprintf(&b, "listen = %q\n", c.Listen)
	fmt.Fprintf(&b, "projection_dir = %q\n", c.ProjectionDir)
	return b.String()
}

func newClient(cfg config.Config, env Env) *Client {
	return &Client{BaseURL: cfg.URL, Token: cfg.Token, HTTP: env.HTTP}
}

func (c *Client) do(method, path string, body any) (*http.Response, []byte, error) {
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, nil, err
		}
		rdr = bytes.NewReader(raw)
	}
	url := strings.TrimRight(c.BaseURL, "/") + path
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		return nil, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return res, nil, err
	}
	return res, raw, nil
}

func decodeAPIError(status int, raw []byte) error {
	var got struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &got); err == nil && got.Error != "" {
		return &apiError{Status: status, Msg: got.Error}
	}
	return &apiError{Status: status, Msg: strings.TrimSpace(string(raw))}
}

func (c *Client) readyz() (bool, error) {
	res, raw, err := c.do(http.MethodGet, "/readyz", nil)
	if err != nil {
		return false, err
	}
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusServiceUnavailable {
		return false, decodeAPIError(res.StatusCode, raw)
	}
	var got struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		return false, err
	}
	return got.OK, nil
}

func (c *Client) bootstrap() (tokenMint, error) {
	res, raw, err := c.do(http.MethodPost, "/v1/admin/bootstrap", map[string]string{})
	if err != nil {
		return tokenMint{}, err
	}
	if res.StatusCode != http.StatusOK {
		return tokenMint{}, decodeAPIError(res.StatusCode, raw)
	}
	var got tokenMint
	if err := json.Unmarshal(raw, &got); err != nil {
		return tokenMint{}, err
	}
	return got, nil
}

func (c *Client) createToken(harness, label string) (tokenMint, error) {
	res, raw, err := c.do(http.MethodPost, "/v1/admin/tokens", map[string]string{
		"harness": harness,
		"label":   label,
	})
	if err != nil {
		return tokenMint{}, err
	}
	if res.StatusCode != http.StatusOK {
		return tokenMint{}, decodeAPIError(res.StatusCode, raw)
	}
	var got tokenMint
	if err := json.Unmarshal(raw, &got); err != nil {
		return tokenMint{}, err
	}
	return got, nil
}

func (c *Client) listTokens() ([]tokenRow, error) {
	res, raw, err := c.do(http.MethodGet, "/v1/admin/tokens", nil)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, decodeAPIError(res.StatusCode, raw)
	}
	var got []tokenRow
	if err := json.Unmarshal(raw, &got); err != nil {
		return nil, err
	}
	return got, nil
}

func (c *Client) revokeToken(id string) error {
	res, raw, err := c.do(http.MethodPost, "/v1/admin/tokens/"+id+"/revoke", map[string]string{})
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return decodeAPIError(res.StatusCode, raw)
	}
	return nil
}

func (c *Client) tokenHarness() (string, error) {
	res, raw, err := c.do(http.MethodGet, "/v1/admin/tokens", nil)
	if err != nil {
		return "", err
	}
	switch res.StatusCode {
	case http.StatusOK:
		return "admin", nil
	case http.StatusForbidden:
		return "", nil
	default:
		return "", decodeAPIError(res.StatusCode, raw)
	}
}

func waitReady(c *Client, env Env) error {
	deadline := time.Now().Add(env.ReadyWait)
	var last error
	for {
		ok, err := c.readyz()
		if err == nil && ok {
			return nil
		}
		if err != nil {
			last = err
		} else {
			last = fmt.Errorf("not ready")
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("timeout waiting for /readyz: %w", last)
		}
		env.Sleep(readyPoll)
	}
}

func runInit(args []string, env Env) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg := loadCfg(env)
	if err := os.MkdirAll(filepath.Dir(cfg.ConfigPath), 0o700); err != nil {
		fmt.Fprintf(env.Stderr, "init: %v\n", err)
		return 1
	}
	if err := os.WriteFile(cfg.ConfigPath, []byte(encodeTOML(cfg)), 0o600); err != nil {
		fmt.Fprintf(env.Stderr, "init: write config: %v\n", err)
		return 1
	}

	c := newClient(cfg, env)
	if err := waitReady(c, env); err != nil {
		fmt.Fprintf(env.Stderr, "init: %v\n", err)
		return 1
	}
	mint, err := c.bootstrap()
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) && ae.Status == http.StatusConflict {
			fmt.Fprintln(env.Stderr, "admin token already exists")
			return 0
		}
		fmt.Fprintf(env.Stderr, "init: bootstrap: %v\n", err)
		return 1
	}
	fmt.Fprintln(env.Stdout, mint.Plaintext)
	return 0
}

func runToken(args []string, env Env) int {
	if len(args) == 0 {
		fmt.Fprintln(env.Stderr, "usage: memory token <create|list|revoke>")
		return 2
	}
	switch args[0] {
	case "create":
		return runTokenCreate(args[1:], env)
	case "list":
		return runTokenList(args[1:], env)
	case "revoke":
		return runTokenRevoke(args[1:], env)
	default:
		fmt.Fprintf(env.Stderr, "unknown token command: %s\n", args[0])
		return 2
	}
}

func runTokenCreate(args []string, env Env) int {
	fs := flag.NewFlagSet("token create", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	harness := fs.String("harness", "", "harness name")
	label := fs.String("label", "", "optional label")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *harness == "" {
		fmt.Fprintln(env.Stderr, "--harness is required")
		return 2
	}
	cfg := loadCfg(env)
	if cfg.Token == "" {
		fmt.Fprintln(env.Stderr, "MEMORY_TOKEN is required")
		return 1
	}
	mint, err := newClient(cfg, env).createToken(*harness, *label)
	if err != nil {
		fmt.Fprintf(env.Stderr, "token create: %v\n", err)
		return 1
	}
	fmt.Fprintln(env.Stdout, mint.Plaintext)
	return 0
}

func runTokenList(args []string, env Env) int {
	fs := flag.NewFlagSet("token list", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg := loadCfg(env)
	if cfg.Token == "" {
		fmt.Fprintln(env.Stderr, "MEMORY_TOKEN is required")
		return 1
	}
	toks, err := newClient(cfg, env).listTokens()
	if err != nil {
		fmt.Fprintf(env.Stderr, "token list: %v\n", err)
		return 1
	}
	for _, tok := range toks {
		state := "active"
		if tok.RevokedAt != nil {
			state = "revoked"
		}
		fmt.Fprintf(env.Stdout, "%s\t%s\t%s\t%s\n", tok.ID, tok.Harness, tok.Label, state)
	}
	return 0
}

func runTokenRevoke(args []string, env Env) int {
	fs := flag.NewFlagSet("token revoke", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	id := fs.String("id", "", "token id")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *id == "" {
		fmt.Fprintln(env.Stderr, "--id is required")
		return 2
	}
	cfg := loadCfg(env)
	if cfg.Token == "" {
		fmt.Fprintln(env.Stderr, "MEMORY_TOKEN is required")
		return 1
	}
	if err := newClient(cfg, env).revokeToken(*id); err != nil {
		fmt.Fprintf(env.Stderr, "token revoke: %v\n", err)
		return 1
	}
	fmt.Fprintln(env.Stdout, "ok")
	return 0
}

func runStatus(args []string, env Env) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg := loadCfg(env)
	c := newClient(cfg, env)
	fmt.Fprintf(env.Stdout, "url\t%s\n", cfg.URL)
	ok, err := c.readyz()
	if err != nil {
		fmt.Fprintf(env.Stdout, "ready\terror\n")
		fmt.Fprintf(env.Stderr, "status: %v\n", err)
		return 1
	}
	if ok {
		fmt.Fprintf(env.Stdout, "ready\tok\n")
	} else {
		fmt.Fprintf(env.Stdout, "ready\tdown\n")
	}
	if cfg.Token != "" {
		harness, herr := c.tokenHarness()
		switch {
		case herr != nil:
			fmt.Fprintf(env.Stdout, "token\tset\n")
		case harness != "":
			fmt.Fprintf(env.Stdout, "harness\t%s\n", harness)
		default:
			fmt.Fprintf(env.Stdout, "token\tset\n")
		}
	}
	return 0
}

func runMCP(args []string, env Env) int {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg := loadCfg(env)
	if cfg.URL == "" {
		fmt.Fprintln(env.Stderr, "MEMORY_URL is required")
		return 1
	}
	if err := mcp.Proxy(context.Background(), env.Stdin, env.Stdout, cfg.URL, cfg.Token, env.HTTP); err != nil {
		fmt.Fprintf(env.Stderr, "mcp: %v\n", err)
		return 1
	}
	return 0
}

func runServe(args []string, env Env) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg := loadCfg(env)
	if cfg.DatabaseURL == "" {
		fmt.Fprintln(env.Stderr, "MEMORY_DATABASE_URL is required")
		return 1
	}
	st, err := store.Open(context.Background(), cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(env.Stderr, "store: %v\n", err)
		return 1
	}
	defer st.Pool.Close()
	if err := http.ListenAndServe(cfg.Listen, api.New(st, cfg)); err != nil {
		fmt.Fprintf(env.Stderr, "listen: %v\n", err)
		return 1
	}
	return 0
}

func openStore(cfg config.Config) (*store.Store, error) {
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("MEMORY_DATABASE_URL is required")
	}
	return store.Open(context.Background(), cfg.DatabaseURL)
}

func runProject(args []string, env Env) int {
	fs := flag.NewFlagSet("project", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	out := fs.String("out", "", "output directory")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *out == "" {
		fmt.Fprintln(env.Stderr, "--out is required")
		return 2
	}
	cfg := loadCfg(env)
	st, err := openStore(cfg)
	if err != nil {
		fmt.Fprintf(env.Stderr, "project: %v\n", err)
		return 1
	}
	defer st.Pool.Close()

	ctx := context.Background()
	mems, err := st.ListActiveMemories(ctx)
	if err != nil {
		fmt.Fprintf(env.Stderr, "project: list memories: %v\n", err)
		return 1
	}
	pages, err := st.ListActivePages(ctx)
	if err != nil {
		fmt.Fprintf(env.Stderr, "project: list pages: %v\n", err)
		return 1
	}
	if err := project.Write(*out, mems, pages); err != nil {
		fmt.Fprintf(env.Stderr, "project: %v\n", err)
		return 1
	}
	fmt.Fprintf(env.Stdout, "ok\t%d memories\t%d pages\n", len(mems), len(pages))
	return 0
}

func runImport(args []string, env Env) int {
	if len(args) == 0 || args[0] != "claude" {
		fmt.Fprintln(env.Stderr, "usage: memory import claude --path <dir> [--dry-run]")
		return 2
	}
	return runImportClaude(args[1:], env)
}

func runImportClaude(args []string, env Env) int {
	fs := flag.NewFlagSet("import claude", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	path := fs.String("path", "", "Claude auto-memory directory")
	dryRun := fs.Bool("dry-run", false, "print plan and write nothing")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *path == "" {
		fmt.Fprintln(env.Stderr, "--path is required")
		return 2
	}
	plan, err := importclaude.Parse(*path)
	if err != nil {
		fmt.Fprintf(env.Stderr, "import: %v\n", err)
		return 1
	}
	if *dryRun {
		printImportPlan(env.Stdout, plan)
		return 0
	}
	cfg := loadCfg(env)
	st, err := openStore(cfg)
	if err != nil {
		fmt.Fprintf(env.Stderr, "import: %v\n", err)
		return 1
	}
	defer st.Pool.Close()
	if err := importclaude.Apply(context.Background(), st, plan, importclaude.Harness); err != nil {
		fmt.Fprintf(env.Stderr, "import: %v\n", err)
		return 1
	}
	fmt.Fprintf(env.Stdout, "ok\t%d memories\n", len(plan.Memories))
	return 0
}

func printImportPlan(w io.Writer, plan importclaude.Plan) {
	fmt.Fprintf(w, "%d memories\n", len(plan.Memories))
	for _, item := range plan.Memories {
		fmt.Fprintf(w, "%s\t%s\t%s\n", item.Kind, item.Title, item.File)
	}
}

func runInbox(args []string, env Env) int {
	fs := flag.NewFlagSet("inbox", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg := loadCfg(env)
	if cfg.Token == "" {
		fmt.Fprintln(env.Stderr, "MEMORY_TOKEN is required")
		return 1
	}
	ps, err := newClient(cfg, env).listInbox()
	if err != nil {
		fmt.Fprintf(env.Stderr, "inbox: %v\n", err)
		return 1
	}
	for _, p := range ps {
		fmt.Fprintf(env.Stdout, "%s\t%s\t%s\t%s\n", p.ID, p.Action, p.Reason, p.CreatedByHarness)
	}
	return 0
}

func runAccept(args []string, env Env) int {
	return runInboxDecide("accept", args, env)
}

func runReject(args []string, env Env) int {
	return runInboxDecide("reject", args, env)
}

func runInboxDecide(action string, args []string, env Env) int {
	fs := flag.NewFlagSet(action, flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(env.Stderr, "usage: memory %s <id>\n", action)
		return 2
	}
	cfg := loadCfg(env)
	if cfg.Token == "" {
		fmt.Fprintln(env.Stderr, "MEMORY_TOKEN is required")
		return 1
	}
	if err := newClient(cfg, env).decideInbox(action, fs.Arg(0)); err != nil {
		fmt.Fprintf(env.Stderr, "%s: %v\n", action, err)
		return 1
	}
	fmt.Fprintln(env.Stdout, "ok")
	return 0
}

func runLint(args []string, env Env) int {
	fs := flag.NewFlagSet("lint", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	project := fs.String("project", "", "project slug")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg := loadCfg(env)
	if cfg.Token == "" {
		fmt.Fprintln(env.Stderr, "MEMORY_TOKEN is required")
		return 1
	}
	findings, err := newClient(cfg, env).lint(*project)
	if err != nil {
		fmt.Fprintf(env.Stderr, "lint: %v\n", err)
		return 1
	}
	for _, f := range findings {
		page := ""
		if f.PageID != "" {
			page = f.PageID
		}
		fmt.Fprintf(env.Stdout, "%s\t%s\t%s\n", f.Kind, f.Message, page)
	}
	return 0
}

type inboxRow struct {
	ID               string `json:"id"`
	Action           string `json:"action"`
	Reason           string `json:"reason"`
	CreatedByHarness string `json:"created_by_harness"`
}

type lintFinding struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	PageID  string `json:"page_id"`
}

func (c *Client) listInbox() ([]inboxRow, error) {
	res, raw, err := c.do(http.MethodGet, "/v1/inbox", nil)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, decodeAPIError(res.StatusCode, raw)
	}
	var got []inboxRow
	if err := json.Unmarshal(raw, &got); err != nil {
		return nil, err
	}
	return got, nil
}

func (c *Client) decideInbox(action, id string) error {
	res, raw, err := c.do(http.MethodPost, "/v1/admin/inbox/"+id+"/"+action, map[string]string{})
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return decodeAPIError(res.StatusCode, raw)
	}
	return nil
}

func (c *Client) lint(project string) ([]lintFinding, error) {
	path := "/v1/lint"
	if project != "" {
		path += "?project=" + url.QueryEscape(project)
	}
	res, raw, err := c.do(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, decodeAPIError(res.StatusCode, raw)
	}
	var got struct {
		Findings []lintFinding `json:"findings"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		return nil, err
	}
	return got.Findings, nil
}
