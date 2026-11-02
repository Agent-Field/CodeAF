package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
)

// A COMMENT ON THE JOB'S ISSUE, AGAINST THE REAL SERVER AND STORE, with a
// GitHub in this test: factory.issue_comment posts the person's own words as
// the person, the job's next read carries the comment, and the poll's read of
// it hands it to the job's manager.

// issueCommentGitHub answers POST /repos/acme/api/issues/12/comments with
// status, and keeps what each post carried.
type issueCommentGitHub struct {
	mu     sync.Mutex
	status int
	posts  []struct{ auth, body string }
}

func (g *issueCommentGitHub) answer(r *http.Request) (int, string, bool) {
	if r.Method != http.MethodPost || r.URL.String() != "https://api.github.com/repos/acme/api/issues/12/comments" {
		return 0, "", false
	}
	raw, _ := io.ReadAll(r.Body)
	var in struct {
		Body string `json:"body"`
	}
	_ = json.Unmarshal(raw, &in)
	g.mu.Lock()
	defer g.mu.Unlock()
	g.posts = append(g.posts, struct{ auth, body string }{r.Header.Get("Authorization"), in.Body})
	if g.status != 0 && g.status != http.StatusCreated {
		return g.status, `{"message":"Resource not accessible by integration"}`, true
	}
	out, _ := json.Marshal(map[string]any{"id": 4401, "html_url": "https://github.com/acme/api/issues/12#issuecomment-4401",
		"body": in.Body, "user": map[string]any{"login": "alice"}, "created_at": "2026-10-16T12:00:00Z"})
	return http.StatusCreated, string(out), true
}

func (g *issueCommentGitHub) sent() int { g.mu.Lock(); defer g.mu.Unlock(); return len(g.posts) }

// issueJob is a running job from issue acme/api#12 whose comments were read.
func issueJob() factory.Item {
	return factory.Item{Product: "acme", Repo: "api", Num: 12, Kind: factory.KindIssue, Origin: factory.OriginForge, Title: "retry the sync",
		State: factory.StateRunning, Assignee: "alice", Comments: []factory.Comment{}, CommentsSeen: []string{},
		Stream: &factory.Stream{Phases: []factory.Phase{{Name: "plan", Kind: factory.StageChat, State: factory.PhaseRunning}}}}
}

