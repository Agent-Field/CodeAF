package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// The poll re-reads pull requests the factory opened. A merged one and a
// closed one are recorded once; an open one keeps the job shipped and says
// so on the pull request, with its reviews and checks.
func TestPollFollowsPullRequestsTheFactoryOpened(t *testing.T) {
	when := time.Date(2026, 10, 7, 9, 41, 0, 0, time.UTC)
	m := newMemAPI(when)
	m.followed = map[string]forge.Pull{
		"acme/api#40": {Number: 40, URL: "https://github.com/acme/api/pull/40", State: "closed", Merged: true, MergedBy: "mei", MergedAt: when, HeadSHA: "sha40", Base: "main"},
		"acme/api#41": {Number: 41, URL: "https://github.com/acme/api/pull/41", State: "closed", HeadSHA: "sha41", Base: "dev"},
		"acme/api#42": {Number: 42, URL: "https://github.com/acme/api/pull/42", State: "open", Draft: true, HeadSHA: "sha42", Base: "main"},
	}
	m.reviews = map[string][]forge.Review{
		"acme/api#40": {{User: "mei", State: "APPROVED", At: when.Add(-time.Hour)}},
		"acme/api#42": {{User: "mei", State: "CHANGES_REQUESTED", At: when}, {User: "mei", State: "COMMENTED", At: when.Add(-time.Hour)}},
	}
	m.required = map[string]int{"acme/api|main": 1}
	m.runs = map[string][]forge.CheckRun{
		"sha40": {{Name: "test", Status: "completed", Conclusion: "success"}},
		"sha42": {
			{Name: "test", Status: "completed", Conclusion: "success"},
			{Name: "test2", Status: "completed", Conclusion: "success"},
			{Name: "lint", Status: "completed", Conclusion: "failure"},
			{Name: "build", Status: "in_progress"},
		},
	}
	st := openStore(t)
	if err := st.SetRepos([]string{"acme/api"}); err != nil {
		t.Fatal(err)
	}
	mk := func(issue, pr int) factory.Item {
		t.Helper()
		it, err := st.Create(factory.Item{
			Repo: "api", Product: "acme", Num: issue, Kind: factory.KindIssue, Title: "work",
			State: factory.StateShipped, Origin: factory.OriginForge,
			PullRequest: &factory.PullRequest{Number: pr, URL: fmt.Sprintf("https://github.com/acme/api/pull/%d", pr), State: factory.PROpen},
		})
		if err != nil {
			t.Fatal(err)
		}
		return it
	}
	merged := mk(12, 40)
	closed := mk(13, 41)
	opened := mk(14, 42)
	if _, err := st.Create(factory.Item{Repo: "api", Product: "acme", Title: "comment only", State: factory.StateShipped, Publish: "comment"}); err != nil {
		t.Fatal(err)
	}
	src := New(m, nil, nil)
	poll := func() {
		t.Helper()
		if _, err := PollOnce(context.Background(), src, st, "", func() time.Time { return when }); err != nil {
			t.Fatal(err)
		}
	}
	poll()

	got := func(id int) factory.Item {
		t.Helper()
		it, err := st.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		return it
	}
	mrg := got(merged.ID)
	if mrg.State != factory.StateShipped || mrg.PullRequest == nil || mrg.PullRequest.State != factory.PRMerged ||
		mrg.PullRequest.MergedBy != "mei" || !mrg.PullRequest.MergedAt.Equal(when) ||
		mrg.PullRequest.ReviewDecision != "approved" || mrg.PullRequest.Required != 1 ||
		mrg.PullRequest.CI != "ci ✓" || mrg.PullRequest.Checks != (factory.PRChecks{Passed: 1}) ||
		len(mrg.PullRequest.Reviews) != 1 || mrg.PullRequest.Reviews[0].By != "mei" || mrg.PullRequest.Reviews[0].State != "approved" {
		t.Fatalf("merged pull request: state %s pr %+v", mrg.State, mrg.PullRequest)
	}
	cld := got(closed.ID)
	if cld.State != factory.StateShipped || cld.PullRequest == nil || cld.PullRequest.State != factory.PRClosed || cld.PullRequest.ReviewDecision != "" {
		t.Fatalf("closed pull request: state %s pr %+v", cld.State, cld.PullRequest)
	}
	opn := got(opened.ID)
	if opn.State != factory.StateShipped || opn.PullRequest == nil || opn.PullRequest.State != factory.PROpen || !opn.PullRequest.Draft ||
		opn.PullRequest.ReviewDecision != "changes_requested" || opn.PullRequest.CI != "ci ✕" ||
		opn.PullRequest.Checks != (factory.PRChecks{Passed: 2, Failed: 1, Pending: 1}) {
		t.Fatalf("open pull request: state %s pr %+v", opn.State, opn.PullRequest)
	}

	rec, err := record.Open(st, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	events, err := rec.Since(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]record.Event{}
	for _, ev := range events {
		seen[ev.Kind] = ev
	}
	if len(events) != 2 || seen[record.PRMerged].Job != merged.ID || seen[record.PRMerged].Handle != "mei" || seen[record.PRMerged].Detail != "pr · https://github.com/acme/api/pull/40" ||
		seen[record.PRClosed].Job != closed.ID || seen[record.PRClosed].Detail != "pr · https://github.com/acme/api/pull/41" {
		t.Fatalf("events %+v", events)
	}
	poll()
	again, err := rec.Since(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 2 {
		t.Fatalf("a second poll recorded %+v", again)
	}
}

// The real client reads merge, reviews, required reviews and check runs off
// the forge's JSON. The server is the fake forge.
func TestFollowPRReadsTheForge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/acme/api/pulls/40":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": 40, "html_url": "https://github.com/acme/api/pull/40", "draft": false,
				"state": "closed", "merged": true, "merged_at": "2026-10-11T09:41:00Z",
				"merged_by": map[string]any{"login": "mei"},
				"user":      map[string]any{"login": "santosh"},
				"head":      map[string]any{"sha": "abc", "ref": "factory/12-fix"},
				"base":      map[string]any{"ref": "main"},
			})
		case "/repos/acme/api/pulls/40/reviews":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"user": map[string]any{"login": "mei"}, "state": "APPROVED", "submitted_at": "2026-10-11T09:00:00Z"},
			})
		case "/repos/acme/api/branches/main/protection":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"required_pull_request_reviews": map[string]any{"required_approving_review_count": 1},
			})
		case "/repos/acme/api/commits/abc/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "failure", "statuses": []any{}})
		case "/repos/acme/api/commits/abc/check-runs":
			_ = json.NewEncoder(w).Encode(map[string]any{"check_runs": []map[string]any{
				{"name": "test", "status": "completed", "conclusion": "success"},
				{"name": "lint", "status": "completed", "conclusion": "failure"},
			}})
		default:
			http.Error(w, r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	api := forge.NewClient("test-token").WithBaseURL(srv.URL).WithSleep(func(context.Context, time.Duration) error { return nil })
	got, err := New(api, nil, nil).FollowPR(context.Background(), "acme", "api", 40)
	if err != nil {
		t.Fatal(err)
	}
	wantAt := time.Date(2026, 10, 11, 9, 41, 0, 0, time.UTC)
	if got.State != factory.PRMerged || got.MergedBy != "mei" || !got.MergedAt.Equal(wantAt) || got.Number != 40 ||
		got.ReviewDecision != "approved" || got.Required != 1 || got.CI != "ci ✕" ||
		got.Checks != (factory.PRChecks{Passed: 1, Failed: 1}) ||
		len(got.Reviews) != 1 || got.Reviews[0].By != "mei" || got.Reviews[0].State != "approved" {
		t.Fatalf("followed %+v", got)
	}
	// The merge gate's facts: the head it would merge, and that checks and
	// reviews were read. GitHub left mergeable out, which is "not yet known".
	if got.HeadSHA != "abc" || !got.ChecksRead || !got.ReviewsRead || got.Mergeable != "" {
		t.Fatalf("gate facts %+v", got)
	}
}

func TestMergeableWordKeepsUnknownApartFromNo(t *testing.T) {
	yes, no := true, false
	if mergeableWord(nil) != "" || mergeableWord(&yes) != factory.MergeableYes || mergeableWord(&no) != factory.MergeableNo {
		t.Fatal("mergeable words")
	}
}
