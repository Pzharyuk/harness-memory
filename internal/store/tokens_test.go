package store

import (
	"context"
	"errors"
	"testing"

	"github.com/Pzharyuk/harness-memory/internal/auth"
)

func TestTokenCreateAuthenticate(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	const secret = "grok-secret"
	hash, err := auth.Hash(secret)
	if err != nil {
		t.Fatal(err)
	}
	created, err := st.CreateToken(ctx, "grok", "dev", hash)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID.String() == "00000000-0000-0000-0000-000000000000" {
		t.Fatal("empty id")
	}
	if created.Harness != "grok" || created.Label != "dev" {
		t.Fatalf("created=%+v", created)
	}
	if created.TokenHash != hash {
		t.Fatal("stored hash mismatch")
	}
	if created.TokenHash == secret {
		t.Fatal("hash stored plaintext")
	}

	got, err := st.Authenticate(ctx, secret)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != created.ID || got.Harness != "grok" {
		t.Fatalf("auth=%+v want id=%s harness=grok", got, created.ID)
	}
	if got.LastUsedAt == nil {
		t.Fatal("last_used_at not set on authenticate")
	}
}

func TestTokenWrongSecretUnauthorized(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	hash, err := auth.Hash("correct-secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateToken(ctx, "claude", "", hash); err != nil {
		t.Fatal(err)
	}

	_, err = st.Authenticate(ctx, "wrong-secret")
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err=%v want ErrUnauthorized", err)
	}
}

func TestTokenRevoke(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	const secret = "revoke-me"
	hash, err := auth.Hash(secret)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := st.CreateToken(ctx, "codex", "temp", hash)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Authenticate(ctx, secret); err != nil {
		t.Fatal(err)
	}
	if err := st.RevokeToken(ctx, tok.ID); err != nil {
		t.Fatal(err)
	}
	_, err = st.Authenticate(ctx, secret)
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("after revoke err=%v want ErrUnauthorized", err)
	}

	list, err := st.ListTokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list len=%d", len(list))
	}
	if list[0].RevokedAt == nil {
		t.Fatal("revoked_at not set")
	}
}

func TestTokenTwoHarnessesIndependent(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	hClaude, err := auth.Hash("claude-secret")
	if err != nil {
		t.Fatal(err)
	}
	hGrok, err := auth.Hash("grok-secret")
	if err != nil {
		t.Fatal(err)
	}
	claude, err := st.CreateToken(ctx, "claude", "primary", hClaude)
	if err != nil {
		t.Fatal(err)
	}
	grok, err := st.CreateToken(ctx, "grok", "primary", hGrok)
	if err != nil {
		t.Fatal(err)
	}
	if claude.ID == grok.ID {
		t.Fatal("same id")
	}

	gotClaude, err := st.Authenticate(ctx, "claude-secret")
	if err != nil {
		t.Fatal(err)
	}
	gotGrok, err := st.Authenticate(ctx, "grok-secret")
	if err != nil {
		t.Fatal(err)
	}
	if gotClaude.ID != claude.ID || gotClaude.Harness != "claude" {
		t.Fatalf("claude auth=%+v", gotClaude)
	}
	if gotGrok.ID != grok.ID || gotGrok.Harness != "grok" {
		t.Fatalf("grok auth=%+v", gotGrok)
	}

	if err := st.RevokeToken(ctx, grok.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Authenticate(ctx, "claude-secret"); err != nil {
		t.Fatalf("claude should still auth: %v", err)
	}
	_, err = st.Authenticate(ctx, "grok-secret")
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked grok err=%v want ErrUnauthorized", err)
	}
}

func TestTokenListAndTouch(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()

	h1, err := auth.Hash("a")
	if err != nil {
		t.Fatal(err)
	}
	h2, err := auth.Hash("b")
	if err != nil {
		t.Fatal(err)
	}
	t1, err := st.CreateToken(ctx, "admin", "one", h1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateToken(ctx, "grok", "two", h2); err != nil {
		t.Fatal(err)
	}

	list, err := st.ListTokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("list len=%d want 2", len(list))
	}
	if list[0].LastUsedAt != nil {
		t.Fatal("last_used_at set before touch")
	}

	if err := st.TouchToken(ctx, t1.ID); err != nil {
		t.Fatal(err)
	}
	list, err = st.ListTokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var touched bool
	for _, tok := range list {
		if tok.ID == t1.ID {
			if tok.LastUsedAt == nil {
				t.Fatal("last_used_at not set after touch")
			}
			touched = true
		}
	}
	if !touched {
		t.Fatal("created token missing from list")
	}
}
