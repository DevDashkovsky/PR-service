package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"pr-service/internal/domain/dto"
	"pr-service/internal/domain/pr"
)

// The reservation, replacement and response are committed together. A competing
// insert waits for this transaction; REPEATABLE READ retries refresh its snapshot.
func (r *prTransaction) BeginReassignment(ctx context.Context, key, prID, oldID string) (*dto.ReassignPullRequestResponse, error) {
	tag, err := r.tx.Exec(ctx, `INSERT INTO pr_reassign_idempotency(idempotency_key,pull_request_id,old_user_id) VALUES($1,$2,$3) ON CONFLICT (idempotency_key) DO NOTHING`, key, prID, oldID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 1 {
		return nil, nil
	}
	var storedPR, storedOld string
	var payload []byte
	if err := r.tx.QueryRow(ctx, `SELECT pull_request_id,old_user_id,response FROM pr_reassign_idempotency WHERE idempotency_key=$1`, key).Scan(&storedPR, &storedOld, &payload); err != nil {
		return nil, err
	}
	if storedPR != prID || storedOld != oldID {
		return nil, pr.ErrIdempotencyConflict
	}
	var response dto.ReassignPullRequestResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		return nil, fmt.Errorf("decode saved reassignment: %w", err)
	}
	return &response, nil
}

func (r *prTransaction) SaveReassignment(ctx context.Context, key string, response *dto.ReassignPullRequestResponse) error {
	payload, err := json.Marshal(response)
	if err != nil {
		return err
	}
	tag, err := r.tx.Exec(ctx, `UPDATE pr_reassign_idempotency SET response=$2 WHERE idempotency_key=$1 AND response IS NULL`, key, payload)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("reassignment reservation is missing or already completed")
	}
	return nil
}
