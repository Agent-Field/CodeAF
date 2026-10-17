package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// The last comments are found from the count alone: the last page, and the
// page before it when the last holds fewer than asked for.
func TestIssueCommentsReadsTheLastPage(t *testing.T) {
	var pages []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		pages = append(pages, r.URL.Path+"?"+r.URL.Query().Get("page"))
		// 101 comments: page 1 holds 1..100, page 2 holds 101.
		from, to := (page-1)*100+1, min(page*100, 101)
		w.Write([]byte("["))
		for i := from; i <= to; i++ {
			if i > from {
				w.Write([]byte(","))
			}
			fmt.Fprintf(w, `{"user":{"login":"u%d"},"body":"c%d","created_at":"2026-10-07T09:00:00Z"}`, i, i)
		}
		w.Write([]byte("]"))
	}))
	defer srv.Close()
	got, err := testClient(srv.URL, "tok").IssueComments(context.Background(), "o", "r", 5, 101, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Body != "c99" || got[2].Body != "c101" || got[2].User != "u101" {
		t.Fatalf("got %+v", got)
	}
	if len(pages) != 2 || pages[0] != "/repos/o/r/issues/5/comments?2" || pages[1] != "/repos/o/r/issues/5/comments?1" {
		t.Fatalf("asked %q", pages)
	}
	none, err := testClient(srv.URL, "tok").IssueComments(context.Background(), "o", "r", 5, 0, 3)
	if err != nil || none != nil || len(pages) != 2 {
		t.Fatal("a count of none asked GitHub")
	}
}

// Files come back most changed first, at most twenty; check runs keep their
// names, states and pages; one issue reads by its number with its count.
func TestFilesChecksAndOneIssue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/pulls/2/files":
			w.Write([]byte("["))
			for i := 0; i < 25; i++ {
				if i > 0 {
					w.Write([]byte(","))
				}
				fmt.Fprintf(w, `{"filename":"f%d.go","additions":%d,"deletions":1}`, i, i)
			}
			w.Write([]byte("]"))
		case "/repos/o/r/commits/s/status":
			w.Write([]byte(`{"state":"success","statuses":[]}`))
		case "/repos/o/r/commits/s/check-runs":
			w.Write([]byte(`{"check_runs":[{"name":"build","status":"completed","conclusion":"failure","html_url":"u1"},{"name":"lint","status":"queued","html_url":"u2"}]}`))
		case "/repos/o/r/issues/4":
			w.Write([]byte(`{"number":4,"title":"t","html_url":"https://github.com/o/r/issues/4","comments":7}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := testClient(srv.URL, "tok")
	files, err := c.PullFiles(context.Background(), "o", "r", 2)
	if err != nil || len(files) != FilesMost || files[0].Path != "f24.go" || files[0].Additions != 24 || files[0].Deletions != 1 {
		t.Fatalf("files %+v %v", files, err)
	}
	state, runs, err := c.CheckRuns(context.Background(), "o", "r", "s")
	if err != nil || state != ChecksFailed || len(runs) != 2 || runs[0] != (CheckRun{Name: "build", Status: "completed", Conclusion: "failure", URL: "u1"}) || runs[1].Status != "queued" {
		t.Fatalf("checks %q %+v %v", state, runs, err)
	}
	if s, err := c.Checks(context.Background(), "o", "r", "s"); err != nil || s != ChecksFailed {
		t.Fatalf("the summary disagrees with the runs: %q %v", s, err)
	}
	is, err := c.Issue(context.Background(), "o", "r", 4)
	if err != nil || is.Number != 4 || is.URL != "https://github.com/o/r/issues/4" || is.Comments != 7 {
		t.Fatalf("issue %+v %v", is, err)
	}
}
