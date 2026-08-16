package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Pzharyuk/harness-memory/internal/auth"
	"github.com/Pzharyuk/harness-memory/internal/types"
)

// ErrUnauthorized is returned when a token is missing, revoked, or does not match.
var ErrUnauthorized = errors.New("unauthorized")

func scanToken(sc interface{ Scan(dest ...any) error }) (types.Token, error) {
	var tok types.Token
	err := sc.Scan(
		&tok.ID,
		&tok.Harness,
		&tok.TokenHash,
		&tok.Label,
		&tok.CreatedAt,
		&tok.LastUsedAt,
		&tok.RevokedAt,
	)
	return tok, err
}

// CreateToken inserts a row with a precomputed argon2id hash (caller uses auth.Hash).
func (s *Store) CreateToken(ctx context.Context, harness, label, hash string) (types.Token, error) {
	if harness == "" {
		return types.Token{}, fmt.Errorf("harness is required")
	}
	if !strings.HasPrefix(hash, "argon2id$") {
		return types.Token{}, fmt.Errorf("token hash must be argon2id")
	}
	tok, err := scanToken(s.Pool.QueryRow(ctx, `
		INSERT INTO tokens (harness, label, token_hash)
		VALUES ($1, $2, $3)
		RETURNING id, harness, token_hash, label, created_at, last_used_at, revoked_at
	`, harness, label, hash))
	if err != nil {
		return types.Token{}, fmt.Errorf("create token: %w", err)
	}
	return tok, nil
}

// Authenticate scans non-revoked hashes and verifies plaintext with auth.Verify.
// Salted hashes cannot be looked up by value; a linear scan is OK for v1 (dozens of tokens).
func (s *Store) Authenticate(ctx context.Context, plaintext string) (types.Token, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, harness, token_hash, label, created_at, last_used_at, revoked_at
		FROM tokens
		WHERE revoked_at IS NULL
	`)
	if err != nil {
		return types.Token{}, fmt.Errorf("authenticate: %w", err)
	}
	defer rows.Close()

	var match types.Token
	found := false
	for rows.Next() {
		tok, err := scanToken(rows)
		if err != nil {
			return types.Token{}, fmt.Errorf("authenticate: %w", err)
		}
		if auth.Verify(tok.TokenHash, plaintext) {
			match = tok
			found = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		return types.Token{}, fmt.Errorf("authenticate: %w", err)
	}
	rows.Close()
	if !found {
		return types.Token{}, ErrUnauthorized
	}
	if err := s.Pool.QueryRow(ctx, `
		UPDATE tokens SET last_used_at = now()
		WHERE id = $1 AND revoked_at IS NULL
		RETURNING last_used_at
	`, match.ID).Scan(&match.LastUsedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return types.Token{}, ErrUnauthorized
		}
		return types.Token{}, fmt.Errorf("touch token: %w", err)
	}
	return match, nil
}

// ListTokens returns all tokens including revoked. The hash is on the struct;
// callers that log or serve JSON must strip it.
func (s *Store) ListTokens(ctx context.Context) ([]types.Token, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, harness, token_hash, label, created_at, last_used_at, revoked_at
		FROM tokens
		ORDER BY created_at ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	defer rows.Close()

	out := []types.Token{}
	for rows.Next() {
		tok, err := scanToken(rows)
		if err != nil {
			return nil, fmt.Errorf("list tokens: %w", err)
		}
		out = append(out, tok)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	return out, nil
}

func (s *Store) RevokeToken(ctx context.Context, id uuid.UUID) error {
	tag, err := s.Pool.Exec(ctx, `
		UPDATE tokens SET revoked_at = coalesce(revoked_at, now())
		WHERE id = $1
	`, id)
	if err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("token not found")
	}
	return nil
}

func (s *Store) TouchToken(ctx context.Context, id uuid.UUID) error {
	tag, err := s.Pool.Exec(ctx, `UPDATE tokens SET last_used_at = now() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("touch token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("token not found")
	}
	return nil
}
