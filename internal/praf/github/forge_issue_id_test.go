package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// SYNTHETIC fixture shaped by GitHub's documented issues response. The list
// row's numeric id is the issue's identity: the factory keeps it on the job
// and refuses a later read whose id differs, so a list that dropped it would
// stop every known job from learning a new label, title or comment.
func TestListIssuesKeepsEachIssuesID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":6090338639,"number":2023,"title":"a","user":{"login":"x"},"labels":[{"name":"factory-testing"}]}]`))
	}))
	defer srv.Close()
	for name, list := range map[string]func(context.Context, string, string, time.Time, string) ([]Issue, string, error){
		"open":    testClient(srv.URL, "tok").ListIssues,
		"changes": testClient(srv.URL, "tok").ListIssueChanges,
	} {
		issues, _, err := list(context.Background(), "o", "r", time.Time{}, "")
		if err != nil || len(issues) != 1 {
			t.Fatalf("%s: read %+v %v", name, issues, err)
		}
		if issues[0].ID != 6090338639 {
			t.Fatalf("%s: id = %d, want 6090338639", name, issues[0].ID)
		}
	}
}
