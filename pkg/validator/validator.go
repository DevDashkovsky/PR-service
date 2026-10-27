package validator

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

var ErrInvalidInput = errors.New("invalid input")

// An empty key selects the legacy, non-idempotent request behavior.
func ValidateIdempotencyKey(key string) error {
	if len(key) > 128 {
		return invalid("Idempotency-Key too long (max 128 characters)")
	}
	for _, char := range key {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') &&
			(char < '0' || char > '9') && char != '-' && char != '_' && char != '.' && char != ':' {
			return invalid("Idempotency-Key must contain only letters, digits, hyphen, underscore, dot or colon")
		}
	}
	return nil
}

func invalid(message string) error { return fmt.Errorf("%w: %s", ErrInvalidInput, message) }

func ValidateTeamName(teamName string) error {
	if teamName == "" {
		return invalid("team_name cannot be empty")
	}
	if len(teamName) > 100 {
		return invalid("team_name too long (max 100 characters)")
	}
	for _, char := range teamName {
		if (char < 'a' || char > 'z') &&
			(char < 'A' || char > 'Z') &&
			(char < '0' || char > '9') && char != '-' && char != '_' {
			return invalid(fmt.Sprintf("team_name contains invalid character: %c", char))
		}
	}
	return nil
}

func ValidateUserID(userID string) error {
	if !utf8.ValidString(userID) {
		return invalid("user_id must be valid UTF-8")
	}
	if strings.TrimSpace(userID) == "" || strings.ContainsRune(userID, 0) {
		return invalid("user_id cannot be empty")
	}
	if utf8.RuneCountInString(userID) > 100 {
		return invalid("user_id too long (max 100 characters)")
	}
	return nil
}

func ValidatePullRequestID(prID string) error {
	if !utf8.ValidString(prID) {
		return invalid("pull_request_id must be valid UTF-8")
	}
	if strings.TrimSpace(prID) == "" || strings.ContainsRune(prID, 0) {
		return invalid("pull_request_id cannot be empty")
	}
	if utf8.RuneCountInString(prID) > 100 {
		return invalid("pull_request_id too long (max 100 characters)")
	}
	return nil
}

func ValidateUsername(username string) error {
	if !utf8.ValidString(username) {
		return invalid("username must be valid UTF-8")
	}
	if username == "" || strings.ContainsRune(username, 0) {
		return invalid("username cannot be empty")
	}
	if utf8.RuneCountInString(username) > 255 {
		return invalid("username too long (max 255 characters)")
	}
	username = strings.TrimSpace(username)
	if username == "" || strings.ContainsRune(username, 0) {
		return invalid("username cannot be only whitespace")
	}
	return nil
}

func ValidateMembersUnique(userIDs []string) error {
	seen := make(map[string]bool)
	for _, id := range userIDs {
		if seen[id] {
			return invalid(fmt.Sprintf("duplicate user_id: %s", id))
		}
		seen[id] = true
	}
	return nil
}
