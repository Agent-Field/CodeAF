package serve

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// D1: A bare 429 on visibility answers the three-field retry body at every
// repository entry point, before issue reads, job creation or repo changes.
func TestBare429VisibilityAnswersRetryNotMissing(t *testing.T) {
	for _, route := range []struct{ name, method, path, body string }{
		{"issues", "GET", "/v1/repos/acme/api/issues", ""},
		{"power off", "PUT", "/v1/repos/acme/api", `{"on":false}`},
		{"power on", "PUT", "/v1/repos/acme/api", `{"on":true}`},
		{"create", "POST", "/v1/jobs", `{"repo":"acme/api","title":"retry"}`},
		{"search", "GET", "/v1/issues?repo=acme/api", ""},
	} {
		t.Run(route.name, func(t *testing.T) {
			h, token, clock := visibilityLimitFloor(t)
			if route.method == "PUT" {
				var err error
				h.config.Store, err = store.Open(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				h.config.Mode, h.config.OperatorLogin = api.FactorySolo, "reader"
				h.seam.SetRepos = func([]string) error { t.Error("changed repositories"); return nil }
			}
			h.seam.New = func(string, string) (int, error) { t.Error("created a job"); return 9, nil }
			probes := 0
			repoIssuesGitHub(h, func(r *http.Request) (int, http.Header, string) {
				probes++
				if r.Method != "GET" || r.URL.String() != "https://api.github.com/repos/acme/api" {
					t.Errorf("unexpected GitHub request: %s %s", r.Method, r.URL)
				}
				return 429, http.Header{}, `{}`
			})
			w := identityRequest(h, route.method, route.path, route.body, token, nil)
			checkVisibilityRetry(t, h, w, clock.Add(time.Minute))
			if probes != 1 {
				t.Fatalf("GitHub calls = %d, want only the visibility probe", probes)
			}
		})
	}
}

// D2: The next request retries a bare 429 within the denial TTL, without
// waiting for retry_at; the recovered grant is cached normally.
func TestBare429VisibilityIsNotCached(t *testing.T) {
	h, token, clock := visibilityLimitFloor(t)
	probes, issues := 0, 0
	repoIssuesGitHub(h, func(r *http.Request) (int, http.Header, string) {
		switch r.URL.Path {
		case "/repos/acme/api":
			probes++
			if probes == 1 {
				return 429, http.Header{}, `{}`
			}
			return 200, http.Header{}, `{}`
		case "/repos/acme/api/issues":
			issues++
			return 200, http.Header{}, `[]`
		default:
			t.Errorf("unexpected GitHub request: %s", r.URL)
			return 500, http.Header{}, `{}`
		}
	})
	checkVisibilityRetry(t, h, identityRequest(h, "GET", "/v1/repos/acme/api/issues", "", token, nil), clock.Add(time.Minute))
	if probes != 1 || issues != 0 {
		t.Fatalf("limited read: probes=%d issues=%d", probes, issues)
	}
	for range 2 {
		readRepoIssuesAnswer(t, h, token, "")
	}
	if probes != 2 || issues != 2 {
		t.Fatalf("recovered reads: probes=%d issues=%d", probes, issues)
	}
}

// D3, D4: The issues read treats a bare 429 as busy with the Factory clock's
// default, while known resets take precedence and retry hints keep their times.
func TestRepoIssuesRateLimitsIncludeBare429(t *testing.T) {
	h, token := repoIssuesFloor(t)
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	h.config.Now = func() time.Time { return now }
	reset := strconv.FormatInt(now.Add(time.Hour).Unix(), 10)
	for _, tc := range []struct {
		name   string
		status int
		header http.Header
		want   time.Time
	}{
		{"bare 429", 429, http.Header{}, now.Add(time.Minute)},
		{"429 reset only", 429, http.Header{"X-Ratelimit-Reset": {reset}}, now.Add(time.Hour)},
		{"429 reset precedence", 429, http.Header{"X-Ratelimit-Reset": {reset}, "Retry-After": {"30"}}, now.Add(time.Hour)},
		{"403 reset precedence", 403, http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {reset}, "Retry-After": {"30"}}, now.Add(time.Hour)},
		{"429 retry seconds", 429, http.Header{"Retry-After": {"30"}}, now.Add(30 * time.Second)},
		{"403 retry seconds", 403, http.Header{"Retry-After": {"30"}}, now.Add(30 * time.Second)},
		{"429 retry date", 429, http.Header{"Retry-After": {now.Add(2 * time.Minute).Format(http.TimeFormat)}}, now.Add(2 * time.Minute)},
		{"403 retry date", 403, http.Header{"Retry-After": {now.Add(2 * time.Minute).Format(http.TimeFormat)}}, now.Add(2 * time.Minute)},
		{"403 no reset", 403, http.Header{"X-Ratelimit-Remaining": {"0"}}, now.Add(time.Minute)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			repoIssuesGitHub(h, func(r *http.Request) (int, http.Header, string) {
				calls++
				if r.URL.Path != "/repos/acme/api/issues" {
					t.Errorf("unexpected GitHub request: %s", r.URL)
				}
				return tc.status, tc.header, `{}`
			})
			w := identityRequest(h, "GET", "/v1/repos/acme/api/issues", "", token, nil)
			checkVisibilityRetry(t, h, w, tc.want)
			if calls != 1 {
				t.Fatalf("GitHub issue reads = %d, want 1", calls)
			}
		})
	}
}
