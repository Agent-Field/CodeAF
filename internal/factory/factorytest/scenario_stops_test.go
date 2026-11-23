package factorytest

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// EVERY STOP THE MAIN AGENT MAY DECIDE GOES TO THE MAIN AGENT FIRST. These
// scenarios hand whole jobs' stops to a scripted main agent on the real
// service, runner and doors: the agent's one turn presses the manager_decide
// door, as its tool does, and the properties hold after every event, while
// the agent holds the stop as well as after.

// notGreen is a test round that reports failing tests.
func notGreen() *factory.StageResult {
	return &factory.StageResult{Done: false, Output: "2 tests fail in the ledger package.", Findings: 2,
		Tests: &factory.TestCounts{Command: "go test ./...", Passed: 98, Failed: 2}}
}

// stopTurns counts the main agent's turns and keeps the stops it was handed.
type stopTurns struct {
	mu    sync.Mutex
	stops []string
	said  []Pressed
}

func (c *stopTurns) took(it factory.Item, wait factory.Wait) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stops = append(c.stops, factory.ManagerStop(it)+":"+wait.Action)
}

func (c *stopTurns) pressed(p Pressed) Pressed {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.said = append(c.said, p)
	return p
}

func (c *stopTurns) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.stops)
}

func (c *stopTurns) all() ([]string, []Pressed) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.stops), slices.Clone(c.said)
}

// logged says the job's log has a line with these words.
func logged(it factory.Item, words string) bool {
	for _, l := range logOf(it) {
		if strings.Contains(l.Text, words) {
			return true
		}
	}
	return false
}

// toStop runs a new job until the main agent has had a turn or the job waits
// on its person for something other than an approval it would say yes to.
func toStop(s *Scenario, turns *stopTurns, until func(factory.Item) bool) factory.Item {
	s.T.Helper()
	it := s.Do("start", func() { s.MustPress(factory.DoStart) })
	for step := 0; step < 10 && !until(it); step++ {
		if it.Wait.Who != factory.WaitYou {
			s.T.Fatalf("the job left its person before the stop: %s\n%s", waitKey(it), s.Story())
		}
		it = s.Do("the person says yes", func() { s.Yes() })
	}
	if !until(it) {
		s.T.Fatalf("the job never reached the stop: %s\n%s", waitKey(it), s.Story())
	}
	return it
}

// A STEP OUT OF ROUNDS IS HANDED TO THE MAIN AGENT, WHICH GIVES IT ANOTHER
// ROUND WITH INSTRUCTIONS, AND THE STEP PASSES. The person is never asked: in
// Manual as in Auto the job goes on to its next real wait, the round's
// instructions reach the delegate, and the decision is on the job's log.
func TestScenarioAStepOutOfRoundsIsGivenAnotherRoundByTheMainAgent(t *testing.T) {
	t.Parallel()
	both(t, func(t *testing.T, mode string) {
		s := NewScenario(t, mode, "write", "test", "proof")
		s.Delegate.Next("test", Round{Result: notGreen()}, Round{Result: notGreen()}, Round{Result: notGreen()})
		turns := &stopTurns{}
		const words = "Run the ledger tests with the race detector on."
		s.StopAgent(func(it factory.Item, wait factory.Wait) (string, error) {
			turns.took(it, wait)
			if it.QKind != factory.QKindOutOfAttempts || it.Wait.Who != factory.WaitMainAgent || it.Wait.Deadline.IsZero() {
				t.Errorf("the turn was given %s (%s) with deadline %v", waitKey(it), it.QKind, it.Wait.Deadline)
			}
			turns.pressed(s.Decide(factory.DecideAnotherRound, "test", words))
			return "Gave test another round.", nil
		})
		it := toStop(s, turns, func(it factory.Item) bool { return s.Delegate.Rounds("proof") > 0 })
		stops, said := turns.all()
		if !slices.Equal(stops, []string{factory.StopRounds + ":" + factory.ActAnswer}) || len(said) != 1 || !said[0].OK() {
			t.Fatalf("the main agent's turns: %v, its presses: %v\n%s", stops, said, s.Story())
		}
		if got := s.Delegate.Rounds("test"); got != 4 {
			t.Fatalf("test ran %d rounds, want its 3 and the one the main agent gave\n%s", got, s.Story())
		}
		if !s.Delegate.Heard("test", 4, words) {
			t.Fatalf("the round the main agent gave never heard its instructions: %v\n%s", s.Delegate.notes(), s.Story())
		}
		if !logged(it, "Main agent: another round · "+words) || logged(it, factory.HandoffUnsettled) {
			t.Fatalf("the log does not say what the main agent decided\n%s", s.Story())
		}
		for _, e := range s.Events {
			if strings.HasSuffix(e, "-> you:answer") {
				t.Fatalf("the person was asked about a stop the main agent settled: %s\n%s", e, s.Story())
			}
		}
	})
}

