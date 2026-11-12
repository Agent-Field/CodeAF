package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckRunLogReadsTheActionsJobAndRejectsOtherURLs(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/repos/acme/api/actions/jobs/2/logs" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing GitHub authorization")
		}
		w.Write([]byte("test failed: assertion"))
	}))
	defer srv.Close()
	c := testClient(srv.URL, "test-token")
	log, err := c.CheckRunLog(context.Background(), "acme", "api", CheckRun{URL: "https://github.com/acme/api/actions/runs/1/job/2"})
	if err != nil || log != "test failed: assertion" {
		t.Fatalf("log=%q err=%v", log, err)
	}
	for _, u := range []string{"https://example.com/acme/api/actions/runs/1/job/2", "https://github.com/other/api/actions/runs/1/job/2", "https://github.com/acme/api/actions/runs/1/job/nope"} {
		if _, err := c.CheckRunLog(context.Background(), "acme", "api", CheckRun{URL: u}); err == nil {
			t.Fatal("accepted noncanonical job log URL")
		}
	}
	if calls != 1 {
		t.Fatal("fetched an untrusted URL")
	}
}
