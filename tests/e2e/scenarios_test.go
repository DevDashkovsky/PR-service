package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
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

type reviewPage struct {
	UserID       string        `json:"user_id"`
	PullRequests []pullRequest `json:"pull_requests"`
	NextCursor   string        `json:"next_cursor"`
}

type apiSuite struct {
	base    string
	client  *http.Client
	prefix  string
	members []map[string]any
}

const userIDKey = "user_id"
const oldUserIDKey = "old_user_id"
const prIDKey = "pull_request_id"
const authorIDKey = "author_id"
const isActiveKey = "is_active"
const membersKey = "members"
const usernameKey = "username"
const teamNameKey = "team_name"
const prNameKey = "pull_request_name"

func TestAPIScenarios(t *testing.T) {
	base := strings.TrimRight(os.Getenv("PR_SERVICE_E2E_URL"), "/")
	if base == "" {
		t.Skip("set PR_SERVICE_E2E_URL to an isolated running API")
	}
	e := &apiSuite{base: base, client: &http.Client{Timeout: 15 * time.Second}, prefix: fmt.Sprintf("e2e-%d", time.Now().UnixNano())}
	e.members = make([]map[string]any, 5)
	for i := range e.members {
		e.members[i] = map[string]any{userIDKey: e.uid(i), usernameKey: fmt.Sprintf("User %d", i), isActiveKey: true}
	}
	team := map[string]any{teamNameKey: e.prefix, membersKey: e.members}
	t.Run("health endpoints", e.healthEndpoints)
	t.Run("JSON routing errors", e.routingErrors)
	check(t, e.call(http.MethodPost, "/team/add", team), http.StatusCreated, "")
	check(t, e.call(http.MethodPost, "/team/add", team), http.StatusBadRequest, "TEAM_EXISTS")
	t.Run("validation does not deactivate", e.validation)
	t.Run("set active updates team", e.setActive)
	t.Run("not found responses", e.notFound)
	t.Run("strict JSON validation", e.strictJSONValidation)
	t.Run("idempotent reassignment survives ABA and merge", e.idempotentReassignment)
	t.Run("candidate cardinalities and review pagination", e.assignmentEdges)
	t.Run("concurrent create", e.concurrentCreate)
	t.Run("concurrent replacement of same reviewer", e.concurrentReassign)
	t.Run("concurrent merge is idempotent", e.concurrentMerge)
	t.Run("merge and reassign agree on final state", e.mergeAndReassign)
	t.Run("moving members leaves an empty array", e.moveMembers)
}

func (e *apiSuite) uid(i int) string { return fmt.Sprintf("%s-u%d", e.prefix, i) }
func (e *apiSuite) create(id string) result {
	return e.call(http.MethodPost, "/pullRequest/create", map[string]any{prIDKey: id, prNameKey: "E2E review", authorIDKey: e.uid(0)})
}
func (e *apiSuite) merge(id string) result {
	return e.call(http.MethodPost, "/pullRequest/merge", map[string]any{prIDKey: id})
}
func (e *apiSuite) reassign(id, old string) result {
	return e.call(http.MethodPost, "/pullRequest/reassign", map[string]any{prIDKey: id, oldUserIDKey: old})
}

func (e *apiSuite) call(method, path string, payload any) result {
	return e.callWithHeaders(method, path, payload, nil)
}