// THE MAIN AGENT'S TURN FAILS, AND THE STOP IS THE PERSON'S AT ONCE: the job
// reads "you: answer" when it next rests, its log says the main agent could
// not be asked, and every answer the person is offered is still taken.
func TestScenarioAFailedTurnHandsTheStopToThePerson(t *testing.T) {
	t.Parallel()
	in(t, factory.AutonomyAuto, func(t *testing.T, mode string) {
		s := NewScenario(t, mode, "write", "test", "proof")
		s.Delegate.Always("test", Round{Result: notGreen()})
		turns := &stopTurns{}
		s.StopAgent(func(it factory.Item, wait factory.Wait) (string, error) {
			turns.took(it, wait)
			return "", errors.New("the conversation could not be opened")
		})
		it := toStop(s, turns, func(it factory.Item) bool { return it.QKind == factory.QKindOutOfAttempts })
		if it.Wait.Who != factory.WaitYou || it.Wait.Action != factory.ActAnswer || turns.count() != 1 {
			t.Fatalf("after a failed turn: wait %s, %d turns\n%s", waitKey(it), turns.count(), s.Story())
		}
		if !logged(it, factory.HandoffLine(factory.HandoffFailed)) {
			t.Fatalf("the log does not say the main agent could not be asked\n%s", s.Story())
		}
		s.Delegate.Forget("test")
		it = s.Do("the person gives it one more round", func() { s.MustPress(factory.DoAnswer) })
		if s.Delegate.Rounds("proof") == 0 || turns.count() != 1 {
			t.Fatalf("the person's answer did not carry the job on: %s, %d turns\n%s", waitKey(it), turns.count(), s.Story())
		}
	})
}

// THE MAIN AGENT HANDS A STOP TO THE PERSON WITH ITS REASON. The job reads
// "you" with the reason on its log, once, and the person's card works.
func TestScenarioTheMainAgentHandsAStopToThePerson(t *testing.T) {
	t.Parallel()
	in(t, factory.AutonomyManual, func(t *testing.T, mode string) {
		s := NewScenario(t, mode, "write", "test", "proof")
		s.Delegate.Always("test", Round{Result: notGreen()})
		turns := &stopTurns{}
		const why = "Both failing tests assert the old rounding rule; someone has to say which rule is right."
		s.StopAgent(func(it factory.Item, wait factory.Wait) (string, error) {
			turns.took(it, wait)
			turns.pressed(s.Decide(factory.DecideHandToPerson, "", why))
			return "I handed it to you.", nil
		})
		it := toStop(s, turns, func(it factory.Item) bool { return it.QKind == factory.QKindOutOfAttempts })
		_, said := turns.all()
		if it.Wait.Who != factory.WaitYou || it.Wait.Action != factory.ActAnswer || len(said) != 1 || !said[0].OK() {
			t.Fatalf("after the hand-over: wait %s, presses %v\n%s", waitKey(it), said, s.Story())
		}
		lines := 0
		for _, l := range logOf(it) {
			if strings.HasPrefix(l.Text, factory.HandoffUnsettled) {
				lines++
				if l.Text != factory.HandoffLine(why) {
					t.Fatalf("the hand-over's line: %q\n%s", l.Text, s.Story())
				}
			}
		}
		if lines != 1 {
			t.Fatalf("the log says the stop was handed over %d times\n%s", lines, s.Story())
		}
		it = s.Do("the person sends the work back", func() {
			if got := s.Post("answer", map[string]any{"choice": factory.ChoiceSendBack, "reason": "Round half to even."}); !got.OK() {
				t.Fatalf("send back: %s\n%s", got, s.Story())
			}
		})
		if s.Delegate.Rounds("write") != 2 {
			t.Fatalf("the person's send back ran no round of write: %s\n%s", waitKey(it), s.Story())
		}
	})
}

