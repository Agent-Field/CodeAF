package serve

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func visibilityLimitFloor(t *testing.T) (*Handler, string, *time.Time) {
	t.Helper()
	h, clock, g := visibilityFloor(t, map[string]map[string]bool{"reader": {"acme/api": true}})
	token := signViewer(t, h, g, clock, "reader")
	h.config.Now = func() time.Time { return *clock }
	return h, token, clock
}

func checkVisibilityRetry(t *testing.T, h *Handler, w *httptest.ResponseRecorder, retry time.Time) {
	t.Helper()
	var got struct {
		Factory api.Factory `json:"factory"`
		Error   string      `json:"error"`
		api.RepoIssuesRetry
	}
	if w.Code != http.StatusTooManyRequests || json.Unmarshal(w.Body.Bytes(), &got) != nil ||
		got.Factory != h.factory || got.Error != "GitHub's rate limit is spent; retry after the reset" || !got.RetryAt.Equal(retry) {
		t.Fatalf("visibility refusal = %d %s, want retry at %s", w.Code, w.Body, retry)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &fields); err != nil || len(fields) != 3 {
		t.Fatalf("retry body shape = %s, err = %v", w.Body, err)
	}
}

// B1, B6: Only caller-named repository checks answer a spent limit with
// retry detail, before any issues read, job creation or repository change.
func TestARateLimitedVisibilityCheckAnswersRetryNotMissing(t *testing.T) {
	for _, limit := range []string{"403 remaining", "429 retry seconds", "403 retry date", "403 no reset"} {
		for _, route := range []struct{ name, method, path, body string }{
			{"issues", "GET", "/v1/repos/acme/api/issues", ""},
			{"power", "PUT", "/v1/repos/acme/api", `{"on":false}`},
			{"create", "POST", "/v1/jobs", `{"repo":"acme/api","title":"retry"}`},
			{"search", "GET", "/v1/issues?repo=acme/api", ""},
		} {
			t.Run(limit+"/"+route.name, func(t *testing.T) {
				h, token, clock := visibilityLimitFloor(t)
				if route.name == "power" {
					var err error
					h.config.Store, err = store.Open(t.TempDir())
					if err != nil {
						t.Fatal(err)
					}
					h.config.Mode, h.config.OperatorLogin = api.FactorySolo, "reader"
					h.seam.SetRepos = func([]string) error { t.Error("changed repositories"); return nil }
				}
				h.seam.New = func(string, string) (int, error) { t.Error("created a job"); return 9, nil }
				status, header, retry := 403, http.Header{}, clock.Add(time.Minute)
				switch limit {
				case "403 remaining":
					header.Set("X-RateLimit-Remaining", "0")
					header.Set("X-RateLimit-Reset", strconv.FormatInt(retry.Unix(), 10))
					header.Set("Retry-After", "5")
				case "429 retry seconds":
					status = 429
					header.Set("Retry-After", "60")
				case "403 retry date":
					header.Set("Retry-After", retry.Format(http.TimeFormat))
				case "403 no reset":
					header.Set("X-RateLimit-Remaining", "0")
				}
				probes := 0
				repoIssuesGitHub(h, func(r *http.Request) (int, http.Header, string) {
					if r.Method != "GET" || r.URL.String() != "https://api.github.com/repos/acme/api" ||
						r.Header.Get("Authorization") != "Bearer gh-reader" || r.Header.Get("Accept") != "application/json" ||
						r.Header.Get("User-Agent") != "codeaf-factory" {
						t.Errorf("unexpected GitHub request: %s %s, headers %v", r.Method, r.URL, r.Header)
					}
					probes++
					return status, header, `{}`
				})
				w := identityRequest(h, route.method, route.path, route.body, token, nil)
				checkVisibilityRetry(t, h, w, retry)
				if probes != 1 {
					t.Fatalf("GitHub calls = %d, want only the visibility probe", probes)
				}
			})
		}
	}
}

