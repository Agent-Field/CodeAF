package serve_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// commentRig is a solo floor watching repos, with a GitHub in this test that
// answers the comment post of the job's issue as answer says.
type commentRig struct {
	*v2Jobs
	mu     sync.Mutex
	posts  int
	answer func() (*http.Response, error)
}

func newCommentRig(t *testing.T, repos ...string) *commentRig {
	t.Helper()
	g := &commentRig{v2Jobs: newOffFloor(t, repos...)}
	g.answer = func() (*http.Response, error) {
		return commentReply(201, `{"id":4401,"html_url":"https://github.com/acme/api/issues/12#issuecomment-4401","user":{"login":"sam"},"created_at":"2026-10-16T12:00:00Z"}`), nil
	}
	g.cfg.Mode = api.FactorySolo
	g.cfg.GitHubToken = func(context.Context) string { return "profile-token" }
	g.cfg.GitHubHTTPClient = &http.Client{Transport: jobsTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/acme/api/issues/12/comments" {
			t.Errorf("unexpected GitHub call %s %s", r.Method, r.URL)
			return commentReply(500, `{}`), nil
		}
		g.mu.Lock()
		g.posts++
		answer := g.answer
		g.mu.Unlock()
		return answer()
	})}
	reopenJobs(t, g.v2Jobs)
	return g
}

func commentReply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func (g *commentRig) sent() int { g.mu.Lock(); defer g.mu.Unlock(); return g.posts }

func commentIssueJob() factory.Item {
	return factory.Item{Product: "acme", Repo: "api", Num: 12, Kind: factory.KindIssue, Origin: factory.OriginForge, Title: "retry the sync", State: factory.StateNew}
}

func (g *commentRig) comment(it factory.Item, body string) *httptest.ResponseRecorder {
	return verbRequest(g.h, "POST", fmt.Sprintf("/v1/jobs/%d/issue_comment", it.ID), body)
}

// commentFailure is a refusal's outcome, reason and status.
func commentFailure(t *testing.T, w *httptest.ResponseRecorder) (outcome, reason string) {
	t.Helper()
	var body struct {
		Outcome string `json:"outcome"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	return body.Outcome, body.Reason
}

// EVERY ANSWER OF THE ROUTE'S OWN SAYS WHETHER ANYTHING WAS POSTED: refused
// for a definitive no, unknown when GitHub's answer cannot say.
func TestIssueCommentFailuresSayWhetherAnythingWasPosted(t *testing.T) {
	for name, tc := range map[string]struct {
		answer  func() (*http.Response, error)
		job     factory.Item
		body    string
		status  int
		outcome string
		posts   int
	}{
		"github says no":  {answer: func() (*http.Response, error) { return commentReply(403, `{"message":"no"}`), nil }, status: 403, outcome: "refused", posts: 1},
		"github expired":  {answer: func() (*http.Response, error) { return commentReply(401, `{}`), nil }, status: 401, outcome: "refused", posts: 1},
		"github invalid":  {answer: func() (*http.Response, error) { return commentReply(422, `{}`), nil }, status: 422, outcome: "refused", posts: 1},
		"lost answer":     {answer: func() (*http.Response, error) { return nil, errors.New("connection reset") }, status: 502, outcome: "unknown", posts: 1},
		"github failed":   {answer: func() (*http.Response, error) { return commentReply(502, `{}`), nil }, status: 502, outcome: "unknown", posts: 1},
		"unreadable yes":  {answer: func() (*http.Response, error) { return commentReply(201, `{"id":`), nil }, status: 502, outcome: "unknown", posts: 1},
		"empty words":     {body: `{"body":"   "}`, status: 422, outcome: "refused"},
		"too many words":  {body: fmt.Sprintf(`{"body":%q}`, strings.Repeat("x", factory.IssueCommentMost+1)), status: 422, outcome: "refused"},
		"no issue to use": {job: factory.Item{Title: "typed in the app", Repo: "acme/api", State: factory.StateNew}, status: 409, outcome: "refused"},
	} {
		t.Run(name, func(t *testing.T) {
			g := newCommentRig(t, "acme/api")
			if tc.answer != nil {
				g.answer = tc.answer
			}
			job := tc.job
			if job.Title == "" {
				job = commentIssueJob()
			}
			body := tc.body
			if body == "" {
				body = `{"body":"which browser?"}`
			}
			w := g.comment(g.create(t, job), body)
			checkJSON(t, w, tc.status)
			if outcome, _ := commentFailure(t, w); outcome != tc.outcome || g.sent() != tc.posts {
				t.Fatalf("outcome %q after %d posts: %s", outcome, g.sent(), w.Body)
			}
		})
	}
	g := newCommentRig(t, "acme/api")
	w := g.comment(g.create(t, commentIssueJob()), `{"body":"which browser?"}`)
	checkJSON(t, w, 200)
	if strings.Contains(w.Body.String(), `"outcome"`) {
		t.Fatalf("a posted comment carries a failure outcome: %s", w.Body)
	}
}

// A DOUBLE CLICK POSTS ONCE: while one comment on a job is being posted, a
// second for the same job is refused before it reaches GitHub, and another
// job is not held up by it.
func TestIssueCommentInFlightGuardPostsOnce(t *testing.T) {
	g := newCommentRig(t, "acme/api")
	entered, release := make(chan struct{}), make(chan struct{})
	first := g.answer
	g.answer = func() (*http.Response, error) {
		close(entered)
		<-release
		return first()
	}
	job := g.create(t, commentIssueJob())
	done := make(chan *httptest.ResponseRecorder)
	go func() { done <- g.comment(job, `{"body":"which browser?"}`) }()
	select {
	case <-entered:
	case w := <-done:
		t.Fatalf("the first comment never reached GitHub: %d %s", w.Code, w.Body)
	}
	w := g.comment(job, `{"body":"which browser?"}`)
	checkJSON(t, w, 409)
	if outcome, reason := commentFailure(t, w); outcome != "refused" || reason != api.ReasonInFlight {
		t.Fatalf("second: %s", w.Body)
	}
	g.mu.Lock()
	g.answer = first
	g.mu.Unlock()
	// The guard is the job's: another job, even of the same issue, posts.
	if w := g.comment(g.create(t, commentIssueJob()), `{"body":"and the version?"}`); w.Code != 200 {
		t.Fatalf("another job was held up: %d %s", w.Code, w.Body)
	}
	close(release)
	if w := <-done; w.Code != 200 || g.sent() != 2 {
		t.Fatalf("first: %d %s after %d posts", w.Code, w.Body, g.sent())
	}
	// Once it has answered, the job takes a comment again.
	if w := g.comment(job, `{"body":"thanks"}`); w.Code != 200 || g.sent() != 3 {
		t.Fatalf("after: %d %s", w.Code, w.Body)
	}
}

// A REPOSITORY THAT IS OFF TAKES NO COMMENT: refused as every other door that
// works on its jobs, before GitHub is asked.
func TestIssueCommentRefusedOnARepositoryThatIsOff(t *testing.T) {
	g := newCommentRig(t, "acme/web")
	w := g.comment(g.create(t, commentIssueJob()), `{"body":"which browser?"}`)
	checkJSON(t, w, 409)
	if _, reason := commentFailure(t, w); reason != api.ReasonRepoOff || g.sent() != 0 {
		t.Fatalf("%s after %d posts", w.Body, g.sent())
	}
}
