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

type reassignmentServiceStub struct {
	pullRequestService
	request dto.ReassignPullRequestRequest
	err     error
}

func (s *reassignmentServiceStub) ReassignReviewer(_ context.Context, request dto.ReassignPullRequestRequest) (*dto.ReassignPullRequestResponse, error) {
	s.request = request
	return &dto.ReassignPullRequestResponse{}, s.err
}

func TestReassignHandlerPassesIdempotencyKey(t *testing.T) {
	service := &reassignmentServiceStub{}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"pull_request_id":"keyed-pr","old_user_id":"reviewer"}`))
	r.Header.Set("Idempotency-Key", "operation-123")
	w := httptest.NewRecorder()
	NewPRHandler(service).ReassignPR(w, r)
	if w.Code != http.StatusOK || service.request.IdempotencyKey != "operation-123" {
		t.Fatalf("status=%d request=%+v", w.Code, service.request)
	}
}

func TestReassignHandlerRejectsInvalidKeys(t *testing.T) {
	for _, values := range [][]string{{""}, {"one", "two"}, {strings.Repeat("k", 129)}, {"key with spaces"}, {"ключ"}, {"a,b"}} {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"pull_request_id":"invalid-key-pr","old_user_id":"reviewer"}`))
		r.Header["Idempotency-Key"] = values
		w := httptest.NewRecorder()
		NewPRHandler(pr.NewService(nil)).ReassignPR(w, r)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("values=%v status=%d", values, w.Code)
		}
	}
}

func TestReassignHandlerMapsIdempotencyConflict(t *testing.T) {
	service := &reassignmentServiceStub{err: fmt.Errorf("wrapped: %w", pr.ErrIdempotencyConflict)}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"pull_request_id":"conflict-pr","old_user_id":"reviewer"}`))
	r.Header.Set("Idempotency-Key", "conflicting-key")
	w := httptest.NewRecorder()
	NewPRHandler(service).ReassignPR(w, r)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "IDEMPOTENCY_CONFLICT") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
