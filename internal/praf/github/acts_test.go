package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// An edit sends only the fields it names, so a change of base never rewrites
// the title or the body.
func TestEditPullRequestSendsOnlyTheFieldsGiven(t *testing.T) {
	var got map[string]any
	c := mergeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/repos/acme/api/pulls/7" {
			http.Error(w, r.Method+" "+r.URL.Path, http.StatusNotFound)
			return
		}
		got = map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "title": "Keep the ledger balanced", "state": "open",
			"base": map[string]any{"ref": "dev", "sha": "b4se"}, "head": map[string]any{"ref": "factory/12", "sha": "h34d"}})
	})
	base := "dev"
	p, err := c.EditPullRequest(context.Background(), "acme", "api", 7, PullEdit{Base: &base})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["base"] != "dev" || p.Base != "dev" || p.BaseSHA != "b4se" {
		t.Fatalf("sent %v, read back %+v", got, p)
	}
	title, body := "A better title", "What changed and why."
	if _, err = c.EditPullRequest(context.Background(), "acme", "api", 7, PullEdit{Title: &title, Body: &body}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["title"] != title || got["body"] != body {
		t.Fatalf("sent %v", got)
	}
	if _, err = c.EditPullRequest(context.Background(), "acme", "api", 7, PullEdit{}); err == nil {
		t.Fatal("an edit of nothing was sent")
	}
}

// GitHub refuses a base that is not there with 422; the caller is given its words.
func TestEditPullRequestKeepsGitHubsRefusal(t *testing.T) {
	c := mergeServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"Validation Failed","errors":[{"resource":"PullRequest","field":"base","code":"invalid"}]}`))
	})
	base := "nope"
	_, err := c.EditPullRequest(context.Background(), "acme", "api", 7, PullEdit{Base: &base})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("err = %v", err)
	}
}

func TestReplyToReviewCommentPostsInTheThread(t *testing.T) {
	var got map[string]any
	c := mergeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/acme/api/pulls/7/comments/501/replies" {
			http.Error(w, r.Method+" "+r.URL.Path, http.StatusNotFound)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 502, "html_url": "https://github.com/acme/api/pull/7#discussion_r502"})
	})
	p, err := c.ReplyToReviewComment(context.Background(), "acme", "api", 7, 501, "Renamed it.")
	if err != nil {
		t.Fatal(err)
	}
	if got["body"] != "Renamed it." || p.ID != "502" || !strings.HasSuffix(p.URL, "discussion_r502") {
		t.Fatalf("sent %v, posted %+v", got, p)
	}
	if _, err = c.ReplyToReviewComment(context.Background(), "acme", "api", 7, 501, "  "); err == nil {
		t.Fatal("an empty reply was sent")
	}
}

// A REPLY AND A COMMENT ARE SENT ONCE: a lost answer may hide one that was
// posted, and a second try would say it twice in the person's name.
func TestRepliesAndCommentsAreNeverRetried(t *testing.T) {
	var calls atomic.Int32
	c := mergeServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	})
	if _, err := c.ReplyToReviewComment(context.Background(), "acme", "api", 7, 501, "Renamed it."); err == nil {
		t.Fatal("a 502 posted a reply")
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("reply sent %d times", n)
	}
	if _, err := c.CommentOnce(context.Background(), "acme", "api", 7, "Done."); err == nil {
		t.Fatal("a 502 posted a comment")
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("comment sent %d times", n-1)
	}
}

func TestCommentOncePostsOnThePullRequestsConversation(t *testing.T) {
	var got map[string]any
	c := mergeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/acme/api/issues/7/comments" {
			http.Error(w, r.Method+" "+r.URL.Path, http.StatusNotFound)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 9, "html_url": "https://github.com/acme/api/pull/7#issuecomment-9"})
	})
	p, err := c.CommentOnce(context.Background(), "acme", "api", 7, "Ready for another look.")
	if err != nil || got["body"] != "Ready for another look." || p.ID != "9" || p.Number != 7 {
		t.Fatalf("sent %v, posted %+v, err %v", got, p, err)
	}
}

func TestResolveReviewThreadNamesTheThread(t *testing.T) {
	var got struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	answer := `{"data":{"resolveReviewThread":{"thread":{"id":"PRRT_1","isResolved":true}}}}`
	c := mergeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/graphql" {
			http.Error(w, r.Method+" "+r.URL.Path, http.StatusNotFound)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = io.WriteString(w, answer)
	})
	if err := c.ResolveReviewThread(context.Background(), "acme", "api", "PRRT_1"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Query, "resolveReviewThread") || got.Variables["id"] != "PRRT_1" {
		t.Fatalf("sent %+v", got)
	}
	// A thread that is gone, and a sign-in that may not resolve it, are each
	// said as what they are.
	answer = `{"data":{"resolveReviewThread":null},"errors":[{"type":"NOT_FOUND","message":"Could not resolve to a node with the global id of 'PRRT_1'"}]}`
	var refusal *ThreadRefusal
	if err := c.ResolveReviewThread(context.Background(), "acme", "api", "PRRT_1"); !errors.As(err, &refusal) || !refusal.Gone || refusal.Permission {
		t.Fatalf("a missing thread: %v", err)
	}
	answer = `{"data":{"resolveReviewThread":null},"errors":[{"type":"FORBIDDEN","message":"Resource not accessible by integration"}]}`
	if err := c.ResolveReviewThread(context.Background(), "acme", "api", "PRRT_1"); !errors.As(err, &refusal) || !refusal.Permission {
		t.Fatalf("a refused sign-in: %v", err)
	}
	answer = `{"data":{"resolveReviewThread":{"thread":{"id":"PRRT_1","isResolved":false}}}}`
	if err := c.ResolveReviewThread(context.Background(), "acme", "api", "PRRT_1"); err == nil {
		t.Fatal("a thread GitHub left open read as resolved")
	}
	if err := c.ResolveReviewThread(context.Background(), "acme", "api", " "); err == nil {
		t.Fatal("resolved a thread with no id")
	}
}

func TestRequestReviewersAsksEachByLogin(t *testing.T) {
	var got map[string][]string
	c := mergeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/acme/api/pulls/7/requested_reviewers" {
			http.Error(w, r.Method+" "+r.URL.Path, http.StatusNotFound)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"number":7}`)
	})
	if err := c.RequestReviewers(context.Background(), "acme", "api", 7, []string{"bob", "carol"}); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(got["reviewers"]) != "[bob carol]" {
		t.Fatalf("sent %v", got)
	}
	if err := c.RequestReviewers(context.Background(), "acme", "api", 7, nil); err == nil {
		t.Fatal("asked nobody for a review")
	}
}

