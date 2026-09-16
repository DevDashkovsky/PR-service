package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"pr-service/internal/domain/dto"
)

func TestWriteContextError(t *testing.T) {
	w := httptest.NewRecorder()
	if !writeContextError(w, fmt.Errorf("database query: %w", context.DeadlineExceeded)) {
		t.Fatal("deadline was not handled")
	}
	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("status=%d, want %d", w.Code, http.StatusGatewayTimeout)
	}
	var response dto.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Error.Code != "GATEWAY_TIMEOUT" {
		t.Fatalf("code=%q, want GATEWAY_TIMEOUT", response.Error.Code)
	}

	canceled := httptest.NewRecorder()
	if !writeContextError(canceled, context.Canceled) {
		t.Fatal("cancellation was not handled")
	}
	if canceled.Body.Len() != 0 {
		t.Fatalf("client cancellation wrote a response: %q", canceled.Body.String())
	}
}
