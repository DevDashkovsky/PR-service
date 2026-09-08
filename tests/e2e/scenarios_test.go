package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type result struct {
	status int
	body   map[string]json.RawMessage
	err    error
}

type pullRequest struct {
	ID        string   `json:"pull_request_id"`
	Status    string   `json:"status"`
	Reviewers []string `json:"assigned_reviewers"`
	MergedAt  string   `json:"mergedAt"`
}

type apiSuite struct {
	base    string
	client  *http.Client
	prefix  string
	members []map[string]any
}

const userIDKey = "user_id"
const prIDKey = "pull_request_id"

func TestAPIScenarios(t *testing.T) {
	base := strings.TrimRight(os.Getenv("PR_SERVICE_E2E_URL"), "/")
	if base == "" {
		t.Skip("set PR_SERVICE_E2E_URL to an isolated running API")
	}
	e := &apiSuite{base: base, client: &http.Client{Timeout: 15 * time.Second}, prefix: fmt.Sprintf("e2e-%d", time.Now().UnixNano())}
	e.members = make([]map[string]any, 5)
	for i := range e.members {
		e.members[i] = map[string]any{userIDKey: e.uid(i), "username": fmt.Sprintf("User %d", i), "is_active": true}
	}
	team := map[string]any{"team_name": e.prefix, "members": e.members}
	check(t, e.call(http.MethodPost, "/team/add", team), http.StatusCreated, "")
	check(t, e.call(http.MethodPost, "/team/add", team), http.StatusBadRequest, "TEAM_EXISTS")
	t.Run("validation does not deactivate", e.validation)
	t.Run("concurrent create", e.concurrentCreate)
	t.Run("concurrent replacement of same reviewer", e.concurrentReassign)
	t.Run("concurrent merge is idempotent", e.concurrentMerge)
	t.Run("merge and reassign agree on final state", e.mergeAndReassign)
	t.Run("moving members leaves an empty array", e.moveMembers)
}

func (e *apiSuite) uid(i int) string { return fmt.Sprintf("%s-u%d", e.prefix, i) }
func (e *apiSuite) create(id string) result {
	return e.call(http.MethodPost, "/pullRequest/create", map[string]any{prIDKey: id, "pull_request_name": "E2E review", "author_id": e.uid(0)})
}
func (e *apiSuite) merge(id string) result {
	return e.call(http.MethodPost, "/pullRequest/merge", map[string]any{prIDKey: id})
}
func (e *apiSuite) reassign(id, old string) result {
	return e.call(http.MethodPost, "/pullRequest/reassign", map[string]any{prIDKey: id, "old_user_id": old})
}

func (e *apiSuite) call(method, path string, payload any) result {
	var raw []byte
	var err error
	if payload != nil {
		raw, err = json.Marshal(payload)
		if err != nil {
			return result{err: err}
		}
	}
	req, err := http.NewRequest(method, e.base+path, bytes.NewReader(raw))
	if err != nil {
		return result{err: err}
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return result{err: err}
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return result{err: err}
	}
	r := result{status: resp.StatusCode}
	r.err = json.Unmarshal(body, &r.body)
	return r
}
func check(t *testing.T, r result, status int, code string) {
	t.Helper()
	if r.err != nil {
		t.Fatal(r.err)
	}
	if r.status != status {
		t.Fatalf("status=%d want=%d body=%s", r.status, status, r.body)
	}
	if code != "" {
		var e struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal(r.body["error"], &e); err != nil {
			t.Fatal(err)
		}
		if e.Code != code {
			t.Fatalf("code=%s want=%s", e.Code, code)
		}
	}
}
func readPR(t *testing.T, r result) pullRequest {
	t.Helper()
	var p pullRequest
	if err := json.Unmarshal(r.body["pr"], &p); err != nil {
		t.Fatal(err)
	}
	return p
}
func parallel(n int, fn func(int) result) []result {
	out := make([]result, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; out[i] = fn(i) }()
	}
	close(start)
	wg.Wait()
	return out
}
func (e *apiSuite) validation(t *testing.T) {
	check(t, e.call(http.MethodPost, "/users/setIsActive", map[string]any{userIDKey: e.uid(1)}), 400, "BAD_REQUEST")
	check(t, e.call(http.MethodPost, "/users/setIsActive", map[string]any{userIDKey: e.uid(1), "is_active": nil}), 400, "BAD_REQUEST")
	check(t, e.call(http.MethodPost, "/pullRequest/merge", map[string]any{}), 400, "BAD_REQUEST")
	r := e.call(http.MethodGet, "/team/get?team_name="+e.prefix, nil)
	check(t, r, 200, "")
	var got []struct {
		ID     string `json:"user_id"`
		Active bool   `json:"is_active"`
	}
	if err := json.Unmarshal(r.body["members"], &got); err != nil {
		t.Fatal(err)
	}
	for _, m := range got {
		if m.ID == e.uid(1) && !m.Active {
			t.Fatal("invalid body deactivated user")
		}
	}
}

