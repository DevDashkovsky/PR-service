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
	teamHandler := NewTeamHandler(teams.NewService(nil, nil))
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
