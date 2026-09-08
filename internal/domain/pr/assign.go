package pr

import (
	"math/rand/v2"
	"pr-service/internal/domain/dto"
)

func assignReviewers(members []dto.TeamMemberDTO, authorID string) []string {
	candidates := make([]string, 0, len(members))
	for _, member := range members {
		if member.IsActive && member.UserID != authorID {
			candidates = append(candidates, member.UserID)
		}
	}
	return selectRandomReviewers(candidates, 2)
}

func selectRandomReviewers(candidates []string, maxCount int) []string {
	if len(candidates) <= maxCount {
		return candidates
	}
	shuffled := append([]string(nil), candidates...)
	rand.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
	return shuffled[:maxCount]
}