func (e *apiSuite) concurrentCreate(t *testing.T) {
	results := parallel(2, func(int) result { return e.create(e.prefix + "-duplicate") })
	success := 0
	for _, r := range results {
		if r.status == 201 {
			check(t, r, 201, "")
			success++
		} else {
			check(t, r, 409, "PR_EXISTS")
		}
	}
	if success != 1 {
		t.Fatalf("successful creates: %d", success)
	}
}

func (e *apiSuite) concurrentReassign(t *testing.T) {
	id := e.prefix + "-reassign"
	r := e.create(id)
	check(t, r, 201, "")
	p := readPR(t, r)
	if len(p.Reviewers) != 2 || slices.Contains(p.Reviewers, e.uid(0)) {
		t.Fatalf("invalid reviewers: %v", p.Reviewers)
	}
	results := parallel(2, func(int) result { return e.reassign(id, p.Reviewers[0]) })
	success := 0
	for _, r := range results {
		if r.status == 200 {
			check(t, r, 200, "")
			success++
		} else {
			check(t, r, 409, "NOT_ASSIGNED")
		}
	}
	if success != 1 {
		t.Fatalf("successful replacements: %d", success)
	}
	final := e.merge(id)
	check(t, final, 200, "")
	if len(readPR(t, final).Reviewers) != 2 {
		t.Fatal("reviewer count changed")
	}
	check(t, e.reassign(id, p.Reviewers[1]), 409, "PR_MERGED")
}

func (e *apiSuite) concurrentMerge(t *testing.T) {
	id := e.prefix + "-merge"
	check(t, e.create(id), 201, "")
	results := parallel(3, func(int) result { return e.merge(id) })
	var at string
	for _, r := range results {
		check(t, r, 200, "")
		p := readPR(t, r)
		if p.Status != "MERGED" || p.MergedAt == "" {
			t.Fatalf("invalid merged PR: %+v", p)
		}
		if at == "" {
			at = p.MergedAt
		}
		if p.MergedAt != at {
			t.Fatal("mergedAt changed")
		}
	}
	again := e.merge(id)
	check(t, again, 200, "")
	if readPR(t, again).MergedAt != at {
		t.Fatal("repeat merge changed mergedAt")
	}
}

func (e *apiSuite) mergeAndReassign(t *testing.T) {
	for i := range 5 {
		id := fmt.Sprintf("%s-mixed-%d", e.prefix, i)
		r := e.create(id)
		check(t, r, 201, "")
		p := readPR(t, r)
		rs := parallel(2, func(index int) result {
			if index == 0 {
				return e.merge(id)
			}
			return e.reassign(id, p.Reviewers[0])
		})
		check(t, rs[0], 200, "")
		if rs[1].status == 200 {
			check(t, rs[1], 200, "")
		} else {
			check(t, rs[1], 409, "PR_MERGED")
		}
		final := e.merge(id)
		check(t, final, 200, "")
		merged, got := readPR(t, rs[0]), readPR(t, final)
		if !slices.Equal(merged.Reviewers, got.Reviewers) || merged.MergedAt != got.MergedAt {
			t.Fatalf("merged PR changed: %+v -> %+v", merged, got)
		}
	}
}

func (e *apiSuite) moveMembers(t *testing.T) {
	check(t, e.call(http.MethodPost, "/team/add", map[string]any{"team_name": e.prefix + "-moved", "members": e.members}), 201, "")
	r := e.call(http.MethodGet, "/team/get?team_name="+e.prefix, nil)
	check(t, r, 200, "")
	if string(r.body["members"]) != "[]" {
		t.Fatalf("empty members=%s", r.body["members"])
	}
}
