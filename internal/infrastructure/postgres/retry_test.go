package postgres

import (
	"context"
	"errors"
	"testing"

	"pr-service/internal/domain/interfaces"
	"pr-service/internal/domain/pr"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestTransactionRetriesRollbackWrites(t *testing.T) {
	for _, tc := range []struct {
		name      string
		conflicts int
		wantBusy  bool
	}{
		{"recovers after four conflicts", 4, false},
		{"reports exhausted conflicts", 5, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := integrationDB(t)
			ctx := t.Context()
			attempts := 0
			err := NewPRRepo(db).WithinTransaction(ctx, func(tx interfaces.PRTransaction) error {
				attempts++
				if _, err := tx.GetUser(ctx, "author"); err != nil {
					return err
				}
				if err := tx.CreatePRWithReviewers(ctx, "retry-request", "Retry", "author", nil); err != nil {
					return err
				}
				if attempts <= tc.conflicts {
					if _, err := NewUserRepo(db).SetUserActive(ctx, "replacement", attempts%2 == 0); err != nil {
						return err
					}
				}
				_, err := tx.GetTeamForAssignment(ctx, "team")
				return err
			})
			if tc.wantBusy {
				var pgErr *pgconn.PgError
				if !errors.Is(err, pr.ErrTransactionBusy) || !errors.As(err, &pgErr) || pgErr.Code != "40001" {
					t.Fatalf("expected retryable contention preserving SQL cause, got %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if attempts != 5 {
				t.Fatalf("attempts=%d, want 5", attempts)
			}
			var count int
			requireNoError(t, db.conn.QueryRow(ctx, `SELECT count(*) FROM pull_requests WHERE pull_request_id='retry-request'`).Scan(&count))
			want := 1
			if tc.wantBusy {
				want = 0
			}
			if count != want {
				t.Fatalf("committed rows=%d, want %d", count, want)
			}
		})
	}
}

func TestTransactionRetryRespectsCancellation(t *testing.T) {
	db := integrationDB(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	attempts := 0
	err := NewPRRepo(db).WithinTransaction(ctx, func(interfaces.PRTransaction) error {
		attempts++
		cancel()
		return &pgconn.PgError{Code: "40001"}
	})
	if !errors.Is(err, context.Canceled) || attempts != 1 {
		t.Fatalf("err=%v attempts=%d, want canceled after one attempt", err, attempts)
	}
}
