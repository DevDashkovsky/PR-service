package validator

import (
	"errors"
	"testing"
)

func TestTextValidatorsRequireUTF8(t *testing.T) {
	validators := map[string]func(string) error{
		"user ID":  ValidateUserID,
		"PR ID":    ValidatePullRequestID,
		"username": ValidateUsername,
	}
	for name, validate := range validators {
		t.Run(name, func(t *testing.T) {
			for _, value := range []string{"\xff", "prefix\xc3", "\xc0\xaf", "\xed\xa0\x80"} {
				if err := validate(value); !errors.Is(err, ErrInvalidInput) {
					t.Errorf("invalid UTF-8 %q: got %v, want ErrInvalidInput", value, err)
				}
			}
			for _, value := range []string{"user-1", "Пользователь", "用户", "�"} {
				if err := validate(value); err != nil {
					t.Errorf("valid UTF-8 %q rejected: %v", value, err)
				}
			}
		})
	}
}

func TestValidateTeamName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"ок", "team-1_a", false},
		{"пусто", "", true},
		{"запрещённый символ", "team name", true},
		{"слишком длинное", string(make([]byte, 101)), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateTeamName(tt.input); (err != nil) != tt.wantErr {
				t.Fatalf("ValidateTeamName(%q) err=%v, wantErr=%v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestValidatePullRequestID(t *testing.T) {
	if err := ValidatePullRequestID(""); err == nil {
		t.Fatal("пустой pull_request_id должен быть невалиден")
	}
	if err := ValidatePullRequestID("pr-123"); err != nil {
		t.Fatalf("валидный pull_request_id отклонён: %v", err)
	}
}

func TestValidateMembersUnique(t *testing.T) {
	if err := ValidateMembersUnique([]string{"a", "b", "a"}); err == nil {
		t.Fatal("ожидалась ошибка для дублирующихся user_id")
	}
	if err := ValidateMembersUnique([]string{"a", "b", "c"}); err != nil {
		t.Fatalf("уникальные id отклонены: %v", err)
	}
}
