package postgres

import (
	"slices"
	"testing"
	"time"

	"pr-service/internal/domain/dto"
)

func TestUserRepo_GetUserReviews_KeysetPagination(t *testing.T) {
	db := integrationDB(t)
	ctx := t.Context()
	repo := NewUserRepo(db)
	createdAt := time.Date(2026, time.September, 16, 10, 0, 0, 123456000, time.UTC)
	ids := make([]string, 0, 7)
	ids = append(ids, "a", "B", "é", "Я", "z", "Z")
	for _, id := range ids {
		if _, err := db.conn.Exec(
			ctx,
			`INSERT INTO pull_requests(pull_request_id, pull_request_name, author_id, status, created_at)
			 VALUES($1, $1, 'author', 'OPEN', $2)`,
			id,
			createdAt,
		); err != nil {
			t.Fatal(err)
		}
		if _, err := db.conn.Exec(
			ctx,
			`INSERT INTO pr_reviewers(pull_request_id, reviewer_id) VALUES($1, 'spare')`,
			id,
		); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.conn.Exec(
		ctx,
		`INSERT INTO pull_requests(pull_request_id, pull_request_name, author_id, status, created_at)
		 VALUES('older', 'older', 'author', 'OPEN', $1)`,
		createdAt.Add(-time.Second),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.conn.Exec(
		ctx,
		`INSERT INTO pr_reviewers(pull_request_id, reviewer_id) VALUES('older', 'spare')`,
	); err != nil {
		t.Fatal(err)
	}

	var got []string
	var cursor *dto.UserReviewsCursor
	for {
		page, err := repo.GetUserReviews(ctx, "spare", dto.UserReviewsPageRequest{
			Limit:  2,
			Cursor: cursor,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.PullRequests) > 2 {
			t.Fatalf("page size = %d, want at most 2", len(page.PullRequests))
		}
		for _, review := range page.PullRequests {
			got = append(got, review.PullRequestID)
		}
		if page.NextCursor == nil {
			break
		}
		cursor = page.NextCursor
	}

	slices.Sort(ids)
	slices.Reverse(ids)
	ids = append(ids, "older")
	want := ids
	if !slices.Equal(got, want) {
		t.Fatalf("reviews = %v, want %v", got, want)
	}
}
