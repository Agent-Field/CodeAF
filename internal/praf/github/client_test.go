package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/praf/schemas"
)

// ---- helpers ---------------------------------------------------------------

func testClient(baseURL, token string) *client {
	return &client{
		token:      token,
		baseURL:    baseURL,
		httpClient: &http.Client{},
		sleepFn:    func(context.Context, time.Duration) error { return nil },
	}
}

// ---- ParsePRURL ------------------------------------------------------------

func TestParsePRURL(t *testing.T) {
	c := testClient("http://x", "")
	cases := []struct {
		url     string
		owner   string
		repo    string
		number  int
		wantErr bool
	}{
		{"https://github.com/octocat/hello/pull/123", "octocat", "hello", 123, false},
		{"http://github.com/foo/bar/pull/7", "foo", "bar", 7, false},
		{"https://github.com/o/r/pull/123/files", "o", "r", 123, false},         // trailing path allowed
		{"https://github.com/o/r/pull/123#discussion_r1", "o", "r", 123, false}, // fragment allowed
		{"https://github.com/o/r/pull/12abc", "o", "r", 12, false},              // not end-anchored: digits only
		{"https://gitlab.com/o/r/pull/1", "", "", 0, true},
		{"https://github.com/o/r/pulls/1", "", "", 0, true},
		{"https://github.com/o/r/pull/abc", "", "", 0, true},
		{"not a url", "", "", 0, true},
		{"", "", "", 0, true},
	}
	for _, tc := range cases {
		owner, repo, number, err := c.ParsePRURL(tc.url)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParsePRURL(%q): expected error, got none", tc.url)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParsePRURL(%q): unexpected error: %v", tc.url, err)
			continue
		}
		if owner != tc.owner || repo != tc.repo || number != tc.number {
			t.Errorf("ParsePRURL(%q) = (%q,%q,%d), want (%q,%q,%d)",
				tc.url, owner, repo, number, tc.owner, tc.repo, tc.number)
		}
	}
}

// ---- fake GitHub for FetchPR ----------------------------------------------

const metaJSON = `{"title":"Add feature","body":"Body text","labels":[{"name":"bug"},{"name":""},{"name":"enhancement"}],"user":{"login":"octocat"},"base":{"sha":"basesha111"},"head":{"sha":"headsha222"}}`

const diffText = "diff --git a/x b/x\n@@ -0,0 +1 @@\n+hi\n"

type ghRec struct {
	mu          sync.Mutex
	metaCalls   int
	filesPages  map[int]bool
	perPageFile string
	filesAccept string
	auth        string
	metaAccept  string
	diffAccept  string
	sawDiff     bool
	metaStatus  func(call int) int
}

func filesPage(page int) []map[string]any {
	switch page {
	case 1:
		out := make([]map[string]any, 100)
		for i := range out {
			out[i] = map[string]any{
				"filename":  fmt.Sprintf("f%d.go", i),
				"status":    "modified",
				"additions": i,
				"deletions": 0,
				"patch":     "@@",
			}
		}
		return out
	case 2:
		return []map[string]any{
			{"filename": "normal.go", "status": "added", "additions": 1, "deletions": 0, "patch": "@@"},
			{"filename": "renamed_new.go", "status": "renamed", "previous_filename": "renamed_old.go", "additions": 0, "deletions": 0, "patch": ""},
			{"filename": "nostatus.go", "additions": 2, "deletions": 1, "patch": "@@"}, // no "status" key -> default "modified"
		}
	default:
		return []map[string]any{}
	}
}

func commitsPage(page int) []map[string]any {
	switch page {
	case 1:
		out := make([]map[string]any, 100)
		for i := range out {
			msg := fmt.Sprintf("c%d", i)
			if i == 0 {
				msg = "" // empty message -> dropped
			}
			out[i] = map[string]any{"commit": map[string]any{"message": msg}}
		}
		return out
	case 2:
		out := make([]map[string]any, 100)
		for i := range out {
			out[i] = map[string]any{"commit": map[string]any{"message": fmt.Sprintf("d%d", i)}}
		}
		return out
	default:
		return []map[string]any{} // empty page -> break
	}
}