// B2: A spent limit never becomes a cached denial; a read after reset probes
// again within the denial TTL, and its readable answer is cached normally.
func TestARateLimitedVisibilityCheckIsNotCached(t *testing.T) {
	for _, status := range []int{403, 429} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			h, token, clock := visibilityLimitFloor(t)
			reset := clock.Add(time.Minute)
			probes, issues := 0, 0
			repoIssuesGitHub(h, func(r *http.Request) (int, http.Header, string) {
				switch r.URL.Path {
				case "/repos/acme/api":
					probes++
					if clock.Before(reset) {
						header := http.Header{}
						header.Set("X-RateLimit-Remaining", "0")
						header.Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
						return status, header, `{}`
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
			checkVisibilityRetry(t, h, identityRequest(h, "GET", "/v1/repos/acme/api/issues", "", token, nil), reset)
			if probes != 1 || issues != 0 {
				t.Fatalf("limited read: probes=%d issues=%d", probes, issues)
			}
			*clock = reset.Add(time.Second)
			for range 2 {
				readRepoIssuesAnswer(t, h, token, "")
			}
			if probes != 2 || issues != 2 {
				t.Fatalf("after reset: probes=%d issues=%d", probes, issues)
			}
		})
	}
}

// B3: Definitive denials remain repository-not-found answers cached for the
// visibility TTL.
func TestVisibilityDenialsStillReadAsMissing(t *testing.T) {
	for _, status := range []int{404, 403} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			h, token, clock := visibilityLimitFloor(t)
			probes := 0
			repoIssuesGitHub(h, func(r *http.Request) (int, http.Header, string) {
				probes++
				if r.URL.Path != "/repos/acme/api" {
					t.Errorf("unexpected GitHub request: %s", r.URL)
				}
				return status, http.Header{}, `{}`
			})
			for range 2 {
				w := identityRequest(h, "GET", "/v1/repos/acme/api/issues", "", token, nil)
				var got struct{ Error string }
				if w.Code != 404 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.Error != "repository not found" {
					t.Fatalf("denied read = %d %s", w.Code, w.Body)
				}
			}
			if probes != 1 {
				t.Fatalf("cached denial asked GitHub %d times", probes)
			}
			*clock = clock.Add(repoVisibilityTTL)
			identityRequest(h, "GET", "/v1/repos/acme/api/issues", "", token, nil)
			if probes != 2 {
				t.Fatalf("expired denial did not ask GitHub again: %d", probes)
			}
		})
	}
}

// B3: Server and transport failures still avoid the visibility cache, while
// a 401 still ends the GitHub sign-in rather than setting a retry hint.
func TestVisibilityTransientAndSignInFailuresKeepTheirAnswers(t *testing.T) {
	for _, failure := range []string{"server", "transport", "sign-in"} {
		t.Run(failure, func(t *testing.T) {
			h, token, _ := visibilityLimitFloor(t)
			probes := 0
			h.config.GitHubHTTPClient = &http.Client{Transport: githubTransport(func(*http.Request) (*http.Response, error) {
				probes++
				if failure == "transport" {
					return nil, errors.New("GitHub unavailable")
				}
				status := 503
				if failure == "sign-in" {
					status = 401
				}
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: http.NoBody}, nil
			})}
			for range 2 {
				w := identityRequest(h, "GET", "/v1/repos/acme/api/issues", "", token, nil)
				var got struct{ Error, Reason string }
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if failure == "sign-in" {
					if w.Code != 401 || got.Reason != api.ReasonGitHubExpired {
						t.Fatalf("ended sign-in = %d %s", w.Code, w.Body)
					}
				} else if w.Code != 404 || got.Error != "repository not found" {
					t.Fatalf("transient visibility failure = %d %s", w.Code, w.Body)
				}
			}
			want := 2
			if failure == "sign-in" {
				want = 1
			}
			if probes != want {
				t.Fatalf("probes = %d, want %d", probes, want)
			}
		})
	}
}

// B3, B6: A hidden job keeps the missing-job answer; a narrowed queue hides
// limited repositories until the limit's retry time, with no retry detail or
// cached denial. Only the first check asks GitHub inside that window.
func TestRateLimitedVisibilityStillHidesJobsAndNarrowsLists(t *testing.T) {
	h, token, clock := visibilityLimitFloor(t)
	limited, probes := true, 0
	repoIssuesGitHub(h, func(*http.Request) (int, http.Header, string) {
		probes++
		if limited {
			return 429, http.Header{"Retry-After": {"60"}}, `{}`
		}
		return 200, http.Header{}, `{}`
	})
	for _, path := range []string{"/v1/jobs/1", "/v1/jobs/999"} {
		w := identityRequest(h, "GET", path, "", token, nil)
		var got struct {
			Error   string          `json:"error"`
			RetryAt json.RawMessage `json:"retry_at"`
		}
		if w.Code != 404 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.Error != "job not found" || got.RetryAt != nil {
			t.Fatalf("hidden or missing job = %d %s", w.Code, w.Body)
		}
	}
	if queue := queueAs(t, h, token, false); len(queue.Jobs) != 0 || len(queue.Repos) != 0 {
		t.Fatalf("limited queue exposed jobs or repositories: %+v", queue)
	}
	if probes != 1 {
		t.Fatalf("hidden-job and queue probes = %d, want 1", probes)
	}
	limited = false
	*clock = clock.Add(time.Minute)
	if queue := queueAs(t, h, token, false); len(queue.Jobs) != 3 || len(queue.Repos) != 2 || probes != 3 {
		t.Fatalf("next queue retained a denial: jobs=%d repos=%d probes=%d", len(queue.Jobs), len(queue.Repos), probes)
	}
}

