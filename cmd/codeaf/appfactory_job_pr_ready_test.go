package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func TestAppFactoryJobPRReady(t *testing.T) {
	f := startActionFactory(t)
	alice := signedIn(t, f, 1, "alice")
	bob := signedIn(t, f, 2, "bob")
	var mu sync.Mutex
	calls, status := 0, http.StatusOK
	remoteReady, graphRefusal := false, false
	f.github.mu.Lock()
	f.github.rest = func(r *http.Request) (int, string, bool) {
		if r.URL.Path != "/repos/acme/api/pulls/42" && r.URL.Path != "/graphql" {
			return 0, "", false
		}
		if r.Header.Get("Authorization") != "Bearer github-token-secret-1" {
			t.Error("ready did not use the caller's credential")
		}
		if r.Method == http.MethodGet {
			mu.Lock()
			draft := !remoteReady
			mu.Unlock()
			return 200, fmt.Sprintf(`{"node_id":"PR_42","state":"open","draft":%t,"head":{"sha":%q}}`, draft, actionHead), true
		}
		var body struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if !strings.Contains(body.Query, "markPullRequestReadyForReview") {
			t.Error("unexpected GitHub mutation")
		}
		mu.Lock()
		defer mu.Unlock()
		calls++
		if graphRefusal {
			return 200, `{"errors":[{"type":"FORBIDDEN","message":"not permitted"}]}`, true
		}
		return status, fmt.Sprintf(`{"data":{"markPullRequestReadyForReview":{"pullRequest":{"isDraft":false,"headRefOid":%q}}}}`, actionHead), true
	}
	f.github.mu.Unlock()
	sent := func() int { mu.Lock(); defer mu.Unlock(); return calls }
	draft := func() factory.Item {
		return factory.Item{Repo: "acme/api", State: factory.StateLanded, Assignee: "alice", Publish: "pr", HeadCommit: actionHead,
			PullRequest: &factory.PullRequest{Number: 42, URL: "https://github.com/acme/api/pull/42", State: factory.PROpen, Draft: true, HeadSHA: actionHead, Mergeable: factory.MergeableYes, ChecksRead: true, ReviewsRead: true}}
	}
	canReady := func(j api.Job) bool {
		raw, _ := json.Marshal(j.Can)
		var can map[string]bool
		_ = json.Unmarshal(raw, &can)
		return can["pr_ready"]
	}
	t.Run("hello", func(t *testing.T) {
		if !strings.Contains(alice.w.hello, `"factory.job_pr_ready"`) {
			t.Fatal("hello missing factory.job_pr_ready")
		}
	})
	t.Run("ready immediately removes the draft merge block", func(t *testing.T) {
		job := f.create(t, draft())
		before := alice.job(t, job.ID)
		if !canReady(before) {
			t.Error("draft PR does not offer can.pr_ready")
		}
		if before.Merge == nil || !strings.HasPrefix(before.Merge.Blocked, "the pull request is a draft") {
			t.Fatalf("draft merge gate: %+v", before.Merge)
		}
		n := sent()
		got := alice.act(t, "factory.job_pr_ready", job.ID, nil)
		if got.Action != "pr_ready" || got.Current == nil || got.Current.PR == nil || got.Current.PR.Draft || canReady(*got.Current) {
			t.Fatalf("ready receipt: %+v", got)
		}
		after := alice.job(t, job.ID)
		if after.PR.Draft || canReady(after) || !after.Can.Merge {
			t.Fatalf("ready read: %+v", after)
		}
		if sent() != n+1 || f.routePOSTs("/pr-ready") != 1 {
			t.Fatal("ready must send one typed POST and one GitHub mutation")
		}
		if f.count("pr_ready") != 1 {
			t.Fatal("missing pr_ready record event")
		}
		alice.refused(t, "factory.job_pr_ready", alice.params(t, job.ID, nil), appwire.CodeConflict)
	})
	t.Run("another person is forbidden", func(t *testing.T) {
		job := f.create(t, draft())
		n := sent()
		if canReady(bob.job(t, job.ID)) {
			t.Error("another person is offered ready")
		}
		bob.refused(t, "factory.job_pr_ready", bob.params(t, job.ID, nil), appwire.CodeForbidden)
		if sent() != n {
			t.Fatal("forbidden reached GitHub")
		}
	})
	t.Run("no PR and already ready conflict", func(t *testing.T) {
		for _, hasPR := range []bool{false, true} {
			it := draft()
			if hasPR {
				it.PullRequest.Draft = false
			} else {
				it.PullRequest = nil
			}
			job := f.create(t, it)
			n := sent()
			alice.refused(t, "factory.job_pr_ready", alice.params(t, job.ID, nil), appwire.CodeConflict)
			if sent() != n {
				t.Fatal("conflict reached GitHub")
			}
		}
	})
	t.Run("stale page never reaches GitHub", func(t *testing.T) {
		job := f.create(t, draft())
		params := alice.params(t, job.ID, nil)
		n := sent()
		alice.act(t, "factory.job_set_priority", job.ID, map[string]any{"priority": "urgent"})
		alice.refused(t, "factory.job_pr_ready", params, appwire.CodeStale)
		if sent() != n {
			t.Fatal("stale reached GitHub")
		}
	})
	t.Run("already ready on GitHub conflicts without a mutation", func(t *testing.T) {
		job := f.create(t, draft())
		n := sent()
		mu.Lock()
		remoteReady = true
		mu.Unlock()
		defer func() { mu.Lock(); remoteReady = false; mu.Unlock() }()
		alice.refused(t, "factory.job_pr_ready", alice.params(t, job.ID, nil), appwire.CodeConflict)
		if sent() != n {
			t.Fatal("already ready reached GitHub mutation")
		}
	})
	t.Run("GraphQL permission refusal is forbidden and never retried", func(t *testing.T) {
		job := f.create(t, draft())
		n := sent()
		mu.Lock()
		graphRefusal = true
		mu.Unlock()
		defer func() { mu.Lock(); graphRefusal = false; mu.Unlock() }()
		alice.refused(t, "factory.job_pr_ready", alice.params(t, job.ID, nil), appwire.CodeForbidden)
		if sent() != n+1 {
			t.Fatal("GraphQL refusal was retried")
		}
	})
	t.Run("uncertain is never retried", func(t *testing.T) {
		job := f.create(t, draft())
		n := sent()
		mu.Lock()
		status = http.StatusBadGateway
		mu.Unlock()
		alice.refused(t, "factory.job_pr_ready", alice.params(t, job.ID, nil), appwire.CodeUncertain)
		if sent() != n+1 {
			t.Fatal("uncertain ready was retried")
		}
		if !alice.job(t, job.ID).PR.Draft {
			t.Fatal("uncertain claimed success")
		}
	})
}