func newFetchServer(t *testing.T, rec *ghRec) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	registerFetchHandlers(mux, rec)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// registerFetchHandlers wires the PR metadata/files/commits/diff endpoints onto
// mux so they can be combined with the App installation-token endpoints.
func registerFetchHandlers(mux *http.ServeMux, rec *ghRec) {
	if rec.filesPages == nil {
		rec.filesPages = map[int]bool{}
	}

	// metadata + diff share this path (branch on Accept header).
	mux.HandleFunc("/repos/o/r/pulls/7", func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.auth = r.Header.Get("Authorization")
		if r.Header.Get("Accept") == "application/vnd.github.v3.diff" {
			rec.sawDiff = true
			rec.diffAccept = r.Header.Get("Accept")
			rec.mu.Unlock()
			_, _ = io.WriteString(w, diffText)
			return
		}
		rec.metaCalls++
		call := rec.metaCalls
		rec.metaAccept = r.Header.Get("Accept")
		statusFn := rec.metaStatus
		rec.mu.Unlock()

		status := http.StatusOK
		if statusFn != nil {
			status = statusFn(call)
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"message":"boom"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, metaJSON)
	})

	mux.HandleFunc("/repos/o/r/pulls/7/files", func(w http.ResponseWriter, r *http.Request) {
		page := atoiDefault(r.URL.Query().Get("page"), 1)
		rec.mu.Lock()
		rec.filesPages[page] = true
		rec.perPageFile = r.URL.Query().Get("per_page")
		rec.filesAccept = r.Header.Get("Accept")
		rec.mu.Unlock()
		_ = json.NewEncoder(w).Encode(filesPage(page))
	})

	mux.HandleFunc("/repos/o/r/pulls/7/commits", func(w http.ResponseWriter, r *http.Request) {
		page := atoiDefault(r.URL.Query().Get("page"), 1)
		_ = json.NewEncoder(w).Encode(commitsPage(page))
	})
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return def
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func TestFetchPR_Success(t *testing.T) {
	rec := &ghRec{}
	srv := newFetchServer(t, rec)
	c := testClient(srv.URL, "test-token")

	data, err := c.FetchPR(context.Background(), "o", "r", 7)
	if err != nil {
		t.Fatalf("FetchPR: %v", err)
	}

	if data.Owner != "o" || data.Repo != "r" || data.Number != 7 {
		t.Errorf("owner/repo/number = %q/%q/%d", data.Owner, data.Repo, data.Number)
	}
	if data.Title != "Add feature" {
		t.Errorf("title = %q", data.Title)
	}
	if data.Description != "Body text" {
		t.Errorf("description = %q", data.Description)
	}
	if want := []string{"bug", "enhancement"}; !equalStrings(data.Labels, want) {
		t.Errorf("labels = %v, want %v (empty-name label must be dropped)", data.Labels, want)
	}
	if data.Author != "octocat" {
		t.Errorf("author = %q", data.Author)
	}
	if data.BaseSHA != "basesha111" || data.HeadSHA != "headsha222" {
		t.Errorf("base/head sha = %q/%q", data.BaseSHA, data.HeadSHA)
	}
	if len(data.ChangedFiles) != 103 {
		t.Errorf("changed files = %d, want 103 (pagination)", len(data.ChangedFiles))
	}
	if len(data.CommitMessages) != 199 {
		t.Errorf("commit messages = %d, want 199 (empty dropped + pagination)", len(data.CommitMessages))
	}
	if data.Diff != diffText {
		t.Errorf("diff = %q, want %q", data.Diff, diffText)
	}

	// renamed + missing-status defaults
	byPath := map[string]schemas.ChangedFile{}
	for _, f := range data.ChangedFiles {
		byPath[f.Path] = f
	}
	if f, ok := byPath["renamed_new.go"]; !ok {
		t.Error("missing renamed_new.go")
	} else if f.PreviousPath == nil || *f.PreviousPath != "renamed_old.go" || f.Status != "renamed" {
		t.Errorf("renamed file = %+v", f)
	}
	if f, ok := byPath["nostatus.go"]; !ok {
		t.Error("missing nostatus.go")
	} else if f.Status != "modified" {
		t.Errorf("missing-status file Status = %q, want default \"modified\"", f.Status)
	} else if f.PreviousPath != nil {
		t.Errorf("nostatus.go PreviousPath = %v, want nil", *f.PreviousPath)
	}

	// headers + pagination assertions
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.auth != "Bearer test-token" {
		t.Errorf("auth header = %q, want %q", rec.auth, "Bearer test-token")
	}
	if rec.metaAccept != "application/vnd.github+json" {
		t.Errorf("metadata Accept = %q", rec.metaAccept)
	}
	if rec.filesAccept != "application/vnd.github+json" {
		t.Errorf("files Accept = %q", rec.filesAccept)
	}
	if rec.perPageFile != "100" {
		t.Errorf("files per_page = %q, want 100", rec.perPageFile)
	}
	if !rec.sawDiff || rec.diffAccept != "application/vnd.github.v3.diff" {
		t.Errorf("diff request: sawDiff=%v accept=%q", rec.sawDiff, rec.diffAccept)
	}
	if !rec.filesPages[1] || !rec.filesPages[2] {
		t.Errorf("files pagination did not reach page 2: %v", rec.filesPages)
	}
}

