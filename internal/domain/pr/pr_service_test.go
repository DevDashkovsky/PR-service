package pr

import (
	"context"
	"errors"
	"pr-service/internal/domain/dto"
	"pr-service/internal/domain/interfaces"
	"pr-service/internal/domain/user"
	"testing"
	"time"
)

const testActive = "active"
const testRequest = "request"
const testOld = "old"

type fakeRepository struct {
	tx        interfaces.PRTransaction
	commitErr error
}

func (r fakeRepository) WithinTransaction(_ context.Context, fn func(interfaces.PRTransaction) error) error {
	if err := fn(r.tx); err != nil {
		return err
	}
	return r.commitErr
}

type fakeTransaction struct {
	interfaces.PRTransaction
	pullRequest *dto.PullRequestDTO
	user        *dto.UserDTO
	members     []dto.TeamMemberDTO
	getErr      error
	exists      bool
	merged      bool
	replaced    string
}

func (tx *fakeTransaction) PRExists(context.Context, string) (bool, error) { return tx.exists, nil }
func (tx *fakeTransaction) GetPRForUpdate(context.Context, string) (*dto.PullRequestDTO, error) {
	return tx.pullRequest, tx.getErr
}
func (tx *fakeTransaction) GetUser(context.Context, string) (*dto.UserDTO, error) {
	return tx.user, tx.getErr
}
func (tx *fakeTransaction) GetTeamForAssignment(context.Context, string) (*dto.TeamDTO, error) {
	return &dto.TeamDTO{Members: tx.members}, nil
}
func (tx *fakeTransaction) CreatePRWithReviewers(context.Context, string, string, string, []string) error {
	return nil
}
func (tx *fakeTransaction) SetMerged(context.Context, string, time.Time) error {
	tx.merged = true
	return nil
}
func (tx *fakeTransaction) ReplaceReviewer(_ context.Context, _, _, newID string) error {
	tx.replaced = newID
	return nil
}

func member(id string, active bool) dto.TeamMemberDTO {
	return dto.TeamMemberDTO{UserID: id, Username: id, IsActive: active}
}

func TestReviewerSelection(t *testing.T) {
	members := make([]dto.TeamMemberDTO, 0, 5)
	members = append(members, member("author", true), member(testActive, true), member("inactive", false))
	got := assignReviewers(members, "author")
	if len(got) != 1 || got[0] != testActive {
		t.Fatalf("reviewers: %v", got)
	}
	if got := assignReviewers(nil, "author"); got == nil || len(got) != 0 {
		t.Fatalf("empty reviewers must be []: %v", got)
	}
	members = append(members, member("other", true), member("third", true))
	got = assignReviewers(members, "author")
	if len(got) != 2 || got[0] == got[1] {
		t.Fatalf("invalid selection: %v", got)
	}
	replacement, err := findReplacementCandidate(members, "author", []string{testActive, "other"}, testActive)
	if err != nil || replacement != "third" {
		t.Fatalf("replacement %s: %v", replacement, err)
	}
	_, err = findReplacementCandidate(members, "author", []string{testActive, "other", "third"}, testActive)
	if !errors.Is(err, ErrNoCandidate) {
		t.Fatal(err)
	}
}

func TestCreateErrors(t *testing.T) {
	dbErr := errors.New("database unavailable")
	for _, tc := range []struct {
		name string
		tx   fakeTransaction
		want error
	}{
		{"duplicate", fakeTransaction{exists: true}, ErrPRExists},
		{"missing author", fakeTransaction{getErr: user.ErrUserNotFound}, ErrAuthorNotFound},
		{"database failure", fakeTransaction{getErr: dbErr}, dbErr},
		{"author without team", fakeTransaction{user: &dto.UserDTO{}}, ErrAuthorNoTeam},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewService(fakeRepository{tx: &tc.tx})
			_, err := s.CreatePR(t.Context(), dto.CreatePullRequestRequest{PullRequestID: testRequest, PullRequestName: "Change", AuthorID: "author"})
			if !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}
}

func TestMergeIdempotenceAndCommitFailure(t *testing.T) {
	timestamp := "2026-01-01T00:00:00.123456Z"
	tx := &fakeTransaction{pullRequest: &dto.PullRequestDTO{PullRequestID: testRequest, Status: dto.StatusMerged, MergedAt: &timestamp}}
	s := NewService(fakeRepository{tx: tx})
	result, err := s.MergePR(t.Context(), dto.MergePullRequestRequest{PullRequestID: testRequest})
	if err != nil || result.MergedAt != timestamp || tx.merged {
		t.Fatalf("result %+v, err %v, changed %v", result, err, tx.merged)
	}
	commitErr := errors.New("commit failed")
	s = NewService(fakeRepository{tx: tx, commitErr: commitErr})
	result, err = s.MergePR(t.Context(), dto.MergePullRequestRequest{PullRequestID: testRequest})
	if result != nil || !errors.Is(err, commitErr) {
		t.Fatalf("uncommitted result: %+v, %v", result, err)
	}
}

func TestReassignRules(t *testing.T) {
	for _, tc := range []struct {
		name, status, old string
		want              error
	}{
		{"merged", dto.StatusMerged, testOld, ErrPRMerged},
		{"not assigned", dto.StatusOpen, "missing", ErrNotAssigned},
		{"no candidate", dto.StatusOpen, testOld, ErrNoCandidate},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &fakeTransaction{pullRequest: &dto.PullRequestDTO{Status: tc.status, AssignedReviewers: []string{testOld}}, user: &dto.UserDTO{TeamName: "team"}}
			s := NewService(fakeRepository{tx: tx})
			_, err := s.ReassignReviewer(t.Context(), dto.ReassignPullRequestRequest{PullRequestID: testRequest, OldUserID: tc.old})
			if !errors.Is(err, tc.want) {
				t.Fatalf("want %v got %v", tc.want, err)
			}
			if tx.replaced != "" {
				t.Fatal("unexpected mutation")
			}
		})
	}
}
