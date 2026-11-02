package main

import (
	"net/http"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// THE DESK CAN SWITCH ON WHETHER A COMMENT WAS POSTED: factory.issue_comment's
// refusals carry the factory's outcome in the typed detail (refused: nothing
// was posted; unknown: read the issue), a double click is refused as in
// flight, and a repository that is off takes no comment. Against the real
// factory server and store, with GitHub in this test.
func TestAppFactoryIssueCommentOutcomesGuardAndRepoOff(t *testing.T) {
	f := startActionFactory(t)
	var mu sync.Mutex
	status := 0
	entered, release := make(chan struct{}), make(chan struct{})
	block := false
	posts := 0
	f.github.mu.Lock()
	f.github.rest = func(r *http.Request) (int, string, bool) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/acme/api/issues/12/comments" {
			return 0, "", false
		}
		mu.Lock()
		posts++
		code, wait := status, block
		mu.Unlock()
		if wait {
			close(entered)
			<-release
		}
		if code != 0 {
			return code, `{"message":"upstream words"}`, true
		}
		return http.StatusCreated, `{"id":4401,"html_url":"https://github.com/acme/api/issues/12#issuecomment-4401","user":{"login":"alice"},"created_at":"2026-10-16T12:00:00Z"}`, true
	}
	f.github.mu.Unlock()
	set := func(code int, wait bool) { mu.Lock(); status, block = code, wait; mu.Unlock() }
	sent := func() int { mu.Lock(); defer mu.Unlock(); return posts }
	alice := signedIn(t, f, 1, "alice")
	bob := signedIn(t, f, 2, "bob")
	job := f.create(t, issueJob())
	params := func(p *person) map[string]any {
		return map[string]any{"factory": p.conn.ID, "job": job.ID, "body": "which browser?"}
	}

	t.Run("a definitive no says nothing was posted", func(t *testing.T) {
		set(http.StatusForbidden, false)
		_, detail := alice.refused(t, "factory.issue_comment", params(alice), appwire.CodeForbidden)
		if detail == nil || detail.Outcome != api.CommentRefused {
			t.Fatalf("detail %+v", detail)
		}
		_, detail = alice.refused(t, "factory.issue_comment", map[string]any{"factory": alice.conn.ID, "job": job.ID, "body": "   "}, appwire.CodeBadParams)
		if detail != nil && detail.Outcome != "" {
			t.Fatalf("a refusal on the wire before the factory was asked carries %+v", detail)
		}
	})

	t.Run("an answer GitHub cannot give says look on GitHub", func(t *testing.T) {
		set(http.StatusBadGateway, false)
		_, detail := alice.refused(t, "factory.issue_comment", params(alice), appwire.CodeUncertain)
		if detail == nil || detail.Outcome != api.CommentUnknown {
			t.Fatalf("detail %+v", detail)
		}
	})

	t.Run("a double click posts once", func(t *testing.T) {
		set(0, true)
		before := sent()
		first := make(chan string, 1)
		go func() {
			_, code, _ := alice.w.call("factory.issue_comment", params(alice))
			first <- code
		}()
		<-entered
		_, detail := bob.refused(t, "factory.issue_comment", params(bob), appwire.CodeConflict)
		if detail == nil || detail.Reason != api.ReasonInFlight || detail.Outcome != api.CommentRefused {
			t.Fatalf("detail %+v", detail)
		}
		set(0, false)
		close(release)
		if code := <-first; code != "" || sent() != before+1 {
			t.Fatalf("first answered %q after %d posts", code, sent()-before)
		}
	})

	t.Run("a repository that is off takes no comment", func(t *testing.T) {
		if err := f.st.SetRepos([]string{"acme/web"}); err != nil {
			t.Fatal(err)
		}
		before := sent()
		_, detail := alice.refused(t, "factory.issue_comment", params(alice), appwire.CodeConflict)
		if detail == nil || detail.Reason != api.ReasonRepoOff || sent() != before {
			t.Fatalf("detail %+v after %d posts", detail, sent()-before)
		}
	})
}