func TestFetchPR_RetryThenSucceed(t *testing.T) {
	for _, status := range []int{500, 429, 403} {
		status := status
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			rec := &ghRec{metaStatus: func(call int) int {
				if call == 1 {
					return status
				}
				return 200
			}}
			srv := newFetchServer(t, rec)
			c := testClient(srv.URL, "test-token")

			data, err := c.FetchPR(context.Background(), "o", "r", 7)
			if err != nil {
				t.Fatalf("FetchPR after retry: %v", err)
			}
			if data.Title != "Add feature" {
				t.Errorf("title = %q", data.Title)
			}
			rec.mu.Lock()
			calls := rec.metaCalls
			rec.mu.Unlock()
			if calls != 2 {
				t.Errorf("metadata calls = %d, want 2 (one failure + one success)", calls)
			}
		})
	}
}

func TestFetchPR_FailFastOn404(t *testing.T) {
	rec := &ghRec{metaStatus: func(call int) int { return 404 }}
	srv := newFetchServer(t, rec)
	c := testClient(srv.URL, "test-token")

	_, err := c.FetchPR(context.Background(), "o", "r", 7)
	if err == nil {
		t.Fatal("expected error on 404")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is not *APIError: %v", err)
	}
	if apiErr.StatusCode != 404 {
		t.Errorf("status = %d, want 404", apiErr.StatusCode)
	}
	rec.mu.Lock()
	calls := rec.metaCalls
	rec.mu.Unlock()
	if calls != 1 {
		t.Errorf("metadata calls = %d, want 1 (fail fast, no retry)", calls)
	}
}

