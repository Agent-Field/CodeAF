package serve

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
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

const actsHead = "1111111111111111111111111111111111111111"

// actsGitHub is GitHub as the sender meets it: one open pull request, and
// every write kept in order.
type actsGitHub struct {
	factorygithub.API

	mu     sync.Mutex
	pull   forge.Pull
	down   bool
	refuse error
	writes []string
	// hold, when set, makes a reply wait after GitHub has it, until the
	// caller's context ends: a service that closes mid-send.
	hold bool
	held chan struct{}
}

var errActsDown = errors.New("dial tcp: no route to github")

func (g *actsGitHub) Writes() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.writes...)
}

func (g *actsGitHub) set(change func(*actsGitHub)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	change(g)
}

func (g *actsGitHub) Pull(context.Context, string, string, int) (forge.Pull, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.down {
		return forge.Pull{}, errActsDown
	}
	return g.pull, nil
}

func (g *actsGitHub) wrote(line string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.refuse != nil {
		return g.refuse
	}
	g.writes = append(g.writes, line)
	return nil
}

func (g *actsGitHub) EditPullRequest(_ context.Context, _, _ string, _ int, edit forge.PullEdit) (forge.Pull, error) {
	line := "edit"
	g.mu.Lock()
	if edit.Base != nil {
		g.pull.Base, line = *edit.Base, line+" base="+*edit.Base
	}
	if edit.Title != nil {
		g.pull.Title, line = *edit.Title, line+" title="+*edit.Title
	}
	if edit.Body != nil {
		g.pull.Body, line = *edit.Body, line+" body"
	}
	after := g.pull
	g.mu.Unlock()
	return after, g.wrote(line)
}

func (g *actsGitHub) ReplyToReviewComment(ctx context.Context, _, _ string, _ int, comment int64, body string) (forge.Posted, error) {
	if err := g.wrote(fmt.Sprintf("reply %d %s", comment, body)); err != nil {
		return forge.Posted{}, err
	}
	g.mu.Lock()
	hold, held := g.hold, g.held
	g.mu.Unlock()
	if hold {
		close(held)
		<-ctx.Done()
		return forge.Posted{}, ctx.Err()
	}
	return forge.Posted{URL: fmt.Sprintf("https://github.com/acme/api/pull/7#discussion_r%d", comment+1)}, nil
}

func (g *actsGitHub) CommentOnce(_ context.Context, _, _ string, _ int, body string) (forge.Posted, error) {
	return forge.Posted{URL: "https://github.com/acme/api/pull/7#issuecomment-9"}, g.wrote("comment " + body)
}

func (g *actsGitHub) ResolveReviewThread(_ context.Context, _, _, thread string) error {
	return g.wrote("resolve " + thread)
}

func (g *actsGitHub) RequestReviewers(_ context.Context, _, _ string, _ int, reviewers []string) error {
	return g.wrote("rerequest " + strings.Join(reviewers, ","))
}

func (g *actsGitHub) RerunFailedChecks(_ context.Context, _, _, sha string) (forge.Rerun, error) {
	return forge.Rerun{Ran: []string{"CI"}}, g.wrote("rerun " + sha)
}

func (g *actsGitHub) MarkDraftPullReady(_ context.Context, _, _ string, _ int, head string) error {
	g.mu.Lock()
	g.pull.Draft = false
	g.mu.Unlock()
	return g.wrote("ready " + head)
}

// actsFloor is a solo service over a store in a temporary folder, its person
// signed in to GitHub, on a clock the test moves.
type actsFloor struct {
	t      *testing.T
	st     *store.Store
	rec    *record.Record
	h      *Handler
	github *actsGitHub

	mu     sync.Mutex
	now    time.Time
	signed bool
	// real, when set, answers GitHub's own API for the real client, in place
	// of the scripted forge.
	real roundTrip
	// team makes the service a team's, where a job's doors are its person's.
	team bool
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func newActsFloor(t *testing.T) *actsFloor {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"acme/api"}); err != nil {
		t.Fatal(err)
	}
	f := &actsFloor{t: t, st: st, now: time.Now(), signed: true}
	f.github = &actsGitHub{pull: forge.Pull{Number: 7, State: "open", Draft: true, HeadSHA: actsHead, Base: "main", Head: "factory/12", URL: "https://github.com/acme/api/pull/7"}}
	if f.rec, err = record.Open(st, record.Options{}); err != nil {
		t.Fatal(err)
	}
	f.open()
	return f
}

// open stands the service up over the store; a restart closes it and opens
// it again.
func (f *actsFloor) open() {
	seam := factory.Seam{
		Load: func() (factory.Snapshot, error) {
			items, err := f.st.List()
			return factory.Snapshot{Items: items}, err
		},
		GitHub: func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "me"}, nil },
	}
	config := Config{Version: "test", Store: f.st, Record: f.rec, Now: f.clock, Mode: api.FactorySolo, LeaseOptions: lease.Options{Now: f.clock},
		GitHubToken: func(context.Context) string {
			f.mu.Lock()
			defer f.mu.Unlock()
			if !f.signed {
				return ""
			}
			return "person-token"
		},
		ActsClient: func(token string) factorygithub.API {
			if token != "person-token" {
				f.t.Errorf("acts were sent with the token %q", token)
			}
			return f.github
		}}
	if f.real != nil {
		config.ActsClient, config.GitHubHTTPClient = nil, &http.Client{Transport: f.real}
	}
	if f.team {
		config.Mode = api.FactoryTeam
		// These tests act as the host's person on this computer, which a team's default no longer allows.
		config.LoopbackTrust = boolPtr(true)
	}
	f.h = New(seam, config)
	f.t.Cleanup(f.h.Close)
}

func (f *actsFloor) clock() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *actsFloor) advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.mu.Unlock()
}

