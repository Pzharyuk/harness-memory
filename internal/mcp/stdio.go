package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxStdioMessage = 10 * 1024 * 1024

// Proxy reads newline-delimited JSON-RPC from in and POSTs each message to
// baseURL+"/mcp" with Bearer token, writing responses to out.
func Proxy(ctx context.Context, in io.Reader, out io.Writer, baseURL, token string, client *http.Client) error {
	if client == nil {
		client = http.DefaultClient
	}
	url := strings.TrimRight(baseURL, "/") + "/mcp"
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), maxStdioMessage)
	for sc.Scan() {
		line := append([]byte(nil), bytes.TrimSpace(sc.Bytes())...)
		if len(line) == 0 {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := forward(ctx, client, url, token, line, out); err != nil {
			return err
		}
	}
	return sc.Err()
}

func forward(ctx context.Context, client *http.Client, url, token string, line []byte, out io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(line))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}

	switch res.StatusCode {
	case http.StatusOK:
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 {
			return nil
		}
		if _, err := out.Write(raw); err != nil {
			return err
		}
		_, err = out.Write([]byte("\n"))
		return err
	case http.StatusAccepted, http.StatusNoContent:
		return nil
	default:
		var parsed rpcRequest
		if err := json.Unmarshal(line, &parsed); err == nil && !isNotification(parsed) {
			resp := rpcResponse{
				JSONRPC: jsonRPCVersion,
				ID:      parsed.ID,
				Error:   &rpcError{Code: -32000, Message: fmt.Sprintf("HTTP %d", res.StatusCode)},
			}
			return json.NewEncoder(out).Encode(resp)
		}
		return fmt.Errorf("mcp proxy: HTTP %d", res.StatusCode)
	}
}