// B2: GitHub said this token's limit is spent until retry_at, so until then the
// Factory asks it nothing more for that token: a second issues read, a queue
// paint over every repository and a job lookup answer from that alone, with
// the same retry_at. The first check after retry_at asks again, and its
// readable answer is cached normally. The limit is never a cached denial.
func TestARateLimitedVisibilityCheckWaitsForRetryAt(t *testing.T) {
	h, token, clock := visibilityLimitFloor(t)
	retry := clock.Add(time.Minute)
	limited, probes := true, 0
	repoIssuesGitHub(h, func(r *http.Request) (int, http.Header, string) {
		switch r.URL.Path {
		case "/repos/acme/api", "/repos/acme/web":
			probes++
			if limited {
				return 403, http.Header{"X-Ratelimit-Remaining": {"4200"}, "Retry-After": {"60"}}, `{}`
			}
			return 200, http.Header{}, `{}`
		case "/repos/acme/api/issues":
			return 200, http.Header{}, `[]`
		default:
			t.Errorf("unexpected GitHub request: %s", r.URL)
			return 500, http.Header{}, `{}`
		}
	})
	checkVisibilityRetry(t, h, identityRequest(h, "GET", "/v1/repos/acme/api/issues", "", token, nil), retry)
	*clock = clock.Add(30 * time.Second)
	checkVisibilityRetry(t, h, identityRequest(h, "GET", "/v1/repos/acme/api/issues", "", token, nil), retry)
	if queue := queueAs(t, h, token, false); len(queue.Jobs) != 0 || len(queue.Repos) != 0 {
		t.Fatalf("limited queue exposed jobs or repositories: %+v", queue)
	}
	if w := identityRequest(h, "GET", "/v1/jobs/1", "", token, nil); w.Code != 404 {
		t.Fatalf("limited job lookup = %d %s", w.Code, w.Body)
	}
	if probes != 1 {
		t.Fatalf("GitHub was asked %d times inside its limit window, want 1", probes)
	}
	limited = false
	*clock = retry
	for range 2 {
		readRepoIssuesAnswer(t, h, token, "")
	}
	if probes != 2 {
		t.Fatalf("after retry_at: probes = %d, want 2", probes)
	}
}

// B2: A retry time far off is relied on for one visibility TTL at most, still
// answering GitHub's own retry_at, and then GitHub is asked again.
func TestAFarRateLimitRetryIsCheckedAgainAfterTheVisibilityTTL(t *testing.T) {
	h, token, clock := visibilityLimitFloor(t)
	reset := clock.Add(55 * time.Minute)
	probes := 0
	repoIssuesGitHub(h, func(r *http.Request) (int, http.Header, string) {
		probes++
		return 403, http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {strconv.FormatInt(reset.Unix(), 10)}}, `{}`
	})
	start := *clock
	checkVisibilityRetry(t, h, identityRequest(h, "GET", "/v1/repos/acme/api/issues", "", token, nil), reset)
	*clock = start.Add(repoVisibilityTTL - time.Second)
	checkVisibilityRetry(t, h, identityRequest(h, "GET", "/v1/repos/acme/api/issues", "", token, nil), reset)
	if probes != 1 {
		t.Fatalf("inside the visibility TTL GitHub was asked %d times, want 1", probes)
	}
	*clock = start.Add(repoVisibilityTTL)
	checkVisibilityRetry(t, h, identityRequest(h, "GET", "/v1/repos/acme/api/issues", "", token, nil), reset)
	if probes != 2 {
		t.Fatalf("after the visibility TTL GitHub was asked %d times, want 2", probes)
	}
}
