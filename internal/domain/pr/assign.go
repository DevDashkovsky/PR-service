package pr

import (
	"context"
	"math/rand"
	"pr-service/pkg/logger"

	"go.uber.org/zap"
)

func (s *Service) assignReviewers(ctx context.Context, authorID, teamName string) ([]string, error) {
	logger.Log.Info("Автоназначение ревьюверов",
		zap.String("author_id", authorID),
		zap.String("team_name", teamName),
	)

	team, err := s.userRepo.GetTeamByName(ctx, teamName)
	if err != nil {
		logger.Log.Error("Ошибка при получении команды для автоназначения",
			zap.String("team_name", teamName),
			zap.Error(err),
		)
		return nil, err
	}

	var candidates []string
	for _, member := range team.Members {
		if member.IsActive && member.UserID != authorID {
			candidates = append(candidates, member.UserID)
		}
	}

	logger.Log.Info("Найдены кандидаты для ревью",
		zap.Int("candidates_count", len(candidates)),
		zap.Strings("candidates", candidates),
	)

	if len(candidates) == 0 {
		logger.Log.Warn("Нет доступных кандидатов для ревью", zap.String("team_name", teamName))
		return []string{}, nil
	}

	if len(candidates) == 1 {
		logger.Log.Info("Назначен 1 ревьювер", zap.String("reviewer", candidates[0]))
		return candidates, nil
	}

	reviewers := selectRandomReviewers(candidates, 2)
	logger.Log.Info("Назначены ревьюверы",
		zap.Int("count", len(reviewers)),
		zap.Strings("reviewers", reviewers),
	)

	return reviewers, nil
}

func selectRandomReviewers(candidates []string, maxCount int) []string {
	if len(candidates) <= maxCount {
		return candidates
	}

	shuffled := make([]string, len(candidates))
	copy(shuffled, candidates)

	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	return shuffled[:maxCount]
}