func TestAppFactoryIssueCommentAgainstTheRealServer(t *testing.T) {
	f := startActionFactory(t)
	gh := &issueCommentGitHub{}
	f.github.mu.Lock()
	f.github.rest = gh.answer
	f.github.mu.Unlock()
	alice := signedIn(t, f, 1, "alice")
	bob := signedIn(t, f, 2, "bob")

	alice.w.mu.Lock()
	hello := alice.w.lines[0]
	alice.w.mu.Unlock()
	for _, feature := range []string{appwire.FeatureFactoryIssueComment, appwire.FeatureFactoryIssueCommentReads} {
		if !strings.Contains(hello, feature) {
			t.Fatalf("hello does not announce %s: %s", feature, hello)
		}
	}

	job := f.create(t, issueJob())
	t.Run("a person posts as themselves and the job's next read carries it", func(t *testing.T) {
		var got appwire.FactoryIssueCommented
		alice.w.must("factory.issue_comment", map[string]any{"factory": alice.conn.ID, "job": job.ID, "body": "  please cover the batch path too  "}, &got)
		c := got.Comment
		if got.Job != job.ID || c.ID != "4401" || c.URL != "https://github.com/acme/api/issues/12#issuecomment-4401" || c.Author != "alice" || c.Body != "please cover the batch path too" || c.At.IsZero() {
			t.Fatalf("posted %+v", got)
		}
		if gh.sent() != 1 || gh.posts[0].auth != "Bearer github-token-secret-1" || gh.posts[0].body != "please cover the batch path too" {
			t.Fatalf("GitHub got %+v, want alice's own token and her words alone", gh.posts)
		}
		read := alice.job(t, job.ID)
		if len(read.Desk.Comments) != 1 || read.Desk.Comments[0].ID != "4401" {
			t.Fatalf("the job's read does not carry the comment: %+v", read.Desk.Comments)
		}
		if len(read.Desk.CommentReads) != 0 {
			t.Fatalf("the manager handled a comment before the poll read its author's standing: %+v", read.Desk.CommentReads)
		}

		// The poll reads it back with its author's standing, and the manager
		// asks about it once.
		polled := []factory.Comment{{ID: "4401", Author: "alice", Body: c.Body, At: c.At, URL: c.URL, Tier: factory.TierCollab}}
		in := issueJob()
		in.Comments = polled
		for range 2 {
			if err := factorygithub.Merge(f.st, factorygithub.Name, []factory.Item{in}); err != nil {
				t.Fatal(err)
			}
		}
		read = bob.job(t, job.ID)
		if len(read.Desk.CommentReads) != 1 {
			t.Fatalf("comment reads %+v", read.Desk.CommentReads)
		}
		r := read.Desk.CommentReads[0]
		if r.CommentID != "4401" || r.Outcome != factory.CommentAsked || r.Line == "" || r.At.IsZero() {
			t.Fatalf("the read %+v", r)
		}
		asked := false
		for _, m := range read.Desk.Thread {
			asked = asked || (m.From == "manager" && strings.Contains(m.Text, "@alice commented on the issue"))
		}
		if !asked {
			t.Fatalf("the manager conversation does not ask about it: %+v", read.Desk.Thread)
		}
	})

	t.Run("a sign-in GitHub will not let comment is refused and nothing is kept", func(t *testing.T) {
		gh.mu.Lock()
		gh.status = http.StatusForbidden
		gh.mu.Unlock()
		defer func() { gh.mu.Lock(); gh.status = 0; gh.mu.Unlock() }()
		before := f.snapshot(t, job.ID)
		msg, _ := bob.refused(t, "factory.issue_comment", map[string]any{"factory": bob.conn.ID, "job": job.ID, "body": "me too"}, appwire.CodeForbidden)
		if !strings.Contains(msg, "can't comment") {
			t.Fatalf("refusal %q", msg)
		}
		if f.snapshot(t, job.ID) != before {
			t.Fatal("a refused comment changed the job")
		}
	})

	t.Run("words the factory cannot post are refused before anything is sent", func(t *testing.T) {
		sent := gh.sent()
		for _, body := range []string{"   ", strings.Repeat("x", factory.IssueCommentMost+1)} {
			alice.refused(t, "factory.issue_comment", map[string]any{"factory": alice.conn.ID, "job": job.ID, "body": body}, appwire.CodeBadParams)
		}
		if gh.sent() != sent {
			t.Fatal("a refused comment reached GitHub")
		}
	})

	t.Run("a job with no GitHub issue takes no comment", func(t *testing.T) {
		local := f.create(t, factory.Item{Title: "typed in the app", State: factory.StateNew})
		sent := gh.sent()
		alice.refused(t, "factory.issue_comment", map[string]any{"factory": alice.conn.ID, "job": local.ID, "body": "hello"}, appwire.CodeConflict)
		if gh.sent() != sent {
			t.Fatal("a comment for a job with no issue reached GitHub")
		}
	})

	t.Run("a factory older than the route says so and posts nothing", func(t *testing.T) {
		target, _ := url.Parse(f.URL)
		proxy := httputil.NewSingleHostReverseProxy(target)
		old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/issue_comment") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":"factory endpoint not found","reason":"not_found"}`))
				return
			}
			proxy.ServeHTTP(w, r)
		}))
		defer old.Close()
		carol := signedInAt(t, f, old.URL, 3, "carol")
		sent := gh.sent()
		msg, _ := carol.refused(t, "factory.issue_comment", map[string]any{"factory": carol.conn.ID, "job": job.ID, "body": "hello"}, appwire.CodeConflict)
		if !strings.Contains(msg, "doesn't support") || gh.sent() != sent {
			t.Fatalf("an older factory answered %q", msg)
		}
	})
}