// TWO EXTRA ROUNDS FROM THE MAIN AGENT, THEN THE PERSON. A step that stops a
// third time goes straight to its person, the main agent's third round is
// refused by the door, and the person's answer starts the count again.
func TestScenarioTheThirdStopOfAStepGoesStraightToThePerson(t *testing.T) {
	t.Parallel()
	in(t, factory.AutonomyAuto, func(t *testing.T, mode string) {
		s := NewScenario(t, mode, "write", "test", "proof")
		s.Delegate.Always("test", Round{Result: notGreen()})
		turns := &stopTurns{}
		s.StopAgent(func(it factory.Item, wait factory.Wait) (string, error) {
			turns.took(it, wait)
			turns.pressed(s.Decide(factory.DecideAnotherRound, "test", "Try once more."))
			return "Gave test another round.", nil
		})
		it := toStop(s, turns, func(it factory.Item) bool {
			return it.QKind == factory.QKindOutOfAttempts && it.Wait.Who == factory.WaitYou
		})
		_, said := turns.all()
		if turns.count() != factory.ManagerRoundsMost || len(said) != factory.ManagerRoundsMost || !said[0].OK() || !said[1].OK() {
			t.Fatalf("the main agent had %d turns, pressed %v\n%s", turns.count(), said, s.Story())
		}
		if got := s.Delegate.Rounds("test"); got != 3+factory.ManagerRoundsMost {
			t.Fatalf("test ran %d rounds\n%s", got, s.Story())
		}
		if it.Handoff == nil || it.Handoff.Wait != "" || it.Handoff.Rounds["test"] != factory.ManagerRoundsMost {
			t.Fatalf("the count on the job: %+v\n%s", it.Handoff, s.Story())
		}
		// The door holds the limit too, whoever asks.
		if got := s.Decide(factory.DecideAnotherRound, "test", "And again."); got.OK() || !strings.Contains(got.Error, "already put this step back to work") {
			t.Fatalf("a third round from the main agent: %s\n%s", got, s.Story())
		}
		// The person answers, and the count starts again: the step's next two
		// stops are the main agent's, and the one after is the person's.
		it = s.Do("the person gives it one more round", func() { s.MustPress(factory.DoAnswer) })
		if turns.count() != 2*factory.ManagerRoundsMost || it.Wait.Who != factory.WaitYou || it.Wait.Action != factory.ActAnswer {
			t.Fatalf("after the person's answer the main agent had %d turns in all: %s\n%s", turns.count(), waitKey(it), s.Story())
		}
		if got := s.Delegate.Rounds("test"); got != 3+2*factory.ManagerRoundsMost+1 {
			t.Fatalf("test ran %d rounds in all\n%s", got, s.Story())
		}
	})
}

// A MANUAL JOB'S PUSH APPROVAL IS NEVER HANDED OVER, and no decision of the
// main agent's is taken on it: nothing goes public in Manual without the
// person.
func TestScenarioAManualPushApprovalIsNeverTheMainAgents(t *testing.T) {
	t.Parallel()
	s := Opened(t, factory.AutonomyManual)
	turns := &stopTurns{}
	s.StopAgent(func(it factory.Item, wait factory.Wait) (string, error) {
		turns.took(it, wait)
		return "", nil
	})
	it := s.Do("a check fails and the repair waits for its push approval", func() {
		s.GitHub.SetChecks(1, 1, 0)
		s.Poll()
	})
	if it.Wait.Who != factory.WaitYou || it.Wait.Action != factory.ActApprovePublish || turns.count() != 0 || it.Handoff != nil {
		t.Fatalf("the push approval: wait %s, %d turns, handoff %+v\n%s", waitKey(it), turns.count(), it.Handoff, s.Story())
	}
	version, _ := s.Store.Revision(s.Job)
	for _, decision := range factory.ManagerDecisions {
		got := s.Decide(decision, "", "The repair is checked; publish it.")
		if got.OK() || !strings.Contains(got.Error, "not on a stop the main agent may decide") {
			t.Fatalf("%s was taken on a Manual push approval: %s\n%s", decision, got, s.Story())
		}
	}
	if after, _ := s.Store.Revision(s.Job); after != version {
		t.Fatalf("a refused decision wrote the job\n%s", s.Story())
	}
	if len(s.GitHub.Writes()) != 2 {
		t.Fatalf("something was published without the person: %v\n%s", s.GitHub.Writes(), s.Story())
	}
	it = s.Do("the person approves the push", func() { s.MustPress(factory.DoApprove) })
	if it.State != factory.StateShipped || turns.count() != 0 {
		t.Fatalf("the approved repair was not pushed: state %s, %d turns\n%s", it.State, turns.count(), s.Story())
	}
}

