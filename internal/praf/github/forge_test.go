package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// An unchanged list is a 304 asked with the ETag the last answer carried, and
// it surfaces as ErrNotModified with nothing read.
func TestListIssuesAsksWithTheETag(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.Header.Get("If-None-Match")+"|"+r.URL.RawQuery)
		if r.Header.Get("If-None-Match") == `"e1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"e1"`)
		_, _ = w.Write([]byte(`[{"number":1,"title":"a","user":{"login":"x"}},{"number":2,"title":"b","pull_request":{}}]`))
	}))
	defer srv.Close()
	c := testClient(srv.URL, "tok")
	since := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	issues, tag, err := c.ListIssues(context.Background(), "o", "r", since, "")
	if err != nil || tag != `"e1"` || len(issues) != 2 || issues[0].User != "x" || !issues[1].Pull {
		t.Fatalf("first read %+v %q %v", issues, tag, err)
	}
	if !strings.Contains(asked[0], "since=2026-10-07T09%3A00%3A00Z") || !strings.Contains(asked[0], "state=open") {
		t.Fatalf("the list was asked as %q", asked[0])
	}
	_, tag, err = c.ListIssues(context.Background(), "o", "r", since, tag)
	if !errors.Is(err, ErrNotModified) || tag != `"e1"` {
		t.Fatalf("an unchanged list answered %q %v", tag, err)
	}
}

// A rate limit is slept for as long as GitHub said, and a plain 403 is an
// answer that is not tried again.
func TestReadsSleepTheRateLimitAndNotARefusal(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch {
		case strings.HasSuffix(r.URL.Path, "/permission"):
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"Must have push access"}`))
		case calls == 1:
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			_, _ = w.Write([]byte(`{"login":"me"}`))
		}
	}))
	defer srv.Close()
	c := testClient(srv.URL, "tok")
	var slept []time.Duration
	c.sleepFn = func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil }
	if me, err := c.Me(context.Background()); err != nil || me != "me" {
		t.Fatalf("me %q %v", me, err)
	}
	if len(slept) != 1 || slept[0] != 7*time.Second {
		t.Fatalf("slept %v, want the 7s GitHub asked for", slept)
	}
	before := calls
	perm, err := c.Permission(context.Background(), "o", "r", "u")
	if err != nil || perm != "none" || calls != before+1 {
		t.Fatalf("a refusal answered %q %v after %d calls", perm, err, calls-before)
	}
}

func TestPermissionOf404IsNone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/stranger/") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"permission":"write","role_name":"maintain"}`))
	}))
	defer srv.Close()
	c := testClient(srv.URL, "tok")
	if p, err := c.Permission(context.Background(), "o", "r", "stranger"); err != nil || p != "none" {
		t.Fatalf("404 answered %q %v", p, err)
	}
	if p, _ := c.Permission(context.Background(), "o", "r", "keeper"); p != "maintain" {
		t.Fatalf("a maintainer answered %q", p)
	}
}

func TestChecksAddUp(t *testing.T) {
	cases := []struct {
		status, runs string
		want         CheckState
	}{
		{`{"statuses":[]}`, `{"check_runs":[]}`, ChecksNone},
		{`{"statuses":[{"state":"success"}]}`, `{"check_runs":[{"status":"completed","conclusion":"success"}]}`, ChecksPassed},
		{`{"statuses":[{"state":"success"}]}`, `{"check_runs":[{"status":"queued"}]}`, ChecksRunning},
		{`{"statuses":[{"state":"pending"}]}`, `{"check_runs":[{"status":"completed","conclusion":"failure"}]}`, ChecksFailed},
		{`{"statuses":[{"state":"error"}]}`, `{"check_runs":[]}`, ChecksFailed},
		{`{"statuses":[]}`, `{"check_runs":[{"status":"completed","conclusion":"skipped"}]}`, ChecksNone},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/status") {
				_, _ = w.Write([]byte(tc.status))
				return
			}
			_, _ = w.Write([]byte(tc.runs))
		}))
		got, err := testClient(srv.URL, "tok").Checks(context.Background(), "o", "r", "sha")
		srv.Close()
		if err != nil || got != tc.want {
			t.Fatalf("%s + %s added up to %q %v, want %q", tc.status, tc.runs, got, err, tc.want)
		}
	}
}

// The repository list pages until a short page and comes back newest pushed
// first.
func TestReposPagesAndSorts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("affiliation") != "owner,collaborator,organization_member" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("page") == "1" {
			var b strings.Builder
			b.WriteString("[")
			for i := 0; i < 100; i++ {
				if i > 0 {
					b.WriteString(",")
				}
				b.WriteString(`{"full_name":"o/old","name":"old","owner":{"login":"o"},"pushed_at":"2026-01-01T00:00:00Z"}`)
			}
			b.WriteString("]")
			_, _ = w.Write([]byte(b.String()))
			return
		}
		_, _ = w.Write([]byte(`[{"full_name":"o/new","name":"new","owner":{"login":"o"},"private":true,"pushed_at":"2026-10-01T00:00:00Z","open_issues_count":12}]`))
	}))
	defer srv.Close()
	repos, err := testClient(srv.URL, "tok").Repos(context.Background())
	if err != nil || len(repos) != 101 || repos[0].Full != "o/new" || !repos[0].Private {
		t.Fatalf("repos %d, first %+v, %v", len(repos), repos[0], err)
	}
	// open_issues_count is carried, and a row that leaves it out says -1.
	if repos[0].Open != 12 || repos[1].Open != -1 {
		t.Fatalf("open counts %d and %d, want 12 and -1", repos[0].Open, repos[1].Open)
	}
}

// A write is tried again on a 5xx and never on a refusal.
func TestWritesRetryOnlyTheServersFault(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/labels") {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		if calls == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"id":9,"html_url":"https://github.com/o/r/issues/1#issuecomment-9"}`))
	}))
	defer srv.Close()
	c := testClient(srv.URL, "tok")
	p, err := c.Comment(context.Background(), "o", "r", 1, "hi")
	if err != nil || p.ID != "9" || calls != 2 {
		t.Fatalf("comment %+v %v after %d calls", p, err, calls)
	}
	calls = 0
	if _, err := c.AddLabels(context.Background(), "o", "r", 1, []string{"x"}); err == nil || calls != 1 {
		t.Fatalf("a refused label was tried %d times: %v", calls, err)
	}
}