// job adds a job whose own pull request is open and watched, in mode.
func (f *actsFloor) job(mode string, change func(*factory.Item)) factory.Item {
	f.t.Helper()
	it := factory.Item{Repo: "api", Product: "acme", Kind: factory.KindIssue, Num: 12, Title: "Keep the ledger balanced", Publish: "pr", State: factory.StateShipped,
		Autonomy: mode, Cap: 10, Starter: "me", HeadCommit: actsHead, PublishStatus: &factory.Publishing{State: "opened"},
		PullRequest: &factory.PullRequest{Number: 7, URL: "https://github.com/acme/api/pull/7", State: factory.PROpen, Draft: true, HeadSHA: actsHead, BaseBranch: "main", HeadBranch: "factory/12",
			FeedbackTracked: true, ChecksRead: true, ReviewsRead: true, Mergeable: factory.MergeableYes, Checks: factory.PRChecks{Pending: 1},
			Reviews: []factory.PRReview{{By: "bob", State: "changes_requested"}},
			FeedbackItems: []factory.PRFeedback{
				{ID: "inline:501", Thread: "PRRT_a", CommentID: 501, ReplyTo: 501, By: "bob", Association: "COLLABORATOR"},
				{ID: "conversation:601", CommentID: 601, By: "passer-by", Association: "NONE", Untrusted: true},
			}},
		FollowUp: &factory.PRFollowUp{Kind: "pr", State: "watching", Reason: "Watching CI, reviews and merge conflicts."},
		Stream:   &factory.Stream{Phases: []factory.Phase{{Name: factory.FollowPRStep, Kind: factory.StageChat, State: factory.PhaseWaiting}}}}
	if change != nil {
		change(&it)
	}
	// A job is added new; its state is then set as the runs before left it.
	id, err := f.st.Add(context.Background(), factory.Item{Repo: it.Repo, Product: it.Product, Kind: it.Kind, Num: it.Num, Title: it.Title})
	if err != nil {
		f.t.Fatal(err)
	}
	if err := f.st.Update(id, func(cur *factory.Item) error {
		it.ID, it.Created = cur.ID, cur.Created
		*cur = it
		return nil
	}); err != nil {
		f.t.Fatal(err)
	}
	return f.item(id)
}

func (f *actsFloor) item(id int) factory.Item {
	f.t.Helper()
	it, err := f.st.Get(id)
	if err != nil {
		f.t.Fatal(err)
	}
	return it
}

// post presses a job door and answers its status and body.
func (f *actsFloor) post(id int, verb string, body any) (int, api.ActsAnswer, string) {
	f.t.Helper()
	raw, _ := json.Marshal(body)
	if s, ok := body.(string); ok {
		raw = []byte(s)
	}
	if body == nil {
		raw = nil
	}
	r := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/v1/jobs/%d/%s", id, verb), strings.NewReader(string(raw)))
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	var answer api.ActsAnswer
	var refusal struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &answer)
	_ = json.Unmarshal(w.Body.Bytes(), &refusal)
	return w.Code, answer, refusal.Error
}

func ask(by string, acts ...api.ActAsk) api.ActsRequest {
	return api.ActsRequest{Acts: acts, By: by, Why: "Answering the review."}
}

func replyAsk(text string) api.ActAsk {
	return api.ActAsk{Kind: factory.ActKindReply, Thread: "inline:501", Text: text}
}

// pass is one settle of the floor, as the job timer runs it.
func (f *actsFloor) pass() {
	f.t.Helper()
	held, err := f.h.leases()
	if err != nil {
		f.t.Fatal(err)
	}
	f.h.settleJobs(false, held)
}

