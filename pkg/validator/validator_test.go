package validator

import "testing"

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