// A FAILED STEP IS HANDED TO THE MAIN AGENT, WHICH RUNS IT AGAIN.
func TestScenarioAFailedStepIsRerunByTheMainAgent(t *testing.T) {
	t.Parallel()
	both(t, func(t *testing.T, mode string) {
		s := NewScenario(t, mode, "write", "proof")
		// An error neither mode retries by itself.
		s.Delegate.Next("write", Round{Err: errors.New("the model refused the request: invalid tool schema")})
		turns := &stopTurns{}
		const words = "The tool schema is fixed; run it again."
		s.StopAgent(func(it factory.Item, wait factory.Wait) (string, error) {
			turns.took(it, wait)
			if it.Failure == nil || it.Failure.Step != "write" {
				t.Errorf("the turn was given a job with failure %+v", it.Failure)
			}
			turns.pressed(s.Decide(factory.DecideRerun, "write", words))
			return "Ran write again.", nil
		})
		it := toStop(s, turns, func(it factory.Item) bool { return s.Delegate.Rounds("proof") > 0 })
		stops, said := turns.all()
		if !slices.Equal(stops, []string{factory.StopFailed + ":" + factory.ActRetry}) || len(said) != 1 || !said[0].OK() {
			t.Fatalf("the main agent's turns: %v, its presses: %v\n%s", stops, said, s.Story())
		}
		if s.Delegate.Rounds("write") != 2 || it.Failure != nil {
			t.Fatalf("write ran %d rounds, failure %+v\n%s", s.Delegate.Rounds("write"), it.Failure, s.Story())
		}
		if !logged(it, "Main agent: ran the step again · "+words) {
			t.Fatalf("the log does not say what the main agent decided\n%s", s.Story())
		}
	})
}

// FOLLOW PR'S SCOPE STOP IS HANDED TO THE MAIN AGENT, WHICH RESUMES THE
// PIPELINE WITH INSTRUCTIONS: the next round runs with them, and in Manual
// its repair still waits for the person's approval before it is pushed.
func TestScenarioAFollowPRScopeStopIsResumedByTheMainAgent(t *testing.T) {
	t.Parallel()
	both(t, func(t *testing.T, mode string) {
		s := Opened(t, mode)
		s.Delegate.Next(factory.FollowPRStep, Round{Result: NeedsYou("The failing dependency audit predates this change; it needs a scope decision.")})
		turns := &stopTurns{}
		const words = "Leave the dependency audit alone. Fix only the lint failure."
		s.StopAgent(func(it factory.Item, wait factory.Wait) (string, error) {
			turns.took(it, wait)
			turns.pressed(s.Decide(factory.DecideResumePipeline, "", words))
			return "Resumed Follow PR with the scope settled.", nil
		})
		before := followRounds(s)
		it := s.Do("a check fails and Follow PR asks what is in scope", func() {
			s.GitHub.SetChecks(1, 1, 0)
			s.Poll()
		})
		stops, said := turns.all()
		if !slices.Equal(stops, []string{factory.StopFollow + ":" + factory.ActAnswer}) || len(said) != 1 || !said[0].OK() {
			t.Fatalf("the main agent's turns: %v, its presses: %v\n%s", stops, said, s.Story())
		}
		if followRounds(s) != before+2 {
			t.Fatalf("Follow PR ran %d rounds, want the one that asked and the one the main agent started\n%s", followRounds(s)-before, s.Story())
		}
		if !s.Delegate.Heard(factory.FollowPRStep, 0, words) {
			t.Fatalf("the resumed round never heard its instructions: %v\n%s", s.Delegate.notes(), s.Story())
		}
		// The round is told who asked for it: the main agent, never the person.
		if !s.Delegate.Heard(factory.FollowPRStep, 0, "The job's main agent resumed the pipeline") || s.Delegate.Heard(factory.FollowPRStep, 0, "The person resumed the pipeline") {
			t.Fatalf("the resumed round was not told the main agent asked for it: %v\n%s", s.Delegate.notes(), s.Story())
		}
		if !logged(it, "Main agent: resumed the pipeline · "+words) {
			t.Fatalf("the log does not say what the main agent decided\n%s", s.Story())
		}
		if mode == factory.AutonomyManual {
			// Nothing public without the person: the repair waits for them.
			if it.Wait.Who != factory.WaitYou || it.Wait.Action != factory.ActApprovePublish || len(s.GitHub.Writes()) != 2 {
				t.Fatalf("in Manual the repair did not wait for its push approval: %s, writes %v\n%s", waitKey(it), s.GitHub.Writes(), s.Story())
			}
			return
		}
		if it.State != factory.StateShipped || it.PullRequest.HeadSHA != it.HeadCommit {
			t.Fatalf("in Auto the repair was not pushed: state %s\n%s", it.State, s.Story())
		}
	})
}

