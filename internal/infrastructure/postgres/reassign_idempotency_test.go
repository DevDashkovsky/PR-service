package postgres

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"pr-service/internal/domain/dto"
	"pr-service/internal/domain/interfaces"
	"pr-service/internal/domain/pr"
)

func idempotentReassignment() dto.ReassignPullRequestRequest {
	return dto.ReassignPullRequestRequest{PullRequestID: testPRID, OldUserID: testOldReviewer, IdempotencyKey: "operation-1"}
}

func TestReassignReplaysOriginalResultAfterABAAndMerge(t *testing.T) {
	db := integrationDB(t)
	ctx := t.Context()
	service := pr.NewService(NewPRRepo(db))
	req := idempotentReassignment()
	original, err := service.ReassignReviewer(ctx, req)
	requireNoError(t, err)
	// Make the original reviewer the only eligible replacement, forcing B -> A.
	_, err = db.conn.Exec(ctx, `UPDATE users SET is_active=(user_id='old')`)
	requireNoError(t, err)
	intervening, err := service.ReassignReviewer(ctx, dto.ReassignPullRequestRequest{PullRequestID: testPRID, OldUserID: original.ReplacedBy})
	requireNoError(t, err)
	if intervening.ReplacedBy != req.OldUserID {
		t.Fatalf("ABA was not established: %+v", intervening)
	}
	// Restore all candidates: an accidental second execution could now succeed.
	_, err = db.conn.Exec(ctx, `UPDATE users SET is_active=true`)
	requireNoError(t, err)
	replayed, err := service.ReassignReviewer(ctx, req)
	requireNoError(t, err)
	if !reflect.DeepEqual(original, replayed) {
		t.Fatalf("response changed: original=%+v replay=%+v", original, replayed)
	}
	merged, err := service.MergePR(ctx, dto.MergePullRequestRequest{PullRequestID: testPRID})
	requireNoError(t, err)
	if !reflect.DeepEqual(merged.AssignedReviewers, intervening.PR.AssignedReviewers) {
		t.Fatal("retry performed another replacement")
	}
	// A fresh service/repository must replay even after the PR has been merged.
	replayed, err = pr.NewService(NewPRRepo(db)).ReassignReviewer(ctx, req)
	requireNoError(t, err)
	if !reflect.DeepEqual(original, replayed) {
		t.Fatalf("merged replay changed the original response: %+v", replayed)
	}
}

func TestConcurrentReassignmentsShareOneResult(t *testing.T) {
	db := integrationDB(t)
	const clients = 16
	start := make(chan struct{})
	results := make([]*dto.ReassignPullRequestResponse, clients)
	errs := make([]error, clients)
	var wg sync.WaitGroup
	for i := range clients {
		wg.Go(func() {
			<-start
			results[i], errs[i] = pr.NewService(NewPRRepo(db)).ReassignReviewer(t.Context(), idempotentReassignment())
		})
	}
	close(start)
	wg.Wait()
	for i := range clients {
		requireNoError(t, errs[i])
		if !reflect.DeepEqual(results[0], results[i]) {
			t.Fatalf("response %d differs from first", i)
		}
	}
	var count int
	requireNoError(t, db.conn.QueryRow(t.Context(), `SELECT count(*) FROM pr_reassign_idempotency WHERE response IS NOT NULL`).Scan(&count))
	if count != 1 {
		t.Fatalf("stored outcomes=%d, want 1", count)
	}
}

func TestIdempotencyKeyCannotChangeRequest(t *testing.T) {
	db := integrationDB(t)
	service := pr.NewService(NewPRRepo(db))
	req := idempotentReassignment()
	_, err := service.ReassignReviewer(t.Context(), req)
	requireNoError(t, err)
	for _, changed := range []dto.ReassignPullRequestRequest{
		{PullRequestID: testPRID, OldUserID: testSecondReviewer, IdempotencyKey: req.IdempotencyKey},
		{PullRequestID: "another-pr", OldUserID: testOldReviewer, IdempotencyKey: req.IdempotencyKey},
	} {
		_, err := service.ReassignReviewer(t.Context(), changed)
		if !errors.Is(err, pr.ErrIdempotencyConflict) {
			t.Fatalf("changed request was not rejected: %v", err)
		}
	}
}

type failAfterCallback struct {
	repo interfaces.PRRepository
	err  error
}

func (r failAfterCallback) WithinTransaction(ctx context.Context, fn func(interfaces.PRTransaction) error) error {
	return r.repo.WithinTransaction(ctx, func(tx interfaces.PRTransaction) error {
		if err := fn(tx); err != nil {
			return err
		}
		return r.err
	})
}

func TestReassignReservationAndMutationRollbackTogether(t *testing.T) {
	db := integrationDB(t)
	injected := errors.New("abort before commit")
	req := idempotentReassignment()
	service := pr.NewService(failAfterCallback{repo: NewPRRepo(db), err: injected})
	result, err := service.ReassignReviewer(t.Context(), req)
	if result != nil || !errors.Is(err, injected) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	var reservations, oldAssignments int
	requireNoError(t, db.conn.QueryRow(t.Context(), `SELECT count(*) FROM pr_reassign_idempotency`).Scan(&reservations))
	requireNoError(t, db.conn.QueryRow(t.Context(), `SELECT count(*) FROM pr_reviewers WHERE pull_request_id='request' AND reviewer_id='old'`).Scan(&oldAssignments))
	if reservations != 0 || oldAssignments != 1 {
		t.Fatalf("partial rollback: reservations=%d old assignments=%d", reservations, oldAssignments)
	}
	_, err = pr.NewService(NewPRRepo(db)).ReassignReviewer(t.Context(), req)
	requireNoError(t, err)
}

func TestFailedReassignmentDoesNotConsumeKey(t *testing.T) {
	db := integrationDB(t)
	ctx := t.Context()
	service := pr.NewService(NewPRRepo(db))
	req := idempotentReassignment()
	_, err := db.conn.Exec(ctx, `UPDATE users SET is_active=false`)
	requireNoError(t, err)
	_, err = service.ReassignReviewer(ctx, req)
	if !errors.Is(err, pr.ErrNoCandidate) {
		t.Fatalf("want NO_CANDIDATE, got %v", err)
	}
	_, err = db.conn.Exec(ctx, `UPDATE users SET is_active=true`)
	requireNoError(t, err)
	_, err = service.ReassignReviewer(ctx, req)
	requireNoError(t, err)
}

func TestWaitingForIdempotencyKeyRespectsDeadline(t *testing.T) {
	db := integrationDB(t)
	owner, err := db.conn.Begin(t.Context())
	requireNoError(t, err)
	defer rollback(owner)
	req := idempotentReassignment()
	_, err = (&prTransaction{tx: owner}).BeginReassignment(t.Context(), req.IdempotencyKey, req.PullRequestID, req.OldUserID)
	requireNoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	service := pr.NewService(NewPRRepo(db))
	_, err = service.ReassignReviewer(ctx, req)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting request ignored deadline: %v", err)
	}
	requireNoError(t, owner.Rollback(t.Context()))
	_, err = service.ReassignReviewer(t.Context(), req)
	requireNoError(t, err)
}
