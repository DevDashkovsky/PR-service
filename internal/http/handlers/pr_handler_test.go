package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pr-service/internal/domain/dto"
	"pr-service/internal/domain/interfaces"
	"pr-service/internal/domain/pr"
)

type busyPRRepository struct{}

func (busyPRRepository) WithinTransaction(context.Context, func(interfaces.PRTransaction) error) error {
	return fmt.Errorf("repository: %w", pr.ErrTransactionBusy)
}

func TestPRHandlersReportTransactionContention(t *testing.T) {
	handler := NewPRHandler(pr.NewService(busyPRRepository{}))
	for _, tc := range []struct {
		name, body string
		handle     http.HandlerFunc
	}{
		{"create", `{"pull_request_id":"busy-pr","pull_request_name":"Change","author_id":"u"}`, handler.CreatePR},
		{"merge", `{"pull_request_id":"busy-pr"}`, handler.MergePR},
		{"reassign", `{"pull_request_id":"busy-pr","old_user_id":"u"}`, handler.ReassignPR},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tc.handle(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body)))
			if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") != "1" {
				t.Fatalf("status=%d headers=%v body=%s", w.Code, w.Header(), w.Body.String())
			}
			var response dto.ErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Error.Code != "SERVICE_UNAVAILABLE" {
				t.Fatalf("unexpected error: %+v", response.Error)
			}
		})
	}
}
