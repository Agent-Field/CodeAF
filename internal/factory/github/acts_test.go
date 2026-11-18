package github

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
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

const actsHead = "1111111111111111111111111111111111111111"

// actsForge is GitHub as the acts meet it: one open pull request, answered by
// the real client over HTTP. A test sets how each write is answered.
type actsForge struct {
	mu sync.Mutex
	// pull is the pull request as GitHub has it.
	state, base, head, title string
	draft                    bool
	// pullStatus fails the read of the pull request with this status.
	pullStatus int
	// answer is the status of a write by its path's last part ("replies",
	// "comments", "requested_reviewers", "pulls", "graphql"), 0 for a yes.
	answer map[string]int
	// graphql is the GraphQL answer's body, when set.
	graphql string
	// writes is every write received, as "METHOD path body".
	writes []string
}

func (f *actsForge) Writes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.writes...)
}

func (f *actsForge) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/repos/acme/api")
	if r.Method == http.MethodGet {
		switch {
		case path == "/pulls/7":
			if f.pullStatus != 0 {
				w.WriteHeader(f.pullStatus)
				_, _ = io.WriteString(w, `{"message":"Not Found"}`)
				return
			}
			fmt.Fprintf(w, `{"number":7,"title":%q,"state":%q,"draft":%t,"node_id":"PR_node","mergeable":true,"html_url":"https://github.com/acme/api/pull/7","head":{"sha":%q,"ref":"factory/12"},"base":{"ref":%q,"sha":"b4se"}}`, f.title, f.state, f.draft, f.head, f.base)
		case path == "/actions/runs":
			_, _ = io.WriteString(w, `{"workflow_runs":[{"id":900,"name":"CI","status":"completed","conclusion":"failure","check_suite_id":41}]}`)
		case strings.HasSuffix(path, "/check-runs"):
			_, _ = io.WriteString(w, `{"total_count":1,"check_runs":[{"name":"test","status":"completed","conclusion":"failure","app":{"slug":"github-actions"},"check_suite":{"id":41}}]}`)
		case strings.HasSuffix(path, "/status"):
			_, _ = io.WriteString(w, `{"statuses":[{"context":"license/cla","state":"failure"}]}`)
		default:
			http.Error(w, path, http.StatusNotFound)
		}
		return
	}
	body, _ := io.ReadAll(r.Body)
	last := path[strings.LastIndex(path, "/")+1:]
	if r.URL.Path == "/graphql" {
		last = "graphql"
	} else if strings.HasPrefix(path, "/pulls/7") && r.Method == http.MethodPatch {
		last = "pulls"
	}
	f.writes = append(f.writes, r.Method+" "+firstOf(path, r.URL.Path)+" "+strings.TrimSpace(string(body)))
	if status := f.answer[last]; status != 0 {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"message":"Validation Failed","errors":[{"resource":"PullRequest","field":"base","code":"invalid"}]}`)
		return
	}
	switch last {
	case "graphql":
		if f.graphql != "" {
			_, _ = io.WriteString(w, f.graphql)
			return
		}
		if strings.Contains(string(body), "markPullRequestReadyForReview") {
			f.draft = false
			fmt.Fprintf(w, `{"data":{"markPullRequestReadyForReview":{"pullRequest":{"isDraft":false,"headRefOid":%q}}}}`, f.head)
			return
		}
		_, _ = io.WriteString(w, `{"data":{"resolveReviewThread":{"thread":{"id":"PRRT_a","isResolved":true}}}}`)
	case "pulls":
		var edit map[string]string
		_ = json.Unmarshal(body, &edit)
		if edit["base"] != "" {
			f.base = edit["base"]
		}
		if edit["title"] != "" {
			f.title = edit["title"]
		}
		fmt.Fprintf(w, `{"number":7,"title":%q,"state":"open","html_url":"https://github.com/acme/api/pull/7","head":{"sha":%q,"ref":"factory/12"},"base":{"ref":%q,"sha":"n3wbase"}}`, f.title, f.head, f.base)
	case "replies":
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":502,"html_url":"https://github.com/acme/api/pull/7#discussion_r502"}`)
	case "comments":
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":9,"html_url":"https://github.com/acme/api/pull/7#issuecomment-9"}`)
	default:
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{}`)
	}
}

func actsFixture(t *testing.T) (*actsForge, *Source, factory.Item) {
	t.Helper()
	f := &actsForge{state: "open", base: "main", head: actsHead, title: "Keep the ledger balanced", draft: true, answer: map[string]int{}}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	client := forge.NewClient("person-token").WithBaseURL(srv.URL).WithSleep(func(context.Context, time.Duration) error { return nil })
	it := factory.Item{ID: 7, Repo: "api", Product: "acme", Kind: factory.KindIssue, Num: 12, Publish: "pr", State: factory.StateShipped, Starter: "me", Title: "Keep the ledger balanced",
		PullRequest: &factory.PullRequest{Number: 7, URL: "https://github.com/acme/api/pull/7", State: factory.PROpen, Draft: true, HeadSHA: actsHead, BaseBranch: "main", HeadBranch: "factory/12",
			Mergeable: factory.MergeableYes, ReviewsRead: true, FeedbackTracked: true,
			FeedbackItems: []factory.PRFeedback{
				{ID: "inline:501", Thread: "PRRT_a", CommentID: 501, ReplyTo: 501, By: "bob"},
				{ID: "inline:502", Thread: "PRRT_a", CommentID: 502, ReplyTo: 501, By: "drive-by", Untrusted: true},
				{ID: "inline:777", CommentID: 777, ReplyTo: 777, By: "bob"},
				{ID: "conversation:601", CommentID: 601, By: "passer-by", Untrusted: true},
			}}}
	return f, New(client, []string{"acme/api"}, func() time.Time { return time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC) }), it
}

func act(t *testing.T, it factory.Item, a factory.PublicAct) factory.PublicAct {
	t.Helper()
	clean, err := factory.CleanAct(it, a)
	if err != nil {
		t.Fatalf("clean: %v", err)
	}
	return clean
}

func failure(t *testing.T, err error) *ActFailure {
	t.Helper()
	var f *ActFailure
	if !errors.As(err, &f) {
		t.Fatalf("err = %v, want an ActFailure", err)
	}
	return f
}

// A reply goes into the thread at its first comment, once, with the footer on
// and the marker that keeps the factory from reading it back as feedback.
func TestActReplyIsPostedInItsThreadWithTheFooter(t *testing.T) {
	f, src, it := actsFixture(t)
	done, err := src.Act(context.Background(), it, act(t, it, factory.PublicAct{Kind: factory.ActKindReply, Thread: "inline:502", Text: "Renamed it."}))
	if err != nil {
		t.Fatal(err)
	}
	writes := f.Writes()
	if len(writes) != 1 || !strings.HasPrefix(writes[0], "POST /pulls/7/comments/501/replies ") {
		t.Fatalf("writes %v", writes)
	}
	var sent struct{ Body string }
	_ = json.Unmarshal([]byte(writes[0][strings.Index(writes[0], "{"):]), &sent)
	if sent.Body != forge.PublicationMarker+"\nRenamed it.\n\n"+factory.PublicActFooter {
		t.Fatalf("posted %q", sent.Body)
	}
	if done.URL != "https://github.com/acme/api/pull/7#discussion_r502" || done.Words != "Replied in inline:502." {
		t.Fatalf("done %+v", done)
	}
}

// A thread the job kept before it kept thread ids is not guessed at: the
// reply waits for the next read of the pull request.
func TestActReplyWaitsForAThreadReadBeforeIdsWereKept(t *testing.T) {
	f, src, it := actsFixture(t)
	it.PullRequest.FeedbackItems = append(it.PullRequest.FeedbackItems, factory.PRFeedback{ID: "inline:888"})
	_, err := src.Act(context.Background(), it, act(t, it, factory.PublicAct{Kind: factory.ActKindReply, Thread: "inline:888", Text: "Renamed it."}))
	if got := failure(t, err); got.How != ActNotSent || len(f.Writes()) != 0 {
		t.Fatalf("%+v, writes %v", got, f.Writes())
	}
}

// A comment on the conversation has no thread on GitHub: its answer goes on
// the conversation, word for word.
func TestActReplyToAConversationCommentIsAComment(t *testing.T) {
	f, src, it := actsFixture(t)
	done, err := src.Act(context.Background(), it, act(t, it, factory.PublicAct{Kind: factory.ActKindReply, Thread: "conversation:601", Text: "It is still needed."}))
	if err != nil {
		t.Fatal(err)
	}
	if writes := f.Writes(); len(writes) != 1 || !strings.HasPrefix(writes[0], "POST /issues/7/comments ") || !strings.Contains(writes[0], "It is still needed.") {
		t.Fatalf("writes %v", writes)
	}
	if !strings.Contains(done.Words, "conversation") {
		t.Fatalf("done %+v", done)
	}
}

func TestActChangeBaseUpdatesTheStoredPullRequest(t *testing.T) {
	f, src, it := actsFixture(t)
	done, err := src.Act(context.Background(), it, act(t, it, factory.PublicAct{Kind: factory.ActKindChangeBase, Base: "dev"}))
	if err != nil {
		t.Fatal(err)
	}
	if writes := f.Writes(); len(writes) != 1 || writes[0] != `PATCH /pulls/7 {"base":"dev"}` {
		t.Fatalf("writes %v", writes)
	}
	pr := *it.PullRequest
	done.Change(&pr)
	// What depended on the old base is unknown until the next read.
	if pr.BaseBranch != "dev" || pr.BaseSHA != "n3wbase" || pr.Mergeable != "" || pr.ReviewsRead || done.Words != "Changed the pull request's base from main to dev." {
		t.Fatalf("stored %+v, words %q", pr, done.Words)
	}
	// The base is already there on GitHub (someone changed it by hand):
	// nothing is sent, and the job's copy is brought up to date.
	again, err := src.Act(context.Background(), it, act(t, it, factory.PublicAct{Kind: factory.ActKindChangeBase, Base: "dev"}))
	if err != nil || len(f.Writes()) != 1 || !strings.Contains(again.Words, "already dev") {
		t.Fatalf("a base already changed: %+v, err %v, writes %v", again, err, f.Writes())
	}
}

func TestActEditKeepsTheNewTitleOnTheJob(t *testing.T) {
	f, src, it := actsFixture(t)
	done, err := src.Act(context.Background(), it, act(t, it, factory.PublicAct{Kind: factory.ActKindEditPR, Title: "Balance the ledger on every write"}))
	if err != nil {
		t.Fatal(err)
	}
	if writes := f.Writes(); len(writes) != 1 || writes[0] != `PATCH /pulls/7 {"title":"Balance the ledger on every write"}` {
		t.Fatalf("writes %v", writes)
	}
	pr := *it.PullRequest
	done.Change(&pr)
	if pr.Title != "Balance the ledger on every write" {
		t.Fatalf("stored title %q", pr.Title)
	}
	// A later repair push keeps the edited title instead of the job's own.
	it.PullRequest = &pr
	if err := src.UpdatePRBody(context.Background(), it, "Proof."); err != nil {
		t.Fatal(err)
	}
	if last := f.Writes()[1]; !strings.Contains(last, `"title":"Balance the ledger on every write"`) {
		t.Fatalf("the repair push sent %s", last)
	}
	// An edit of the description alone leaves the title as it is.
	body, err := src.Act(context.Background(), it, act(t, it, factory.PublicAct{Kind: factory.ActKindEditPR, Body: "What changed."}))
	if err != nil || f.Writes()[2] != `PATCH /pulls/7 {"body":"What changed."}` {
		t.Fatalf("description edit: %+v, err %v, writes %v", body, err, f.Writes())
	}
	edited := *it.PullRequest
	body.Change(&edited)
	if edited.Body != "What changed." || edited.Title != "Balance the ledger on every write" {
		t.Fatalf("stored after the description edit: title %q, body %q", edited.Title, edited.Body)
	}
}

// AN EDITED DESCRIPTION IS NOT UNDONE BY THE NEXT REPAIR PUSH. Follow PR's
// push used to write the proof over whatever the description said; once a
// public act has edited it, the push sends the title alone.
func TestARepairPushKeepsAnEditedDescription(t *testing.T) {
	f, src, it := actsFixture(t)
	done, err := src.Act(context.Background(), it, act(t, it, factory.PublicAct{Kind: factory.ActKindEditPR, Body: "What changed, in the person's words."}))
	if err != nil {
		t.Fatal(err)
	}
	pr := *it.PullRequest
	done.Change(&pr)
	it.PullRequest = &pr
	if err := src.UpdatePRBody(context.Background(), it, "Proof: go test ./... passed."); err != nil {
		t.Fatal(err)
	}
	writes := f.Writes()
	if len(writes) != 2 || writes[1] != `PATCH /pulls/7 {"title":"Keep the ledger balanced"}` {
		t.Fatalf("the repair push sent %v", writes)
	}
	// A description nobody edited still gets the proof.
	f2, src2, plain := actsFixture(t)
	if err := src2.UpdatePRBody(context.Background(), plain, "Proof: go test ./... passed."); err != nil {
		t.Fatal(err)
	}
	if w := f2.Writes(); len(w) != 1 || !strings.Contains(w[0], `"body":"Proof: go test ./... passed."`) {
		t.Fatalf("an unedited description: %v", w)
	}
}

func TestActResolveMarksTheWholeThreadResolved(t *testing.T) {
	f, src, it := actsFixture(t)
	done, err := src.Act(context.Background(), it, act(t, it, factory.PublicAct{Kind: factory.ActKindResolveThread, Thread: "inline:501"}))
	if err != nil {
		t.Fatal(err)
	}
	if writes := f.Writes(); len(writes) != 1 || !strings.Contains(writes[0], "resolveReviewThread") || !strings.Contains(writes[0], "PRRT_a") {
		t.Fatalf("writes %v", writes)
	}
	pr := *it.PullRequest
	done.Change(&pr)
	if !pr.FeedbackItems[0].Resolved || !pr.FeedbackItems[1].Resolved || pr.FeedbackItems[2].Resolved || it.PullRequest.FeedbackItems[0].Resolved {
		t.Fatalf("stored %+v", pr.FeedbackItems)
	}
	// A thread whose id the job has not read yet is tried again later, with
	// nothing sent.
	_, err = src.Act(context.Background(), it, act(t, it, factory.PublicAct{Kind: factory.ActKindResolveThread, Thread: "inline:777"}))
	if got := failure(t, err); got.How != ActNotSent || len(f.Writes()) != 1 {
		t.Fatalf("no thread id: %+v, writes %v", got, f.Writes())
	}
}

func TestActRerequestAndRerun(t *testing.T) {
	f, src, it := actsFixture(t)
	done, err := src.Act(context.Background(), it, act(t, it, factory.PublicAct{Kind: factory.ActKindRerequestReview, Reviewers: []string{"bob", "carol"}}))
	if err != nil || done.Words != "Asked @bob and @carol to review again." {
		t.Fatalf("done %+v, err %v", done, err)
	}
	if writes := f.Writes(); len(writes) != 1 || writes[0] != `POST /pulls/7/requested_reviewers {"reviewers":["bob","carol"]}` {
		t.Fatalf("writes %v", writes)
	}
	it.PullRequest.Checks = factory.PRChecks{Failed: 1}
	it.PullRequest.ChecksRead = true
	done, err = src.Act(context.Background(), it, act(t, it, factory.PublicAct{Kind: factory.ActKindRerunChecks}))
	if err != nil {
		t.Fatal(err)
	}
	// A check that cannot be run again is said in the result; the act is done.
	if done.Words != "Asked GitHub to run again: CI. Could not run license/cla again: it is a commit status, which only the service that set it can run again." {
		t.Fatalf("words %q", done.Words)
	}
	if writes := f.Writes(); len(writes) != 2 || !strings.HasPrefix(writes[1], "POST /actions/runs/900/rerun-failed-jobs") {
		t.Fatalf("writes %v", writes)
	}
}

func TestActMarkReady(t *testing.T) {
	f, src, it := actsFixture(t)
	done, err := src.Act(context.Background(), it, act(t, it, factory.PublicAct{Kind: factory.ActKindMarkReady}))
	if err != nil {
		t.Fatal(err)
	}
	pr := *it.PullRequest
	done.Change(&pr)
	if pr.Draft || len(f.Writes()) != 1 || !strings.Contains(f.Writes()[0], "markPullRequestReadyForReview") {
		t.Fatalf("stored draft %v, writes %v", pr.Draft, f.Writes())
	}
	// Already ready on GitHub: nothing is sent and the job's copy follows.
	again, err := src.Act(context.Background(), it, act(t, it, factory.PublicAct{Kind: factory.ActKindMarkReady}))
	if err != nil || len(f.Writes()) != 1 || !strings.Contains(again.Words, "already ready") {
		t.Fatalf("already ready: %+v, err %v", again, err)
	}
	// Commits the job has not read yet: the yes was not to them, so it waits
	// for the next read.
	f.mu.Lock()
	f.draft, f.head = true, strings.Repeat("2", 40)
	f.mu.Unlock()
	_, err = src.Act(context.Background(), it, act(t, it, factory.PublicAct{Kind: factory.ActKindMarkReady}))
	if got := failure(t, err); got.How != ActNotSent || !strings.Contains(got.Words, "has not read yet") || len(f.Writes()) != 1 {
		t.Fatalf("a moved head: %+v, writes %v", got, f.Writes())
	}
}

// WHAT A REAL PERSON HITS. Each failure says in plain words what happened and
// whether anything was sent, which decides whether the act is tried again.
func TestActFailuresSayWhatHappenedAndWhetherAnythingWasSent(t *testing.T) {
	reply := factory.PublicAct{Kind: factory.ActKindReply, Thread: "inline:501", Text: "Renamed it."}
	comment := factory.PublicAct{Kind: factory.ActKindComment, Text: "Ready for another look."}
	base := factory.PublicAct{Kind: factory.ActKindChangeBase, Base: "dev"}
	resolve := factory.PublicAct{Kind: factory.ActKindResolveThread, Thread: "inline:501"}
	review := factory.PublicAct{Kind: factory.ActKindRerequestReview, Reviewers: []string{"bob"}}
	cases := []struct {
		name   string
		act    factory.PublicAct
		set    func(*actsForge)
		how    string
		words  string
		writes int
	}{
		{"GitHub is down: nothing was sent", reply, func(f *actsForge) { f.pullStatus = http.StatusBadGateway }, ActNotSent, "GitHub did not answer.", 0},
		{"the sign-in expired", reply, func(f *actsForge) { f.pullStatus = http.StatusUnauthorized }, ActRefused, "Sign in to GitHub again", 0},
		{"the sign-in cannot read the pull request", reply, func(f *actsForge) { f.pullStatus = http.StatusNotFound }, ActRefused, "cannot read this pull request", 0},
		{"the pull request was closed meanwhile", reply, func(f *actsForge) { f.state = "closed" }, ActRefused, "The pull request is closed.", 0},
		{"the sign-in may not reply", reply, func(f *actsForge) { f.answer["replies"] = http.StatusForbidden }, ActRefused, "Your GitHub sign-in may not reply in this review thread.", 1},
		{"the comment it answers was deleted", reply, func(f *actsForge) { f.answer["replies"] = http.StatusNotFound }, ActRefused, "no longer on the pull request", 1},
		{"a reply whose answer was lost is never sent again", reply, func(f *actsForge) { f.answer["replies"] = http.StatusBadGateway }, ActUnknown, "did not confirm this was posted", 1},
		{"a comment whose answer was lost is never sent again", comment, func(f *actsForge) { f.answer["comments"] = http.StatusGatewayTimeout }, ActUnknown, "did not confirm this was posted", 1},
		{"a base that does not exist", base, func(f *actsForge) { f.answer["pulls"] = http.StatusUnprocessableEntity }, ActRefused, "GitHub refused to change this pull request's base: Validation Failed (base is invalid).", 1},
		{"the sign-in may not change the base", base, func(f *actsForge) { f.answer["pulls"] = http.StatusForbidden }, ActRefused, "may not change this pull request's base", 1},
		{"a thread that is gone", resolve, func(f *actsForge) {
			f.graphql = `{"data":{"resolveReviewThread":null},"errors":[{"type":"NOT_FOUND","message":"Could not resolve to a node"}]}`
		}, ActRefused, "That review thread is no longer on the pull request.", 1},
		{"the sign-in may not resolve", resolve, func(f *actsForge) {
			f.graphql = `{"data":{"resolveReviewThread":null},"errors":[{"type":"FORBIDDEN","message":"Resource not accessible"}]}`
		}, ActRefused, "may not resolve this review thread", 1},
		{"a resolve GitHub did not answer says the same thing next time", resolve, func(f *actsForge) { f.answer["graphql"] = http.StatusBadGateway }, ActNotSent, "GitHub did not answer.", 1},
		{"a reviewer GitHub will not ask", review, func(f *actsForge) { f.answer["requested_reviewers"] = http.StatusUnprocessableEntity }, ActRefused, "GitHub refused to ask for a review of this pull request", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, src, it := actsFixture(t)
			a := act(t, it, tc.act)
			tc.set(f)
			_, err := src.Act(context.Background(), it, a)
			got := failure(t, err)
			if got.How != tc.how || !strings.Contains(got.Words, tc.words) {
				t.Fatalf("how %s, words %q; want %s, %q", got.How, got.Words, tc.how, tc.words)
			}
			// A write that is retried by the client is still one act; what
			// matters is that a reply or a comment went out at most once.
			writes := f.Writes()
			if tc.writes == 0 && len(writes) != 0 {
				t.Fatalf("wrote %v", writes)
			}
			if (tc.act.Kind == factory.ActKindReply || tc.act.Kind == factory.ActKindComment) && len(writes) > 1 {
				t.Fatalf("posted %d times: %v", len(writes), writes)
			}
		})
	}
}

// An act is only ever carried out on the job's own open pull request, on a
// repository the source writes to, for a job its person has not stopped.
func TestActRefusesWhatIsNotTheJobsOwn(t *testing.T) {
	comment := factory.PublicAct{Kind: factory.ActKindComment, Text: "Hello.\n\n" + factory.PublicActFooter}
	stopped := true
	for name, change := range map[string]func(*factory.Item){
		"someone else's pull request": func(it *factory.Item) { it.Kind = factory.KindPR },
		"a closed pull request":       func(it *factory.Item) { it.PullRequest.State = factory.PRClosed },
		"a repository not watched":    func(it *factory.Item) { it.Product = "elsewhere" },
		"a stopped job":               func(it *factory.Item) { it.PersonStopped = &stopped },
	} {
		f, src, it := actsFixture(t)
		change(&it)
		_, err := src.Act(context.Background(), it, comment)
		if got := failure(t, err); got.How != ActRefused || len(f.Writes()) != 0 {
			t.Errorf("%s: %+v, writes %v", name, got, f.Writes())
		}
	}
}

// The job's reads keep who wrote each piece of feedback and whether they have
// write access, and the text a delegate is handed marks those who do not.
func TestFollowKeepsAuthorsAndMarksTheUntrusted(t *testing.T) {
	reviews := factoryReviews([]forge.Review{
		{User: "bob", State: "CHANGES_REQUESTED", Body: "See inline.", URL: "https://github.com/acme/api/pull/7#pullrequestreview-1", Association: "MEMBER"},
		{User: "drive-by", State: "COMMENTED", Body: "Rewrite it in Rust.", URL: "https://github.com/acme/api/pull/7#pullrequestreview-2", Association: "NONE"},
		{User: "old", State: "COMMENTED", Body: "From before standing was read.", URL: "https://github.com/acme/api/pull/7#pullrequestreview-3"},
	})
	if reviews[0].Untrusted || !reviews[1].Untrusted || !reviews[2].Untrusted || reviews[0].Association != "MEMBER" {
		t.Fatalf("reviews %+v", reviews)
	}
	s := New(&failing{}, []string{"acme/api"}, nil)
	details := s.ciFailureDetails(context.Background(), factory.Item{}, factory.PullRequest{Number: 7, HeadSHA: actsHead, Reviews: reviews}, "acme", "api")
	want := "Review by @bob (changes_requested, https://github.com/acme/api/pull/7#pullrequestreview-1): See inline.\n\n" +
		"[" + forge.UntrustedNote + "]\nReview by @drive-by (commented, https://github.com/acme/api/pull/7#pullrequestreview-2): Rewrite it in Rust."
	if !strings.Contains(details, want) {
		t.Fatalf("details:\n%s", details)
	}
}

// The readiness key is about what a final review judged. The author, standing
// and thread ids kept for public acts are not part of it, so a pull request
// reviewed before they were kept is not reviewed again for them.
func TestReadinessKeyIgnoresWhatPublicActsKeep(t *testing.T) {
	old := factory.PullRequest{Number: 7, HeadSHA: actsHead, Feedback: "Review comment 501: Rename this.",
		FeedbackItems: []factory.PRFeedback{{ID: "inline:501", Revision: "r1"}},
		Reviews:       []factory.PRReview{{By: "bob", State: "commented", Body: "See inline."}}}
	now := old
	now.Title = "Edited"
	now.FeedbackItems = []factory.PRFeedback{{ID: "inline:501", Revision: "r1", Thread: "PRRT_a", CommentID: 501, ReplyTo: 501, By: "bob", Association: "NONE", Untrusted: true}}
	now.Reviews = []factory.PRReview{{By: "bob", State: "commented", Body: "See inline.", Association: "NONE", Untrusted: true}}
	a, _ := json.Marshal(reviewedFacts(old))
	b, _ := json.Marshal(reviewedFacts(now))
	c, _ := json.Marshal(old)
	if string(a) != string(b) || string(a) != string(c) {
		t.Fatalf("the reviewed facts differ:\n%s\n%s\n%s", a, b, c)
	}
	if now.FeedbackItems[0].By != "bob" || now.Title != "Edited" {
		t.Fatal("reading the reviewed facts changed the pull request")
	}
}