func TestFetchPR_RetriesExhausted(t *testing.T) {
	rec := &ghRec{metaStatus: func(call int) int { return 500 }} // always fails
	srv := newFetchServer(t, rec)
	c := testClient(srv.URL, "test-token")

	_, err := c.FetchPR(context.Background(), "o", "r", 7)
	if err == nil {
		t.Fatal("expected error after retries exhausted")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 500 {
		t.Errorf("expected *APIError 500, got %v", err)
	}
	rec.mu.Lock()
	calls := rec.metaCalls
	rec.mu.Unlock()
	if calls != 4 {
		t.Errorf("metadata calls = %d, want 4 (all attempts)", calls)
	}
}

// ---- PostReview ------------------------------------------------------------

type postRec struct {
	mu       sync.Mutex
	auth     string
	body     []byte
	bodies   [][]byte
	calls    int
	status   int
	statusFn func(call int) int
	respBody string
}

func newPostServer(t *testing.T, rec *postRec) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/pulls/7/reviews", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.auth = r.Header.Get("Authorization")
		rec.body = b
		rec.bodies = append(rec.bodies, append([]byte(nil), b...))
		rec.calls++
		call := rec.calls
		status := rec.status
		if rec.statusFn != nil {
			status = rec.statusFn(call)
		}
		respBody := rec.respBody
		rec.mu.Unlock()
		if status >= 400 {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, respBody)
			return
		}
		if respBody == "" {
			respBody = `{"id":42,"state":"COMMENTED"}`
		}
		_, _ = io.WriteString(w, respBody)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestPostReview_PayloadShape(t *testing.T) {
	rec := &postRec{}
	srv := newPostServer(t, rec)
	c := testClient(srv.URL, "test-token")

	review := schemas.GitHubReview{
		Body:  "Summary body",
		Event: "COMMENT",
		Comments: []schemas.GitHubComment{
			{Path: "a.go", Line: 5, Side: "RIGHT", Body: "keep me"},
			{Path: "", Line: 9, Side: "RIGHT", Body: "no path -> drop"},
			{Path: "b.go", Line: 0, Side: "LEFT", Body: "line 0 -> drop"},
		},
	}
	result, err := c.PostReview(context.Background(), "o", "r", 7, review, "abc123def456")
	if err != nil {
		t.Fatalf("PostReview: %v", err)
	}
	if got, _ := result["id"].(float64); got != 42 {
		t.Errorf("result id = %v, want 42", result["id"])
	}

	rec.mu.Lock()
	body := rec.body
	auth := rec.auth
	rec.mu.Unlock()

	if auth != "Bearer test-token" {
		t.Errorf("auth = %q", auth)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("payload not json: %v (%s)", err, body)
	}
	if payload["body"] != "Summary body" {
		t.Errorf("body = %v", payload["body"])
	}
	if payload["event"] != "COMMENT" {
		t.Errorf("event = %v", payload["event"])
	}
	if payload["commit_id"] != "abc123def456" {
		t.Errorf("commit_id = %v", payload["commit_id"])
	}
	comments, ok := payload["comments"].([]any)
	if !ok {
		t.Fatalf("comments not an array: %v", payload["comments"])
	}
	if len(comments) != 1 {
		t.Fatalf("comments len = %d, want 1 (filtered)", len(comments))
	}
	cm := comments[0].(map[string]any)
	if cm["path"] != "a.go" || cm["line"].(float64) != 5 || cm["side"] != "RIGHT" || cm["body"] != "keep me" {
		t.Errorf("kept comment = %v", cm)
	}
}

func TestPostReview_NoCommitID_NoComments(t *testing.T) {
	rec := &postRec{}
	srv := newPostServer(t, rec)
	c := testClient(srv.URL, "test-token")

	// zero comments -> no "comments" key at all; empty commitID -> no commit_id key
	review := schemas.GitHubReview{Body: "b", Event: "APPROVE", Comments: []schemas.GitHubComment{}}
	if _, err := c.PostReview(context.Background(), "o", "r", 7, review, ""); err != nil {
		t.Fatalf("PostReview: %v", err)
	}
	rec.mu.Lock()
	body := rec.body
	rec.mu.Unlock()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["commit_id"]; ok {
		t.Errorf("commit_id present, want absent: %v", payload)
	}
	if _, ok := payload["comments"]; ok {
		t.Errorf("comments present for empty list, want absent: %v", payload)
	}
}

func TestPostReview_AllCommentsFilteredStillEmitsEmptyList(t *testing.T) {
	rec := &postRec{}
	srv := newPostServer(t, rec)
	c := testClient(srv.URL, "test-token")

	// non-empty original list, but all fail the path/line filter -> "comments": []
	review := schemas.GitHubReview{
		Body:  "b",
		Event: "COMMENT",
		Comments: []schemas.GitHubComment{
			{Path: "", Line: 1, Side: "RIGHT", Body: "x"},
			{Path: "y.go", Line: 0, Side: "RIGHT", Body: "y"},
		},
	}
	if _, err := c.PostReview(context.Background(), "o", "r", 7, review, ""); err != nil {
		t.Fatalf("PostReview: %v", err)
	}
	rec.mu.Lock()
	body := rec.body
	rec.mu.Unlock()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	comments, ok := payload["comments"].([]any)
	if !ok {
		t.Fatalf("comments key absent, want present empty list: %v", payload)
	}
	if len(comments) != 0 {
		t.Errorf("comments len = %d, want 0", len(comments))
	}
}