// sent waits for the sender to have nothing in hand and answers the job.
func (f *actsFloor) sent(id int) factory.Item {
	f.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		f.h.acts.mu.Lock()
		busy := f.h.acts.busy
		f.h.acts.mu.Unlock()
		it := f.item(id)
		_, due := factory.NextAct(it, f.clock())
		running := false
		for _, a := range it.Acts {
			running = running || a.State == factory.ActRunning
		}
		if !busy && !due && !running {
			return it
		}
		if !busy {
			f.pass()
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("the sender never came to rest: %+v", it.Acts)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func (f *actsFloor) events(kind string) []record.Event {
	f.t.Helper()
	all, err := f.rec.Since(context.Background(), time.Time{})
	if err != nil {
		f.t.Fatal(err)
	}
	var out []record.Event
	for _, e := range all {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func logged(it factory.Item, part string) bool {
	if it.Stream == nil {
		return false
	}
	for _, l := range it.Stream.Log {
		if strings.Contains(l.Text, part) {
			return true
		}
	}
	return false
}

// AUTO: the manager's act is carried out with the person's sign-in, once,
// with the footer, and the job's log and the record say what was posted.
func TestAutoActIsCarriedOutOnceAndRecorded(t *testing.T) {
	f := newActsFloor(t)
	it := f.job(factory.AutonomyAuto, nil)
	status, answer, refusal := f.post(it.ID, api.ActVerb, ask(factory.ActByMainAgent, replyAsk("Renamed it.")))
	if status != http.StatusOK || len(answer.Results) != 1 || answer.Results[0].State != factory.ActApproved || answer.Results[0].ID == "" {
		t.Fatalf("status %d %s, answer %+v", status, refusal, answer)
	}
	got := f.sent(it.ID)
	want := "reply 501 " + forge.PublicationMarker + "\nRenamed it.\n\n" + factory.PublicActFooter
	if writes := f.github.Writes(); len(writes) != 1 || writes[0] != want {
		t.Fatalf("writes %q", writes)
	}
	a := got.Acts[0]
	if a.State != factory.ActDone || a.URL == "" || a.Result != "Replied in inline:501." || a.DoneAt.IsZero() || a.By != factory.ActByMainAgent || a.Why != "Answering the review." {
		t.Fatalf("act %+v", a)
	}
	if !logged(got, "Replied in inline:501. https://github.com/acme/api/pull/7#discussion_r502") {
		t.Fatalf("log %+v", got.Stream.Log)
	}
	events := f.events(record.PRActed)
	if len(events) != 1 || events[0].Verb != "replied" || events[0].Actor.Kind != "agent" || events[0].Handle != "me" || !strings.Contains(events[0].Detail, "discussion_r502") {
		t.Fatalf("record %+v", events)
	}
	// Later passes, and the same ask again, send nothing more.
	f.pass()
	f.post(it.ID, api.ActVerb, ask(factory.ActByMainAgent, replyAsk("Renamed it.")))
	if got = f.sent(it.ID); len(f.github.Writes()) != 1 || len(got.Acts) != 1 {
		t.Fatalf("sent again: %q, acts %+v", f.github.Writes(), got.Acts)
	}
	if got.Wait == nil || got.Wait.Who != factory.WaitGitHub || factory.WaitFault(got) != nil {
		t.Fatalf("wait %+v, fault %v", got.Wait, factory.WaitFault(got))
	}
}

// MANUAL: the same act waits for the person. Nothing is sent until their yes,
// and one yes covers the whole set.
func TestManualActsWaitForOneApproval(t *testing.T) {
	f := newActsFloor(t)
	it := f.job(factory.AutonomyManual, nil)
	status, answer, refusal := f.post(it.ID, api.ActVerb, ask(factory.ActByMainAgent, replyAsk("Renamed it."), api.ActAsk{Kind: factory.ActKindResolveThread, Thread: "501"}, api.ActAsk{Kind: factory.ActKindChangeBase, Base: "dev"}))
	if status != http.StatusOK || answer.Waiting != "post 1 reply, resolve 1 review thread and change the base to dev" {
		t.Fatalf("status %d %s, answer %+v", status, refusal, answer)
	}
	for _, r := range answer.Results {
		if r.State != factory.ActProposed {
			t.Fatalf("result %+v", r)
		}
	}
	f.pass()
	got := f.sent(it.ID)
	if len(f.github.Writes()) != 0 {
		t.Fatalf("sent before the yes: %q", f.github.Writes())
	}
	if got.Wait.Who != factory.WaitYou || got.Wait.Action != factory.ActApproveActs || got.Wait.Words != "Approve: post 1 reply, resolve 1 review thread and change the base to dev?" {
		t.Fatalf("wait %+v", got.Wait)
	}
	if err := factory.WaitFault(got); err != nil {
		t.Fatal(err)
	}
	if !logged(got, "Waiting for your approval: post 1 reply, resolve 1 review thread and change the base to dev.") {
		t.Fatalf("log %+v", got.Stream.Log)
	}
	// A yes for a set that is not the one waiting is refused, with nothing approved.
	if status, _, words := f.post(it.ID, api.ActsApproveVerb, api.ActsApproveRequest{IDs: []string{got.Acts[0].ID}}); status != http.StatusConflict || words != actsChanged {
		t.Fatalf("a stale yes: %d %s", status, words)
	}
	if n := len(factory.ProposedActs(f.item(it.ID))); n != 3 {
		t.Fatalf("%d acts still wait", n)
	}
	ids := []string{got.Acts[0].ID, got.Acts[1].ID, got.Acts[2].ID}
	if status, _, words := f.post(it.ID, api.ActsApproveVerb, api.ActsApproveRequest{IDs: ids}); status != http.StatusOK {
		t.Fatalf("approve: %d %s", status, words)
	}
	got = f.sent(it.ID)
	writes := f.github.Writes()
	if len(writes) != 3 || !strings.HasPrefix(writes[0], "reply 501 ") || writes[1] != "resolve PRRT_a" || writes[2] != "edit base=dev" {
		t.Fatalf("writes %q", writes)
	}
	// The job's stored pull request follows what the acts changed.
	if got.PullRequest.BaseBranch != "dev" || !got.PullRequest.FeedbackItems[0].Resolved {
		t.Fatalf("stored pull request %+v", got.PullRequest)
	}
	if got.Wait.Who != factory.WaitGitHub || factory.WaitFault(got) != nil {
		t.Fatalf("after the acts the job reads %+v", got.Wait)
	}
	for _, a := range got.Acts {
		if a.State != factory.ActDone || a.ApprovedBy != "me" {
			t.Fatalf("act %+v", a)
		}
	}
	if len(f.events(record.ActsApproved)) != 1 || len(f.events(record.PRActed)) != 3 || f.events(record.PRActed)[0].Actor.Kind != "person" {
		t.Fatalf("record %+v %+v", f.events(record.ActsApproved), f.events(record.PRActed))
	}
	if status, _, words := f.post(it.ID, api.ActsApproveVerb, nil); status != http.StatusConflict || words != actsNone {
		t.Fatalf("a second yes: %d %s", status, words)
	}
}

func TestDeclinedActsAreNeverSent(t *testing.T) {
	f := newActsFloor(t)
	it := f.job(factory.AutonomyManual, nil)
	f.post(it.ID, api.ActVerb, ask(factory.ActByMainAgent, replyAsk("Renamed it.")))
	status, answer, refusal := f.post(it.ID, api.ActsDeclineVerb, api.ActsDeclineRequest{Words: "Say why you renamed it."})
	if status != http.StatusOK || answer.Results[0].State != factory.ActDeclined || answer.Waiting != "" {
		t.Fatalf("status %d %s, answer %+v", status, refusal, answer)
	}
	f.pass()
	got := f.sent(it.ID)
	if len(f.github.Writes()) != 0 || got.Acts[0].State != factory.ActDeclined || got.Acts[0].Result != "Say why you renamed it." {
		t.Fatalf("writes %q, act %+v", f.github.Writes(), got.Acts[0])
	}
	if got.Wait.Who != factory.WaitGitHub || !logged(got, "@me declined: post 1 reply. Say why you renamed it.") || len(f.events(record.ActsDeclined)) != 1 {
		t.Fatalf("wait %+v, log %+v", got.Wait, got.Stream.Log)
	}
	if status, _, _ := f.post(it.ID, api.ActsDeclineVerb, nil); status != http.StatusConflict {
		t.Fatalf("a second no: %d", status)
	}
}

// The person's own ask is carried out in Manual without a second yes.
func TestThePersonsOwnActNeedsNoApproval(t *testing.T) {
	f := newActsFloor(t)
	it := f.job(factory.AutonomyManual, nil)
	status, answer, _ := f.post(it.ID, api.ActVerb, ask(factory.ActByYou, api.ActAsk{Kind: factory.ActKindComment, Text: "On it."}, api.ActAsk{Kind: factory.ActKindRerequestReview}, api.ActAsk{Kind: factory.ActKindMarkReady}))
	if status != http.StatusOK || answer.Results[0].State != factory.ActApproved {
		t.Fatalf("status %d, answer %+v", status, answer)
	}
	got := f.sent(it.ID)
	writes := f.github.Writes()
	if len(writes) != 3 || !strings.HasPrefix(writes[0], "comment ") || writes[1] != "rerequest bob" || writes[2] != "ready "+actsHead {
		t.Fatalf("writes %q", writes)
	}
	if got.PullRequest.Draft || len(f.events(record.PRReady)) != 1 || len(f.events(record.PRActed)) != 2 {
		t.Fatalf("draft %v, record %+v", got.PullRequest.Draft, f.events(record.PRReady))
	}
}

// A PROPOSAL IS REFUSED ONLY FOR what the rules name, each in plain words,
// and a refused proposal keeps and sends nothing.
func TestActIsRefusedWholeWithNothingKeptOrSent(t *testing.T) {
	stopped := true
	cases := []struct {
		name   string
		mode   string
		change func(*factory.Item)
		before func(*actsFloor)
		body   any
		status int
		words  string
	}{
		{"not signed in to GitHub", factory.AutonomyAuto, nil, func(f *actsFloor) { f.signed = false }, ask(factory.ActByMainAgent, replyAsk("Renamed it.")), http.StatusForbidden, actsSignIn},
		{"the pull request is closed", factory.AutonomyAuto, func(it *factory.Item) { it.PullRequest.State = factory.PRClosed }, nil, ask(factory.ActByMainAgent, replyAsk("Renamed it.")), http.StatusConflict, "finished"},
		{"the job has no pull request yet", factory.AutonomyAuto, func(it *factory.Item) { it.PullRequest, it.Publish, it.State = nil, "", factory.StateRunning }, nil, ask(factory.ActByMainAgent, api.ActAsk{Kind: factory.ActKindComment, Text: "Hello."}), http.StatusConflict, "no pull request yet"},
		{"the budget is reached", factory.AutonomyAuto, func(it *factory.Item) { it.SpentBefore = it.Cap }, nil, ask(factory.ActByMainAgent, replyAsk("Renamed it.")), http.StatusConflict, "The budget is reached"},
		{"the person stopped the job", factory.AutonomyAuto, func(it *factory.Item) { it.PersonStopped = &stopped }, nil, ask(factory.ActByMainAgent, replyAsk("Renamed it.")), http.StatusConflict, "stopped this job"},
		{"the repository is switched off", factory.AutonomyAuto, nil, func(f *actsFloor) { _ = f.st.SetRepos([]string{"acme/other"}) }, ask(factory.ActByMainAgent, replyAsk("Renamed it.")), http.StatusConflict, "acme/api"},
		{"one act of several is wrong", factory.AutonomyAuto, nil, nil, ask(factory.ActByMainAgent, replyAsk("Renamed it."), api.ActAsk{Kind: factory.ActKindReply, Thread: "inline:999", Text: "And this."}), http.StatusUnprocessableEntity, "Act 2 (reply): This job's pull request has no thread inline:999. Nothing was kept or sent."},
		{"nothing to push", factory.AutonomyAuto, nil, nil, ask(factory.ActByMainAgent, replyAsk("Renamed it."), api.ActAsk{Kind: factory.ActKindPush}), http.StatusConflict, "Act 2 (push): Nothing is checked and ready to push."},
		{"nobody said who proposes", factory.AutonomyAuto, nil, nil, api.ActsRequest{Acts: []api.ActAsk{replyAsk("Renamed it.")}}, http.StatusUnprocessableEntity, "by must be"},
		{"no acts", factory.AutonomyAuto, nil, nil, api.ActsRequest{By: factory.ActByYou}, http.StatusUnprocessableEntity, "at least one act"},
		{"a misspelled field", factory.AutonomyAuto, nil, nil, `{"acts":[{"kind":"comment","words":"hello"}],"by":"you"}`, http.StatusUnprocessableEntity, "unknown field"},
		{"a job that moved since it was read", factory.AutonomyAuto, nil, nil, `{"acts":[{"kind":"comment","text":"hello"}],"by":"you","expect":999}`, http.StatusConflict, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newActsFloor(t)
			it := f.job(tc.mode, tc.change)
			if tc.before != nil {
				tc.before(f)
			}
			status, _, words := f.post(it.ID, api.ActVerb, tc.body)
			if status != tc.status || !strings.Contains(words, tc.words) {
				t.Fatalf("status %d, words %q; want %d, %q", status, words, tc.status, tc.words)
			}
			f.pass()
			if got := f.sent(it.ID); len(got.Acts) != 0 || len(f.github.Writes()) != 0 {
				t.Fatalf("kept %+v, sent %q", got.Acts, f.github.Writes())
			}
		})
	}
}

// The step the job is on is never a reason: a job in the middle of a step
// takes an act and has it carried out.
func TestActIsTakenWhileAStepRuns(t *testing.T) {
	f := newActsFloor(t)
	it := f.job(factory.AutonomyAuto, func(it *factory.Item) {
		it.State = factory.StateRunning
		it.FollowUp.State = "running"
		it.Stream.Phases[0].State = factory.PhaseRunning
	})
	if status, _, words := f.post(it.ID, api.ActVerb, ask(factory.ActByMainAgent, replyAsk("Looking at it now."))); status != http.StatusOK {
		t.Fatalf("refused: %d %s", status, words)
	}
	if got := f.sent(it.ID); len(f.github.Writes()) != 1 || got.Acts[0].State != factory.ActDone || got.State != factory.StateRunning {
		t.Fatalf("writes %q, act %+v, state %s", f.github.Writes(), got.Acts, got.State)
	}
}

// The body's expect may be the job's version as a plain number.
func TestActTakesTheVersionAsANumber(t *testing.T) {
	f := newActsFloor(t)
	it := f.job(factory.AutonomyManual, nil)
	version, _ := f.st.Revision(it.ID)
	body := fmt.Sprintf(`{"acts":[{"kind":"comment","text":"hello"}],"by":"main_agent","why":"","expect":%d}`, version)
	if status, _, words := f.post(it.ID, api.ActVerb, body); status != http.StatusOK {
		t.Fatalf("the current version: %d %s", status, words)
	}
	if status, _, words := f.post(it.ID, api.ActsApproveVerb, `{"expect":0}`); status != http.StatusOK {
		t.Fatalf("expect 0: %d %s", status, words)
	}
	if status, _, _ := f.post(it.ID, api.ActsDeclineVerb, `{"words":""}`); status != http.StatusConflict {
		t.Fatalf("nothing waits, and decline answered %d", status)
	}
}

// GITHUB IS DOWN: nothing was sent, so the act stays approved and is tried
// again on a later pass, a bounded number of times, and then fails saying why.
func TestAnActIsTriedAgainWhileGitHubIsDownThenFails(t *testing.T) {
	f := newActsFloor(t)
	it := f.job(factory.AutonomyAuto, nil)
	f.github.set(func(g *actsGitHub) { g.down = true })
	f.post(it.ID, api.ActVerb, ask(factory.ActByMainAgent, replyAsk("Renamed it.")))
	got := f.sent(it.ID)
	a := got.Acts[0]
	if a.State != factory.ActApproved || a.Tries != 1 || !a.NextAt.After(f.clock()) || a.Error != "GitHub did not answer." {
		t.Fatalf("after the first try: %+v", a)
	}
	if !logged(got, "Not sent yet: reply in inline:501. GitHub did not answer. It will be tried again.") {
		t.Fatalf("log %+v", got.Stream.Log)
	}
	// The job timer is set for the next try, and the floor is not idle past it.
	if due := jobTimerDue([]factory.Item{got}, f.clock(), nil); !due.Equal(a.NextAt) {
		t.Fatalf("timer due %v, want %v", due, a.NextAt)
	}
	// Before its time nothing is tried.
	f.pass()
	if got = f.sent(it.ID); got.Acts[0].Tries != 1 {
		t.Fatalf("tried early: %+v", got.Acts[0])
	}
	// GitHub comes back: the next try posts it, once.
	f.github.set(func(g *actsGitHub) { g.down = false })
	f.advance(time.Hour)
	f.pass()
	if got = f.sent(it.ID); got.Acts[0].State != factory.ActDone || len(f.github.Writes()) != 1 {
		t.Fatalf("after GitHub came back: %+v, writes %q", got.Acts[0], f.github.Writes())
	}

	// One that never comes back fails after its tries, and does not block the job.
	down := newActsFloor(t)
	job := down.job(factory.AutonomyAuto, nil)
	down.github.set(func(g *actsGitHub) { g.down = true })
	down.post(job.ID, api.ActVerb, ask(factory.ActByMainAgent, replyAsk("Renamed it.")))
	for try := 0; try < factory.ActTriesMost+1; try++ {
		got = down.sent(job.ID)
		down.advance(time.Hour)
		down.pass()
	}
	got = down.sent(job.ID)
	if a := got.Acts[0]; a.State != factory.ActFailed || a.Tries != factory.ActTriesMost || !strings.Contains(a.Error, "GitHub did not answer. It was tried 5 times.") {
		t.Fatalf("after every try: %+v", a)
	}
	if !logged(got, "Not sent: reply in inline:501. GitHub did not answer. It was tried 5 times.") || len(down.github.Writes()) != 0 {
		t.Fatalf("log %+v", got.Stream.Log)
	}
	if _, due := factory.NextAct(got, down.clock()); due || !factory.ActsDueAt(got, down.clock()).IsZero() || factory.WaitFault(got) != nil {
		t.Fatalf("a failed act still holds the job: wait %+v, fault %v", got.Wait, factory.WaitFault(got))
	}
}

// A refusal of GitHub's is final and said on the job's log in plain words.
func TestAnActGitHubRefusesFailsWithItsReason(t *testing.T) {
	f := newActsFloor(t)
	it := f.job(factory.AutonomyAuto, nil)
	f.github.set(func(g *actsGitHub) {
		g.refuse = &forge.APIError{StatusCode: http.StatusForbidden, Body: `{"message":"Resource not accessible by personal access token"}`}
	})
	f.post(it.ID, api.ActVerb, ask(factory.ActByMainAgent, replyAsk("Renamed it."), api.ActAsk{Kind: factory.ActKindComment, Text: "Ready."}))
	got := f.sent(it.ID)
	if a := got.Acts[0]; a.State != factory.ActFailed || a.Error != "Your GitHub sign-in may not reply in this review thread." {
		t.Fatalf("act %+v", a)
	}
	// The act behind it is still tried: a failed act does not block the rest.
	if a := got.Acts[1]; a.State != factory.ActFailed || a.Error != "Your GitHub sign-in may not comment on this pull request." {
		t.Fatalf("second act %+v", a)
	}
	if !logged(got, "Not sent: reply in inline:501. Your GitHub sign-in may not reply in this review thread.") || len(f.events(record.PRActed)) != 0 {
		t.Fatalf("log %+v", got.Stream.Log)
	}
}

// A CRASH MID-SEND NEVER POSTS TWICE. An act left running by a service that
// is gone is failed by the next one, with words that say to look first.
func TestAnActLeftRunningByADeadServiceIsNotSentAgain(t *testing.T) {
	f := newActsFloor(t)
	it := f.job(factory.AutonomyManual, nil)
	f.post(it.ID, api.ActVerb, ask(factory.ActByYou, replyAsk("Renamed it.")))
	f.sent(it.ID)
	if len(f.github.Writes()) != 1 {
		t.Fatalf("writes %q", f.github.Writes())
	}
	// The document as a service that died after sending and before writing
	// "done" left it.
	if err := f.st.Update(it.ID, func(cur *factory.Item) error {
		cur.Acts[0].State, cur.Acts[0].URL, cur.Acts[0].Result, cur.Acts[0].DoneAt = factory.ActRunning, "", "", time.Time{}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f.h.Close()
	f.open()
	f.pass()
	got := f.sent(it.ID)
	if a := got.Acts[0]; a.State != factory.ActFailed || a.Error != factory.ActCutOff {
		t.Fatalf("act %+v", a)
	}
	if len(f.github.Writes()) != 1 || !logged(got, "Not confirmed: reply in inline:501. "+factory.ActCutOff) {
		t.Fatalf("writes %q, log %+v", f.github.Writes(), got.Stream.Log)
	}
	f.advance(time.Hour)
	f.pass()
	if f.sent(it.ID); len(f.github.Writes()) != 1 {
		t.Fatalf("sent again later: %q", f.github.Writes())
	}
}

// A service that closes while a reply is on its way to GitHub leaves it
// failed as not confirmed; the service that opens next does not send it again.
func TestClosingMidSendDoesNotSendAgain(t *testing.T) {
	f := newActsFloor(t)
	it := f.job(factory.AutonomyAuto, nil)
	f.github.set(func(g *actsGitHub) { g.hold, g.held = true, make(chan struct{}) })
	f.post(it.ID, api.ActVerb, ask(factory.ActByMainAgent, replyAsk("Renamed it.")))
	select {
	case <-f.github.held:
	case <-time.After(10 * time.Second):
		t.Fatal("the reply never reached GitHub")
	}
	if a := f.item(it.ID).Acts[0]; a.State != factory.ActRunning {
		t.Fatalf("while it is being sent the act reads %+v", a)
	}
	f.h.Close()
	f.github.set(func(g *actsGitHub) { g.hold = false })
	f.open()
	f.advance(time.Hour)
	f.pass()
	got := f.sent(it.ID)
	if a := got.Acts[0]; a.State != factory.ActFailed || !strings.Contains(a.Error, "did not confirm this was posted") {
		t.Fatalf("act %+v", a)
	}
	if len(f.github.Writes()) != 1 {
		t.Fatalf("writes %q", f.github.Writes())
	}
}

// PUSH IS THE PUBLISH APPROVAL. In Manual it already waits on the person, and
// the acts proposed beside it wait with it; their one approval of the push
// approves the set, which follows the push. In Auto the manager presses it.
func TestPushPressesOrWaitsOnThePublishApproval(t *testing.T) {
	landed := func(it *factory.Item) {
		it.State = factory.StateLanded
		it.HeadCommit = strings.Repeat("2", 40)
		it.FollowUp = &factory.PRFollowUp{Kind: "pr", State: "waiting_approval", HeadSHA: actsHead, Reason: "Repair and proof passed locally; updating the existing PR."}
		it.Proof = []factory.Claim{{Text: "works", OK: true}}
		it.Stream.Phases[0].State = factory.PhaseDone
	}
	f := newActsFloor(t)
	it := f.job(factory.AutonomyManual, landed)
	status, answer, words := f.post(it.ID, api.ActVerb, ask(factory.ActByMainAgent, api.ActAsk{Kind: factory.ActKindPush}, replyAsk("Fixed in the next push.")))
	if status != http.StatusOK || answer.Results[0].State != api.ActWaitingOnYou || answer.Results[1].State != factory.ActProposed {
		t.Fatalf("status %d %s, answer %+v", status, words, answer)
	}
	got := f.item(it.ID)
	if got.Approval != nil || got.Wait.Action != factory.ActApprovePublish || !strings.Contains(got.Wait.Words, "Approving also approves: post 1 reply.") {
		t.Fatalf("approval %+v, wait %+v", got.Approval, got.Wait)
	}
	// The person approves the push: the set is approved with it and waits for it.
	raw, _ := json.Marshal(api.Approve{Commit: got.HeadCommit})
	r := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/v1/jobs/%d/approve", it.ID), strings.NewReader(string(raw)))
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", w.Code, w.Body)
	}
	got = f.item(it.ID)
	a := got.Acts[0]
	if got.Approval == nil || got.Approval.State != "approving" || a.State != factory.ActApproved || !a.WithPush || a.AfterPush != got.HeadCommit || a.ApprovedBy != "me" {
		t.Fatalf("approval %+v, act %+v", got.Approval, a)
	}
	if _, due := factory.NextAct(got, f.clock()); due || len(f.github.Writes()) != 0 {
		t.Fatal("the reply is due before the push")
	}
	// Undo takes the yes back from the push and from the set.
	if err := f.st.Update(it.ID, func(cur *factory.Item) error { cur.Approval = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	f.h.settleActs([]factory.Item{f.item(it.ID)})
	if got = f.item(it.ID); got.Acts[0].State != factory.ActProposed || len(f.github.Writes()) != 0 {
		t.Fatalf("after Undo: %+v", got.Acts[0])
	}

	auto := newActsFloor(t)
	job := auto.job(factory.AutonomyAuto, landed)
	status, answer, words = auto.post(job.ID, api.ActVerb, ask(factory.ActByMainAgent, api.ActAsk{Kind: factory.ActKindPush}, replyAsk("Fixed in the next push.")))
	if status != http.StatusOK || answer.Results[0].State != api.ActPressed || answer.Results[1].State != factory.ActApproved {
		t.Fatalf("Auto: status %d %s, answer %+v", status, words, answer)
	}
	got = auto.item(job.ID)
	if got.Approval == nil || got.Approval.State != "approving" || got.Acts[0].AfterPush != got.HeadCommit || !logged(got, "Auto: the main agent approved pushing the checked work.") {
		t.Fatalf("Auto: approval %+v, act %+v", got.Approval, got.Acts[0])
	}
	if len(auto.github.Writes()) != 0 {
		t.Fatalf("Auto sent before the push: %q", auto.github.Writes())
	}
}

// THE ACTS OF A CALL THAT PRESSED THE PUSH ARE HELD TO IT. A reply written to
// go with a push ("Fixed in the new commit") must not be posted when the
// person Undoes that push inside its window: it goes back to waiting for the
// person, and nothing is sent, whoever pressed the push and in whatever mode.
func TestActsAskedInTheCallThatPressedAPushAreHeldBackWhenItsUndone(t *testing.T) {
	for _, c := range []struct{ name, mode, by string }{
		{"the manager's push in Auto", factory.AutonomyAuto, factory.ActByMainAgent},
		{"the person's own push in Manual", factory.AutonomyManual, factory.ActByYou},
	} {
		f := newActsFloor(t)
		it := f.job(c.mode, func(it *factory.Item) {
			it.State = factory.StateLanded
			it.HeadCommit = strings.Repeat("2", 40)
			it.FollowUp = &factory.PRFollowUp{Kind: "pr", State: "waiting_approval", HeadSHA: actsHead, Reason: "Repair and proof passed locally; updating the existing PR."}
			it.Proof = []factory.Claim{{Text: "works", OK: true}}
			it.Stream.Phases[0].State = factory.PhaseDone
		})
		status, answer, words := f.post(it.ID, api.ActVerb, ask(c.by, api.ActAsk{Kind: factory.ActKindPush}, replyAsk("Fixed in the new commit.")))
		if status != http.StatusOK || answer.Results[0].State != api.ActPressed || answer.Results[1].State != factory.ActApproved {
			t.Fatalf("%s: status %d %s, answer %+v", c.name, status, words, answer)
		}
		got := f.item(it.ID)
		if got.Approval == nil || got.Approval.State != "approving" || len(got.Acts) != 1 || got.Acts[0].State != factory.ActApproved || got.Acts[0].AfterPush != got.HeadCommit {
			t.Fatalf("%s: approval %+v, acts %+v", c.name, got.Approval, got.Acts)
		}
		// THE PERSON UNDOES THE PUSH inside its window.
		if err := f.st.Update(it.ID, func(cur *factory.Item) error { cur.Approval = nil; return nil }); err != nil {
			t.Fatal(err)
		}
		f.h.settleActs([]factory.Item{f.item(it.ID)})
		got = f.item(it.ID)
		if len(got.Acts) != 1 || got.Acts[0].State != factory.ActProposed || got.Acts[0].ApprovedBy != "" || got.Acts[0].AfterPush != "" {
			t.Fatalf("%s, after Undo: act %+v, want it waiting for the person", c.name, got.Acts)
		}
		if _, due := factory.NextAct(got, f.clock()); due || len(f.github.Writes()) != 0 {
			t.Fatalf("%s: the reply was due or sent after the push was undone: %q", c.name, f.github.Writes())
		}
	}
}

// THE MAIN AGENT'S PUSH IN AUTO IS AUTO'S, and so are the acts it carries:
// the approval reads automatic, the acts that waited beside it are approved
// as the main agent's, and the person who authorized the press keeps Undo.
// Switching to Manual then withdraws the push and puts the acts back to wait.
func TestTheManagersAutoPushIsCreditedToTheMainAgent(t *testing.T) {
	f := newActsFloor(t)
	it := f.job(factory.AutonomyManual, func(it *factory.Item) {
		it.State = factory.StateLanded
		it.HeadCommit = strings.Repeat("2", 40)
		it.FollowUp = &factory.PRFollowUp{Kind: "pr", State: "waiting_approval", HeadSHA: actsHead, Reason: "Repair and proof passed locally; updating the existing PR."}
		it.Proof = []factory.Claim{{Text: "works", OK: true}}
		it.Stream.Phases[0].State = factory.PhaseDone
	})
	// A reply proposed in Manual waits beside the push.
	if status, answer, words := f.post(it.ID, api.ActVerb, ask(factory.ActByMainAgent, replyAsk("Fixed in the next push."))); status != http.StatusOK || answer.Results[0].State != factory.ActProposed {
		t.Fatalf("status %d %s, answer %+v", status, words, answer)
	}
	if err := f.st.Update(it.ID, func(cur *factory.Item) error { cur.Autonomy = factory.AutonomyAuto; return nil }); err != nil {
		t.Fatal(err)
	}
	status, answer, words := f.post(it.ID, api.ActVerb, ask(factory.ActByMainAgent, api.ActAsk{Kind: factory.ActKindPush}))
	if status != http.StatusOK || answer.Results[0].State != api.ActPressed {
		t.Fatalf("status %d %s, answer %+v", status, words, answer)
	}
	got := f.item(it.ID)
	if a := got.Approval; a == nil || a.State != "approving" || !a.Automatic || a.DecidedBy != "me" {
		t.Fatalf("approval %+v", got.Approval)
	}
	if a := got.Acts[0]; a.State != factory.ActApproved || a.ApprovedBy != factory.ActByMainAgent || !a.WithPush || a.AfterPush != got.HeadCommit {
		t.Fatalf("act %+v", a)
	}
	// Manual withdraws Auto's push, and Auto's yes on the act with it.
	if err := f.st.Update(it.ID, func(cur *factory.Item) error {
		cur.Autonomy = factory.AutonomyManual
		cur.Approval = nil
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f.h.settleActs([]factory.Item{f.item(it.ID)})
	if got = f.item(it.ID); got.Acts[0].State != factory.ActProposed || got.Acts[0].ApprovedBy != "" || len(f.github.Writes()) != 0 {
		t.Fatalf("after Manual: %+v", got.Acts[0])
	}
}

// MERGE IS NEVER CARRIED OUT HERE: the job offers it to its person again.
func TestMergeIsOnlyEverOffered(t *testing.T) {
	f := newActsFloor(t)
	it := f.job(factory.AutonomyAuto, func(it *factory.Item) {
		it.PullRequest.Draft = false
		it.PullRequest.Checks = factory.PRChecks{Passed: 2}
		it.PullRequest.ReviewDecision = "approved"
	})
	status, answer, words := f.post(it.ID, api.ActVerb, ask(factory.ActByMainAgent, api.ActAsk{Kind: factory.ActKindMerge}))
	if status != http.StatusOK || answer.Results[0].State != api.ActOffered || !strings.Contains(answer.Results[0].Words, "Merging is the person's") {
		t.Fatalf("status %d %s, answer %+v", status, words, answer)
	}
	got := f.sent(it.ID)
	if got.MergeOfferedAt.IsZero() || got.Merging != nil || len(got.Acts) != 0 || len(f.github.Writes()) != 0 || got.Wait.Action != factory.ActMerge {
		t.Fatalf("offered at %v, merging %+v, acts %+v, wait %+v", got.MergeOfferedAt, got.Merging, got.Acts, got.Wait)
	}
	// A pull request the gate does not pass is not offered, and the answer says why.
	draft := f.job(factory.AutonomyAuto, nil)
	_, answer, _ = f.post(draft.ID, api.ActVerb, ask(factory.ActByYou, api.ActAsk{Kind: factory.ActKindMerge}))
	if answer.Results[0].State != api.ActNotOffered || !strings.Contains(answer.Results[0].Words, "merge is not offered") {
		t.Fatalf("a draft: %+v", answer.Results)
	}
}

// A job that ends with acts waiting sends none of them.
func TestActsEndWhenThePullRequestCloses(t *testing.T) {
	f := newActsFloor(t)
	it := f.job(factory.AutonomyManual, nil)
	f.post(it.ID, api.ActVerb, ask(factory.ActByMainAgent, replyAsk("Renamed it.")))
	if err := f.st.Update(it.ID, func(cur *factory.Item) error {
		pr := *cur.PullRequest
		pr.State = factory.PRClosed
		cur.PullRequest = &pr
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f.pass()
	got := f.sent(it.ID)
	if got.Acts[0].State != factory.ActDeclined || len(f.github.Writes()) != 0 || got.Wait.Who != factory.WaitDone {
		t.Fatalf("act %+v, wait %+v", got.Acts[0], got.Wait)
	}
	if status, _, _ := f.post(it.ID, api.ActsApproveVerb, nil); status != http.StatusConflict {
		t.Fatalf("approve on a closed pull request: %d", status)
	}
}

// The live pull request closed since the job last read it: the act is
// refused at the read, with nothing sent.
func TestAnActOnAPullRequestClosedOnGitHubSendsNothing(t *testing.T) {
	f := newActsFloor(t)
	it := f.job(factory.AutonomyAuto, nil)
	f.github.set(func(g *actsGitHub) { g.pull.State = "closed" })
	f.post(it.ID, api.ActVerb, ask(factory.ActByMainAgent, replyAsk("Renamed it.")))
	got := f.sent(it.ID)
	if a := got.Acts[0]; a.State != factory.ActFailed || a.Error != "The pull request is closed." || len(f.github.Writes()) != 0 {
		t.Fatalf("act %+v, writes %q", a, f.github.Writes())
	}
}

// A decision the person put away refuses the doors, as it does every decision.
func TestActsDoorsAreShutWhileADecisionIsPutAway(t *testing.T) {
	for _, verb := range []string{api.ActVerb, api.ActsApproveVerb, api.ActsDeclineVerb} {
		if !decisionAction(verb) || !actVerb(verb) {
			t.Errorf("%s is not a decision door", verb)
		}
	}
	if !offVerbs[api.ActVerb] || !offVerbs[api.ActsApproveVerb] || offVerbs[api.ActsDeclineVerb] {
		t.Fatalf("off verbs %v", offVerbs)
	}
}

// With no client supplied the sender is GitHub's own API, called with the
// person's sign-in and nothing else: the read of the pull request, then the
// one write.
func TestActsAreSentToGitHubWithThePersonsSignIn(t *testing.T) {
	f := newActsFloor(t)
	var mu sync.Mutex
	var calls []string
	f.real = func(r *http.Request) (*http.Response, error) {
		body := ""
		if r.Body != nil {
			raw, _ := io.ReadAll(r.Body)
			body = string(raw)
		}
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.String()+" "+r.Header.Get("Authorization")+" "+body)
		mu.Unlock()
		answer := `{"number":7,"state":"open","draft":true,"html_url":"https://github.com/acme/api/pull/7","head":{"sha":"` + actsHead + `","ref":"factory/12"},"base":{"ref":"main","sha":"b4se"}}`
		status := http.StatusOK
		if r.Method == http.MethodPost {
			status, answer = http.StatusCreated, `{"id":502,"html_url":"https://github.com/acme/api/pull/7#discussion_r502"}`
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(answer))}, nil
	}
	f.h.Close()
	f.open()
	it := f.job(factory.AutonomyAuto, nil)
	if status, _, words := f.post(it.ID, api.ActVerb, ask(factory.ActByMainAgent, replyAsk("Renamed it."))); status != http.StatusOK {
		t.Fatalf("refused: %d %s", status, words)
	}
	got := f.sent(it.ID)
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 2 || calls[0] != "GET https://api.github.com/repos/acme/api/pulls/7 Bearer person-token " ||
		!strings.HasPrefix(calls[1], "POST https://api.github.com/repos/acme/api/pulls/7/comments/501/replies Bearer person-token ") || !strings.Contains(calls[1], "Posted by a WorkAF factory agent.") {
		t.Fatalf("calls %q", calls)
	}
	if a := got.Acts[0]; a.State != factory.ActDone || a.URL != "https://github.com/acme/api/pull/7#discussion_r502" {
		t.Fatalf("act %+v", a)
	}
}

// ONLY THE JOB'S PERSON proposes or answers its acts. On a team's factory
// someone else is refused at every one of the three doors, and a team never
// sends an act with the host's own sign-in.
func TestActsAreTheJobsPersonsOnly(t *testing.T) {
	f := newActsFloor(t)
	f.team = true
	f.h.Close()
	f.open()
	theirs := f.job(factory.AutonomyManual, func(it *factory.Item) {
		it.Starter = "someone-else"
		it.Acts = []factory.PublicAct{{ID: "act_1", Kind: factory.ActKindComment, Text: "Ready.\n\n" + factory.PublicActFooter, By: factory.ActByMainAgent, State: factory.ActProposed}}
	})
	for verb, body := range map[string]any{
		api.ActVerb:         ask(factory.ActByYou, api.ActAsk{Kind: factory.ActKindComment, Text: "Mine now."}),
		api.ActsApproveVerb: nil,
		api.ActsDeclineVerb: nil,
	} {
		if status, _, words := f.post(theirs.ID, verb, body); status != http.StatusForbidden || !strings.Contains(words, "only @someone-else") {
			t.Errorf("%s by someone else: %d %s", verb, status, words)
		}
	}
	got := f.item(theirs.ID)
	if len(got.Acts) != 1 || got.Acts[0].State != factory.ActProposed || len(f.github.Writes()) != 0 {
		t.Fatalf("acts %+v, writes %q", got.Acts, f.github.Writes())
	}
	// The job's own person, with no GitHub sign-in of their own on this team's
	// factory, is told to sign in: the host's sign-in is never used for them.
	mine := f.job(factory.AutonomyAuto, nil)
	if status, _, words := f.post(mine.ID, api.ActVerb, ask(factory.ActByYou, api.ActAsk{Kind: factory.ActKindComment, Text: "Hello."})); status != http.StatusForbidden || words != actsSignIn {
		t.Fatalf("no sign-in on a team: %d %s", status, words)
	}
	// And an act already approved waits for that sign-in, then fails saying so.
	if err := f.st.Update(mine.ID, func(cur *factory.Item) error {
		cur.Acts = []factory.PublicAct{{ID: "act_2", Kind: factory.ActKindComment, Text: "Hello.\n\n" + factory.PublicActFooter, By: factory.ActByYou, State: factory.ActApproved, ApprovedBy: "me"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f.pass()
	if a := f.sent(mine.ID).Acts[0]; a.State != factory.ActApproved || a.Tries != 1 || a.Error != "The job's person is not signed in to GitHub." || len(f.github.Writes()) != 0 {
		t.Fatalf("with no sign-in the act reads %+v, writes %q", a, f.github.Writes())
	}
}
