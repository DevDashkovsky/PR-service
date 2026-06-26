package pr

import (
	"context"
	"errors"
	"os"
	"pr-service/internal/domain/dto"
	"pr-service/internal/domain/user"
	"pr-service/pkg/logger"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	_ = logger.Init(true)
	os.Exit(m.Run())
}

type mockPRRepo struct {
	prExists         func(ctx context.Context, prID string) (bool, error)
	createWithRev    func(ctx context.Context, prID, prName, authorID string, reviewerIDs []string) error
	getPR            func(ctx context.Context, prID string) (*dto.PullRequestDTO, error)
	setMerged        func(ctx context.Context, prID string, mergedAt time.Time) error
	getReviewers     func(ctx context.Context, prID string) ([]string, error)
	replaceReviewer  func(ctx context.Context, prID, oldID, newID string) error
	isReviewerAssign func(ctx context.Context, prID, reviewerID string) (bool, error)
}

func (m *mockPRRepo) PRExists(ctx context.Context, prID string) (bool, error) {
	return m.prExists(ctx, prID)
}
func (m *mockPRRepo) CreatePRWithReviewers(ctx context.Context, prID, prName, authorID string, reviewerIDs []string) error {
	return m.createWithRev(ctx, prID, prName, authorID, reviewerIDs)
}
func (m *mockPRRepo) GetPR(ctx context.Context, prID string) (*dto.PullRequestDTO, error) {
	return m.getPR(ctx, prID)
}
func (m *mockPRRepo) SetMerged(ctx context.Context, prID string, mergedAt time.Time) error {
	return m.setMerged(ctx, prID, mergedAt)
}
func (m *mockPRRepo) GetReviewers(ctx context.Context, prID string) ([]string, error) {
	return m.getReviewers(ctx, prID)
}
func (m *mockPRRepo) ReplaceReviewer(ctx context.Context, prID, oldID, newID string) error {
	return m.replaceReviewer(ctx, prID, oldID, newID)
}
func (m *mockPRRepo) IsReviewerAssigned(ctx context.Context, prID, reviewerID string) (bool, error) {
	return m.isReviewerAssign(ctx, prID, reviewerID)
}

type mockUserRepo struct {
	getUser    func(ctx context.Context, userID string) (*dto.UserDTO, error)
	getTeam    func(ctx context.Context, teamName string) (*dto.TeamDTO, error)
	getReviews func(ctx context.Context, userID string) ([]dto.PullRequestShortDTO, error)
	setActive  func(ctx context.Context, userID string, isActive bool) (*dto.UserDTO, error)
}

func (m *mockUserRepo) GetUserReviews(ctx context.Context, userID string) ([]dto.PullRequestShortDTO, error) {
	return m.getReviews(ctx, userID)
}
func (m *mockUserRepo) SetUserActive(ctx context.Context, userID string, isActive bool) (*dto.UserDTO, error) {
	return m.setActive(ctx, userID, isActive)
}
func (m *mockUserRepo) GetUser(ctx context.Context, userID string) (*dto.UserDTO, error) {
	return m.getUser(ctx, userID)
}
func (m *mockUserRepo) GetTeamByName(ctx context.Context, teamName string) (*dto.TeamDTO, error) {
	return m.getTeam(ctx, teamName)
}

func member(id string, active bool) dto.TeamMemberDTO {
	return dto.TeamMemberDTO{UserID: id, Username: id, IsActive: active}
}

func TestSelectRandomReviewers(t *testing.T) {
	tests := []struct {
		name       string
		candidates []string
		maxCount   int
		wantLen    int
	}{
		{"меньше лимита", []string{"a"}, 2, 1},
		{"ровно лимит", []string{"a", "b"}, 2, 2},
		{"больше лимита", []string{"a", "b", "c", "d"}, 2, 2},
		{"пусто", []string{}, 2, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectRandomReviewers(tt.candidates, tt.maxCount)
			if len(got) != tt.wantLen {
				t.Fatalf("длина = %d, ожидалось %d", len(got), tt.wantLen)
			}
			// выбранные должны быть подмножеством кандидатов и без дублей
			seen := map[string]bool{}
			set := map[string]bool{}
			for _, c := range tt.candidates {
				set[c] = true
			}
			for _, g := range got {
				if !set[g] {
					t.Fatalf("%q не входит в кандидатов", g)
				}
				if seen[g] {
					t.Fatalf("дубль %q", g)
				}
				seen[g] = true
			}
		})
	}
}

func TestAssignReviewers(t *testing.T) {
	tests := []struct {
		name       string
		members    []dto.TeamMemberDTO
		authorID   string
		wantCount  int
		mustExlude []string
	}{
		{
			name:       "исключает автора",
			members:    []dto.TeamMemberDTO{member("author", true), member("u1", true)},
			authorID:   "author",
			wantCount:  1,
			mustExlude: []string{"author"},
		},
		{
			name:       "исключает неактивных",
			members:    []dto.TeamMemberDTO{member("author", true), member("u1", true), member("u2", false)},
			authorID:   "author",
			wantCount:  1,
			mustExlude: []string{"author", "u2"},
		},
		{
			name:      "максимум двое из многих",
			members:   []dto.TeamMemberDTO{member("author", true), member("u1", true), member("u2", true), member("u3", true)},
			authorID:  "author",
			wantCount: 2,
		},
		{
			name:      "нет кандидатов кроме автора",
			members:   []dto.TeamMemberDTO{member("author", true)},
			authorID:  "author",
			wantCount: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewService(&mockPRRepo{}, &mockUserRepo{
				getTeam: func(_ context.Context, name string) (*dto.TeamDTO, error) {
					return &dto.TeamDTO{TeamName: name, Members: tt.members}, nil
				},
			})
			got, err := s.assignReviewers(context.Background(), tt.authorID, "team")
			if err != nil {
				t.Fatalf("неожиданная ошибка: %v", err)
			}
			if len(got) != tt.wantCount {
				t.Fatalf("ревьюверов = %d, ожидалось %d", len(got), tt.wantCount)
			}
			for _, ex := range tt.mustExlude {
				for _, g := range got {
					if g == ex {
						t.Fatalf("%q не должен попадать в ревьюверы", ex)
					}
				}
			}
		})
	}
}