// A FOLLOW PR STOP THAT NEEDS NOTHING DONE GOES BACK TO WAITING ON GITHUB.
// The job is read by the poller again, the same facts start nothing, and the
// next real event starts a round.
func TestScenarioAFollowPRStopGoesBackToWaitingOnGitHub(t *testing.T) {
	t.Parallel()
	in(t, factory.AutonomyAuto, func(t *testing.T, mode string) {
		s := Opened(t, mode)
		s.Delegate.Next(factory.FollowPRStep, Round{Result: NeedsYou("The failing dependency audit predates this change; it needs a scope decision.")})
		turns := &stopTurns{}
		s.StopAgent(func(it factory.Item, wait factory.Wait) (string, error) {
			turns.took(it, wait)
			turns.pressed(s.Decide(factory.DecideWaitGitHub, "", "The audit fails on main too; nothing here needs changing."))
			return "Nothing to do until GitHub changes.", nil
		})
		it := s.Do("a check fails and Follow PR asks what is in scope", func() {
			s.GitHub.SetChecks(1, 1, 0)
			s.Poll()
		})
		_, said := turns.all()
		if it.Wait.Who != factory.WaitGitHub || len(said) != 1 || !said[0].OK() || it.Question != "" {
			t.Fatalf("after wait_github: wait %s, presses %v, question %q\n%s", waitKey(it), said, it.Question, s.Story())
		}
		rounds := followRounds(s)
		it = s.Do("GitHub is read again and nothing has changed", s.Poll)
		if followRounds(s) != rounds || it.Wait.Who != factory.WaitGitHub || turns.count() != 1 {
			t.Fatalf("the same facts started something: %d rounds, wait %s, %d turns\n%s", followRounds(s)-rounds, waitKey(it), turns.count(), s.Story())
		}
		it = s.Do("someone pushes to the branch and its checks fail", func() {
			s.OutsidePush()
			s.GitHub.SetChecks(0, 2, 0)
			s.Poll()
		})
		if followRounds(s) != rounds+1 {
			t.Fatalf("a new event started no round: wait %s\n%s", waitKey(it), s.Story())
		}
	})
}

// THE PERSON PRESSES THEIR CARD WHILE THE MAIN AGENT'S TURN IS IN FLIGHT.
// While the agent holds the stop the properties hold, the person's offers
// among them. The person's answer is taken; the agent's decision, arriving
// second, is refused in a sentence and changes nothing: the step gets one
// more round, not two.
func TestScenarioThePersonAnswersWhileTheMainAgentDecides(t *testing.T) {
	t.Parallel()
	s := NewScenario(t, factory.AutonomyManual, "write", "test", "proof")
	s.Delegate.Next("test", Round{Result: notGreen()}, Round{Result: notGreen()}, Round{Result: notGreen()})
	turns := &stopTurns{}
	entered, release, ended := make(chan struct{}), make(chan struct{}), make(chan struct{})
	s.StopAgent(func(it factory.Item, wait factory.Wait) (string, error) {
		defer close(ended)
		turns.took(it, wait)
		close(entered)
		<-release
		turns.pressed(s.Decide(factory.DecideAnotherRound, "test", "Try it once more."))
		return "Gave test another round.", nil
	})
	s.Events = append(s.Events, "start, and test runs out of rounds")
	s.MustPress(factory.DoStart)
	select {
	case <-entered:
	case <-time.After(30 * time.Second):
		t.Fatalf("the main agent was never handed the stop\n%s", s.Story())
	}
	it := s.Item()
	if it.Wait.Who != factory.WaitMainAgent || it.Wait.What != factory.ActAnswer || it.Wait.Deadline.IsZero() {
		t.Fatalf("while the main agent decides the job waits on %s\n%s", waitKey(it), s.Story())
	}
	if !factory.Offered(it, factory.OfferByYou, factory.DoAnswer) || !factory.Offered(it, factory.OfferByMainAgent, factory.DecideAnotherRound) {
		t.Fatalf("while the main agent decides the job offers %v\n%s", it.Offers, s.Story())
	}
	s.Check()
	s.Events = append(s.Events, "the person gives it one more round while the main agent decides")
	s.MustPress(factory.DoAnswer)
	close(release)
	select {
	case <-ended:
	case <-time.After(30 * time.Second):
		t.Fatalf("the main agent's turn never ended\n%s", s.Story())
	}
	it = s.Do("the main agent's decision arrives second", nil)
	_, said := turns.all()
	if len(said) != 1 || said[0].OK() || !strings.Contains(said[0].Error, "not on a stop the main agent may decide") {
		t.Fatalf("the main agent's late decision: %v\n%s", said, s.Story())
	}
	if got := s.Delegate.Rounds("test"); got != 4 {
		t.Fatalf("test ran %d rounds, want its 3 and the person's one\n%s", got, s.Story())
	}
	if logged(it, "Main agent:") || logged(it, factory.HandoffUnsettled) {
		t.Fatalf("the log says the main agent decided or gave up a stop its person settled\n%s", s.Story())
	}
}