func TestPostReview_422TypedError(t *testing.T) {
	rec := &postRec{
		status:   422,
		respBody: `{"message":"Unprocessable Entity","errors":[{"resource":"PullRequestReview","code":"custom","message":"Review cannot be submitted on your own pull request"}]}`,
	}
	srv := newPostServer(t, rec)
	c := testClient(srv.URL, "test-token")

	review := schemas.GitHubReview{Body: "b", Event: "REQUEST_CHANGES", Comments: []schemas.GitHubComment{}}
	_, err := c.PostReview(context.Background(), "o", "r", 7, review, "")
	if err == nil {
		t.Fatal("expected 422 error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is not *APIError: %v", err)
	}
	if apiErr.StatusCode != 422 {
		t.Errorf("status = %d, want 422", apiErr.StatusCode)
	}
	if !strings.Contains(apiErr.Body, "own pull request") {
		t.Errorf("body does not carry the 422 reason: %q", apiErr.Body)
	}
}

func TestPostReview_RetriesTransientServerFailure(t *testing.T) {
	rec := &postRec{statusFn: func(call int) int {
		if call == 1 {
			return http.StatusGatewayTimeout
		}
		return http.StatusOK
	}}
	srv := newPostServer(t, rec)
	c := testClient(srv.URL, "test-token")
	var delays []time.Duration
	c.sleepFn = func(_ context.Context, d time.Duration) error {
		delays = append(delays, d)
		return nil
	}

	result, err := c.PostReview(context.Background(), "o", "r", 7, schemas.GitHubReview{Body: "b", Event: "COMMENT"}, "sha")
	if err != nil {
		t.Fatalf("PostReview: %v", err)
	}
	if got, _ := result["id"].(float64); got != 42 {
		t.Errorf("result id = %v, want 42", result["id"])
	}
	rec.mu.Lock()
	calls := rec.calls
	bodies := append([][]byte(nil), rec.bodies...)
	rec.mu.Unlock()
	if calls != 2 {
		t.Errorf("POST calls = %d, want 2", calls)
	}
	if len(bodies) != 2 || !bytes.Equal(bodies[0], bodies[1]) {
		t.Errorf("retry payloads = %q, want two identical payloads", bodies)
	}
	if want := []time.Duration{2 * time.Second}; !equalDurations(delays, want) {
		t.Errorf("backoff delays = %v, want %v", delays, want)
	}
}

func TestPostReview_RetriesExhausted(t *testing.T) {
	rec := &postRec{status: http.StatusInternalServerError, respBody: `{"message":"boom"}`}
	srv := newPostServer(t, rec)
	c := testClient(srv.URL, "test-token")
	var delays []time.Duration
	c.sleepFn = func(_ context.Context, d time.Duration) error {
		delays = append(delays, d)
		return nil
	}

	_, err := c.PostReview(context.Background(), "o", "r", 7, schemas.GitHubReview{}, "")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("error = %v, want final *APIError 500", err)
	}
	rec.mu.Lock()
	calls := rec.calls
	rec.mu.Unlock()
	if calls != postReviewMaxAttempts {
		t.Errorf("POST calls = %d, want %d", calls, postReviewMaxAttempts)
	}
	if want := []time.Duration{2 * time.Second, 4 * time.Second}; !equalDurations(delays, want) {
		t.Errorf("backoff delays = %v, want %v", delays, want)
	}
}

func TestPostReview_DoesNotRetry422(t *testing.T) {
	rec := &postRec{status: http.StatusUnprocessableEntity, respBody: `{"message":"unprocessable"}`}
	srv := newPostServer(t, rec)
	c := testClient(srv.URL, "test-token")
	var delays []time.Duration
	c.sleepFn = func(_ context.Context, d time.Duration) error {
		delays = append(delays, d)
		return nil
	}

	_, err := c.PostReview(context.Background(), "o", "r", 7, schemas.GitHubReview{}, "")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("error = %v, want *APIError 422", err)
	}
	rec.mu.Lock()
	calls := rec.calls
	rec.mu.Unlock()
	if calls != 1 || len(delays) != 0 {
		t.Errorf("calls/delays = %d/%v, want 1/none", calls, delays)
	}
}

