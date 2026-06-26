package validator

import (
	"errors"
	"fmt"
	"strings"
)

func ValidateTeamName(teamName string) error {
	if teamName == "" {
		return errors.New("team_name cannot be empty")
	}
	if len(teamName) > 100 {
		return errors.New("team_name too long (max 100 characters)")
	}
	for _, char := range teamName {
		if !((char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '-' || char == '_') {
			return fmt.Errorf("team_name contains invalid character: %c", char)
		}
	}
	return nil
}

func ValidateUserID(userID string) error {
	if userID == "" {
		return errors.New("user_id cannot be empty")
	}
	if len(userID) > 100 {
		return errors.New("user_id too long (max 100 characters)")
	}
	return nil
}

func ValidatePullRequestID(prID string) error {
	if prID == "" {
		return errors.New("pull_request_id cannot be empty")
	}
	if len(prID) > 100 {
		return errors.New("pull_request_id too long (max 100 characters)")
	}
	return nil
}

func ValidateUsername(username string) error {
	if username == "" {
		return errors.New("username cannot be empty")
	}
	if len(username) > 255 {
		return errors.New("username too long (max 255 characters)")
	}
	username = strings.TrimSpace(username)
	if username == "" {
		return errors.New("username cannot be only whitespace")
	}
	return nil
}

func ValidateMembersUnique(userIDs []string) error {
	seen := make(map[string]bool)
	for _, id := range userIDs {
		if seen[id] {
			return fmt.Errorf("duplicate user_id: %s", id)
		}
		seen[id] = true
	}
	return nil
}