// THE SERVICE RESTARTS IN THE MIDDLE OF THE MAIN AGENT'S TURN. The turn is
// lost with the service that gave it, no second one is given, and the stop is
// the person's as soon as the new service looks. The stop is a failed step,
// which no runner holds, so the restart is of the service alone.
func TestScenarioARestartInTheMiddleOfTheMainAgentsTurn(t *testing.T) {
	t.Parallel()
	s := NewScenario(t, factory.AutonomyAuto, "write", "proof")
	s.Delegate.Next("write", Round{Err: errors.New("the model refused the request: invalid tool schema")})
	turns := &stopTurns{}
	entered, release := make(chan struct{}, 4), make(chan struct{})
	defer close(release)
	s.StopAgent(func(it factory.Item, wait factory.Wait) (string, error) {
		turns.took(it, wait)
		entered <- struct{}{}
		<-release
		return "", nil
	})
	s.Events = append(s.Events, "start, and write fails")
	s.MustPress(factory.DoStart)
	select {
	case <-entered:
	case <-time.After(30 * time.Second):
		t.Fatalf("the main agent was never handed the stop\n%s", s.Story())
	}
	// The stop is handed over the moment the failure is reported, a breath
	// before the runner has let the job go: the restart is of a service
	// whose runner holds nothing.
	for deadline := time.Now().Add(30 * time.Second); s.held() || s.Item().State == factory.StateRunning; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the runner never let the failed step go\n%s", s.Story())
		}
	}
	if it := s.Item(); it.Wait.Who != factory.WaitMainAgent || it.Wait.What != factory.ActRetry {
		t.Fatalf("while the main agent decides the job waits on %s\n%s", waitKey(it), s.Story())
	}
	it := s.Do("the app quits and reopens while the main agent decides", s.RestartService)
	if it.Wait.Who != factory.WaitYou || it.Wait.Action != factory.ActRetry || turns.count() != 1 {
		t.Fatalf("after the restart: wait %s, %d turns\n%s", waitKey(it), turns.count(), s.Story())
	}
	if !logged(it, factory.HandoffLine(factory.HandoffLate)) {
		t.Fatalf("the log does not say the main agent did not answer\n%s", s.Story())
	}
	it = s.Do("the person runs the step again", func() { s.MustPress(factory.DoRerunStep) })
	if s.Delegate.Rounds("write") != 2 || it.Failure != nil {
		t.Fatalf("the person's rerun did not run: %d rounds, failure %+v\n%s", s.Delegate.Rounds("write"), it.Failure, s.Story())
	}
}