func (e *apiSuite) callWithHeaders(method, path string, payload any, headers map[string]string) result {
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
	for name, value := range headers {
		req.Header.Set(name, value)
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

func (e *apiSuite) idempotentReassignment(t *testing.T) {
	author, first, second := e.prefix+"-idem-author", e.prefix+"-idem-a", e.prefix+"-idem-b"
	check(t, e.call(http.MethodPost, "/team/add", map[string]any{
		teamNameKey: e.prefix + "-idem-team",
		membersKey: []map[string]any{
			{userIDKey: author, usernameKey: "Author", isActiveKey: true},
			{userIDKey: first, usernameKey: "First", isActiveKey: true},
			{userIDKey: second, usernameKey: "Second", isActiveKey: false},
		},
	}), http.StatusCreated, "")
	id := e.prefix + "-idem-pr"
	check(t, e.call(http.MethodPost, "/pullRequest/create", map[string]any{
		prIDKey: id, prNameKey: "Idempotent reassignment", authorIDKey: author,
	}), http.StatusCreated, "")
	check(t, e.call(http.MethodPost, "/users/setIsActive", map[string]any{
		userIDKey: second, isActiveKey: true,
	}), http.StatusOK, "")
	payload := map[string]any{prIDKey: id, oldUserIDKey: first}
	headers := map[string]string{"Idempotency-Key": e.prefix + "-operation"}
	original := e.reassignWithLostResponse(t, payload, headers)
	check(t, original, http.StatusOK, "")
	results := parallel(8, func(int) result {
		return e.callWithHeaders(http.MethodPost, "/pullRequest/reassign", payload, headers)
	})
	for _, response := range results {
		check(t, response, http.StatusOK, "")
		assertSameReassignment(t, original, response)
		if got := readPR(t, response).Reviewers; !slices.Equal(got, []string{second}) {
			t.Fatalf("replacement=%v, want %s", got, second)
		}
	}
	check(t, e.callWithHeaders(http.MethodPost, "/pullRequest/reassign", map[string]any{
		prIDKey: id, oldUserIDKey: second,
	}, headers), http.StatusConflict, "IDEMPOTENCY_CONFLICT")
	check(t, e.callWithHeaders(http.MethodPost, "/pullRequest/reassign", map[string]any{
		prIDKey: id + "-other", oldUserIDKey: first,
	}, headers), http.StatusConflict, "IDEMPOTENCY_CONFLICT")
	check(t, e.reassign(id, second), http.StatusOK, "") // B -> A without the original key.
	replay := e.callWithHeaders(http.MethodPost, "/pullRequest/reassign", payload, headers)
	check(t, replay, http.StatusOK, "")
	assertSameReassignment(t, results[0], replay)
	merged := e.merge(id)
	check(t, merged, http.StatusOK, "")
	if got := readPR(t, merged).Reviewers; !slices.Equal(got, []string{first}) {
		t.Fatalf("retry changed current assignment: %v", got)
	}
	replay = e.callWithHeaders(http.MethodPost, "/pullRequest/reassign", payload, headers)
	check(t, replay, http.StatusOK, "")
	assertSameReassignment(t, results[0], replay)
}

func (e *apiSuite) reassignWithLostResponse(t *testing.T, payload any, headers map[string]string) result {
	t.Helper()
	target, err := url.Parse(e.base)
	if err != nil {
		t.Fatal(err)
	}
	observed := make(chan result, 1)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ModifyResponse = func(response *http.Response) error {
		captured := result{status: response.StatusCode}
		captured.err = json.NewDecoder(response.Body).Decode(&captured.body)
		select {
		case observed <- captured:
		default:
		}
		return errors.New("injected loss after upstream response")
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		conn, _, err := http.NewResponseController(w).Hijack()
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_ = conn.Close()
	}
	server := httptest.NewServer(proxy)
	defer server.Close()
	viaProxy := *e
	viaProxy.base = server.URL
	if response := viaProxy.callWithHeaders(http.MethodPost, "/pullRequest/reassign", payload, headers); response.err == nil {
		t.Fatal("client unexpectedly received the discarded response")
	}
	select {
	case response := <-observed:
		return response
	default:
		t.Fatal("proxy did not observe a committed API response")
		return result{}
	}
}

func assertSameReassignment(t *testing.T, want, got result) {
	t.Helper()
	if !bytes.Equal(want.body["pr"], got.body["pr"]) || !bytes.Equal(want.body["replaced_by"], got.body["replaced_by"]) {
		t.Fatalf("reassignment response changed: %s -> %s", want.body, got.body)
	}
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
func readReviewPage(t *testing.T, r result) reviewPage {
	t.Helper()
	var page reviewPage
	raw, err := json.Marshal(r.body)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatal(err)
	}
	return page
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

func (e *apiSuite) healthEndpoints(t *testing.T) {
	for _, path := range []string{"/healthz", "/readyz"} {
		r := e.call(http.MethodGet, path, nil)
		check(t, r, http.StatusOK, "")
		if string(r.body["status"]) != `"ok"` {
			t.Fatalf("%s status=%s", path, r.body["status"])
		}
	}
}

func (e *apiSuite) routingErrors(t *testing.T) {
	check(t, e.call(http.MethodGet, "/unknown-"+e.prefix, nil), http.StatusNotFound, "NOT_FOUND")
	check(t, e.call(http.MethodGet, "/team/add", nil), http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
}

func (e *apiSuite) validation(t *testing.T) {
	check(t, e.call(http.MethodPost, "/users/setIsActive", map[string]any{userIDKey: e.uid(1)}), 400, "BAD_REQUEST")
	check(t, e.call(http.MethodPost, "/users/setIsActive", map[string]any{userIDKey: e.uid(1), isActiveKey: nil}), 400, "BAD_REQUEST")
	check(t, e.call(http.MethodPost, "/pullRequest/merge", map[string]any{}), 400, "BAD_REQUEST")
	r := e.call(http.MethodGet, "/team/get?team_name="+e.prefix, nil)
	check(t, r, 200, "")
	var got []struct {
		ID     string `json:"user_id"`
		Active bool   `json:"is_active"`
	}
	if err := json.Unmarshal(r.body[membersKey], &got); err != nil {
		t.Fatal(err)
	}
	for _, m := range got {
		if m.ID == e.uid(1) && !m.Active {
			t.Fatal("invalid body deactivated user")
		}
	}
}

func (e *apiSuite) setActive(t *testing.T) {
	for _, active := range []bool{false, true} {
		r := e.call(http.MethodPost, "/users/setIsActive", map[string]any{
			userIDKey:   e.uid(1),
			isActiveKey: active,
		})
		check(t, r, http.StatusOK, "")
		var updated struct {
			ID     string `json:"user_id"`
			Active bool   `json:"is_active"`
		}
		if err := json.Unmarshal(r.body["user"], &updated); err != nil {
			t.Fatal(err)
		}
		if updated.ID != e.uid(1) || updated.Active != active {
			t.Fatalf("updated user=%+v want id=%s active=%t", updated, e.uid(1), active)
		}

		team := e.call(http.MethodGet, "/team/get?team_name="+url.QueryEscape(e.prefix), nil)
		check(t, team, http.StatusOK, "")
		var members []struct {
			ID     string `json:"user_id"`
			Active bool   `json:"is_active"`
		}
		if err := json.Unmarshal(team.body[membersKey], &members); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, member := range members {
			if member.ID == e.uid(1) {
				found = true
				if member.Active != active {
					t.Fatalf("team member active=%t want=%t", member.Active, active)
				}
			}
		}
		if !found {
			t.Fatalf("user %s is absent from team", e.uid(1))
		}
	}
}

func (e *apiSuite) notFound(t *testing.T) {
	unknownTeam := e.prefix + "-unknown-team"
	check(t, e.call(
		http.MethodGet,
		"/team/get?team_name="+url.QueryEscape(unknownTeam),
		nil,
	), http.StatusNotFound, "NOT_FOUND")

	unknownUser := e.prefix + "-unknown-user"
	check(t, e.call(http.MethodPost, "/users/setIsActive", map[string]any{
		userIDKey:   unknownUser,
		isActiveKey: false,
	}), http.StatusNotFound, "NOT_FOUND")
	reviews := e.call(
		http.MethodGet,
		"/users/getReview?user_id="+url.QueryEscape(unknownUser),
		nil,
	)
	check(t, reviews, http.StatusOK, "")
	page := readReviewPage(t, reviews)
	if page.UserID != unknownUser || len(page.PullRequests) != 0 || page.NextCursor != "" {
		t.Fatalf("unknown user reviews=%+v", page)
	}

	check(t, e.call(http.MethodPost, "/pullRequest/create", map[string]any{
		prIDKey:     e.prefix + "-unknown-author",
		prNameKey:   "Unknown author",
		authorIDKey: unknownUser,
	}), http.StatusNotFound, "NOT_FOUND")
	unknownPR := e.prefix + "-unknown-pr"
	check(t, e.merge(unknownPR), http.StatusNotFound, "NOT_FOUND")
	check(t, e.reassign(unknownPR, e.uid(1)), http.StatusNotFound, "NOT_FOUND")
}

func (e *apiSuite) strictJSONValidation(t *testing.T) {
	check(t, e.call(http.MethodGet, "/users/getReview?user_id=%FF", nil), http.StatusBadRequest, "BAD_REQUEST")
	id := e.prefix + "-unknown-field"
	check(t, e.call(http.MethodPost, "/pullRequest/create", map[string]any{
		prIDKey:     id,
		prNameKey:   "Unknown field",
		authorIDKey: e.uid(0),
		"extra":     true,
	}), http.StatusBadRequest, "BAD_REQUEST")
	check(t, e.create(id), http.StatusCreated, "")

	check(t, e.call(http.MethodPost, "/pullRequest/create", map[string]any{
		prIDKey:     e.prefix + "-nul\x00",
		prNameKey:   "NUL",
		authorIDKey: e.uid(0),
	}), http.StatusBadRequest, "BAD_REQUEST")
}

func (e *apiSuite) assignmentEdges(t *testing.T) {
	zeroAuthor := e.prefix + "-zero-author"
	check(t, e.call(http.MethodPost, "/team/add", map[string]any{
		teamNameKey: e.prefix + "-zero-candidates",
		membersKey: []map[string]any{{
			userIDKey:   zeroAuthor,
			usernameKey: "Zero Author",
			isActiveKey: true,
		}},
	}), http.StatusCreated, "")
	zero := e.call(http.MethodPost, "/pullRequest/create", map[string]any{
		prIDKey:     e.prefix + "-zero-reviewers",
		prNameKey:   "Zero reviewers",
		authorIDKey: zeroAuthor,
	})
	check(t, zero, http.StatusCreated, "")
	if reviewers := readPR(t, zero).Reviewers; len(reviewers) != 0 {
		t.Fatalf("zero-candidate reviewers=%v", reviewers)
	}

	oneAuthor := e.prefix + "-one-author"
	oneReviewer := e.prefix + "-one-reviewer"
	check(t, e.call(http.MethodPost, "/team/add", map[string]any{
		teamNameKey: e.prefix + "-one-candidate",
		membersKey: []map[string]any{
			{userIDKey: oneAuthor, usernameKey: "One Author", isActiveKey: true},
			{userIDKey: oneReviewer, usernameKey: "One Reviewer", isActiveKey: true},
		},
	}), http.StatusCreated, "")

	created := make(map[string]struct{}, 3)
	for i := range 3 {
		id := fmt.Sprintf("%s-one-reviewer-%d", e.prefix, i)
		r := e.call(http.MethodPost, "/pullRequest/create", map[string]any{
			prIDKey:     id,
			prNameKey:   "One reviewer",
			authorIDKey: oneAuthor,
		})
		check(t, r, http.StatusCreated, "")
		if reviewers := readPR(t, r).Reviewers; !slices.Equal(reviewers, []string{oneReviewer}) {
			t.Fatalf("one-candidate reviewers=%v", reviewers)
		}
		created[id] = struct{}{}
	}

	query := url.Values{"user_id": {oneReviewer}, "limit": {"1"}}
	firstResult := e.call(http.MethodGet, "/users/getReview?"+query.Encode(), nil)
	check(t, firstResult, http.StatusOK, "")
	first := readReviewPage(t, firstResult)
	if first.UserID != oneReviewer || len(first.PullRequests) != 1 || first.NextCursor == "" {
		t.Fatalf("first review page=%+v", first)
	}
	if _, ok := created[first.PullRequests[0].ID]; !ok {
		t.Fatalf("unexpected PR on first page: %s", first.PullRequests[0].ID)
	}

	query.Set("cursor", first.NextCursor)
	secondResult := e.call(http.MethodGet, "/users/getReview?"+query.Encode(), nil)
	check(t, secondResult, http.StatusOK, "")
	second := readReviewPage(t, secondResult)
	if second.UserID != oneReviewer || len(second.PullRequests) != 1 {
		t.Fatalf("second review page=%+v", second)
	}
	if second.PullRequests[0].ID == first.PullRequests[0].ID {
		t.Fatalf("duplicate PR across pages: %s", second.PullRequests[0].ID)
	}
	if _, ok := created[second.PullRequests[0].ID]; !ok {
		t.Fatalf("unexpected PR on second page: %s", second.PullRequests[0].ID)
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
	check(t, e.call(http.MethodPost, "/team/add", map[string]any{teamNameKey: e.prefix + "-moved", membersKey: e.members}), 201, "")
	r := e.call(http.MethodGet, "/team/get?team_name="+e.prefix, nil)
	check(t, r, 200, "")
	if string(r.body[membersKey]) != "[]" {
		t.Fatalf("empty members=%s", r.body[membersKey])
	}
}
