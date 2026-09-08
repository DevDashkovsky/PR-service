package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"pr-service/internal/domain/dto"
	"pr-service/internal/domain/interfaces"
	"pr-service/internal/domain/pr"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func integrationDB(t *testing.T) *Postgres {
	t.Helper()
	dsn := os.Getenv("PR_SERVICE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set PR_SERVICE_TEST_DATABASE_URL to run real PostgreSQL tests")
	}
	ctx := t.Context()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("pr_test_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	config.ConnConfig.RuntimeParams["timezone"] = "Asia/Novosibirsk"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	ddl, err := os.ReadFile(filepath.Join("..", "..", "..", "init.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(ddl)); err != nil {
		t.Fatal(err)
	}
	db := &Postgres{conn: pool}
	members := make([]dto.TeamMemberDTO, 0, 5)
	for _, id := range []string{"author", "old", "second", "replacement", "spare"} {
		members = append(members, dto.TeamMemberDTO{UserID: id, Username: id, IsActive: true})
	}
	if err := NewTeamRepo(db).CreateTeam(ctx, dto.TeamDTO{TeamName: "team", Members: members}); err != nil {
		t.Fatal(err)
	}
	if err := NewPRRepo(db).WithinTransaction(ctx, func(tx interfaces.PRTransaction) error {
		return tx.CreatePRWithReviewers(ctx, "request", "Change", "author", []string{"old", "second"})
	}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestWaitingOperationsRetryAfterReassign(t *testing.T) {
	for _, operation := range []string{"merge", "different reviewer"} {
		t.Run(operation, func(t *testing.T) { testWaitingOperation(t, operation) })
	}
}

func testWaitingOperation(t *testing.T, operation string) {
	t.Helper()
	db := integrationDB(t)
	repo := NewPRRepo(db)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	first, err := db.conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(first)
	tx := &prTransaction{tx: first}
	if _, err := tx.GetPRForUpdate(ctx, "request"); err != nil {
		t.Fatal(err)
	}
	snapshotReady := make(chan struct{})
	releaseWaiter := make(chan struct{})
	done := make(chan error, 1)
	attempts := 0
	var observed []string
	go func() {
		done <- repo.WithinTransaction(ctx, func(waiter interfaces.PRTransaction) error {
			attempts++
			if attempts == 1 {
				if _, err := waiter.PRExists(ctx, "request"); err != nil {
					return err
				}
				close(snapshotReady)
				select {
				case <-releaseWaiter:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			result, err := waiter.GetPRForUpdate(ctx, "request")
			if err != nil {
				return err
			}
			observed = append([]string(nil), result.AssignedReviewers...)
			if operation == "merge" {
				return waiter.SetMerged(ctx, "request", time.Now().UTC())
			}
			return waiter.ReplaceReviewer(ctx, "request", "second", "spare")
		})
	}()
	select {
	case <-snapshotReady:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	requireNoError(t, tx.ReplaceReviewer(ctx, "request", "old", "replacement"))
	close(releaseWaiter)
	requireNoError(t, first.Commit(ctx))
	requireNoError(t, <-done)
	if attempts < 2 || slices.Contains(observed, "old") || !slices.Contains(observed, "replacement") {
		t.Fatalf("attempts=%d stale reviewers=%v", attempts, observed)
	}
}

func TestAssignmentRetriesAfterActivityChange(t *testing.T) {
	db := integrationDB(t)
	repo := NewPRRepo(db)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	attempts := 0
	err := repo.WithinTransaction(ctx, func(tx interfaces.PRTransaction) error {
		attempts++
		if _, err := tx.GetUser(ctx, "author"); err != nil {
			return err
		}
		if attempts == 1 {
			if _, err := NewUserRepo(db).SetUserActive(ctx, "replacement", false); err != nil {
				return err
			}
		}
		team, err := tx.GetTeamForAssignment(ctx, "team")
		if err != nil {
			return err
		}
		for _, member := range team.Members {
			if member.UserID == "replacement" && member.IsActive {
				return fmt.Errorf("selected inactive user from stale snapshot")
			}
		}
		return nil
	})
	if err != nil || attempts != 2 {
		t.Fatalf("attempts=%d err=%v", attempts, err)
	}
}

func TestConcurrentMergesPreserveTimestamp(t *testing.T) {
	db := integrationDB(t)
	service := pr.NewService(NewPRRepo(db))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	const count = 8
	results := make(chan *dto.MergedPullRequestDTO, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for range count {
		wg.Go(func() {
			result, err := service.MergePR(ctx, dto.MergePullRequestRequest{PullRequestID: "request"})
			if err != nil {
				errs <- err
				return
			}
			results <- result
		})
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	var timestamp string
	for result := range results {
		if timestamp == "" {
			timestamp = result.MergedAt
		}
		if result.MergedAt != timestamp {
			t.Errorf("timestamp changed: %s != %s", result.MergedAt, timestamp)
		}
	}
	parsed, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(parsed) > time.Minute || parsed.Location() != time.UTC {
		t.Fatalf("invalid UTC timestamp %s", timestamp)
	}
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestAssignmentLockOrderUsesByteOrder(t *testing.T) {
	db := integrationDB(t)
	ctx := t.Context()
	var collation string
	err := db.conn.QueryRow(ctx, `SELECT collname FROM pg_collation WHERE collprovider='i' AND collname LIKE 'en-US%' ORDER BY collname LIMIT 1`).Scan(&collation)
	if errors.Is(err, pgx.ErrNoRows) {
		t.Skip("PostgreSQL has no en-US ICU collation")
	}
	requireNoError(t, err)
	_, err = db.conn.Exec(ctx, `ALTER TABLE users ALTER COLUMN user_id TYPE VARCHAR(255) COLLATE `+pgx.Identifier{collation}.Sanitize())
	requireNoError(t, err)
	ids := []string{"a", "B", "é", "Я", "z", "Z"}
	members := make([]dto.TeamMemberDTO, 0, len(ids))
	for _, id := range ids {
		members = append(members, dto.TeamMemberDTO{UserID: id, Username: id, IsActive: true})
	}
	requireNoError(t, NewTeamRepo(db).CreateTeam(ctx, dto.TeamDTO{TeamName: "mixed", Members: members}))
	requireNoError(t, NewPRRepo(db).WithinTransaction(ctx, func(tx interfaces.PRTransaction) error {
		team, err := tx.GetTeamForAssignment(ctx, "mixed")
		if err != nil {
			return err
		}
		got := make([]string, 0, len(team.Members))
		for _, member := range team.Members {
			got = append(got, member.UserID)
		}
		slices.Sort(ids)
		if !slices.Equal(got, ids) {
			return fmt.Errorf("lock order %v differs from Go upsert order %v", got, ids)
		}
		return nil
	}))
}