func TestPostReview_DoesNotRetryOtherClientErrors(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			rec := &postRec{status: status}
			srv := newPostServer(t, rec)
			c := testClient(srv.URL, "test-token")
			var sleeps int
			c.sleepFn = func(context.Context, time.Duration) error {
				sleeps++
				return nil
			}

			_, err := c.PostReview(context.Background(), "o", "r", 7, schemas.GitHubReview{}, "")
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != status {
				t.Fatalf("error = %v, want *APIError %d", err, status)
			}
			rec.mu.Lock()
			calls := rec.calls
			rec.mu.Unlock()
			if calls != 1 || sleeps != 0 {
				t.Errorf("calls/sleeps = %d/%d, want 1/0", calls, sleeps)
			}
		})
	}
}

func TestIsRetryablePostReviewError_StatusBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		want   bool
	}{
		{name: "below server errors", status: http.StatusRequestTimeout, want: false},
		{name: "first server error", status: http.StatusInternalServerError, want: true},
		{name: "last server error", status: 599, want: true},
		{name: "above server errors", status: 600, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := &APIError{StatusCode: tc.status}
			if got := isRetryablePostReviewError(err); got != tc.want {
				t.Errorf("isRetryablePostReviewError(status %d) = %t, want %t", tc.status, got, tc.want)
			}
		})
	}

	if !isRetryablePostReviewError(&transportError{err: errors.New("connection reset")}) {
		t.Error("transport error should be retryable")
	}
	if isRetryablePostReviewError(errors.New("payload marshal failure")) {
		t.Error("ordinary local error should not be retryable")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestPostReview_RetriesTransportFailure(t *testing.T) {
	c := testClient("http://github.test", "test-token")
	var calls int
	c.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("temporary network failure")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"id":42}`)),
		}, nil
	})}
	var delays []time.Duration
	c.sleepFn = func(_ context.Context, d time.Duration) error {
		delays = append(delays, d)
		return nil
	}

	if _, err := c.PostReview(context.Background(), "o", "r", 7, schemas.GitHubReview{}, ""); err != nil {
		t.Fatalf("PostReview: %v", err)
	}
	if calls != 2 {
		t.Errorf("transport calls = %d, want 2", calls)
	}
	if want := []time.Duration{2 * time.Second}; !equalDurations(delays, want) {
		t.Errorf("backoff delays = %v, want %v", delays, want)
	}
}

func TestPostReview_StopsWhenBackoffCanceled(t *testing.T) {
	rec := &postRec{status: http.StatusBadGateway}
	srv := newPostServer(t, rec)
	c := testClient(srv.URL, "test-token")
	c.sleepFn = func(context.Context, time.Duration) error { return context.Canceled }

	_, err := c.PostReview(context.Background(), "o", "r", 7, schemas.GitHubReview{}, "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	rec.mu.Lock()
	calls := rec.calls
	rec.mu.Unlock()
	if calls != 1 {
		t.Errorf("POST calls = %d, want 1 after canceled backoff", calls)
	}
}

func TestPostReview_MalformedSuccessResponseDoesNotRetry(t *testing.T) {
	rec := &postRec{respBody: "not-json"}
	srv := newPostServer(t, rec)
	c := testClient(srv.URL, "test-token")
	var sleeps int
	c.sleepFn = func(context.Context, time.Duration) error {
		sleeps++
		return nil
	}

	_, err := c.PostReview(context.Background(), "o", "r", 7, schemas.GitHubReview{}, "")
	if err == nil {
		t.Fatal("expected JSON decode error")
	}
	rec.mu.Lock()
	calls := rec.calls
	rec.mu.Unlock()
	if calls != 1 || sleeps != 0 {
		t.Errorf("calls/sleeps = %d/%d, want 1/0", calls, sleeps)
	}
}

func TestNewClient_TakesTheTokenItIsHanded(t *testing.T) {
	t.Setenv("GH_TOKEN", "from-env")
	if c := NewClient("explicit"); c.token != "explicit" || c.baseURL != defaultBaseURL {
		t.Errorf("token %q base %q", c.token, c.baseURL)
	}
	if c := NewClient(""); c.token != "" {
		t.Errorf("token = %q, want none: the caller resolves the token, the client reads no environment", c.token)
	}
}

// ---- small helpers ---------------------------------------------------------

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalDurations(a, b []time.Duration) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