// rerunServer is a head with one failed and one passed Actions workflow run,
// one failed check of another app, and one failing commit status.
func rerunServer(t *testing.T, refuse map[string]int) (*client, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var posts []string
	c := mergeServer(t, func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/repos/acme/api")
		if r.Method == http.MethodPost {
			mu.Lock()
			posts = append(posts, path)
			mu.Unlock()
			if status := refuse[path]; status != 0 {
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"message":"Resource not accessible by personal access token"}`)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{}`)
			return
		}
		if status := refuse[path]; status != 0 {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"message":"Not Found"}`)
			return
		}
		switch path {
		case "/actions/runs":
			if r.URL.Query().Get("head_sha") != "h34d" {
				t.Errorf("workflow runs read for %q", r.URL.Query().Get("head_sha"))
			}
			_, _ = io.WriteString(w, `{"total_count":2,"workflow_runs":[
				{"id":900,"name":"CI","status":"completed","conclusion":"failure","check_suite_id":41},
				{"id":901,"name":"Docs","status":"completed","conclusion":"success","check_suite_id":42}]}`)
		case "/commits/h34d/check-runs":
			_, _ = io.WriteString(w, `{"total_count":4,"check_runs":[
				{"name":"test","status":"completed","conclusion":"failure","app":{"slug":"github-actions"},"check_suite":{"id":41}},
				{"name":"docs","status":"completed","conclusion":"success","app":{"slug":"github-actions"},"check_suite":{"id":42}},
				{"name":"scan","status":"completed","conclusion":"failure","app":{"slug":"scanner"},"check_suite":{"id":77}},
				{"name":"scan (deep)","status":"completed","conclusion":"timed_out","app":{"slug":"scanner"},"check_suite":{"id":77}}]}`)
		case "/commits/h34d/status":
			_, _ = io.WriteString(w, `{"state":"failure","total_count":2,"statuses":[{"context":"license/cla","state":"failure"},{"context":"deploy/preview","state":"success"}]}`)
		default:
			http.Error(w, r.Method+" "+path, http.StatusNotFound)
		}
	})
	return c, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), posts...)
	}
}

// Each failed workflow run has its failed jobs run again, each other app's
// failed suite is requested once however many of its checks failed, and a
// failing commit status is said as one nobody here can run again.
func TestRerunFailedChecksRunsWhatItCanAndSaysTheRest(t *testing.T) {
	c, posts := rerunServer(t, nil)
	got, err := c.RerunFailedChecks(context.Background(), "acme", "api", "h34d")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(posts()) != "[/actions/runs/900/rerun-failed-jobs /check-suites/77/rerequest]" {
		t.Fatalf("asked GitHub for %v", posts())
	}
	if fmt.Sprint(got.Ran) != "[CI scan, scan (deep)]" {
		t.Fatalf("ran %v", got.Ran)
	}
	if len(got.Skipped) != 1 || got.Skipped[0].Name != "license/cla" || !strings.Contains(got.Skipped[0].Why, "commit status") {
		t.Fatalf("skipped %+v", got.Skipped)
	}
}

// A check GitHub will not run again for this sign-in is said in the result;
// the checks that can be are still run again.
func TestRerunFailedChecksSaysACheckItMayNotRunAgain(t *testing.T) {
	c, posts := rerunServer(t, map[string]int{"/check-suites/77/rerequest": http.StatusForbidden})
	got, err := c.RerunFailedChecks(context.Background(), "acme", "api", "h34d")
	if err != nil {
		t.Fatal(err)
	}
	if len(posts()) != 2 || fmt.Sprint(got.Ran) != "[CI]" {
		t.Fatalf("asked %v, ran %v", posts(), got.Ran)
	}
	if len(got.Skipped) != 2 || got.Skipped[0].Name != "scan, scan (deep)" || !strings.Contains(got.Skipped[0].Why, "may not run it again") {
		t.Fatalf("skipped %+v", got.Skipped)
	}
}

// A sign-in that may not read the workflow runs still has the other apps'
// checks run again, and the Actions check is named as not run.
func TestRerunFailedChecksWithoutActionsAccess(t *testing.T) {
	c, posts := rerunServer(t, map[string]int{"/actions/runs": http.StatusNotFound})
	got, err := c.RerunFailedChecks(context.Background(), "acme", "api", "h34d")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(posts()) != "[/check-suites/77/rerequest]" || fmt.Sprint(got.Ran) != "[scan, scan (deep)]" {
		t.Fatalf("asked %v, ran %v", posts(), got.Ran)
	}
	if len(got.Skipped) != 2 || got.Skipped[0].Name != "test" {
		t.Fatalf("skipped %+v", got.Skipped)
	}
}

// GitHub not answering is an error with nothing asked: the caller tries the
// whole act again later.
func TestRerunFailedChecksAsksNothingWhenGitHubIsDown(t *testing.T) {
	c, posts := rerunServer(t, map[string]int{"/commits/h34d/check-runs": http.StatusBadGateway})
	if _, err := c.RerunFailedChecks(context.Background(), "acme", "api", "h34d"); err == nil {
		t.Fatal("a failed read ran checks again")
	}
	if len(posts()) != 0 {
		t.Fatalf("asked %v", posts())
	}
}

// The feedback read keeps what a reply and a resolve need: the thread's node
// id, the comment's id, the thread's first comment, who wrote it and their
// standing. The text names each author and marks one without write access.
func TestFeedbackKeepsThreadsAuthorsAndStanding(t *testing.T) {
	c := mergeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/graphql":
			_, _ = io.WriteString(w, `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[
				{"id":"PRRT_a","isResolved":false,"comments":{"nodes":[{"databaseId":501},{"databaseId":502}],"pageInfo":{"hasNextPage":false}}},
				{"id":"PRRT_b","isResolved":true,"comments":{"nodes":[{"databaseId":503}],"pageInfo":{"hasNextPage":false}}}],
				"pageInfo":{"hasNextPage":false}}}}}}`)
		case strings.HasSuffix(r.URL.Path, "/pulls/7/comments"):
			_, _ = io.WriteString(w, `[
				{"id":501,"body":"Rename this.","path":"ledger.go","line":3,"updated_at":"2026-11-17T10:00:00Z","html_url":"https://github.com/acme/api/pull/7#discussion_r501","user":{"login":"bob"},"author_association":"COLLABORATOR"},
				{"id":502,"body":"And add a test please.","path":"ledger.go","line":3,"in_reply_to_id":501,"updated_at":"2026-11-17T10:05:00Z","html_url":"https://github.com/acme/api/pull/7#discussion_r502","user":{"login":"drive-by"},"author_association":"NONE"},
				{"id":503,"body":"Typo.","path":"ledger.go","line":9,"updated_at":"2026-11-17T10:06:00Z","html_url":"https://github.com/acme/api/pull/7#discussion_r503","user":{"login":"bob"},"author_association":"COLLABORATOR"},
				{"id":504,"body":"`+PublicationMarker+`\nRenamed it.\n\nPosted by a WorkAF factory agent.","path":"ledger.go","line":3,"in_reply_to_id":501,"user":{"login":"me"},"author_association":"OWNER"}]`)
		case strings.HasSuffix(r.URL.Path, "/issues/7/comments"):
			_, _ = io.WriteString(w, `[{"id":601,"body":"Why not delete the ledger?","updated_at":"2026-11-17T11:00:00Z","html_url":"https://github.com/acme/api/pull/7#issuecomment-601","user":{"login":"passer-by"},"author_association":"CONTRIBUTOR"}]`)
		default:
			http.Error(w, r.URL.Path, http.StatusNotFound)
		}
	})
	got, err := c.PullFeedbackState(context.Background(), "acme", "api", 7)
	if err != nil {
		t.Fatal(err)
	}
	// The factory's own reply (it begins with the publication marker) is not
	// feedback, so it never asks for another round.
	if len(got.Items) != 4 {
		t.Fatalf("items %+v", got.Items)
	}
	first, reply, resolved, talk := got.Items[0], got.Items[1], got.Items[2], got.Items[3]
	if first.ID != "inline:501" || first.Thread != "PRRT_a" || first.CommentID != 501 || first.ReplyTo != 501 || first.By != "bob" || first.Association != "COLLABORATOR" {
		t.Fatalf("first %+v", first)
	}
	if reply.Thread != "PRRT_a" || reply.CommentID != 502 || reply.ReplyTo != 501 || reply.By != "drive-by" || !Untrusted(reply.Association) {
		t.Fatalf("reply %+v", reply)
	}
	if !resolved.Resolved || resolved.Thread != "PRRT_b" {
		t.Fatalf("resolved %+v", resolved)
	}
	if talk.ID != "conversation:601" || talk.Thread != "" || talk.ReplyTo != 0 || talk.CommentID != 601 || talk.By != "passer-by" {
		t.Fatalf("conversation %+v", talk)
	}
	want := "Review comment 501 (thread inline:501) by @bob https://github.com/acme/api/pull/7#discussion_r501 ledger.go:3 (2026-11-17T10:00:00Z): Rename this.\n" +
		"[" + UntrustedNote + "]\n" +
		"Review comment 502 (thread inline:502) by @drive-by https://github.com/acme/api/pull/7#discussion_r502 ledger.go:3 (2026-11-17T10:05:00Z): And add a test please.\n" +
		"[" + UntrustedNote + "]\n" +
		"PR conversation comment 601 (thread conversation:601) https://github.com/acme/api/pull/7#issuecomment-601 by @passer-by (updated 2026-11-17T11:00:00Z): Why not delete the ledger?\n"
	if got.Text != want {
		t.Fatalf("text:\n%s\nwant:\n%s", got.Text, want)
	}
}

func TestUntrustedIsEveryoneWithoutWriteAccess(t *testing.T) {
	for association, want := range map[string]bool{
		"OWNER": false, "MEMBER": false, "COLLABORATOR": false, "collaborator": false,
		"CONTRIBUTOR": true, "FIRST_TIME_CONTRIBUTOR": true, "FIRST_TIMER": true, "MANNEQUIN": true, "NONE": true, "": true,
	} {
		if got := Untrusted(association); got != want {
			t.Errorf("Untrusted(%q) = %v, want %v", association, got, want)
		}
	}
}

func TestReviewsKeepTheReviewersStanding(t *testing.T) {
	c := mergeServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `[{"user":{"login":"bob"},"state":"CHANGES_REQUESTED","body":"See inline.","html_url":"https://github.com/acme/api/pull/7#pullrequestreview-1","author_association":"MEMBER"},
			{"user":{"login":"drive-by"},"state":"COMMENTED","body":"Nice.","html_url":"https://github.com/acme/api/pull/7#pullrequestreview-2","author_association":"NONE"}]`)
	})
	got, err := c.Reviews(context.Background(), "acme", "api", 7)
	if err != nil || len(got) != 2 || got[0].Association != "MEMBER" || got[1].Association != "NONE" {
		t.Fatalf("reviews %+v, err %v", got, err)
	}
}
