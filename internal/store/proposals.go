package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Pzharyuk/harness-memory/internal/types"
)

func scanProposal(sc interface{ Scan(dest ...any) error }) (types.Proposal, error) {
	var p types.Proposal
	err := sc.Scan(
		&p.ID,
		&p.Action,
		&p.Payload,
		&p.Reason,
		&p.Status,
		&p.CreatedByHarness,
		&p.CreatedAt,
	)
	return p, err
}

func insertProposal(ctx context.Context, q querier, p types.Proposal) (types.Proposal, error) {
	if p.Action == "" {
		return types.Proposal{}, fmt.Errorf("proposal action is required")
	}
	if p.CreatedByHarness == "" {
		return types.Proposal{}, fmt.Errorf("proposal harness is required")
	}
	if p.Status == "" {
		p.Status = types.ProposalStatusOpen
	}
	payload := p.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	out, err := scanProposal(q.QueryRow(ctx, `
		INSERT INTO proposals (action, payload, reason, status, created_by_harness)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, action, payload, reason, status, created_by_harness, created_at
	`, p.Action, payload, p.Reason, p.Status, p.CreatedByHarness))
	if err != nil {
		return types.Proposal{}, fmt.Errorf("insert proposal: %w", err)
	}
	return out, nil
}

// InsertProposal files an inbox item. Status defaults to open.
func (s *Store) InsertProposal(ctx context.Context, p types.Proposal) (types.Proposal, error) {
	return insertProposal(ctx, s.Pool, p)
}
