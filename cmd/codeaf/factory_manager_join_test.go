package main

import (
	"context"
	"errors"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/factorytest"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE MANAGER'S TOOLS, THROUGH THE REAL DOOR, INTO THE REAL SERVICE ───────
//
// The manager's tools were built against a press that only recorded what it
// was sent (factory_manager_acts_test.go), and the service's doors for the
// same verbs against requests a test wrote by hand. Each side passed, and
// nothing had carried one press from the tool's door into the service that
// takes it. The service refuses a body with a field it does not know, so a
// body that differs by one name fails only when a real job is on the line.
//
// THE RIG IS THE WHOLE ROAD. The factory is a scenario's
// (internal/factory/factorytest): the real store, lease desk, service, runner
// and poller, with a scripted delegate and a scripted GitHub. The press is the
// app's own ([installAppLocalPress]) on the app's own hub, which POSTs the
// job's verb to the service's address with the app's bearer. The door is the
// one the job's conversation is opened with ([runDoor]). A test presses that
// door as a tool does and reads THE JOB THE SERVICE STORED, never what was
// sent.

// joinBearer is the bearer the rig's hub sends, as the app's hub sends the
// one its own factory was started with.
const joinBearer = "join-rig-app-bearer-fixture"

// joinRig is one scenario whose job is managed by a conversation, with the
// app's press hung over it.
type joinRig struct {
	t    *testing.T
	s    *factorytest.Scenario
	talk string
	door session.ManagerPresser
}

// newJoin builds a scenario in mode with the steps named, its person signed
// in to GitHub, its review threads read one by one, and its job managed by a
// conversation of its own.
func newJoin(t *testing.T, mode string, steps ...string) *joinRig {
	t.Helper()
	root := t.TempDir()
	t.Setenv("CODEAF_HOME", root)
	t.Setenv("HOME", root)
	s := factorytest.NewScenario(t, mode, steps...)
	s.TrackThreads()
	s.SignIn()
	j := &joinRig{t: t, s: s, talk: filepath.Join(t.TempDir(), "transcript.jsonl")}
	s.Change(func(it *factory.Item) { it.Talk = j.talk })
	// THE APP'S SERVICE TAKES THE APP'S BEARER AND NO OTHER; the scenario's
	// takes none, for its own runner and person. The front stands where the
	// app's check stands: it passes on only what carries the bearer.
	front := httptest.NewServer(&httputil.ReverseProxy{Rewrite: func(r *httputil.ProxyRequest) {
		target, _ := url.Parse(s.URL())
		r.SetURL(target)
		if r.In.Header.Get("Authorization") != "Bearer "+joinBearer {
			t.Errorf("a press reached the factory without the app's bearer: %s", r.In.URL.Path)
		}
		r.Out.Header.Del("Authorization")
	}})
	t.Cleanup(front.Close)
	hub := testFactoryHub(t)
	if err := hub.showLocal(appLocalID, front.URL, true); err != nil {
		t.Fatal(err)
	}
	hub.hold(appLocalID, joinBearer, 0)
	backend := newFakeBackend(newAppRecorder(), nil)
	backend.factoryOnce.Do(func() { backend.factories = hub })
	t.Cleanup(func() { backend.Close() })
	t.Cleanup(installAppLocalPress(backend))
	door, ok := runDoor(s.Store, t.TempDir(), "").(session.ManagerPresser)
	if !ok {
		t.Fatal("the floor's run door cannot press")
	}
	j.door = door
	return j
}

// joinTurn is one turn of the manager's conversation: the person's own words
// in it, "" on a turn the engine started, and when it began.
type joinTurn struct {
	j    *joinRig
	said string
	at   time.Time
}

// turn begins a turn now. said is what the person typed to begin it; a turn
// the engine began (a stop, a stall, a step's question) has none.
func (j *joinRig) turn(said string) joinTurn { return joinTurn{j: j, said: said, at: j.s.Now()} }

// press is one press of a tool in the turn, with what the session adds to
// every press: the person's words of the turn and when the turn began.
func (t joinTurn) press(p session.ManagerPress) (string, error) {
	p.Said, p.TurnAt = t.said, t.at
	return t.j.door.Press(context.Background(), t.j.talk, p)
}

// must is press for a press the test expects to be taken.
func (t joinTurn) must(p session.ManagerPress) string {
	t.j.t.Helper()
	line, err := t.press(p)
	if err != nil {
		t.j.t.Fatalf("%s was refused: %v\n%s", p.Action, err, t.j.s.Story())
	}
	return line
}

// refused is press for a press the test expects to be refused with words
// that hold want, having changed nothing: the job's revision is as it was.
func (t joinTurn) refused(p session.ManagerPress, want string) string {
	t.j.t.Helper()
	before, _ := t.j.s.Store.Revision(t.j.s.Job)
	line, err := t.press(p)
	if err == nil {
		t.j.t.Fatalf("%s was taken: %q\n%s", p.Action, line, t.j.s.Story())
	}
	if !strings.HasPrefix(err.Error(), "Nothing changed: ") || !strings.Contains(err.Error(), want) {
		t.j.t.Fatalf("%s was refused with %q, want a refusal that says %q\n%s", p.Action, err, want, t.j.s.Story())
	}
	// THE REFUSAL IS A SENTENCE: the wire's code word for it ("conflict: ",
	// "bad_params: ") is the app's, and is not read to the person.
	if joinCodeWord.MatchString(err.Error()) {
		t.j.t.Fatalf("%s was refused with the wire's code word in the sentence: %q", p.Action, err)
	}
	if after, _ := t.j.s.Store.Revision(t.j.s.Job); after != before {
		t.j.t.Fatalf("%s was refused and still wrote the job\n%s", p.Action, t.j.s.Story())
	}
	return err.Error()
}

// joinCodeWord is a refusal that begins with a wire code word.
var joinCodeWord = regexp.MustCompile(`^Nothing changed: [a-z_]+: `)

// acts is the press `factory_act` makes.
func joinActs(why string, acts ...session.PublicAct) session.ManagerPress {
	return session.ManagerPress{Action: session.ActionAct, Words: why, Acts: acts}
}

func joinReply(thread, text string) session.PublicAct {
	return session.PublicAct{Kind: session.ActReply, Thread: thread, Text: text}
}

// joinReviewed is a rig at an open pull request with one review thread read:
// the reviewer's comment set a Follow PR round off, and its repair is pushed
// (Auto) or waits for the person's approval to be pushed (Manual). It answers
// the thread's name as the job stores it.
func joinReviewed(t *testing.T, mode string) (*joinRig, string, factory.Item) {
	t.Helper()
	j := newJoin(t, mode, "write", "proof")
	j.s.Open()
	thread := j.s.GitHub.AddThread("bob", "COLLABORATOR", "Rename balanced to settled.")
	it := j.s.Do("a reviewer comments on the code and GitHub is read", j.s.Poll)
	return j, thread, it
}

// actOf is the job's stored act of kind, the newest first.
func actOf(t *testing.T, it factory.Item, kind string) factory.PublicAct {
	t.Helper()
	for i := len(it.Acts) - 1; i >= 0; i-- {
		if it.Acts[i].Kind == kind {
			return it.Acts[i]
		}
	}
	t.Fatalf("the job stores no %s act: %+v", kind, it.Acts)
	return factory.PublicAct{}
}

// writesOf counts the scripted GitHub's writes that begin with prefix.
func writesOf(s *factorytest.Scenario, prefix string) int {
	n := 0
	for _, w := range s.GitHub.Writes() {
		if strings.HasPrefix(w, prefix) {
			n++
		}
	}
	return n
}

// AUTO: `factory_act` WITH A REPLY IS STORED APPROVED AND POSTED ONCE. The
// press comes from a turn the engine started, with no word of the person's.
// The service stores the act approved by the main agent, its own sender posts
// it to GitHub once, as the person, with the footer, and asking again posts
// nothing more.
func TestManagerJoinAutoReplyIsApprovedAndPostedOnce(t *testing.T) {
	j, thread, it := joinReviewed(t, factory.AutonomyAuto)
	if it.State != factory.StateShipped || it.Wait.Who != factory.WaitGitHub {
		t.Fatalf("Auto did not push the repair: %s\n%s", factorytest.WaitKey(it), j.s.Story())
	}
	var line string
	it = j.s.Do("the manager replies through its tool", func() {
		line = j.turn("").must(joinActs("the rename is in", joinReply(thread, "Renamed it to settled.")))
		j.s.ActsSent()
	})
	if !strings.Contains(line, "Accepted: 1 public act (reply)") || !strings.Contains(line, "This job is in Auto") {
		t.Errorf("the tool read back %q", line)
	}
	act := actOf(t, it, factory.ActKindReply)
	if act.State != factory.ActDone || act.By != factory.ActByMainAgent || act.ApprovedBy != factory.ActByMainAgent || act.Thread != thread || act.Why != "the rename is in" || act.URL == "" {
		t.Fatalf("the service stored %+v\n%s", act, j.s.Story())
	}
	want := forge.PublicationMarker + "\nRenamed it to settled.\n\n" + factory.PublicActFooter
	if posted := j.s.GitHub.Posted(); len(posted) != 1 || posted[0] != want {
		t.Fatalf("posted %q, want %q once\n%s", posted, want, j.s.Story())
	}
	j.s.Do("the manager asks for the same reply again", func() {
		j.turn("").must(joinActs("the rename is in", joinReply(thread, "Renamed it to settled.")))
		j.s.ActsSent()
	})
	if len(j.s.GitHub.Posted()) != 1 || writesOf(j.s, "reply "+thread) != 1 {
		t.Fatalf("the reply was posted twice: %v\n%s", j.s.GitHub.Writes(), j.s.Story())
	}
}

// MANUAL: `factory_act` WITH A REPLY IS STORED PROPOSED AND NOTHING IS SENT.
// The job reads "you: approve_acts". The manager may not approve what it
// proposed in the same turn, whatever the person said to begin that turn; on
// the person's next turn their yes sends it, once, with the footer.
func TestManagerJoinManualReplyWaitsForThePersonsYes(t *testing.T) {
	j, thread, it := joinReviewed(t, factory.AutonomyManual)
	if it.Wait.Action != factory.ActApprovePublish {
		t.Fatalf("Manual did not wait to push the repair: %s\n%s", factorytest.WaitKey(it), j.s.Story())
	}
	it = j.s.Do("the person tells the manager to push, and it approves", func() {
		j.turn("yes, push the fix").must(session.ManagerPress{Action: session.ActionApprove, Words: "they said to push the fix"})
	})
	if it.State != factory.StateShipped || it.Wait.Who != factory.WaitGitHub {
		t.Fatalf("the approved repair was not pushed: %s\n%s", factorytest.WaitKey(it), j.s.Story())
	}
	asked := j.turn("answer bob's comment and tell him it is ready")
	var line string
	it = j.s.Do("the person asks for a reply, and the manager proposes it", func() {
		line = asked.must(joinActs("bob's rename is in", joinReply(thread, "Renamed it to settled."), session.PublicAct{Kind: session.ActComment, Text: "Ready for another look."}))
		j.s.ActsSent()
	})
	if !strings.Contains(line, "Proposed: 2 public acts (reply, comment)") || !strings.Contains(line, "nothing is sent until the person approves") {
		t.Errorf("the tool read back %q", line)
	}
	if it.Wait.Who != factory.WaitYou || it.Wait.Action != factory.ActApproveActs || it.Wait.Words != "Approve: post 1 reply and 1 comment?" {
		t.Fatalf("the job reads %s %q\n%s", factorytest.WaitKey(it), it.Wait.Words, j.s.Story())
	}
	for _, act := range it.Acts {
		if act.State != factory.ActProposed || act.By != factory.ActByMainAgent || act.ApprovedBy != "" {
			t.Fatalf("the service stored %+v\n%s", act, j.s.Story())
		}
	}
	// THE SAME TURN: the person's words began it before the acts existed.
	asked.refused(session.ManagerPress{Action: session.ActionApproveActs}, "you proposed these in this same turn")
	// A TURN THE ENGINE BEGAN has no word of theirs at all.
	j.turn("").refused(session.ManagerPress{Action: session.ActionApproveActs}, "approving public acts is the person's")
	if posted := j.s.GitHub.Posted(); len(posted) != 0 {
		t.Fatalf("posted before the person's yes: %q\n%s", posted, j.s.Story())
	}
	it = j.s.Do("the person says yes, and the manager approves", func() {
		line = j.turn("yes, post both").must(session.ManagerPress{Action: session.ActionApproveActs})
		j.s.ActsSent()
	})
	if !strings.Contains(line, "approved, as the person") {
		t.Errorf("the tool read back %q", line)
	}
	posted := j.s.GitHub.Posted()
	if len(posted) != 2 || posted[0] != forge.PublicationMarker+"\nRenamed it to settled.\n\n"+factory.PublicActFooter || !strings.HasSuffix(posted[1], "\n\n"+factory.PublicActFooter) {
		t.Fatalf("posted %q\n%s", posted, j.s.Story())
	}
	for _, act := range it.Acts {
		if act.State != factory.ActDone || act.ApprovedBy != "me" {
			t.Fatalf("after the yes the service stores %+v\n%s", act, j.s.Story())
		}
	}
	if it.Wait.Who != factory.WaitGitHub {
		t.Fatalf("after the acts the job waits on %s\n%s", factorytest.WaitKey(it), j.s.Story())
	}
}

// MANUAL: DECLINED ACTS ARE NEVER SENT, and the manager may decline by
// itself: a no sends nothing, so it needs no word of the person's.
func TestManagerJoinDeclinedActsAreNeverSent(t *testing.T) {
	j, thread, _ := joinReviewed(t, factory.AutonomyManual)
	it := j.s.Do("the manager proposes a reply beside the waiting push", func() {
		j.turn("").must(joinActs("bob's rename is in", joinReply(thread, "Renamed it to settled.")))
		j.s.ActsSent()
	})
	if it.Wait.Action != factory.ActApprovePublish || !strings.Contains(it.Wait.Words, "Approving also approves: post 1 reply.") {
		t.Fatalf("the job reads %s %q\n%s", factorytest.WaitKey(it), it.Wait.Words, j.s.Story())
	}
	var line string
	it = j.s.Do("the manager declines them", func() {
		line = j.turn("").must(session.ManagerPress{Action: session.ActionDeclineActs, Words: "Bob withdrew the comment."})
		j.s.ActsSent()
	})
	act := actOf(t, it, factory.ActKindReply)
	if act.State != factory.ActDeclined || act.Result != "Bob withdrew the comment." || !strings.Contains(line, "Nothing was sent") {
		t.Fatalf("the service stored %+v, the tool read back %q\n%s", act, line, j.s.Story())
	}
	it = j.s.Do("the person approves the push", func() { j.s.MustPress(factory.DoApprove) })
	it = j.s.Do("GitHub is read", j.s.Poll)
	if len(j.s.GitHub.Posted()) != 0 || it.State != factory.StateShipped {
		t.Fatalf("a declined act was sent, or the push was not: posted %q, %s\n%s", j.s.GitHub.Posted(), factorytest.WaitKey(it), j.s.Story())
	}
	// With nothing waiting the door says so in the service's own sentence.
	if got := j.turn("").refused(session.ManagerPress{Action: session.ActionDeclineActs, Words: "again"}, "No act is waiting"); got != "Nothing changed: No act is waiting for approval." {
		t.Fatalf("the refusal read %q", got)
	}
}

// EVERY OTHER KIND OF ACT, THROUGH THE REAL DOOR, REACHES GITHUB AS WRITTEN.
// One call carries a round's acts in order, as the tool asks for them; the
// service carries each out with the person's sign-in and the job reads what
// each did.
func TestManagerJoinEveryKindOfActIsCarriedOut(t *testing.T) {
	j, thread, _ := joinReviewed(t, factory.AutonomyAuto)
	before := len(j.s.GitHub.Writes())
	it := j.s.Do("the manager edits the pull request, comments, asks for a review, resolves the thread and marks it ready", func() {
		j.turn("").must(joinActs("the review round is done",
			session.PublicAct{Kind: session.ActEditPR, Title: "Keep the ledger settled", Body: "Renames balanced to settled.\n\nCloses #12."},
			session.PublicAct{Kind: session.ActComment, Text: "Ready for another look."},
			session.PublicAct{Kind: session.ActRerequestReview, Reviewers: []string{"bob"}},
			session.PublicAct{Kind: session.ActResolveThread, Thread: thread},
			session.PublicAct{Kind: session.ActMarkReady},
		))
		j.s.ActsSent()
	})
	for _, act := range it.Acts {
		if act.State != factory.ActDone {
			t.Fatalf("%s is %s (%s)\n%s", act.Kind, act.State, act.Error, j.s.Story())
		}
	}
	writes := j.s.GitHub.Writes()[before:]
	want := []string{"edit pr #7 title body", "comment #7", "rerequest bob", "resolve " + thread, "ready "}
	if len(writes) != len(want) {
		t.Fatalf("GitHub was written %v, want %v\n%s", writes, want, j.s.Story())
	}
	for i := range want {
		if !strings.HasPrefix(writes[i], want[i]) {
			t.Fatalf("GitHub was written %v, want %v in that order\n%s", writes, want, j.s.Story())
		}
	}
	pull, _ := j.s.GitHub.PullRequest()
	if pull.Title != "Keep the ledger settled" || pull.Body != "Renames balanced to settled.\n\nCloses #12." || pull.Draft || !j.s.GitHub.Resolved(thread) {
		t.Fatalf("GitHub's pull request reads %q %q draft=%v\n%s", pull.Title, pull.Body, pull.Draft, j.s.Story())
	}
	if it.PullRequest.Draft {
		t.Fatalf("the job still reads its pull request as a draft\n%s", j.s.Story())
	}
	if posted := j.s.GitHub.Posted(); len(posted) != 1 || posted[0] != forge.PublicationMarker+"\nReady for another look.\n\n"+factory.PublicActFooter {
		t.Fatalf("posted %q\n%s", posted, j.s.Story())
	}
	// A CHANGE OF BASE changes the base the job stores.
	j.s.Branch("dev")
	it = j.s.Do("the manager changes the base", func() {
		j.turn("").must(joinActs("the work belongs on dev", session.PublicAct{Kind: session.ActChangeBase, Base: "dev"}))
		j.s.ActsSent()
	})
	if act := actOf(t, it, factory.ActKindChangeBase); act.State != factory.ActDone || it.PullRequest.BaseBranch != "dev" {
		t.Fatalf("the change of base: %+v, the job's base %q\n%s", act, it.PullRequest.BaseBranch, j.s.Story())
	}
	if pull, _ = j.s.GitHub.PullRequest(); pull.Base != "dev" {
		t.Fatalf("GitHub's base is %q\n%s", pull.Base, j.s.Story())
	}
	// AN ACT THE JOB CANNOT TAKE IS REFUSED WHOLE, in the service's sentence,
	// and names which act of the call it was.
	got := j.turn("").refused(joinActs("one more",
		session.PublicAct{Kind: session.ActComment, Text: "One more thing."},
		joinReply("inline:9999", "No such thread."),
	), "Act 2 (reply): This job's pull request has no thread inline:9999. Nothing was kept or sent.")
	if len(j.s.GitHub.Posted()) != 1 {
		t.Fatalf("a refused call posted its first act: %q", got)
	}
}

// FAILED CHECKS ARE RUN AGAIN on the manager's ask, on a job that is stopped:
// an act is taken in any state of the job.
func TestManagerJoinRerunChecksOnAStoppedJob(t *testing.T) {
	j := newJoin(t, factory.AutonomyAuto, "write", "proof")
	j.s.Open()
	j.s.Delegate.Always(factory.FollowPRStep, factorytest.Round{Result: factorytest.NeedsYou("The failing check is flaky infrastructure, not this change.")})
	it := j.s.Do("a check fails and Follow PR hands it to the person", func() {
		j.s.GitHub.SetChecks(1, 1, 0)
		j.s.Poll()
	})
	if it.Wait.Who != factory.WaitYou || it.PullRequest.Checks.Failed != 1 {
		t.Fatalf("not the stop: %s, checks %+v\n%s", factorytest.WaitKey(it), it.PullRequest.Checks, j.s.Story())
	}
	it = j.s.Do("the manager runs the failed checks again", func() {
		j.turn("").must(joinActs("lint is flaky", session.PublicAct{Kind: session.ActRerunChecks}))
		j.s.ActsSent()
	})
	if act := actOf(t, it, factory.ActKindRerunChecks); act.State != factory.ActDone || !strings.Contains(act.Result, "Asked GitHub to run again: lint-0.") || writesOf(j.s, "rerun checks lint-0") != 1 {
		t.Fatalf("the rerun: %+v, writes %v\n%s", act, j.s.GitHub.Writes(), j.s.Story())
	}
}

// A PUSH IS THE PUBLISH APPROVAL, NEVER AN ACT OF ITS OWN. In Manual, with
// the approval waiting, the manager's `push` leaves it waiting on the person
// and sends nothing; with nothing checked and ready it is refused in words.
func TestManagerJoinPushIsThePublishApproval(t *testing.T) {
	j, thread, it := joinReviewed(t, factory.AutonomyManual)
	if it.Wait.Action != factory.ActApprovePublish {
		t.Fatalf("Manual did not wait to push the repair: %s\n%s", factorytest.WaitKey(it), j.s.Story())
	}
	pushes := writesOf(j.s, "push ")
	var line string
	it = j.s.Do("the manager asks to push, with a reply to follow it", func() {
		line = j.turn("").must(joinActs("the repair is checked", session.PublicAct{Kind: session.ActPush}, joinReply(thread, "Renamed it; it is in the push.")))
		j.s.ActsSent()
	})
	if it.Wait.Who != factory.WaitYou || it.Wait.Action != factory.ActApprovePublish || !strings.Contains(it.Wait.Words, "Approving also approves: post 1 reply.") {
		t.Fatalf("the job reads %s %q\n%s", factorytest.WaitKey(it), it.Wait.Words, j.s.Story())
	}
	if writesOf(j.s, "push ") != pushes || len(j.s.GitHub.Posted()) != 0 || len(it.Acts) != 1 || it.Acts[0].Kind != factory.ActKindReply || it.Acts[0].State != factory.ActProposed {
		t.Fatalf("something went out, or the push was kept as an act: %v, acts %+v\n%s", j.s.GitHub.Writes(), it.Acts, j.s.Story())
	}
	// The tool says what became of each: the reply is proposed, and the push
	// is not an act to approve but the publish approval, still the person's.
	if !strings.Contains(line, "Proposed: 1 public act (reply)") || !strings.Contains(line, "The push was not sent. The push approval is already waiting on the person. "+pushWaits) {
		t.Errorf("the tool read back %q", line)
	}
	// THE PERSON'S ONE YES COVERS THE PUSH AND THE REPLY: the reply goes out
	// after the push has landed.
	it = j.s.Do("the person says yes, and the manager approves the push", func() {
		j.turn("yes, push it and reply").must(session.ManagerPress{Action: session.ActionApprove, Words: "they said yes to the push and the reply"})
		j.s.Settle()
		j.s.ActsSent()
	})
	writes := j.s.GitHub.Writes()
	lastPush, replied := -1, -1
	for i, w := range writes {
		if strings.HasPrefix(w, "push ") {
			lastPush = i
		}
		if strings.HasPrefix(w, "reply "+thread) {
			replied = i
		}
	}
	if writesOf(j.s, "push ") != pushes+1 || replied < lastPush || len(j.s.GitHub.Posted()) != 1 || it.Acts[0].State != factory.ActDone || it.Acts[0].ApprovedBy != "me" {
		t.Fatalf("the reply did not follow the approved push: %v, acts %+v\n%s", writes, it.Acts, j.s.Story())
	}
	// NOTHING CHECKED AND READY: the push is refused, and says so.
	j.turn("").refused(joinActs("push again", session.PublicAct{Kind: session.ActPush}), "Nothing is checked and ready to push.")
	j.turn("").refused(joinActs("open it", session.PublicAct{Kind: session.ActOpenPR}), "Nothing is checked and ready to push.")
}

// A MERGE ACT ONLY OFFERS THE MERGE. Whatever the mode and whoever spoke, the
// tool never sends a merge: the job's Merge card is shown to the person
// again, and nothing is merged. Before the job offers Merge the tool says why
// in the gate's own words.
func TestManagerJoinMergeActOnlyOffersTheMerge(t *testing.T) {
	j := newJoin(t, factory.AutonomyAuto, "write", "proof")
	it := j.s.Open()
	j.turn("merge it").refused(joinActs("they asked to merge", session.PublicAct{Kind: session.ActMerge}), "merge is not offered")
	it = j.s.Do("GitHub goes green and is read", func() {
		j.s.GitHub.SetChecks(2, 0, 0)
		j.s.Poll()
	})
	if it.Wait.Who != factory.WaitYou || it.Wait.Action != factory.ActMerge {
		t.Fatalf("the job does not offer the merge: %s\n%s", factorytest.WaitKey(it), j.s.Story())
	}
	var line string
	it = j.s.Do("the person asks to merge, and the manager's act offers it", func() {
		line = j.turn("merge it").must(joinActs("they asked to merge", session.PublicAct{Kind: session.ActMerge}))
	})
	if it.MergeOfferedAt.IsZero() || it.Merging != nil || writesOf(j.s, "merge ") != 0 || j.s.GitHub.Open() == false {
		t.Fatalf("the merge act: offered at %v, merging %+v, writes %v\n%s", it.MergeOfferedAt, it.Merging, j.s.GitHub.Writes(), j.s.Story())
	}
	if !strings.Contains(line, "Nothing is merged") || len(it.Acts) != 0 {
		t.Fatalf("the tool read back %q, acts %+v", line, it.Acts)
	}
}

// joinTask is the press `factory_task` makes.
func joinTask(words string, now bool) session.ManagerPress {
	return session.ManagerPress{Action: session.ActionTask, Words: words, Now: now}
}

const joinTaskWords = "Add a note on the ledger's rounding to the README."

// taskPhase is the run's step named name, as the service stores it.
func taskPhase(it factory.Item, name string) (factory.Phase, bool) {
	if it.Stream == nil {
		return factory.Phase{}, false
	}
	for _, ph := range it.Stream.Phases {
		if ph.Name == name {
			return ph, true
		}
	}
	return factory.Phase{}, false
}

// `factory_task` ON A RUNNING JOB waits for the round running now, or cuts it
// when the manager says now. The service stores the task as a step of the
// run with the instructions whole and the main agent as who asked.
func TestManagerJoinTaskOnARunningJob(t *testing.T) {
	for _, now := range []bool{false, true} {
		name := "after the round"
		if now {
			name = "now"
		}
		t.Run(name, func(t *testing.T) {
			j := newJoin(t, factory.AutonomyAuto, "write", "test", "proof")
			entered, release := j.s.HoldRound("write", factorytest.Round{})
			defer release()
			j.s.Events = append(j.s.Events, "start, and the manager sends a task while write's first round runs")
			j.s.MustPress(factory.DoStart)
			j.s.Begun(entered)
			line := j.turn("").must(joinTask(joinTaskWords, now))
			if !strings.Contains(line, "Task taken") || strings.Contains(line, "interrupts the round") != now {
				t.Errorf("the tool read back %q", line)
			}
			if !now {
				it := j.s.Item()
				ph, ok := taskPhase(it, "task")
				if !ok || ph.Task == nil || ph.Task.Words != joinTaskWords || ph.Task.By != factory.TaskByMainAgent || ph.State != factory.PhasePending {
					t.Fatalf("the service stored the task as %+v\n%s", ph, j.s.Story())
				}
				if it.Wait.Who != factory.WaitWorking || it.Wait.What != "write" {
					t.Fatalf("the task took the job off the round it waits for: %s\n%s", factorytest.WaitKey(it), j.s.Story())
				}
				j.s.Check()
				release()
			}
			it := j.s.Do("the task runs and the job goes on", nil)
			want := "write#1 task#1 test#1 proof#1"
			if now {
				// The cut round runs again after the task.
				want = "write#1 task#1 write#1 test#1 proof#1"
			}
			if got := j.s.Delegate.Sequence(); got != want {
				t.Fatalf("rounds ran as %s, want %s\n%s", got, want, j.s.Story())
			}
			if it.PullRequest == nil {
				t.Fatalf("the job did not go on to open its pull request: %s\n%s", factorytest.WaitKey(it), j.s.Story())
			}
		})
	}
}

// `factory_task` ON A LANDED JOB withdraws the approval it waited for: the
// task runs, the job lands on a new commit, and in Manual that commit waits
// for the person again. Nothing is published by the task.
func TestManagerJoinTaskOnALandedJob(t *testing.T) {
	j := newJoin(t, factory.AutonomyManual, "write", "proof")
	it := j.s.Do("start", func() { j.s.MustPress(factory.DoStart) })
	if it.State != factory.StateLanded || it.Wait.Action != factory.ActApprovePublish {
		t.Fatalf("the job did not land and wait for its approval: %s %s\n%s", it.State, factorytest.WaitKey(it), j.s.Story())
	}
	landed := it.HeadCommit
	j.s.Delegate.Next("task", factorytest.Round{Edit: true})
	it = j.s.Do("the manager sends a task to the landed job", func() { j.turn("").must(joinTask(joinTaskWords, false)) })
	if j.s.Delegate.Rounds("task") != 1 || it.HeadCommit == landed || it.HeadCommit == "" {
		t.Fatalf("the task did not run, or left the commit as it was: %d rounds, %s then %s\n%s", j.s.Delegate.Rounds("task"), landed, it.HeadCommit, j.s.Story())
	}
	if it.State != factory.StateLanded || it.Wait.Who != factory.WaitYou || it.Wait.Action != factory.ActApprovePublish || j.s.GitHub.Open() {
		t.Fatalf("the new commit does not wait for the person's approval: %s %s\n%s", it.State, factorytest.WaitKey(it), j.s.Story())
	}
}

// `factory_task` ON A JOB WHOSE PULL REQUEST IS OPEN is a Follow PR round
// with the instructions as its request, and its commit goes through the
// approval every repair push goes through.
func TestManagerJoinTaskOnAnOpenPullRequest(t *testing.T) {
	j := newJoin(t, factory.AutonomyManual, "write", "proof")
	j.s.Open()
	opened, _ := j.s.GitHub.PullRequest()
	rounds := j.s.Delegate.Rounds(factory.FollowPRStep)
	it := j.s.Do("the manager sends a task, and GitHub is read", func() {
		j.turn("").must(joinTask(joinTaskWords, false))
		j.s.Poll()
	})
	if j.s.Delegate.Rounds(factory.FollowPRStep) != rounds+1 || !j.s.Delegate.Heard(factory.FollowPRStep, 0, joinTaskWords) || j.s.Delegate.Rounds("task") != 0 {
		t.Fatalf("the task was not a Follow PR round with its instructions: %s\n%s", j.s.Delegate.Sequence(), j.s.Story())
	}
	if it.Wait.Who != factory.WaitYou || it.Wait.Action != factory.ActApprovePublish {
		t.Fatalf("in Manual the task's commit does not wait for its push approval: %s\n%s", factorytest.WaitKey(it), j.s.Story())
	}
	if pull, _ := j.s.GitHub.PullRequest(); pull.HeadSHA != opened.HeadSHA {
		t.Fatalf("the task's commit was pushed before its approval\n%s", j.s.Story())
	}
	// A TASK THE SERVICE REFUSES comes back in the service's own sentence.
	// The scripted delegate spends nothing, so the money is written here.
	j.s.Do("the person approves the push", func() { j.s.MustPress(factory.DoApprove) })
	j.s.Change(func(it *factory.Item) { it.Stream.Spent = it.Cap })
	if got := j.turn("").refused(joinTask(joinTaskWords, false), "budget"); got != "Nothing changed: Job budget reached. Raise the budget before sending a task." {
		t.Fatalf("the refusal read %q", got)
	}
}

// joinDecision is the press `factory_answer` makes with one decision on a
// stop: the decision as its action, the step it names, and the manager's
// instructions or reason.
func joinDecision(decision, step, words string) session.ManagerPress {
	return session.ManagerPress{Action: decision, Step: step, Words: words}
}

// stopPresses keeps what the manager's turn on a stop pressed and was told.
type stopPresses struct {
	mu    sync.Mutex
	turns int
	lines []string
	errs  []string
}

func (p *stopPresses) took(line string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lines = append(p.lines, line)
	if err != nil {
		p.errs = append(p.errs, err.Error())
	} else {
		p.errs = append(p.errs, "")
	}
}

func (p *stopPresses) all() (turns int, lines, errs []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.turns, append([]string(nil), p.lines...), append([]string(nil), p.errs...)
}

// decideOnStops gives the scripted main agent one turn on every stop handed
// to it: A TURN THE ENGINE BEGAN, with no word of the person's, that presses
// each of presses through the manager's real door, in order.
func (j *joinRig) decideOnStops(presses func(it factory.Item) []session.ManagerPress) *stopPresses {
	kept := &stopPresses{}
	j.s.StopAgent(func(it factory.Item, _ factory.Wait) (string, error) {
		kept.mu.Lock()
		kept.turns++
		kept.mu.Unlock()
		turn := j.turn("")
		for _, p := range presses(it) {
			kept.took(turn.press(p))
		}
		return "Settled.", nil
	})
	return kept
}

// sayYesUntil starts the job and says yes for the person until there holds.
func (j *joinRig) sayYesUntil(there func(factory.Item) bool) factory.Item {
	j.t.Helper()
	it := j.s.Do("start", func() { j.s.MustPress(factory.DoStart) })
	for step := 0; step < 10 && !there(it); step++ {
		if it.Wait.Who != factory.WaitYou {
			break
		}
		it = j.s.Do("the person says yes", func() { j.s.Yes() })
	}
	if !there(it) {
		j.t.Fatalf("the job did not get there: %s\n%s", factorytest.WaitKey(it), j.s.Story())
	}
	return it
}

// joinNotGreen is a test round that reports failing tests.
func joinNotGreen() *factory.StageResult {
	return &factory.StageResult{Done: false, Output: "2 tests fail in the ledger package.", Findings: 2,
		Tests: &factory.TestCounts{Command: "go test ./...", Passed: 98, Failed: 2}}
}

// joinLogged says the job's log has a line with these words.
func joinLogged(it factory.Item, words string) bool {
	if it.Stream == nil {
		return false
	}
	for _, l := range it.Stream.Log {
		if strings.Contains(l.Text, words) {
			return true
		}
	}
	return false
}

// A STEP OUT OF ROUNDS, DECIDED THROUGH `factory_answer`. The stop is handed
// to the main agent, whose turn presses one decision through its real door.
// A decision that does not fit the stop is refused first, in the service's
// own sentence, and changes nothing; the one that fits moves the job as it
// says, and the person is never asked.
func TestManagerJoinDecisionsOnAStepOutOfRounds(t *testing.T) {
	const words = "Both failures are on the base branch too; run only the ledger package."
	for _, decision := range []string{session.ActionAnotherRound, session.ActionSendBack, session.ActionAccept} {
		t.Run(decision, func(t *testing.T) {
			j := newJoin(t, factory.AutonomyManual, "write", "test", "proof")
			j.s.Delegate.Next("test", factorytest.Round{Result: joinNotGreen()}, factorytest.Round{Result: joinNotGreen()}, factorytest.Round{Result: joinNotGreen()})
			kept := j.decideOnStops(func(factory.Item) []session.ManagerPress {
				return []session.ManagerPress{
					joinDecision(session.ActionRetryPublish, "", ""),
					joinDecision(decision, "write", words),
					joinDecision(decision, "test", words),
				}
			})
			it := j.sayYesUntil(func(it factory.Item) bool { return j.s.Delegate.Rounds("proof") > 0 })
			turns, lines, errs := kept.all()
			if turns != 1 || len(errs) != 3 {
				t.Fatalf("the main agent had %d turns and pressed %v\n%s", turns, errs, j.s.Story())
			}
			if !strings.HasPrefix(errs[0], "Nothing changed: retry_publish does not fit this stop. It takes: another_round, send_back, rerun") {
				t.Errorf("a decision that does not fit was answered %q", errs[0])
			}
			if errs[1] != "Nothing changed: This job is stopped at test, not at write. Read its status again before deciding" {
				t.Errorf("a decision for another step's stop was answered %q", errs[1])
			}
			if errs[2] != "" || !strings.Contains(lines[2], "This confirms the decision, not its outcome") {
				t.Fatalf("%s was answered %q (%s)\n%s", decision, lines[2], errs[2], j.s.Story())
			}
			write, test := j.s.Delegate.Rounds("write"), j.s.Delegate.Rounds("test")
			switch decision {
			case session.ActionAnotherRound:
				if test != 4 || write != 1 || !j.s.Delegate.Heard("test", 4, words) || !joinLogged(it, "Main agent: another round · "+words) {
					t.Fatalf("after another round: test ran %d, write %d\n%s", test, write, j.s.Story())
				}
			case session.ActionSendBack:
				if write != 2 || test != 4 || !j.s.Delegate.Heard("write", 2, words) || !joinLogged(it, "Main agent: sent back · "+words) {
					t.Fatalf("after the send back: write ran %d, test %d\n%s", write, test, j.s.Story())
				}
			case session.ActionAccept:
				if test != 3 || write != 1 || !joinLogged(it, "Main agent: accepted the step as it stands · "+words) {
					t.Fatalf("after the accept: test ran %d, write %d\n%s", test, write, j.s.Story())
				}
			}
			for _, e := range j.s.Events {
				if strings.HasSuffix(e, "-> you:answer") {
					t.Fatalf("the person was asked about a stop the main agent settled: %s\n%s", e, j.s.Story())
				}
			}
		})
	}
}

// A FAILED STEP IS RUN AGAIN through `factory_answer` rerun, with the step
// named or left out for the one the job stopped on.
func TestManagerJoinRerunOnAFailedStep(t *testing.T) {
	for _, step := range []string{"write", ""} {
		t.Run("step "+step, func(t *testing.T) {
			j := newJoin(t, factory.AutonomyAuto, "write", "proof")
			j.s.Delegate.Next("write", factorytest.Round{Err: errors.New("the model refused the request: invalid tool schema")})
			const words = "The tool schema is fixed; run it again."
			kept := j.decideOnStops(func(factory.Item) []session.ManagerPress {
				return []session.ManagerPress{joinDecision(session.ActionAnotherRound, "", words), joinDecision(session.ActionRerun, step, words)}
			})
			it := j.sayYesUntil(func(it factory.Item) bool { return j.s.Delegate.Rounds("proof") > 0 })
			_, _, errs := kept.all()
			if len(errs) != 2 || !strings.HasPrefix(errs[0], "Nothing changed: another_round does not fit this stop. It takes: rerun") || errs[1] != "" {
				t.Fatalf("the main agent's presses were answered %q\n%s", errs, j.s.Story())
			}
			if j.s.Delegate.Rounds("write") != 2 || it.Failure != nil || !joinLogged(it, "Main agent: ran the step again · "+words) {
				t.Fatalf("write ran %d rounds, failure %+v\n%s", j.s.Delegate.Rounds("write"), it.Failure, j.s.Story())
			}
		})
	}
}

// A FOLLOW PR STOP is resumed with instructions, or put back to waiting on
// GitHub, through `factory_answer`.
func TestManagerJoinDecisionsOnAFollowPRStop(t *testing.T) {
	const words = "Leave the dependency audit alone. Fix only the lint failure."
	for _, decision := range []string{session.ActionResumePipeline, session.ActionWaitGitHub} {
		t.Run(decision, func(t *testing.T) {
			j := newJoin(t, factory.AutonomyAuto, "write", "proof")
			j.s.Open()
			j.s.Delegate.Next(factory.FollowPRStep, factorytest.Round{Result: factorytest.NeedsYou("The failing dependency audit predates this change; it needs a scope decision.")})
			kept := j.decideOnStops(func(factory.Item) []session.ManagerPress {
				return []session.ManagerPress{joinDecision(session.ActionAccept, "", words), joinDecision(decision, "", words)}
			})
			rounds := j.s.Delegate.Rounds(factory.FollowPRStep)
			it := j.s.Do("a check fails and Follow PR asks what is in scope", func() {
				j.s.GitHub.SetChecks(1, 1, 0)
				j.s.Poll()
			})
			turns, _, errs := kept.all()
			if turns != 1 || len(errs) != 2 || !strings.HasPrefix(errs[0], "Nothing changed: accept does not fit this stop. It takes: resume_pipeline, wait_github") || errs[1] != "" {
				t.Fatalf("the main agent had %d turns and was answered %q\n%s", turns, errs, j.s.Story())
			}
			ran := j.s.Delegate.Rounds(factory.FollowPRStep) - rounds
			if decision == session.ActionWaitGitHub {
				if it.Wait.Who != factory.WaitGitHub || ran != 1 || it.Question != "" || !joinLogged(it, "Main agent: went back to waiting on GitHub") {
					t.Fatalf("after wait_github: %s, %d rounds, question %q\n%s", factorytest.WaitKey(it), ran, it.Question, j.s.Story())
				}
				return
			}
			if ran != 2 || !j.s.Delegate.Heard(factory.FollowPRStep, 0, words) || !joinLogged(it, "Main agent: resumed the pipeline · "+words) {
				t.Fatalf("after resume_pipeline: %d rounds\n%s", ran, j.s.Story())
			}
			if it.State != factory.StateShipped || it.PullRequest.HeadSHA != it.HeadCommit {
				t.Fatalf("in Auto the resumed round's repair was not pushed: %s\n%s", factorytest.WaitKey(it), j.s.Story())
			}
		})
	}
}

// A PUBLISH THAT FAILED AFTER THE PERSON APPROVED IT is retried through
// `factory_answer` retry_publish: the approval still stands for that commit.
func TestManagerJoinRetryPublishOnAFailedPublish(t *testing.T) {
	j := newJoin(t, factory.AutonomyManual, "write", "proof")
	j.s.Open()
	kept := j.decideOnStops(func(factory.Item) []session.ManagerPress {
		j.s.GitHub.Down(false)
		return []session.ManagerPress{joinDecision(session.ActionRetryPublish, "", "GitHub is reachable again.")}
	})
	it := j.s.Do("a check fails and the repair waits for its push approval", func() {
		j.s.GitHub.SetChecks(1, 1, 0)
		j.s.Poll()
	})
	if it.Wait.Action != factory.ActApprovePublish {
		t.Fatalf("the repair is not waiting to be pushed: %s\n%s", factorytest.WaitKey(it), j.s.Story())
	}
	it = j.s.Do("GitHub goes down and the person approves the push", func() {
		j.s.GitHub.Down(true)
		j.s.MustPress(factory.DoApprove)
	})
	turns, _, errs := kept.all()
	if turns != 1 || len(errs) != 1 || errs[0] != "" {
		t.Fatalf("the main agent had %d turns and was answered %q; the job waits on %s\n%s", turns, errs, factorytest.WaitKey(it), j.s.Story())
	}
	if it.State != factory.StateShipped || it.PullRequest.HeadSHA != it.HeadCommit || it.PublishError != "" || !joinLogged(it, "Main agent: retried publishing · GitHub is reachable again.") {
		t.Fatalf("the retried publish did not push the repair: %s, error %q\n%s", it.State, it.PublishError, j.s.Story())
	}
}

// ANY STOP MAY BE HANDED TO THE PERSON through `factory_answer`
// hand_to_person, with the manager's reason on the job's log, and the person's
// own card then works. After two extra rounds the third is refused: the next
// move is the person's.
func TestManagerJoinHandToPersonAndTheRoundsLimit(t *testing.T) {
	j := newJoin(t, factory.AutonomyAuto, "write", "test", "proof")
	j.s.Delegate.Always("test", factorytest.Round{Result: joinNotGreen()})
	const why = "Both failing tests assert the old rounding rule; someone has to say which rule is right."
	hand := false
	kept := j.decideOnStops(func(it factory.Item) []session.ManagerPress {
		if hand {
			return []session.ManagerPress{joinDecision(session.ActionHandToPerson, "", why)}
		}
		return []session.ManagerPress{joinDecision(session.ActionAnotherRound, "test", "Try once more.")}
	})
	// TWO EXTRA ROUNDS, THEN THE STOP GOES STRAIGHT TO THE PERSON.
	it := j.sayYesUntil(func(it factory.Item) bool {
		return it.QKind == factory.QKindOutOfAttempts && it.Wait.Who == factory.WaitYou
	})
	turns, _, errs := kept.all()
	if turns != factory.ManagerRoundsMost || len(errs) != factory.ManagerRoundsMost || errs[0] != "" || errs[1] != "" || j.s.Delegate.Rounds("test") != 3+factory.ManagerRoundsMost {
		t.Fatalf("the main agent had %d turns, answered %q; test ran %d rounds\n%s", turns, errs, j.s.Delegate.Rounds("test"), j.s.Story())
	}
	j.turn("").refused(joinDecision(session.ActionAnotherRound, "test", "And again."), "already put this step back to work 2 times")
	// A task there is one more round too, and is refused the same way.
	j.turn("").refused(joinTask("Fix the two tests.", false), "already put this step back to work 2 times")
	// The person answers, the count starts again, and this time the main
	// agent hands the stop over with its reason.
	hand = true
	it = j.s.Do("the person gives it one more round, and the main agent hands the next stop to them", func() { j.s.MustPress(factory.DoAnswer) })
	_, lines, errs := kept.all()
	if last := len(errs) - 1; errs[last] != "" || !strings.Contains(lines[last], "handed to the person") {
		t.Fatalf("hand_to_person was answered %q (%s)\n%s", lines[last], errs[last], j.s.Story())
	}
	if it.Wait.Who != factory.WaitYou || it.Wait.Action != factory.ActAnswer || !joinLogged(it, factory.HandoffLine(why)) {
		t.Fatalf("after the hand-over the job reads %s\n%s", factorytest.WaitKey(it), j.s.Story())
	}
	// Already with the person: there is nothing left to hand over.
	j.turn("").refused(joinDecision(session.ActionHandToPerson, "", why), "This stop is already with the person")
	j.s.Delegate.Forget("test")
	it = j.s.Do("the person gives it one more round", func() { j.s.MustPress(factory.DoAnswer) })
	if j.s.Delegate.Rounds("proof") == 0 {
		t.Fatalf("the person's answer did not carry the job on: %s\n%s", factorytest.WaitKey(it), j.s.Story())
	}
}

// A MANUAL PUSH APPROVAL IS NO STOP OF THE MAIN AGENT'S: every decision of
// `factory_answer` is refused on it, in words, and nothing is published.
func TestManagerJoinNoDecisionIsTakenOnAManualPushApproval(t *testing.T) {
	j, _, it := joinReviewed(t, factory.AutonomyManual)
	if it.Wait.Action != factory.ActApprovePublish {
		t.Fatalf("Manual did not wait to push the repair: %s\n%s", factorytest.WaitKey(it), j.s.Story())
	}
	writes := len(j.s.GitHub.Writes())
	for _, decision := range factory.ManagerDecisions {
		j.turn("").refused(joinDecision(decision, "", "The repair is checked; publish it."), "not on a stop the main agent may decide")
	}
	if len(j.s.GitHub.Writes()) != writes {
		t.Fatalf("something was published without the person: %v\n%s", j.s.GitHub.Writes()[writes:], j.s.Story())
	}
}

// THE AUTHORITY TABLE, END TO END, IN MANUAL. Approving a publish, marking
// the pull request ready, approving proposed acts and merging are each
// pressed by the manager on a turn the engine began, and CHANGE NOTHING ON
// THE SERVICE; on a turn the person began with their own words, each does
// what it says. The job walks from a repair waiting to be pushed to merged.
func TestManagerJoinTheAuthorityTableInManual(t *testing.T) {
	j, thread, it := joinReviewed(t, factory.AutonomyManual)
	if it.Wait.Action != factory.ActApprovePublish {
		t.Fatalf("Manual did not wait to push the repair: %s\n%s", factorytest.WaitKey(it), j.s.Story())
	}
	engine := func() joinTurn { return j.turn("") }

	// APPROVE A PUBLISH.
	pushes := writesOf(j.s, "push ")
	engine().refused(session.ManagerPress{Action: session.ActionApprove, Words: "the person told me to approve"}, "approving publication is the person's, and nothing in this turn came from them")
	if it = j.s.Do("time passes", j.s.Tick); it.Wait.Action != factory.ActApprovePublish || writesOf(j.s, "push ") != pushes {
		t.Fatalf("the manager's own approval moved the job: %s, %v\n%s", factorytest.WaitKey(it), j.s.GitHub.Writes(), j.s.Story())
	}
	it = j.s.Do("the person says to push, and the manager approves", func() {
		j.turn("push the fix").must(session.ManagerPress{Action: session.ActionApprove, Words: "they said to push the fix"})
	})
	if it.State != factory.StateShipped || writesOf(j.s, "push ") != pushes+1 || it.PullRequest.HeadSHA != it.HeadCommit {
		t.Fatalf("the person's approval did not push the repair: %s\n%s", factorytest.WaitKey(it), j.s.Story())
	}

	// MARK THE PULL REQUEST READY.
	engine().refused(session.ManagerPress{Action: session.ActionMarkReady, Words: "the person told me to mark it ready"}, "marking the pull request ready is the person's, and nothing in this turn came from them")
	engine().must(joinActs("it is ready", session.PublicAct{Kind: session.ActMarkReady}))
	if it = j.s.ActsSent(); !it.PullRequest.Draft || writesOf(j.s, "ready ") != 0 || actOf(t, it, factory.ActKindMarkReady).State != factory.ActProposed {
		t.Fatalf("the manager marked the pull request ready by itself: draft %v, %v\n%s", it.PullRequest.Draft, j.s.GitHub.Writes(), j.s.Story())
	}
	engine().must(session.ManagerPress{Action: session.ActionDeclineActs, Words: "The person will say when."})
	it = j.s.Do("the person says to mark it ready, and the manager does", func() {
		line := j.turn("mark it ready for review").must(session.ManagerPress{Action: session.ActionMarkReady, Words: "they said to mark it ready"})
		if !strings.Contains(line, "is marked ready for review") {
			t.Errorf("the tool read back %q", line)
		}
	})
	if pull, _ := j.s.GitHub.PullRequest(); it.PullRequest.Draft || pull.Draft || writesOf(j.s, "ready ") != 1 {
		t.Fatalf("the person's word did not mark it ready: job draft %v, GitHub draft %v\n%s", it.PullRequest.Draft, pull.Draft, j.s.Story())
	}

	// APPROVE PROPOSED ACTS.
	it = j.s.Do("the manager proposes a reply", func() {
		engine().must(joinActs("bob's rename is in", joinReply(thread, "Renamed it to settled.")))
		j.s.ActsSent()
	})
	engine().refused(session.ManagerPress{Action: session.ActionApproveActs}, "approving public acts is the person's, and nothing in this turn came from them")
	if it = j.s.Do("time passes", j.s.Tick); it.Wait.Action != factory.ActApproveActs || len(j.s.GitHub.Posted()) != 0 {
		t.Fatalf("the manager's own approval sent the reply: %s, posted %q\n%s", factorytest.WaitKey(it), j.s.GitHub.Posted(), j.s.Story())
	}
	it = j.s.Do("the person says yes, and the manager approves the reply", func() {
		j.turn("yes, post it").must(session.ManagerPress{Action: session.ActionApproveActs})
		j.s.ActsSent()
	})
	if len(j.s.GitHub.Posted()) != 1 || actOf(t, it, factory.ActKindReply).State != factory.ActDone {
		t.Fatalf("the person's yes did not post the reply: %q\n%s", j.s.GitHub.Posted(), j.s.Story())
	}

	// MERGE.
	it = j.s.Do("GitHub goes green and is read", func() {
		j.s.GitHub.SetChecks(2, 0, 0)
		j.s.Poll()
	})
	if it.Wait.Who != factory.WaitYou || it.Wait.Action != factory.ActMerge {
		t.Fatalf("the job does not offer the merge: %s %q\n%s", factorytest.WaitKey(it), it.Wait.Words, j.s.Story())
	}
	if got := engine().refused(session.ManagerPress{Action: session.ActionMerge, Words: "the person told me to merge"}, "merging is the person's alone"); got != factory.ManagerMergeRefusal {
		t.Fatalf("the refusal read %q", got)
	}
	if it = j.s.Do("time passes", j.s.Tick); it.Merging != nil || writesOf(j.s, "merge ") != 0 || it.Wait.Action != factory.ActMerge {
		t.Fatalf("the manager's own merge moved the job: %+v\n%s", it.Merging, j.s.Story())
	}
	it = j.s.Do("the person says to merge, and the manager presses Merge", func() {
		line := j.turn("merge it now").must(session.ManagerPress{Action: session.ActionMerge, Words: "they said to merge it now"})
		if !strings.Contains(line, "Merge pressed for pull request #7, as the person") {
			t.Errorf("the tool read back %q", line)
		}
	})
	if it.Wait.Who != factory.WaitDone || writesOf(j.s, "merge ") != 1 {
		t.Fatalf("the person's word did not merge it: %s\n%s", factorytest.WaitKey(it), j.s.Story())
	}
	// A FINISHED JOB takes nothing more, and says so.
	engine().refused(joinTask(joinTaskWords, false), "This job is finished, so it takes no more tasks.")
	engine().refused(joinActs("one more", session.PublicAct{Kind: session.ActComment, Text: "Thanks."}), "This job is finished, so nothing more is posted or changed for it.")
}

// THE AUTHORITY TABLE IN AUTO. The manager marks the pull request ready by
// itself, and its acts are carried out with no word of the person's. The
// merge is still theirs alone: refused on the manager's own turn, pressed on
// their word.
func TestManagerJoinTheAuthorityTableInAuto(t *testing.T) {
	j := newJoin(t, factory.AutonomyAuto, "write", "proof")
	it := j.s.Open()
	if !it.PullRequest.Draft {
		t.Fatalf("the pull request did not open as a draft\n%s", j.s.Story())
	}
	it = j.s.Do("the manager marks the pull request ready by itself", func() {
		line := j.turn("").must(session.ManagerPress{Action: session.ActionMarkReady, Words: "the work is checked"})
		if !strings.Contains(line, "is marked ready for review") {
			t.Errorf("the tool read back %q", line)
		}
	})
	if pull, _ := j.s.GitHub.PullRequest(); it.PullRequest.Draft || pull.Draft || writesOf(j.s, "ready ") != 1 {
		t.Fatalf("Auto's manager did not mark it ready: job draft %v, GitHub draft %v\n%s", it.PullRequest.Draft, pull.Draft, j.s.Story())
	}
	it = j.s.Do("GitHub goes green and is read", func() {
		j.s.GitHub.SetChecks(2, 0, 0)
		j.s.Poll()
	})
	if it.Wait.Who != factory.WaitYou || it.Wait.Action != factory.ActMerge {
		t.Fatalf("the job does not offer the merge: %s %q\n%s", factorytest.WaitKey(it), it.Wait.Words, j.s.Story())
	}
	j.turn("").refused(session.ManagerPress{Action: session.ActionMerge, Words: "it is green"}, "merging is the person's alone, in Manual and in Auto")
	if it = j.s.Do("time passes", j.s.Tick); it.Merging != nil || writesOf(j.s, "merge ") != 0 {
		t.Fatalf("Auto's manager merged by itself: %+v\n%s", it.Merging, j.s.Story())
	}
	it = j.s.Do("the person says to merge, and the manager presses Merge", func() {
		j.turn("merge it").must(session.ManagerPress{Action: session.ActionMerge, Words: "they said to merge it"})
	})
	if it.Wait.Who != factory.WaitDone || writesOf(j.s, "merge ") != 1 {
		t.Fatalf("the person's word did not merge it: %s\n%s", factorytest.WaitKey(it), j.s.Story())
	}
}

// IN AUTO, WHAT AUTO ITSELF STOPPED AT IS NOT THE MANAGER'S TO PUSH PAST, by
// whichever tool it asks. Auto paused on a failing repository policy check:
// the manager's `push` leaves the approval waiting and says why, its own
// `approve` and `resume` are refused, and nothing is published. On the
// person's word `resume` carries Auto on, and the work is published.
func TestManagerJoinAutoPausedOnAFailingCheck(t *testing.T) {
	j := newJoin(t, factory.AutonomyManual, "write", "proof")
	it := j.s.Do("start", func() { j.s.MustPress(factory.DoStart) })
	if it.State != factory.StateLanded || it.Wait.Action != factory.ActApprovePublish {
		t.Fatalf("the job did not land and wait for its approval: %s %s\n%s", it.State, factorytest.WaitKey(it), j.s.Story())
	}
	// A repository policy check is no step of the scenario's pipeline, so
	// its failing verdict is written here: on the sheet, and on the proof
	// step's result as the round that reported it.
	policy := factory.Claim{Text: "no secret is committed", OK: false, Evidence: "a key-shaped string in testdata/ledger.json", Medium: "policy"}
	j.s.Change(func(it *factory.Item) {
		it.Policy = []factory.Claim{policy}
		last := &it.Stream.Phases[len(it.Stream.Phases)-1]
		result := *last.Result
		result.Claims = append(append([]factory.Claim(nil), result.Claims...), policy)
		last.Result = &result
	})
	j.s.Events = append(j.s.Events, "a repository policy check fails, and the person switches the job to Auto")
	if got := j.s.Post("control", map[string]any{"mode": factory.AutonomyAuto}); !got.OK() {
		t.Fatalf("switch to Auto: %s", got)
	}
	it = j.s.Settle()
	if it.State != factory.StateLanded || it.Wait.Who != factory.WaitYou || !factory.OffersAutoResume(it) {
		t.Fatalf("Auto did not pause on the failing check: %s %s\n%s", it.State, factorytest.WaitKey(it), j.s.Story())
	}
	line := j.turn("").must(joinActs("the work is checked", session.PublicAct{Kind: session.ActPush}))
	if !strings.HasPrefix(line, "The push was not sent. Auto stopped here for the person. Repository policy needs attention: no secret is committed.") {
		t.Errorf("the tool read back %q", line)
	}
	j.turn("").refused(session.ManagerPress{Action: session.ActionApprove, Words: "it is checked"}, "Auto stopped here for the person, and nothing in this turn came from them")
	j.turn("").refused(session.ManagerPress{Action: session.ActionResume, Words: "carry on"}, "only the person can tell Auto to carry on")
	j.s.Tick()
	if it = j.s.Settle(); it.State != factory.StateLanded || j.s.GitHub.Open() || len(j.s.GitHub.Writes()) != 0 {
		t.Fatalf("the manager published past the failing check: %s, %v\n%s", factorytest.WaitKey(it), j.s.GitHub.Writes(), j.s.Story())
	}
	it = j.s.Do("the person says to carry on, and the manager resumes Auto", func() {
		line := j.turn("carry on, that string is a test fixture").must(session.ManagerPress{Action: session.ActionResume, Words: "carry on, that string is a test fixture"})
		if !strings.Contains(line, "Auto resumed, as the person") {
			t.Errorf("the tool read back %q", line)
		}
	})
	if !j.s.GitHub.Open() || it.State != factory.StateShipped {
		t.Fatalf("the person's word did not carry Auto on to publish: %s\n%s", factorytest.WaitKey(it), j.s.Story())
	}
}

// MANUAL: THE FINAL REVIEW'S APPROVAL TO MARK THE PULL REQUEST READY is the
// person's, pressed by the manager only on their word. The same `approve`
// that publishes checked work passes it; the manager's own is refused, and
// nothing is marked ready.
func TestManagerJoinApprovingTheReadyReviewInManual(t *testing.T) {
	j := newJoin(t, factory.AutonomyManual, "write", "proof")
	j.s.Open()
	it := j.s.Do("GitHub goes green, and the final review passes", func() {
		j.s.GitHub.SetChecks(2, 0, 0)
		j.s.Poll()
	})
	if it.Wait.Who != factory.WaitYou || it.Wait.Action != factory.ActApproveReady {
		t.Fatalf("the job does not ask to mark the pull request ready: %s\n%s", factorytest.WaitKey(it), j.s.Story())
	}
	j.turn("").refused(session.ManagerPress{Action: session.ActionApprove, Words: "the review passed"}, "approving publication is the person's, and nothing in this turn came from them")
	if it = j.s.Do("time passes", j.s.Tick); it.Wait.Action != factory.ActApproveReady || !it.PullRequest.Draft || writesOf(j.s, "ready ") != 0 {
		t.Fatalf("the manager's own approval marked it ready: %s\n%s", factorytest.WaitKey(it), j.s.Story())
	}
	it = j.s.Do("the person says to mark it ready, and the manager approves", func() {
		// The tool says what this approval does: it marks ready, and
		// publishes nothing.
		line := j.turn("yes, mark it ready").must(session.ManagerPress{Action: session.ActionApprove, Words: "they said to mark it ready"})
		if !strings.Contains(line, "Approved, as the person") || !strings.Contains(line, "marked ready for review") || strings.Contains(line, "publishes the checked work") {
			t.Errorf("the tool read back %q", line)
		}
	})
	if pull, _ := j.s.GitHub.PullRequest(); pull.Draft || it.PullRequest.Draft || writesOf(j.s, "ready ") != 1 || it.Wait.Action != factory.ActMerge {
		t.Fatalf("the person's word did not mark it ready: %s\n%s", factorytest.WaitKey(it), j.s.Story())
	}
}

// THE PUSH'S LINE, ROAD BY ROAD. Auto's own approval takes a waiting publish
// the moment any request reaches the service, so a scenario never rests with
// one for the manager to press; the three roads are read here off jobs shaped
// by hand, with a press that only records.
func TestThePushLineSaysWhichRoadThePushTook(t *testing.T) {
	st, web, _ := talkLab(t)
	sent := fakePress(t, nil)
	door := runDoor(st, web, "").(session.ManagerPresser)
	push := joinActs("the work is checked", session.PublicAct{Kind: session.ActPush})
	for _, c := range []struct {
		name  string
		shape func(*factory.Item)
		want  string
	}{
		{"Auto presses the waiting approval", func(it *factory.Item) { waitsOnPublishApproval(it); it.Autonomy = factory.AutonomyAuto }, pushPressed},
		{"Manual leaves it with the person", func(it *factory.Item) { waitsOnPublishApproval(it); it.Autonomy = factory.AutonomyManual },
			"The push was not sent. The push approval is already waiting on the person. " + pushWaits},
		{"Auto leaves what Auto stopped at with the person", func(it *factory.Item) {
			waitsOnPublishApproval(it)
			it.Autonomy, it.Proof = factory.AutonomyAuto, nil
		}, "The push was not sent. Auto stopped here for the person. Proof is incomplete. Request changes or take over to continue. " + pushWaits},
	} {
		_, talk := managedJob(t, st, c.shape)
		*sent = nil
		line, err := door.Press(context.Background(), talk, push)
		if err != nil || line != c.want || len(*sent) != 1 || (*sent)[0].verb != "act" {
			t.Errorf("%s: %q (%v), sent %+v\nwant %q", c.name, line, err, *sent, c.want)
		}
	}
}

// WHAT THE FACTORY ANSWERED IS READ BACK AS ITS SENTENCE, whichever press it
// was. A refusal is "Nothing changed: " and the sentence; a write whose
// outcome is not known is the sentence alone, so the tool says it was not
// confirmed. The wire's code word is in neither, and the wire's error is
// still there for a caller that asks its code.
func TestAPressTheFactoryDidNotTakeReadsAsItsSentence(t *testing.T) {
	st, web, _ := talkLab(t)
	door := runDoor(st, web, "").(session.ManagerPresser)
	_, talk := managedJob(t, st, func(it *factory.Item) {
		hasMergeablePullRequest(it)
		it.Autonomy = factory.AutonomyAuto
	})
	presses := []session.ManagerPress{
		joinActs("x", session.PublicAct{Kind: session.ActComment, Text: "Hello."}),
		joinTask("do it", false),
		joinDecision(session.ActionRerun, "test", ""),
		{Action: session.ActionApproveActs},
		{Action: session.ActionDeclineActs},
		{Action: session.ActionMarkReady, Words: "ready", Said: "mark it ready"},
		{Action: session.ActionOfferMerge},
		{Action: session.ActionMerge, Words: "merge", Said: "merge it"},
	}
	for _, c := range []struct {
		answer *appwire.Error
		want   string
	}{
		{appwire.Errorf(appwire.CodeConflict, "This job is finished, so it takes no more tasks."), "Nothing changed: This job is finished, so it takes no more tasks."},
		{appwire.Errorf(appwire.CodeBadParams, "Name the thread."), "Nothing changed: Name the thread."},
		{appwire.Errorf(appwire.CodeForbidden, "Sign in to GitHub first."), "Nothing changed: Sign in to GitHub first."},
		{appwire.Errorf(appwire.CodeUncertain, "the factory did not answer; the action may have happened, so read the job before trying again"), "the factory did not answer; the action may have happened, so read the job before trying again"},
	} {
		fakePress(t, func(string) error { return c.answer })
		for _, p := range presses {
			if p.Action == session.ActionMarkReady {
				// Mark ready is offered on a draft; the other presses do not
				// read it.
				if err := st.Update(mustManaged(t, door, talk), func(it *factory.Item) error { it.PullRequest.Draft = true; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			_, err := door.Press(context.Background(), talk, p)
			var wire *appwire.Error
			if err == nil || err.Error() != c.want || !errors.As(err, &wire) || wire.Code != c.answer.Code {
				t.Errorf("%s answered %s read back as %q, want %q", p.Action, c.answer.Code, err, c.want)
			}
			if p.Action == session.ActionMarkReady {
				if err := st.Update(mustManaged(t, door, talk), func(it *factory.Item) error { it.PullRequest.Draft = false; return nil }); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

// mustManaged is the id of the job talk manages.
func mustManaged(t *testing.T, door session.ManagerPresser, talk string) int {
	t.Helper()
	it, err := door.(seamRunDoor).managed(talk)
	if err != nil {
		t.Fatal(err)
	}
	return it.ID
}