func TestFindReplacementCandidate(t *testing.T) {
	members := []dto.TeamMemberDTO{
		member("author", true),
		member("old", true),
		member("current", true),
		member("free", true),
		member("inactive", false),
	}
	s := NewService(&mockPRRepo{}, &mockUserRepo{
		getTeam: func(_ context.Context, name string) (*dto.TeamDTO, error) {
			return &dto.TeamDTO{TeamName: name, Members: members}, nil
		},
	})

	got, err := s.findReplacementCandidate(context.Background(), "team", "author", []string{"current"}, "old")
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if got != "free" {
		t.Fatalf("кандидат = %q, ожидался \"free\"", got)
	}

	_, err = s.findReplacementCandidate(context.Background(), "team", "author", []string{"current", "free"}, "old")
	if !errors.Is(err, ErrNoCandidate) {
		t.Fatalf("ожидалась ErrNoCandidate, получено: %v", err)
	}
}

func TestCreatePR_AuthorErrors(t *testing.T) {
	baseUserRepo := func(getUser func(context.Context, string) (*dto.UserDTO, error)) *mockUserRepo {
		return &mockUserRepo{getUser: getUser}
	}
	req := dto.CreatePullRequestRequest{PullRequestID: "pr1", PullRequestName: "name", AuthorID: "author"}

	t.Run("автор отсутствует -> ErrAuthorNotFound", func(t *testing.T) {
		s := NewService(
			&mockPRRepo{prExists: func(context.Context, string) (bool, error) { return false, nil }},
			baseUserRepo(func(context.Context, string) (*dto.UserDTO, error) {
				return nil, user.ErrUserNotFound
			}),
		)
		_, err := s.CreatePR(context.Background(), req)
		if !errors.Is(err, ErrAuthorNotFound) {
			t.Fatalf("ожидалась ErrAuthorNotFound, получено: %v", err)
		}
	})

	t.Run("ошибка БД -> НЕ ErrAuthorNotFound", func(t *testing.T) {
		dbErr := errors.New("connection refused")
		s := NewService(
			&mockPRRepo{prExists: func(context.Context, string) (bool, error) { return false, nil }},
			baseUserRepo(func(context.Context, string) (*dto.UserDTO, error) {
				return nil, dbErr
			}),
		)
		_, err := s.CreatePR(context.Background(), req)
		if errors.Is(err, ErrAuthorNotFound) {
			t.Fatalf("ошибка БД не должна превращаться в ErrAuthorNotFound")
		}
		if !errors.Is(err, dbErr) {
			t.Fatalf("исходная ошибка БД должна пробрасываться, получено: %v", err)
		}
	})
}

func TestMergePR_GetPRErrors(t *testing.T) {
	req := dto.MergePullRequestRequest{PullRequestID: "pr1"}

	t.Run("PR отсутствует -> ErrPRNotFound", func(t *testing.T) {
		s := NewService(&mockPRRepo{
			getPR: func(context.Context, string) (*dto.PullRequestDTO, error) {
				return nil, ErrPRNotFound
			},
		}, &mockUserRepo{})
		_, err := s.MergePR(context.Background(), req)
		if !errors.Is(err, ErrPRNotFound) {
			t.Fatalf("ожидалась ErrPRNotFound, получено: %v", err)
		}
	})

	t.Run("ошибка БД -> НЕ ErrPRNotFound", func(t *testing.T) {
		dbErr := errors.New("connection refused")
		s := NewService(&mockPRRepo{
			getPR: func(context.Context, string) (*dto.PullRequestDTO, error) {
				return nil, dbErr
			},
		}, &mockUserRepo{})
		_, err := s.MergePR(context.Background(), req)
		if errors.Is(err, ErrPRNotFound) {
			t.Fatalf("ошибка БД не должна превращаться в ErrPRNotFound (404)")
		}
		if !errors.Is(err, dbErr) {
			t.Fatalf("исходная ошибка БД должна пробрасываться, получено: %v", err)
		}
	})

	t.Run("повторный merge идемпотентен", func(t *testing.T) {
		mergedAt := "2026-01-01T00:00:00Z"
		s := NewService(&mockPRRepo{
			getPR: func(context.Context, string) (*dto.PullRequestDTO, error) {
				return &dto.PullRequestDTO{
					PullRequestID: "pr1", Status: dto.StatusMerged, MergedAt: &mergedAt,
				}, nil
			},
		}, &mockUserRepo{})
		res, err := s.MergePR(context.Background(), req)
		if err != nil {
			t.Fatalf("неожиданная ошибка: %v", err)
		}
		if res.MergedAt != mergedAt {
			t.Fatalf("mergedAt = %q, ожидалось %q", res.MergedAt, mergedAt)
		}
	})
}