// THE MAIN AGENT ACCEPTS A STEP AS IT STANDS, WITH ITS REASON, OR SENDS THE
// WORK BACK. Both are the person's own answers pressed for them: the accept
// is taken only where the job offers it and carries the run on, the send back
// runs the step before again, and each is on the job's log with its reason.
func TestScenarioTheMainAgentAcceptsAStepOrSendsItBack(t *testing.T) {
	t.Parallel()
	for _, decision := range []string{factory.DecideAccept, factory.DecideSendBack} {
		t.Run(decision, func(t *testing.T) {
			t.Parallel()
			s := NewScenario(t, factory.AutonomyManual, "write", "test", "proof")
			s.Delegate.Next("test", Round{Result: notGreen()}, Round{Result: notGreen()}, Round{Result: notGreen()})
			turns := &stopTurns{}
			const why = "Both failures are on the base branch too."
			s.StopAgent(func(it factory.Item, wait factory.Wait) (string, error) {
				turns.took(it, wait)
				if decision == factory.DecideAccept && !factory.Offered(it, factory.OfferByMainAgent, factory.DecideAccept) {
					t.Errorf("accept is not offered to the main agent: %v", it.Offers)
				}
				turns.pressed(s.Decide(decision, "test", why))
				return "Settled.", nil
			})
			it := toStop(s, turns, func(it factory.Item) bool { return s.Delegate.Rounds("proof") > 0 })
			_, said := turns.all()
			if len(said) != 1 || !said[0].OK() {
				t.Fatalf("the main agent's press: %v\n%s", said, s.Story())
			}
			switch decision {
			case factory.DecideAccept:
				if s.Delegate.Rounds("test") != 3 || s.Delegate.Rounds("write") != 1 || !logged(it, "Main agent: accepted the step as it stands · "+why) {
					t.Fatalf("after the accept: test ran %d rounds, write %d\n%s", s.Delegate.Rounds("test"), s.Delegate.Rounds("write"), s.Story())
				}
			default:
				if s.Delegate.Rounds("write") != 2 || s.Delegate.Rounds("test") != 4 || !logged(it, "Main agent: sent back · "+why) {
					t.Fatalf("after the send back: write ran %d rounds, test %d\n%s", s.Delegate.Rounds("write"), s.Delegate.Rounds("test"), s.Story())
				}
				if !s.Delegate.Heard("write", 2, why) {
					t.Fatalf("the step sent back to never heard why: %v\n%s", s.Delegate.notes(), s.Story())
				}
			}
			for _, e := range s.Events {
				if strings.HasSuffix(e, "-> you:answer") {
					t.Fatalf("the person was asked about a stop the main agent settled: %s\n%s", e, s.Story())
				}
			}
		})
	}
}

// A PUBLISH THAT FAILED AFTER ITS APPROVAL IS RETRIED BY THE MAIN AGENT. The
// approval the person gave still stands for that commit, so the retry needs
// no second one, in Manual as in Auto.
func TestScenarioAFailedPublishIsRetriedByTheMainAgent(t *testing.T) {
	t.Parallel()
	s := Opened(t, factory.AutonomyManual)
	turns := &stopTurns{}
	s.StopAgent(func(it factory.Item, wait factory.Wait) (string, error) {
		turns.took(it, wait)
		s.GitHub.Down(false)
		turns.pressed(s.Decide(factory.DecideRetryPublish, "", "GitHub is reachable again."))
		return "Retried the publish.", nil
	})
	it := s.Do("a check fails and the repair waits for its push approval", func() {
		s.GitHub.SetChecks(1, 1, 0)
		s.Poll()
	})
	if !factory.FollowUpUpdateWaiting(it) || turns.count() != 0 {
		t.Fatalf("the repair is not waiting to be pushed: %+v, %d turns\n%s", it.FollowUp, turns.count(), s.Story())
	}
	it = s.Do("GitHub goes down and the person approves the push", func() {
		s.GitHub.Down(true)
		s.MustPress(factory.DoApprove)
	})
	stops, said := turns.all()
	if !slices.Equal(stops, []string{factory.StopPublish + ":" + factory.ActRetryPublish}) || len(said) != 1 || !said[0].OK() {
		t.Fatalf("the main agent's turns: %v, its presses: %v; the job waits on %s\n%s", stops, said, waitKey(it), s.Story())
	}
	if it.State != factory.StateShipped || it.PullRequest.HeadSHA != it.HeadCommit || it.PublishError != "" {
		t.Fatalf("the retried publish did not push the repair: state %s, error %q\n%s", it.State, it.PublishError, s.Story())
	}
	if !logged(it, "Main agent: retried publishing · GitHub is reachable again.") {
		t.Fatalf("the log does not say what the main agent decided\n%s", s.Story())
	}
}
