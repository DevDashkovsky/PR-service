package postgres

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"pr-service/internal/domain/dto"
	"pr-service/internal/domain/interfaces"
	prdomain "pr-service/internal/domain/pr"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PRRepo struct{ db *pgxpool.Pool }
type prTransaction struct{ tx pgx.Tx }

func NewPRRepo(db *Postgres) *PRRepo { return &PRRepo{db: db.conn} }

func (r *PRRepo) WithinTransaction(ctx context.Context, fn func(interfaces.PRTransaction) error) error {
	const maxAttempts = 5
	var err error
	for attempt := range maxAttempts {
		if err := ctx.Err(); err != nil {
			return err
		}
		err = r.runTransaction(ctx, fn)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || (pgErr.Code != "40001" && pgErr.Code != "40P01") {
			return err
		}
		if attempt < maxAttempts-1 {
			backoff := 10 * time.Millisecond << attempt
			timer := time.NewTimer(backoff + time.Duration(rand.Int64N(int64(backoff))))
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	return fmt.Errorf("%w: %w", prdomain.ErrTransactionBusy, err)
}

func (r *PRRepo) runTransaction(ctx context.Context, fn func(interfaces.PRTransaction) error) error {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return err
	}
	defer rollback(tx)
	if err := fn(&prTransaction{tx: tx}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *prTransaction) PRExists(ctx context.Context, id string) (bool, error) {
	var exists bool
	err := r.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pull_requests WHERE pull_request_id=$1)`, id).Scan(&exists)
	return exists, err
}

func (r *prTransaction) CreatePRWithReviewers(ctx context.Context, id, name, author string, reviewers []string) error {
	result, err := r.tx.Exec(ctx, `INSERT INTO pull_requests(pull_request_id,pull_request_name,author_id,status,created_at) VALUES($1,$2,$3,'OPEN',$4) ON CONFLICT (pull_request_id) DO NOTHING`, id, name, author, time.Now().UTC())
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return prdomain.ErrPRExists
	}
	for _, reviewer := range reviewers {
		if _, err := r.tx.Exec(ctx, `INSERT INTO pr_reviewers(pull_request_id,reviewer_id) VALUES($1,$2)`, id, reviewer); err != nil {
			return err
		}
	}
	return nil
}

func (r *prTransaction) GetPRForUpdate(ctx context.Context, id string) (*dto.PullRequestDTO, error) {
	var result dto.PullRequestDTO
	var merged pgtype.Timestamp
	err := r.tx.QueryRow(ctx, `SELECT pull_request_id,pull_request_name,author_id,status,merged_at FROM pull_requests WHERE pull_request_id=$1 FOR UPDATE`, id).Scan(&result.PullRequestID, &result.PullRequestName, &result.AuthorID, &result.Status, &merged)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, prdomain.ErrPRNotFound
	}
	if err != nil {
		return nil, err
	}
	if merged.Valid {
		value := merged.Time.UTC().Format(time.RFC3339Nano)
		result.MergedAt = &value
	}
	rows, err := r.tx.Query(ctx, `SELECT reviewer_id FROM pr_reviewers WHERE pull_request_id=$1 ORDER BY reviewer_id COLLATE "C"`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result.AssignedReviewers = []string{}
	for rows.Next() {
		var reviewer string
		if err := rows.Scan(&reviewer); err != nil {
			return nil, err
		}
		result.AssignedReviewers = append(result.AssignedReviewers, reviewer)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &result, nil
}

func (r *prTransaction) SetMerged(ctx context.Context, id string, at time.Time) error {
	result, err := r.tx.Exec(ctx, `UPDATE pull_requests SET status='MERGED',merged_at=$2 WHERE pull_request_id=$1 AND status='OPEN'`, id, at.UTC())
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return prdomain.ErrPRMerged
	}
	return nil
}

func (r *prTransaction) ReplaceReviewer(ctx context.Context, id, oldID, newID string) error {
	result, err := r.tx.Exec(ctx, `DELETE FROM pr_reviewers WHERE pull_request_id=$1 AND reviewer_id=$2`, id, oldID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return prdomain.ErrNotAssigned
	}
	_, err = r.tx.Exec(ctx, `INSERT INTO pr_reviewers(pull_request_id,reviewer_id) VALUES($1,$2)`, id, newID)
	if err != nil {
		return err
	}
	_, err = r.tx.Exec(ctx, `UPDATE pull_requests SET status=status WHERE pull_request_id=$1`, id)
	return err
}

func (r *prTransaction) GetUser(ctx context.Context, id string) (*dto.UserDTO, error) {
	return (&UserRepo{db: r.tx}).GetUser(ctx, id)
}

func (r *prTransaction) GetTeamForAssignment(ctx context.Context, name string) (*dto.TeamDTO, error) {
	rows, err := r.tx.Query(ctx, `SELECT user_id,username,COALESCE(is_active,false) FROM users WHERE team_name=$1 ORDER BY user_id COLLATE "C" FOR SHARE`, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := &dto.TeamDTO{TeamName: name, Members: []dto.TeamMemberDTO{}}
	for rows.Next() {
		var member dto.TeamMemberDTO
		if err := rows.Scan(&member.UserID, &member.Username, &member.IsActive); err != nil {
			return nil, err
		}
		result.Members = append(result.Members, member)
	}
	return result, rows.Err()
}
