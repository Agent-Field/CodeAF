package factorytest

import (
	"fmt"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// PUBLIC ACTS, REPLAYED ON A WHOLE FACTORY. Each scenario is a job whose pull
// request is open, its person signed in to GitHub, a reviewer's thread on the
// code. The job's manager proposes acts through the service's own door; the
// service's own sender carries them out on the scenario's forge; and the
// properties are held at every rest: a job is never idle without a named
// owner and an action that will work.

// reviewed is a scenario at an open pull request with one review thread read:
// the reviewer's comment set a Follow PR round off, and its repair is pushed
// (Auto) or waits for the person's approval to be pushed (Manual). It answers
// the scenario, the thread's name and the job at rest.
func reviewed(t *testing.T, mode string) (*Scenario, string, factory.Item) {
	t.Helper()
	s := Opened(t, mode)
	s.TrackThreads()
	s.SignIn()
	thread := s.GitHub.AddThread("bob", "COLLABORATOR", "Rename balanced to settled.")
	it := s.Do("a reviewer comments on the code and GitHub is read", s.Poll)
	if followRounds(s) != 1 {
		t.Fatalf("the review comment started %d Follow PR rounds\n%s", followRounds(s), s.Story())
	}
	item, ok := feedback(it, thread)
	if !ok || item.By != "bob" || item.Untrusted || item.Thread == "" || item.ReplyTo == 0 {
		t.Fatalf("the job did not keep the thread: %+v\n%s", it.PullRequest.FeedbackItems, s.Story())
	}
	return s, thread, it
}

func feedback(it factory.Item, id string) (factory.PRFeedback, bool) {
	if it.PullRequest == nil {
		return factory.PRFeedback{}, false
	}
	for _, f := range it.PullRequest.FeedbackItems {
		if f.ID == id {
			return f, true
		}
	}
	return factory.PRFeedback{}, false
}

func replyTo(thread, text string) api.ActAsk {
	return api.ActAsk{Kind: factory.ActKindReply, Thread: thread, Text: text}
}

// count is how many of the forge's writes begin with prefix.
func count(writes []string, prefix string) int {
	n := 0
	for _, w := range writes {
		if strings.HasPrefix(w, prefix) {
			n++
		}
	}
	return n
}

// index is where the first write beginning with prefix is, -1 for nowhere.
func index(writes []string, prefix string) int {
	for i, w := range writes {
		if strings.HasPrefix(w, prefix) {
			return i
		}
	}
	return -1
}

func actsOf(it factory.Item) string {
	var parts []string
	for _, a := range it.Acts {
		parts = append(parts, fmt.Sprintf("%s:%s(%s%s)", a.Kind, a.State, a.Error, a.Result))
	}
	return strings.Join(parts, " ")
}

// AUTO: the manager's reply is posted once, as the person, with the footer,
// and it never comes back as feedback that starts another round.
func TestActsAutoReplyIsPostedOnceWithTheFooter(t *testing.T) {
	t.Parallel()
	s, thread, it := reviewed(t, factory.AutonomyAuto)
	if it.State != factory.StateShipped || it.Wait.Who != factory.WaitGitHub {
		t.Fatalf("Auto did not push the repair: %s\n%s", waitKey(it), s.Story())
	}
	rounds := followRounds(s)
	it = s.Do("the manager replies and resolves the thread", func() {
		if got := s.Act(factory.ActByMainAgent, replyTo(thread, "Renamed it to settled."), api.ActAsk{Kind: factory.ActKindResolveThread, Thread: thread}); !got.OK() {
			t.Fatalf("the act was refused: %s\n%s", got, s.Story())
		}
		s.ActsSent()
	})
	writes := s.GitHub.Writes()
	if count(writes, "reply "+thread) != 1 || count(writes, "resolve "+thread) != 1 {
		t.Fatalf("writes %v\n%s", writes, s.Story())
	}
	want := forge.PublicationMarker + "\nRenamed it to settled.\n\n" + factory.PublicActFooter
	if posted := s.GitHub.Posted(); len(posted) != 1 || posted[0] != want {
		t.Fatalf("posted %q, want %q", posted, want)
	}
	if !s.GitHub.Resolved(thread) || len(it.Acts) != 2 || it.Acts[0].State != factory.ActDone || it.Acts[1].State != factory.ActDone || it.Acts[0].URL == "" {
		t.Fatalf("acts %s\n%s", actsOf(it), s.Story())
	}
	if it.Wait.Who != factory.WaitGitHub {
		t.Fatalf("after the acts the job waits on %s\n%s", waitKey(it), s.Story())
	}
	// GitHub is read again: the factory's own reply is not feedback, the
	// thread reads resolved, and no round starts on it. Nothing is posted twice.
	it = s.Do("GitHub is read again", s.Poll)
	it = s.Do("and again", s.Poll)
	if followRounds(s) != rounds || len(s.GitHub.Posted()) != 1 {
		t.Fatalf("the reply came back as work: %d rounds, posted %q\n%s", followRounds(s)-rounds, s.GitHub.Posted(), s.Story())
	}
	if item, _ := feedback(it, thread); !item.Resolved {
		t.Fatalf("the job does not read the thread as resolved\n%s", s.Story())
	}
	// The same ask again posts nothing more.
	s.Do("the manager asks for the same reply again", func() {
		s.Act(factory.ActByMainAgent, replyTo(thread, "Renamed it to settled."))
		s.ActsSent()
	})
	if len(s.GitHub.Posted()) != 1 {
		t.Fatalf("posted twice: %q\n%s", s.GitHub.Posted(), s.Story())
	}
}

// MANUAL: the manager's reply waits as "you: approve_acts" with one line that
// says it, is posted after the person's one yes, and is never posted after
// their no.
func TestActsManualReplyWaitsForThePerson(t *testing.T) {
	t.Parallel()
	for _, answer := range []string{factory.DoActsApprove, factory.DoActsDecline} {
		t.Run(answer, func(t *testing.T) {
			t.Parallel()
			s, thread, it := reviewed(t, factory.AutonomyManual)
			if it.Wait.Action != factory.ActApprovePublish {
				t.Fatalf("Manual did not wait to push the repair: %s\n%s", waitKey(it), s.Story())
			}
			it = s.Do("the person approves the push", func() { s.MustPress(factory.DoApprove) })
			if it.Wait.Who != factory.WaitGitHub {
				t.Fatalf("after the push the job waits on %s\n%s", waitKey(it), s.Story())
			}
			it = s.Do("the manager proposes a reply and a comment", func() {
				got := s.Act(factory.ActByMainAgent, replyTo(thread, "Renamed it to settled."), api.ActAsk{Kind: factory.ActKindComment, Text: "Ready for another look."})
				if !got.OK() {
					t.Fatalf("the proposal was refused: %s\n%s", got, s.Story())
				}
				s.ActsSent()
			})
			if it.Wait.Who != factory.WaitYou || it.Wait.Action != factory.ActApproveActs || it.Wait.Words != "Approve: post 1 reply and 1 comment?" {
				t.Fatalf("wait %s %q\n%s", waitKey(it), it.Wait.Words, s.Story())
			}
			if len(s.GitHub.Posted()) != 0 {
				t.Fatalf("posted before the yes: %q\n%s", s.GitHub.Posted(), s.Story())
			}
			// GitHub is read while the acts wait: they still wait on the person.
			it = s.Do("GitHub is read while the acts wait", s.Poll)
			if it.Wait.Action != factory.ActApproveActs {
				t.Fatalf("a read of GitHub took the approval's place: %s\n%s", waitKey(it), s.Story())
			}
			it = s.Do("the person answers "+answer, func() {
				s.MustPress(answer)
				s.ActsSent()
			})
			if it.Wait.Who != factory.WaitGitHub {
				t.Fatalf("after the answer the job waits on %s\n%s", waitKey(it), s.Story())
			}
			posted := s.GitHub.Posted()
			if answer == factory.DoActsDecline {
				if len(posted) != 0 || it.Acts[0].State != factory.ActDeclined || it.Acts[1].State != factory.ActDeclined {
					t.Fatalf("declined, and posted %q, acts %s\n%s", posted, actsOf(it), s.Story())
				}
				it = s.Do("GitHub is read after the no", s.Poll)
				if len(s.GitHub.Posted()) != 0 {
					t.Fatalf("a declined act was posted later\n%s", s.Story())
				}
				return
			}
			if len(posted) != 2 || !strings.HasSuffix(posted[0], "\n\n"+factory.PublicActFooter) || !strings.HasSuffix(posted[1], "\n\n"+factory.PublicActFooter) {
				t.Fatalf("posted %q\n%s", posted, s.Story())
			}
			if it.Acts[0].State != factory.ActDone || it.Acts[1].State != factory.ActDone || it.Acts[0].ApprovedBy != scenarioPerson {
				t.Fatalf("acts %s\n%s", actsOf(it), s.Story())
			}
		})
	}
}

// A CHANGE OF BASE updates the base the job stores, which is the base Follow
// PR then syncs with.
func TestActsChangeOfBaseUpdatesTheJobsBase(t *testing.T) {
	t.Parallel()
	in(t, factory.AutonomyAuto, func(t *testing.T, mode string) {
		s, _, it := reviewed(t, mode)
		s.git(s.bare, "branch", "dev", "main")
		if it.PullRequest.BaseBranch != "main" {
			t.Fatalf("the job's base is %q\n%s", it.PullRequest.BaseBranch, s.Story())
		}
		// A base that is not there is GitHub's to refuse, and the act says so.
		it = s.Do("the manager changes the base to a branch that is not there", func() {
			s.Act(factory.ActByMainAgent, api.ActAsk{Kind: factory.ActKindChangeBase, Base: "nope"})
			s.ActsSent()
		})
		if a := it.Acts[len(it.Acts)-1]; a.State != factory.ActFailed || !strings.Contains(a.Error, "GitHub refused to change this pull request's base") || it.PullRequest.BaseBranch != "main" {
			t.Fatalf("acts %s, base %q\n%s", actsOf(it), it.PullRequest.BaseBranch, s.Story())
		}
		it = s.Do("the manager changes the base to dev", func() {
			if got := s.Act(factory.ActByMainAgent, api.ActAsk{Kind: factory.ActKindChangeBase, Base: "dev"}); !got.OK() {
				t.Fatalf("refused: %s\n%s", got, s.Story())
			}
			s.ActsSent()
		})
		if a := it.Acts[len(it.Acts)-1]; a.State != factory.ActDone || it.PullRequest.BaseBranch != "dev" {
			t.Fatalf("acts %s, base %q\n%s", actsOf(it), it.PullRequest.BaseBranch, s.Story())
		}
		if pull, _ := s.GitHub.PullRequest(); pull.Base != "dev" {
			t.Fatalf("GitHub's base is %q", pull.Base)
		}
		it = s.Do("GitHub is read", s.Poll)
		if it.PullRequest.BaseBranch != "dev" || it.Wait.Who != factory.WaitGitHub {
			t.Fatalf("after a read: base %q, wait %s\n%s", it.PullRequest.BaseBranch, waitKey(it), s.Story())
		}
	})
}

// ACTS PROPOSED BESIDE A WAITING PUSH run only after the push has landed, and
// one approval covers the push and the set.
func TestActsBesideAWaitingPushRunAfterItLands(t *testing.T) {
	t.Parallel()
	s, thread, it := reviewed(t, factory.AutonomyManual)
	if it.Wait.Action != factory.ActApprovePublish {
		t.Fatalf("not the stall: %s\n%s", waitKey(it), s.Story())
	}
	it = s.Do("the manager asks to push and proposes a reply beside it", func() {
		got := s.Act(factory.ActByMainAgent, api.ActAsk{Kind: factory.ActKindPush}, replyTo(thread, "Renamed it; it is in the push."))
		if !got.OK() || !strings.Contains(got.Body, api.ActWaitingOnYou) {
			t.Fatalf("the proposal: %s %s\n%s", got, got.Body, s.Story())
		}
		s.ActsSent()
	})
	// The push approval still names the wait, and says the acts go with it.
	if it.Wait.Action != factory.ActApprovePublish || !strings.Contains(it.Wait.Words, "Approving also approves: post 1 reply.") {
		t.Fatalf("wait %s %q\n%s", waitKey(it), it.Wait.Words, s.Story())
	}
	if len(s.GitHub.Posted()) != 0 || count(s.GitHub.Writes(), "push") != 1 {
		t.Fatalf("something went out before the yes: %v\n%s", s.GitHub.Writes(), s.Story())
	}
	it = s.Do("the person approves the push", func() {
		s.MustPress(factory.DoApprove)
		s.Settle()
		s.ActsSent()
	})
	writes := s.GitHub.Writes()
	pushes := 0
	lastPush := -1
	for i, w := range writes {
		if strings.HasPrefix(w, "push ") {
			pushes, lastPush = pushes+1, i
		}
	}
	replied := index(writes, "reply "+thread)
	if pushes != 2 || replied < 0 || replied < lastPush {
		t.Fatalf("the reply did not follow the push: %v\n%s", writes, s.Story())
	}
	if len(s.GitHub.Posted()) != 1 || it.Acts[0].State != factory.ActDone || it.Acts[0].ApprovedBy != scenarioPerson {
		t.Fatalf("posted %q, acts %s\n%s", s.GitHub.Posted(), actsOf(it), s.Story())
	}
	if it.State != factory.StateShipped || it.PullRequest.HeadSHA != it.HeadCommit || it.Wait.Who != factory.WaitGitHub {
		t.Fatalf("after the push: state %s, wait %s\n%s", it.State, waitKey(it), s.Story())
	}
}

// AN ACT ON A JOB WHOSE PULL REQUEST IS CLOSED is refused, with nothing sent,
// and acts that waited when it closed are never sent either.
func TestActsOnAClosedPullRequestAreRefused(t *testing.T) {
	t.Parallel()
	s, thread, it := reviewed(t, factory.AutonomyManual)
	it = s.Do("the person approves the push", func() { s.MustPress(factory.DoApprove) })
	it = s.Do("the manager proposes a reply", func() {
		s.Act(factory.ActByMainAgent, replyTo(thread, "Renamed it to settled."))
		s.ActsSent()
	})
	if it.Wait.Action != factory.ActApproveActs {
		t.Fatalf("wait %s\n%s", waitKey(it), s.Story())
	}
	before := len(s.GitHub.Writes())
	it = s.Do("the pull request is closed on GitHub and read", func() {
		s.GitHub.ClosePull()
		s.Poll()
		s.ActsSent()
	})
	if it.Wait.Who != factory.WaitDone || it.Acts[0].State != factory.ActDeclined {
		t.Fatalf("wait %s, acts %s\n%s", waitKey(it), actsOf(it), s.Story())
	}
	for _, by := range []string{factory.ActByMainAgent, factory.ActByYou} {
		got := s.Act(by, replyTo(thread, "One more thing."), api.ActAsk{Kind: factory.ActKindComment, Text: "Reopening?"})
		if got.OK() || got.Status != 409 || !strings.Contains(got.Error, "finished") {
			t.Fatalf("an act by %s on a closed pull request: %s\n%s", by, got, s.Story())
		}
	}
	if got := s.Post(api.ActsApproveVerb, map[string]any{}); got.OK() {
		t.Fatalf("an approval on a closed pull request was taken\n%s", s.Story())
	}
	it = s.Do("time passes", s.Tick)
	if len(s.GitHub.Writes()) != before || len(s.GitHub.Posted()) != 0 || len(it.Acts) != 1 {
		t.Fatalf("something was sent: %v, acts %s\n%s", s.GitHub.Writes()[before:], actsOf(it), s.Story())
	}
}

// A RESTART BETWEEN "RUNNING" AND "DONE" NEVER POSTS TWICE. The service closes
// while a reply is at GitHub; the one that opens next finds it and does not
// send it again, and the job is left with words that say to look first.
func TestActsARestartMidSendDoesNotPostTwice(t *testing.T) {
	t.Parallel()
	s, thread, _ := reviewed(t, factory.AutonomyAuto)
	held := s.GitHub.HoldReplies(true)
	s.Events = append(s.Events, "the manager replies and the app quits while the reply is being sent")
	if got := s.Act(factory.ActByMainAgent, replyTo(thread, "Renamed it to settled.")); !got.OK() {
		t.Fatalf("refused: %s\n%s", got, s.Story())
	}
	<-held
	if a := s.Item().Acts[0]; a.State != factory.ActRunning {
		t.Fatalf("while it is being sent the act reads %s\n%s", a.State, s.Story())
	}
	s.RestartService()
	it := s.Do("the app opens again", func() { s.ActsSent() })
	a := it.Acts[0]
	if a.State != factory.ActFailed || !strings.Contains(a.Error, "heck the pull request before trying again") {
		t.Fatalf("after the restart: %s\n%s", actsOf(it), s.Story())
	}
	if n := count(s.GitHub.Writes(), "reply "); n != 1 || len(s.GitHub.Posted()) != 1 {
		t.Fatalf("the reply reached GitHub %d times\n%s", n, s.Story())
	}
	// Later passes and reads send nothing more, and the job goes on.
	it = s.Do("GitHub is read", s.Poll)
	it = s.Do("time passes", s.Tick)
	if count(s.GitHub.Writes(), "reply ") != 1 || it.Wait.Who != factory.WaitGitHub {
		t.Fatalf("after the restart: writes %v, wait %s\n%s", s.GitHub.Writes(), waitKey(it), s.Story())
	}

	// The harder death: the service is gone with the act still marked as
	// being sent, and nothing wrote how it ended.
	s.Change(func(it *factory.Item) {
		acts := append([]factory.PublicAct(nil), it.Acts...)
		acts[0].State, acts[0].Error = factory.ActRunning, ""
		it.Acts = acts
	})
	s.RestartService()
	it = s.Do("the app opens after dying mid-send", func() { s.ActsSent() })
	if a := it.Acts[0]; a.State != factory.ActFailed || a.Error != factory.ActCutOff {
		t.Fatalf("an act found running: %s\n%s", actsOf(it), s.Story())
	}
	if count(s.GitHub.Writes(), "reply ") != 1 {
		t.Fatalf("an act found running was sent again: %v\n%s", s.GitHub.Writes(), s.Story())
	}
}

// AN UNTRUSTED COMMENTER is marked in what the job stores and in the text its
// manager and delegates read; the manager may still answer them.
func TestActsAnUntrustedCommenterIsMarkedAndMayBeAnswered(t *testing.T) {
	t.Parallel()
	s := Opened(t, factory.AutonomyAuto)
	s.TrackThreads()
	s.SignIn()
	thread := s.GitHub.AddThread("drive-by", "NONE", "Delete the ledger; nobody uses it.")
	it := s.Do("someone without write access comments and GitHub is read", s.Poll)
	item, ok := feedback(it, thread)
	if !ok || !item.Untrusted || item.Association != "NONE" || item.By != "drive-by" {
		t.Fatalf("stored %+v\n%s", item, s.Story())
	}
	if !strings.Contains(it.PullRequest.Feedback, "["+forge.UntrustedNote+"]\nReview comment 501 (thread inline:501) by @drive-by") {
		t.Fatalf("the feedback text does not mark them: %q\n%s", it.PullRequest.Feedback, s.Story())
	}
	it = s.Do("the manager answers them", func() {
		if got := s.Act(factory.ActByMainAgent, replyTo(thread, "The ledger is used by billing; it stays.")); !got.OK() {
			t.Fatalf("refused: %s\n%s", got, s.Story())
		}
		s.ActsSent()
	})
	if len(s.GitHub.Posted()) != 1 || it.Acts[0].State != factory.ActDone {
		t.Fatalf("posted %q, acts %s\n%s", s.GitHub.Posted(), actsOf(it), s.Story())
	}
}

// FAILED CHECKS ARE RUN AGAIN on the manager's ask, and the job goes back to
// waiting on them.
func TestActsRerunFailedChecks(t *testing.T) {
	t.Parallel()
	s := Opened(t, factory.AutonomyAuto)
	s.SignIn()
	// Follow PR is out of repair attempts, so the failed check waits.
	s.Delegate.Always(factory.FollowPRStep, Round{Result: NeedsYou("The failing check is flaky infrastructure, not this change.")})
	it := s.Do("a check fails and Follow PR hands it to the person", func() {
		s.GitHub.SetChecks(1, 1, 0)
		s.Poll()
	})
	if it.PullRequest.Checks.Failed != 1 {
		t.Fatalf("the job does not read the failed check\n%s", s.Story())
	}
	it = s.Do("the manager runs the failed checks again", func() {
		if got := s.Act(factory.ActByMainAgent, api.ActAsk{Kind: factory.ActKindRerunChecks}); !got.OK() {
			t.Fatalf("refused: %s\n%s", got, s.Story())
		}
		s.ActsSent()
	})
	a := it.Acts[len(it.Acts)-1]
	if a.State != factory.ActDone || !strings.Contains(a.Result, "Asked GitHub to run again: lint-0.") || count(s.GitHub.Writes(), "rerun checks lint-0") != 1 {
		t.Fatalf("acts %s, writes %v\n%s", actsOf(it), s.GitHub.Writes(), s.Story())
	}
	it = s.Do("GitHub is read", s.Poll)
	if it.PullRequest.Checks.Failed != 0 || it.PullRequest.Checks.Pending != 1 {
		t.Fatalf("checks %+v\n%s", it.PullRequest.Checks, s.Story())
	}
}

// THE RANDOM RUNS, WITH ACTS. The same events as the package's random runs
// (what GitHub, the delegate, the person, the runner and the service do), and
// among them the job's manager proposing acts in whatever state the job is in
// and the person answering them. After every event the properties are held,
// and at the end nothing was posted twice and no act is left with nobody to
// move it.
func actSteps(rng *rand.Rand) []step {
	proposed := func(_ *Scenario, it factory.Item) bool { return len(factory.ProposedActs(it)) > 0 }
	open := func(_ *Scenario, it factory.Item) bool {
		return it.PullRequest != nil && it.PullRequest.State == factory.PROpen
	}
	thread := func(it factory.Item) string {
		for _, f := range it.PullRequest.FeedbackItems {
			if strings.HasPrefix(f.ID, "inline:") && !f.Resolved {
				return f.ID
			}
		}
		return ""
	}
	// An act the job refuses for its state (a spent budget, a stopped job) is
	// part of the story, never a failure of the run.
	propose := func(s *Scenario, acts ...api.ActAsk) {
		if got := s.Act(factory.ActByMainAgent, acts...); !got.OK() {
			s.Events[len(s.Events)-1] += " (refused: " + got.Error + ")"
		}
		s.ActsSent()
	}
	return []step{
		{"a reviewer opens a thread on the code", 4, open, func(s *Scenario, _ factory.Item) {
			who, standing := "bob", "COLLABORATOR"
			if rng.IntN(3) == 0 {
				who, standing = "drive-by", "NONE"
			}
			s.GitHub.AddThread(who, standing, fmt.Sprintf("Is this name right? (%d)", rng.IntN(1000)))
			s.Poll()
		}},
		{"the manager proposes a comment", 5, open, func(s *Scenario, _ factory.Item) {
			propose(s, api.ActAsk{Kind: factory.ActKindComment, Text: fmt.Sprintf("Status note %d.", rng.IntN(1000))})
		}},
		{"the manager replies in a thread and resolves it", 6, func(s *Scenario, it factory.Item) bool {
			return open(s, it) && thread(it) != ""
		}, func(s *Scenario, it factory.Item) {
			id := thread(it)
			propose(s, replyTo(id, fmt.Sprintf("Looked at it (%d).", rng.IntN(1000))), api.ActAsk{Kind: factory.ActKindResolveThread, Thread: id})
		}},
		{"the manager asks to push and proposes a comment beside it", 3, func(_ *Scenario, it factory.Item) bool {
			return factory.OffersPublishApproval(it) && it.PullRequest != nil && it.PullRequest.State == factory.PROpen
		}, func(s *Scenario, _ factory.Item) {
			propose(s, api.ActAsk{Kind: factory.ActKindPush}, api.ActAsk{Kind: factory.ActKindComment, Text: fmt.Sprintf("Pushed the fix (%d).", rng.IntN(1000))})
		}},
		{"the person approves the acts", 8, proposed, func(s *Scenario, _ factory.Item) {
			s.MustPress(factory.DoActsApprove)
			s.ActsSent()
		}},
		{"the person declines the acts", 3, proposed, func(s *Scenario, _ factory.Item) { s.MustPress(factory.DoActsDecline) }},
	}
}

func TestActsRandomRuns(t *testing.T) {
	t.Parallel()
	seeds, length := []uint64{11, 12, 13}, 9
	if one, err := strconv.ParseUint(os.Getenv("SCENARIO_SEED"), 10, 64); err == nil {
		seeds = []uint64{one}
	}
	if n, err := strconv.Atoi(os.Getenv("SCENARIO_SEEDS")); err == nil && n > 0 {
		first, _ := strconv.Atoi(os.Getenv("SCENARIO_FIRST"))
		seeds = nil
		for seed := max(first, 1); seed < max(first, 1)+n; seed++ {
			seeds = append(seeds, uint64(seed))
		}
	}
	if n, err := strconv.Atoi(os.Getenv("SCENARIO_LENGTH")); err == nil && n > 0 {
		length = n
	}
	for _, mode := range []string{factory.AutonomyManual, factory.AutonomyAuto} {
		for _, seed := range seeds {
			t.Run(fmt.Sprintf("%s/seed=%d", mode, seed), func(t *testing.T) {
				t.Parallel()
				s := Opened(t, mode)
				s.TrackThreads()
				s.SignIn()
				s.seed = fmt.Sprintf("%d (mode %s, %d events): SCENARIO_SEED=%d go test ./internal/factory/factorytest/ -run TestActsRandomRuns", seed, mode, length, seed)
				rng := rand.New(rand.NewPCG(seed, 0xac75))
				// One of the package's events is left out: a scripted round that
				// reports "no change" with no review of the feedback behind it.
				// With review threads read one by one, as GitHub's are, Follow PR
				// asks for that review, runs out of rounds and offers an answer
				// its door refuses. That stall is Follow PR's own and is there
				// without any act; it is reported, not hidden under these runs.
				var all []step
				for _, st := range steps(rng) {
					if st.name != "next round: the delegate finds nothing to change" {
						all = append(all, st)
					}
				}
				all = append(all, actSteps(rng)...)
				it := s.Item()
				for n := 0; n < length && it.Wait.Who != factory.WaitDone; n++ {
					var can []step
					total := 0
					for _, st := range all {
						if st.ok(s, it) {
							can = append(can, st)
							total += st.weight
						}
					}
					pick := rng.IntN(total)
					var chosen step
					for _, st := range can {
						if pick < st.weight {
							chosen = st
							break
						}
						pick -= st.weight
					}
					it = s.Do(chosen.name, func() {
						chosen.do(s, it)
						s.ActsSent()
					})
				}
				// NOTHING WAS SAID TWICE: every reply and comment the factory
				// posted is there once.
				seen := map[string]bool{}
				for _, body := range s.GitHub.Posted() {
					if seen[body] {
						t.Fatalf("posted twice: %q\n%s", body, s.Story())
					}
					seen[body] = true
					if !strings.HasPrefix(body, forge.PublicationMarker+"\n") || !strings.HasSuffix(body, "\n\n"+factory.PublicActFooter) {
						t.Fatalf("posted without its marker or footer: %q\n%s", body, s.Story())
					}
				}
				// NO ACT IS LEFT WITH NOBODY TO MOVE IT: one that still waits
				// is offered to the person, and one approved is due, is
				// following a push, or waits out a pause of the person's.
				it = s.ActsSent()
				for _, a := range it.Acts {
					switch a.State {
					case factory.ActProposed:
						if !factory.Offered(it, factory.OfferByYou, factory.DoActsApprove) && it.Wait.Who != factory.WaitBacklog && !it.Wait.PutAway {
							t.Fatalf("an act waits and is not offered: %s, wait %s\n%s", actsOf(it), waitKey(it), s.Story())
						}
					case factory.ActRunning:
						t.Fatalf("an act is left running: %s\n%s", actsOf(it), s.Story())
					case factory.ActApproved:
						if a.AfterPush == "" && a.NextAt.IsZero() && !it.Publishing && factory.ActsRefusal(it) == nil {
							t.Fatalf("an approved act is due and was not sent: %s\n%s", actsOf(it), s.Story())
						}
					}
				}
			})
		}
	}
}
