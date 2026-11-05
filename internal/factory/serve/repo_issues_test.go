package serve

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func repoIssuesFloor(t *testing.T) (*Handler, string) {
	t.Helper()
	h, clock, g := visibilityFloor(t, map[string]map[string]bool{"reader": {"acme/api": true}})
	token := signViewer(t, h, g, clock, "reader")
	// Cache both visibility answers before installing the issues-only transport.
	for repo, status := range map[string]int{"api": 200, "web": 404} {
		w := identityRequest(h, "GET", "/v1/repos/acme/"+repo+"/facts", "", token, nil)
		if w.Code != status {
			t.Fatalf("prime %s = %d %s", repo, w.Code, w.Body)
		}
	}
	return h, token
}

func repoIssuesGitHub(h *Handler, serve func(*http.Request) (int, http.Header, string)) {
	h.config.GitHubHTTPClient = &http.Client{Transport: githubTransport(func(r *http.Request) (*http.Response, error) {
		status, header, body := serve(r)
		return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
}

func readRepoIssuesAnswer(t *testing.T, h *Handler, token, query string) (api.RepoIssues, *httptest.ResponseRecorder) {
	t.Helper()
	w := identityRequest(h, "GET", "/v1/repos/acme/api/issues"+query, "", token, nil)
	var got api.RepoIssues
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil {
		t.Fatalf("read = %d %s", w.Code, w.Body)
	}
	return got, w
}

// C2, C3, C4: Every issue state and field survives; PRs only count toward
// rows and newest. The caller's cursor and the fixed ordering reach GitHub.
func TestRepoIssuesListsEveryStateAndCountsPullRequests(t *testing.T) {
	h, token := repoIssuesFloor(t)
	const since = "2026-11-01T02:03:04.123+05:30"
	long := strings.Repeat("界", api.RepoIssueBodyMax/3) + "🙂" + "x"
	rows := []map[string]any{
		{"id": int64(9007199254740993), "number": 7, "title": "Open retry", "body": long, "html_url": "https://github.com/acme/api/issues/7", "state": "open", "created_at": "2026-10-01T00:00:00Z", "updated_at": "2026-11-01T01:00:00Z"},
		{"id": 82, "number": 8, "title": "Closed retry", "body": "fixed", "html_url": "https://github.com/acme/api/issues/8", "state": "closed", "created_at": "2026-10-02T00:00:00Z", "updated_at": "2026-11-01T02:00:00Z"},
		{"id": 83, "number": 9, "updated_at": "2026-11-01T03:00:00Z", "pull_request": map[string]any{"url": "https://api.github.com/repos/acme/api/pulls/9"}},
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	repoIssuesGitHub(h, func(r *http.Request) (int, http.Header, string) {
		calls++
		want := url.Values{"state": {"all"}, "sort": {"updated"}, "direction": {"asc"}, "per_page": {"50"}, "since": {since}, "page": {"3"}}
		if r.Method != "GET" || r.URL.Host != "api.github.com" || r.URL.Path != "/repos/acme/api/issues" || !reflect.DeepEqual(r.URL.Query(), want) {
			t.Errorf("GitHub request = %s %s", r.Method, r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer gh-reader" || r.Header.Get("Accept") != "application/vnd.github+json" ||
			r.Header.Get("Content-Type") != "application/json" || r.Header.Get("User-Agent") != "codeaf-factory" {
			t.Errorf("GitHub headers = %v", r.Header)
		}
		return 200, http.Header{}, string(raw)
	})
	got, _ := readRepoIssuesAnswer(t, h, token, "?"+url.Values{"since": {since}, "page": {"3"}}.Encode())
	if calls != 1 || got.Repo != "acme/api" || got.Rows != 3 || len(got.Issues) != 2 || got.Newest == nil || got.Newest.Format(time.RFC3339) != "2026-11-01T03:00:00Z" {
		t.Fatalf("result = %+v, calls = %d", got, calls)
	}
	checkRepoIssueFields(t, got.Issues)
}

func checkRepoIssueFields(t *testing.T, issues []api.RepoIssue) {
	t.Helper()
	first, second := issues[0], issues[1]
	if first.ID != 9007199254740993 || first.Number != 7 || first.Title != "Open retry" || first.State != "open" ||
		first.URL != "https://github.com/acme/api/issues/7" || first.CreatedAt.Format(time.RFC3339) != "2026-10-01T00:00:00Z" ||
		first.UpdatedAt.Format(time.RFC3339) != "2026-11-01T01:00:00Z" || !first.BodyCut || !utf8.ValidString(first.Body) ||
		first.Body != strings.Repeat("界", api.RepoIssueBodyMax/3) {
		t.Fatalf("first issue = %+v", first)
	}
	if second.ID != 82 || second.Number != 8 || second.Title != "Closed retry" || second.State != "closed" || second.Body != "fixed" || second.BodyCut ||
		second.URL != "https://github.com/acme/api/issues/8" || second.CreatedAt.Format(time.RFC3339) != "2026-10-02T00:00:00Z" || !first.UpdatedAt.Before(second.UpdatedAt) {
		t.Fatalf("second issue = %+v", second)
	}
}

// C3, C4: Empty and PR-only results retain the right cursor and row count;
// a full result counts 50 rows and defaults to the first request with no since.
func TestRepoIssuesEmptyPullRequestOnlyAndFullResults(t *testing.T) {
	h, token := repoIssuesFloor(t)
	pr := `{"updated_at":"2026-11-01T03:00:00Z","pull_request":{}}`
	for _, tc := range []struct {
		name, body string
		rows       int
	}{
		{"empty", `[]`, 0},
		{"pull requests", `[` + pr + `]`, 1},
		{"full", `[` + strings.Repeat(pr+",", api.RepoIssuesPerPage-1) + pr + `]`, api.RepoIssuesPerPage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoIssuesGitHub(h, func(r *http.Request) (int, http.Header, string) {
				if r.URL.Query().Get("page") != "1" || r.URL.Query().Has("since") {
					t.Errorf("default query = %s", r.URL.RawQuery)
				}
				return 200, http.Header{}, tc.body
			})
			got, w := readRepoIssuesAnswer(t, h, token, "")
			if got.Rows != tc.rows || got.Issues == nil || len(got.Issues) != 0 || (got.Newest == nil) != (tc.rows == 0) ||
				!strings.Contains(w.Body.String(), `"issues":[]`) || (tc.rows == 0 && strings.Contains(w.Body.String(), `"newest"`)) {
				t.Fatalf("result = %s", w.Body)
			}
		})
	}
}

// C4, C5, C8: Invalid queries, hidden repositories and writes never call GitHub.
func TestRepoIssuesRefusesQueriesHiddenRepositoriesAndWrites(t *testing.T) {
	h, token := repoIssuesFloor(t)
	repoIssuesGitHub(h, func(r *http.Request) (int, http.Header, string) {
		t.Errorf("unexpected GitHub request: %s %s", r.Method, r.URL)
		return 500, http.Header{}, ""
	})
	for _, query := range []string{"?other=x", "?state=all", "?since=a&since=b", "?page=1&page=2", "?page=0", "?page=11", "?page=x", "?page=", "?since=yesterday", "?since=2026-11-01", "?bad=%zz"} {
		w := identityRequest(h, "GET", "/v1/repos/acme/api/issues"+query, "", token, nil)
		if w.Code != 422 {
			t.Errorf("%s = %d %s", query, w.Code, w.Body)
		}
	}
	w := identityRequest(h, "GET", "/v1/repos/acme/web/issues", "", token, nil)
	if w.Code != 404 || !strings.Contains(w.Body.String(), `"error":"repository not found"`) {
		t.Fatalf("hidden repository = %d %s", w.Code, w.Body)
	}
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "HEAD"} {
		w := identityRequest(h, method, "/v1/repos/acme/api/issues", "", token, nil)
		if w.Code != 405 || w.Header().Get("Allow") != "GET" {
			t.Errorf("%s = %d, Allow %q", method, w.Code, w.Header().Get("Allow"))
		}
	}
}

// C6: Both GitHub limit statuses carry the reset, with retry-after as fallback.
func TestRepoIssuesRateLimitsCarryRetryAt(t *testing.T) {
	h, token := repoIssuesFloor(t)
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	h.config.Now = func() time.Time { return now }
	for _, tc := range []struct {
		status int
		header http.Header
		want   time.Time
	}{
		{403, http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {"1793494800"}}, now.Add(time.Hour)},
		{429, http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {"1793494800"}, "Retry-After": {"30"}}, now.Add(time.Hour)},
		{403, http.Header{"Retry-After": {"30"}}, now.Add(30 * time.Second)},
		{429, http.Header{"Retry-After": {now.Add(time.Minute).Format(http.TimeFormat)}}, now.Add(time.Minute)},
	} {
		repoIssuesGitHub(h, func(*http.Request) (int, http.Header, string) { return tc.status, tc.header, `{}` })
		w := identityRequest(h, "GET", "/v1/repos/acme/api/issues", "", token, nil)
		var retry api.RepoIssuesRetry
		if w.Code != 429 || json.Unmarshal(w.Body.Bytes(), &retry) != nil || !retry.RetryAt.Equal(tc.want) {
			t.Errorf("GitHub %d = %d %s, want %s", tc.status, w.Code, w.Body, tc.want)
		}
	}
}

// C8: The route itself stores nothing and appends no activity; the Factory's
// ordinary per-request housekeeping runs as it does for every authenticated request.
// This fixture has no job store and supplies a fixed snapshot, so it covers
// only the route's own effects.
func TestRepoIssuesRouteStoresNothingAndAppendsNoActivity(t *testing.T) {
	h, token := repoIssuesFloor(t)
	before, err := h.config.Record.Since(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := h.seam.Load()
	if err != nil {
		t.Fatal(err)
	}
	repoIssuesGitHub(h, func(*http.Request) (int, http.Header, string) { return 200, http.Header{}, `[]` })
	readRepoIssuesAnswer(t, h, token, "")
	after, err := h.config.Record.Since(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	next, err := h.seam.Load()
	if err != nil || !reflect.DeepEqual(before, after) || !reflect.DeepEqual(snapshot, next) {
		t.Fatalf("route changed the fixture's snapshot or activity: %v", err)
	}
}

// C2: Bodies retain the first 8,192 bytes on a rune boundary, including matching
// words after character 4,000. Only bodies beyond the byte limit are marked cut.
func TestRepoIssuesBodyCutBoundary(t *testing.T) {
	h, token := repoIssuesFloor(t)
	ascii := strings.Repeat("a", 8192)
	emoji := strings.Repeat("🙂", 2048)
	han := strings.Repeat("界", 2730)
	matching := strings.Repeat("a", 4001) + " relatedness " + strings.Repeat("z", 5000)
	for _, tc := range []struct {
		name, body, want string
		cut              bool
	}{
		{"empty", "", "", false},
		{"ASCII below limit", ascii[:8191], ascii[:8191], false},
		{"ASCII at limit", ascii, ascii, false},
		{"ASCII beyond limit", ascii + "a", ascii, true},
		{"matching word after character 4000", matching, matching[:8192], true},
		{"multibyte below limit", han, han, false},
		{"multibyte at limit", emoji, emoji, false},
		{"multibyte crossing limit", han + "🙂", han, true},
		{"multibyte at last byte", ascii[:8191] + "🙂", ascii[:8191], true},
		{"multibyte beyond limit", emoji + "🙂", emoji, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal([]map[string]any{{"body": tc.body, "updated_at": "2026-11-01T00:00:00Z"}})
			if err != nil {
				t.Fatal(err)
			}
			repoIssuesGitHub(h, func(*http.Request) (int, http.Header, string) { return 200, http.Header{}, string(raw) })
			got, _ := readRepoIssuesAnswer(t, h, token, "")
			if len(got.Issues) != 1 {
				t.Fatalf("issues = %+v", got.Issues)
			}
			issue := got.Issues[0]
			if issue.Body != tc.want || issue.BodyCut != tc.cut || len(issue.Body) > 8192 || !utf8.ValidString(issue.Body) {
				t.Fatalf("body = %q (%d bytes), cut = %t; want %q, cut = %t", issue.Body, len(issue.Body), issue.BodyCut, tc.want, tc.cut)
			}
		})
	}
}

// C5 and the sign-in contract: GitHub's own 404 stays not found, and a
// rejected GitHub sign-in is github_expired rather than a missing repository.
func TestRepoIssuesMapsGitHubRepositoryAndSignInFailures(t *testing.T) {
	for _, tc := range []struct{ github, status int }{{404, 404}, {403, 502}, {401, 401}} {
		h, token := repoIssuesFloor(t)
		repoIssuesGitHub(h, func(*http.Request) (int, http.Header, string) { return tc.github, http.Header{}, `{}` })
		w := identityRequest(h, "GET", "/v1/repos/acme/api/issues", "", token, nil)
		if w.Code != tc.status {
			t.Fatalf("GitHub %d = %d %s", tc.github, w.Code, w.Body)
		}
		if tc.github == 401 && !strings.Contains(w.Body.String(), `"reason":"github_expired"`) {
			t.Fatalf("GitHub sign-in = %s", w.Body)
		}
	}
}

// The credential contract permits the local profile only on a solo Factory.
func TestRepoIssuesOnlySoloMayUseTheFactoryProfile(t *testing.T) {
	for _, mode := range []string{api.FactoryTeam, api.FactorySolo} {
		t.Run(mode, func(t *testing.T) {
			h, _ := repoIssuesFloor(t)
			h.config.Mode = mode
			h.config.GitHubToken = func(context.Context) string { return "gh-profile" }
			calls := 0
			repoIssuesGitHub(h, func(r *http.Request) (int, http.Header, string) {
				calls++
				if mode != api.FactorySolo || r.Header.Get("Authorization") != "Bearer gh-profile" {
					t.Errorf("%s used profile: %v", mode, r.Header)
				}
				return 200, http.Header{}, `[]`
			})
			w := identityRequest(h, "GET", "/v1/repos/acme/api/issues", "", "floor-secret", nil)
			wantStatus, wantCalls := 401, 0
			if mode == api.FactorySolo {
				wantStatus, wantCalls = 200, 1
			}
			if w.Code != wantStatus || calls != wantCalls {
				t.Fatalf("%s = %d %s, GitHub calls %d", mode, w.Code, w.Body, calls)
			}
		})
	}
}

// C2, C3: Oversized or invalid upstream results are refused within the
// bounded body read rather than reported as a valid partial result.
func TestRepoIssuesRefusesInvalidGitHubResults(t *testing.T) {
	h, token := repoIssuesFloor(t)
	for _, body := range []string{`{}`, `[] {}`, `[` + strings.Repeat(`{},`, api.RepoIssuesPerPage) + `{}` + `]`} {
		repoIssuesGitHub(h, func(*http.Request) (int, http.Header, string) { return 200, http.Header{}, body })
		w := identityRequest(h, "GET", "/v1/repos/acme/api/issues", "", token, nil)
		if w.Code != 502 {
			t.Fatalf("invalid GitHub result = %d %s", w.Code, w.Body)
		}
	}
	spaces := &repoIssuesSpaces{}
	h.config.GitHubHTTPClient = &http.Client{Transport: githubTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(io.LimitReader(spaces, repoIssuesGitHubMax+2))}, nil
	})}
	w := identityRequest(h, "GET", "/v1/repos/acme/api/issues", "", token, nil)
	if w.Code != 502 || spaces.read != repoIssuesGitHubMax+1 {
		t.Fatalf("oversized GitHub result = %d %s, read %d bytes", w.Code, w.Body, spaces.read)
	}
}

type repoIssuesSpaces struct{ read int }

func (s *repoIssuesSpaces) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}
	s.read += len(p)
	return len(p), nil
}
