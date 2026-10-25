package handlers

import (
	"net/http"
	"net/http/httptest"
	"pr-service/internal/domain/dto"
	"pr-service/internal/domain/pr"
	"pr-service/internal/domain/teams"
	"pr-service/internal/domain/user"
	"strings"
	"testing"
)

func TestInvalidRequests(t *testing.T) {
	prHandler := NewPRHandler(pr.NewService(nil))
	teamHandler := NewTeamHandler(teams.NewService(nil))
	userHandler := NewUserHandler(user.NewService(nil))
	for _, tc := range []struct {
		name, body string
		handler    http.HandlerFunc
	}{
		{"missing PR field", `{}`, prHandler.CreatePR},
		{"missing merge ID", `{}`, prHandler.MergePR},
		{"invalid old ID", `{"pull_request_id":"pr"}`, prHandler.ReassignPR},
		{"trailing object", `{"pull_request_id":"pr"} {}`, prHandler.MergePR},
		{"trailing garbage", `{"pull_request_id":"pr"} garbage`, prHandler.MergePR},
		{"unknown field", `{"pull_request_id":"pr","extra":true}`, prHandler.MergePR},
		{"null object", `null`, prHandler.MergePR},
		{"missing activity", `{"user_id":"u"}`, userHandler.SetUserActive},
		{"null activity", `{"user_id":"u","is_active":null}`, userHandler.SetUserActive},
		{"missing members", `{"team_name":"team"}`, teamHandler.CreateTeam},
		{"null members", `{"team_name":"team","members":null}`, teamHandler.CreateTeam},
		{"member missing activity", `{"team_name":"team","members":[{"user_id":"u","username":"User"}]}`, teamHandler.CreateTeam},
		{"invalid team", `{"team_name":"bad name","members":[]}`, teamHandler.CreateTeam},
		{"oversized whitespace", `{"pull_request_id":"pr"}` + strings.Repeat(" ", maxRequestBody), prHandler.MergePR},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			w := httptest.NewRecorder()
			tc.handler(w, r)
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "BAD_REQUEST") {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestExplicitFalseAndEmptyMembers(t *testing.T) {
	for _, body := range []string{`{"user_id":"u","is_active":false}`, `{"team_name":"team","members":[]}`} {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		w := httptest.NewRecorder()
		var target any
		if strings.Contains(body, "team_name") {
			target = &dto.TeamDTO{}
		} else {
			target = &dto.SetUserActiveRequest{}
		}
		if err := decodeRequest(w, r, target); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDecodeRequestRejectsCaseVariants(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		target any
	}{
		{"activity override", `{"user_id":"u","is_active":true,"IS_ACTIVE":false}`, &dto.SetUserActiveRequest{}},
		{"merge target override", `{"pull_request_id":"pr-1","PULL_REQUEST_ID":"pr-2"}`, &dto.MergePullRequestRequest{}},
		{"create author override", `{"pull_request_id":"pr","pull_request_name":"Change","author_id":"u","AUTHOR_ID":"other"}`, &dto.CreatePullRequestRequest{}},
		{"reassign reviewer override", `{"pull_request_id":"pr","old_user_id":"u","OLD_USER_ID":"other"}`, &dto.ReassignPullRequestRequest{}},
		{"team name override", `{"team_name":"team","TEAM_NAME":"other","members":[]}`, &dto.TeamDTO{}},
		{"members alias", `{"team_name":"team","MEMBERS":[]}`, &dto.TeamDTO{}},
		{"member activity override", `{"team_name":"team","members":[{"user_id":"u","username":"User","is_active":true,"IS_ACTIVE":false}]}`, &dto.TeamDTO{}},
		{"member ID alias", `{"team_name":"team","members":[{"USER_ID":"u","username":"User","is_active":true}]}`, &dto.TeamDTO{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			if err := decodeRequest(httptest.NewRecorder(), request, tt.target); err == nil {
				t.Fatalf("accepted a field not declared in the contract: %+v", tt.target)
			}
		})
	}
}

func TestDecodeRequestAcceptsContractFields(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		target any
	}{
		{"activity", `{"user_id":"u","is_active":false}`, &dto.SetUserActiveRequest{}},
		{"merge", `{"pull_request_id":"pr"}`, &dto.MergePullRequestRequest{}},
		{"create", `{"pull_request_id":"pr","pull_request_name":"Change","author_id":"u"}`, &dto.CreatePullRequestRequest{}},
		{"reassign", `{"pull_request_id":"pr","old_user_id":"u"}`, &dto.ReassignPullRequestRequest{}},
		{"team", `{"team_name":"team","members":[{"user_id":"u","username":"User","is_active":false}]}`, &dto.TeamDTO{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			if err := decodeRequest(httptest.NewRecorder(), request, tt.target); err != nil {
				t.Fatalf("rejected valid request: %v", err)
			}
		})
	}
}
