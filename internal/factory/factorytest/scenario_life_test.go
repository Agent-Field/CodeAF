package factorytest

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// ONE JOB'S WHOLE LIFE WITH ITS MAIN AGENT, IN AUTO AND IN MANUAL. Every part
// of phase 2 on one job, through the real service, runner and poller: a plan
// approved, a step out of rounds that the main agent gives another round, a
// push, a reviewer's comment that Follow PR repairs, the main agent's reply
// and resolve carried out as public acts, checks that pass, and the person's
// merge. After every event the properties hold: the job is never idle without
// a named owner and an action that will work.
//
// WHO IS ASKED FOR WHAT is the authority table's. In Auto the person is asked
// for the merge and nothing else. In Manual they are asked for the plan, each
// push, marking the pull request ready and the merge, and ONE approval covers
// a repair's push and the acts proposed beside it.
func TestScenarioAWholeLifeWithItsMainAgent(t *testing.T) {
	t.Parallel()
	both(t, func(t *testing.T, mode string) {
		manual := mode == factory.AutonomyManual
		s := NewScenario(t, mode, "plan", factory.ApproveName, "write", "test", "proof")
		s.TrackThreads()
		s.SignIn()
		s.Delegate.Next("test", Round{Result: notGreen()}, Round{Result: notGreen()}, Round{Result: notGreen()})
		turns := &stopTurns{}
		const instructions = "Run the ledger tests with the race detector on."
		s.StopAgent(func(it factory.Item, wait factory.Wait) (string, error) {
			turns.took(it, wait)
			turns.pressed(s.Decide(factory.DecideAnotherRound, "test", instructions))
			return "Gave test another round.", nil
		})
		// asked is every action the person was asked for, in order: one entry
		// for each time the job came to wait on them.
		var asked []string
		var since time.Time
		rest := func(name string, event func()) factory.Item {
			it := s.Do(name, event)
			if it.Wait.Who == factory.WaitYou && !it.Wait.Since.Equal(since) {
				asked, since = append(asked, it.Wait.Action), it.Wait.Since
			}
			return it
		}
		yes := func(action, door string) factory.Item {
			t.Helper()
			it := s.Item()
			if it.Wait.Who != factory.WaitYou || it.Wait.Action != action {
				t.Fatalf("the job waits on %s, want you:%s\n%s", waitKey(it), action, s.Story())
			}
			return rest("the person says yes to "+action, func() { s.MustPress(door) })
		}

		// START, THE PLAN, WRITE, A TEST STEP OUT OF ROUNDS, PROOF, THE PUSH.
		it := rest("start", func() { s.MustPress(factory.DoStart) })
		if manual {
			yes(factory.ActApprovePlan, factory.DoApprove)
			if s.GitHub.Open() {
				t.Fatalf("Manual published before the person approved it\n%s", s.Story())
			}
			it = yes(factory.ActApprovePublish, factory.DoApprove)
		}
		if it.State != factory.StateShipped || it.PullRequest == nil || !it.PullRequest.Draft || it.Wait.Who != factory.WaitGitHub {
			t.Fatalf("the job did not open its draft pull request: %s\n%s", waitKey(it), s.Story())
		}
		stops, said := turns.all()
		if !slices.Equal(stops, []string{factory.StopRounds + ":" + factory.ActAnswer}) || len(said) != 1 || !said[0].OK() {
			t.Fatalf("the main agent's turns: %v, its presses: %v\n%s", stops, said, s.Story())
		}
		if s.Delegate.Rounds("test") != 4 || !s.Delegate.Heard("test", 4, instructions) || !logged(it, "Main agent: another round · "+instructions) {
			t.Fatalf("test ran %d rounds, and the fourth heard %v\n%s", s.Delegate.Rounds("test"), s.Delegate.notes(), s.Story())
		}

		// A REVIEWER WITH WRITE ACCESS COMMENTS ON THE CODE, AND FOLLOW PR
		// FIXES IT. In Manual the repair waits to be pushed.
		opened, _ := s.GitHub.PullRequest()
		thread := s.GitHub.AddThread("bob", "COLLABORATOR", "Rename balanced to settled.")
		it = rest("a reviewer comments on the code and GitHub is read", s.Poll)
		if item, ok := feedback(it, thread); !ok || item.By != "bob" || item.Untrusted || followRounds(s) != 1 {
			t.Fatalf("the comment was not read, or started %d rounds: %+v\n%s", followRounds(s), it.PullRequest.FeedbackItems, s.Story())
		}
		if manual {
			if pull, _ := s.GitHub.PullRequest(); it.Wait.Action != factory.ActApprovePublish || pull.HeadSHA != opened.HeadSHA {
				t.Fatalf("in Manual the repair did not wait to be pushed: %s\n%s", waitKey(it), s.Story())
			}
		} else if pull, _ := s.GitHub.PullRequest(); pull.HeadSHA == opened.HeadSHA || it.Wait.Who != factory.WaitGitHub {
			t.Fatalf("in Auto the repair was not pushed: %s\n%s", waitKey(it), s.Story())
		}

		// THE MAIN AGENT PROPOSES A REPLY AND A RESOLVE. Auto carries them
		// out. Manual keeps them beside the waiting push: nothing is sent,
		// and the push approval says it covers them.
		it = rest("the main agent proposes a reply and a resolve", func() {
			got := s.Act(factory.ActByMainAgent, replyTo(thread, "Renamed it to settled."), api.ActAsk{Kind: factory.ActKindResolveThread, Thread: thread})
			if !got.OK() {
				t.Fatalf("the acts were refused: %s\n%s", got, s.Story())
			}
			s.ActsSent()
		})
		if manual {
			if it.Wait.Action != factory.ActApprovePublish || !strings.Contains(it.Wait.Words, "Approving also approves: post 1 reply and resolve 1 review thread.") {
				t.Fatalf("the push approval does not say it covers the acts: %s %q\n%s", waitKey(it), it.Wait.Words, s.Story())
			}
			if len(s.GitHub.Posted()) != 0 || s.GitHub.Resolved(thread) || count(s.GitHub.Writes(), "push ") != 1 {
				t.Fatalf("something went out before the person's yes: %v\n%s", s.GitHub.Writes(), s.Story())
			}
			it = rest("the person approves the push, and the acts with it", func() {
				s.MustPress(factory.DoApprove)
				s.Settle()
				s.ActsSent()
			})
		}
		// THE REPLY IS POSTED ONCE, AS THE PERSON, WITH THE FOOTER, AFTER THE
		// PUSH HAS LANDED, AND THE THREAD IS RESOLVED.
		writes := s.GitHub.Writes()
		lastPush := -1
		for i, w := range writes {
			if strings.HasPrefix(w, "push ") {
				lastPush = i
			}
		}
		if count(writes, "push ") != 2 || count(writes, "reply "+thread) != 1 || index(writes, "reply "+thread) < lastPush || index(writes, "resolve "+thread) < index(writes, "reply "+thread) {
			t.Fatalf("the reply and the resolve did not follow the push, once each: %v\n%s", writes, s.Story())
		}
		want := forge.PublicationMarker + "\nRenamed it to settled.\n\n" + factory.PublicActFooter
		if posted := s.GitHub.Posted(); len(posted) != 1 || posted[0] != want || !s.GitHub.Resolved(thread) {
			t.Fatalf("posted %q, want %q once, and the thread resolved\n%s", posted, want, s.Story())
		}
		approver := factory.ActByMainAgent
		if manual {
			approver = scenarioPerson
		}
		for _, a := range it.Acts {
			if a.State != factory.ActDone || a.ApprovedBy != approver || a.By != factory.ActByMainAgent {
				t.Fatalf("acts %s, approved by %q\n%s", actsOf(it), a.ApprovedBy, s.Story())
			}
		}
		if it.Wait.Who != factory.WaitGitHub || it.PullRequest.HeadSHA != it.HeadCommit {
			t.Fatalf("after the push and the acts the job waits on %s\n%s", waitKey(it), s.Story())
		}
		rounds := followRounds(s)

		// CHECKS PASS, THE REVIEWER APPROVES, AND THE JOB ASKS FOR THE MERGE.
		it = rest("GitHub goes green, the reviewer approves, and GitHub is read", func() {
			s.GitHub.SetChecks(2, 0, 0)
			s.GitHub.Review("bob", "APPROVED", "", s.Now())
			s.Poll()
		})
		if manual {
			it = yes(factory.ActApproveReady, factory.DoApprove)
		}
		if pull, _ := s.GitHub.PullRequest(); pull.Draft || it.PullRequest.Draft {
			t.Fatalf("the pull request is still a draft\n%s", s.Story())
		}
		// The factory's own reply never came back as feedback to repair: the
		// one round since is the readiness review.
		if followRounds(s) != rounds+1 || len(s.GitHub.Posted()) != 1 {
			t.Fatalf("the reply came back as work: %d rounds since, posted %q\n%s", followRounds(s)-rounds, s.GitHub.Posted(), s.Story())
		}
		it = yes(factory.ActMerge, factory.DoMerge)
		if it.Wait.Who != factory.WaitDone || count(s.GitHub.Writes(), "merge ") != 1 {
			t.Fatalf("the person's merge did not finish the job: %s\n%s", waitKey(it), s.Story())
		}
		it = rest("GitHub is read after the merge", s.Poll)
		if it.Wait.Who != factory.WaitDone || len(s.GitHub.Posted()) != 1 || turns.count() != 1 {
			t.Fatalf("the finished job moved: %s, posted %q, %d turns\n%s", waitKey(it), s.GitHub.Posted(), turns.count(), s.Story())
		}

		// WHAT THE PERSON WAS ASKED FOR, AND NOTHING ELSE.
		wantAsked := []string{factory.ActMerge}
		if manual {
			wantAsked = []string{factory.ActApprovePlan, factory.ActApprovePublish, factory.ActApprovePublish, factory.ActApproveReady, factory.ActMerge}
		}
		if !slices.Equal(asked, wantAsked) {
			t.Fatalf("the person was asked for %v, want %v\n%s", asked, wantAsked, s.Story())
		}
	})
}
