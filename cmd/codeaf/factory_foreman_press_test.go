package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/session"
)

// foremanHand is the Factory manager's door over st, as its tool holds it.
func foremanHand(t *testing.T, st *store.Store, web, profile string) session.FloorHand {
	t.Helper()
	hand, ok := floorDoor(st, web, profile).(session.FloorHand)
	if !ok {
		t.Fatal("the Factory manager's door cannot press or pass words on")
	}
	return hand
}

// The jobs the Factory manager presses on, one for each offer, beside the
// ones factory_manager_acts_test.go already shapes.
var planStages = []factory.Stage{{Name: "plan", Ask: "plan", On: true}, {Name: factory.ApproveName, Kind: factory.StageGate, On: true}, {Name: "write", Kind: factory.StageChat, On: true, Until: factory.UntilProven}}

func waitsOnPlanApproval(it *factory.Item) {
	it.State, it.QKind, it.Assignee, it.Cap = factory.StateNeedsYou, factory.QKindApprove, "sam", 5
	it.Question = "Plan ready. Approve it?"
	it.Stages = planStages
	it.Stream = &factory.Stream{Cur: 1, Phases: []factory.Phase{{Name: "plan", Kind: factory.StageChat, State: factory.PhaseDone, Round: 1}, {Name: factory.ApproveName, Kind: factory.StageGate, State: factory.PhaseWaiting}, {Name: "write", Kind: factory.StageChat, State: factory.PhasePending}}}
}

func asksAStepQuestion(it *factory.Item) {
	it.State, it.QKind, it.Assignee, it.Cap = factory.StateNeedsYou, factory.QKindStep, "sam", 5
	it.Question = "round half up or half even?"
	it.Stages = planStages
	it.Stream = &factory.Stream{Cur: 2, Phases: []factory.Phase{{Name: "plan", Kind: factory.StageChat, State: factory.PhaseDone, Round: 1}, {Name: factory.ApproveName, Kind: factory.StageGate, State: factory.PhaseDone}, {Name: "write", Kind: factory.StageChat, State: factory.PhaseWaiting, Round: 1}}}
	it.Asking = &factory.Asked{Stage: "write", Question: it.Question, Options: []string{"Half up", "Half even"}, With: factory.AskedYou, At: time.Now()}
}

func isOutOfRounds(it *factory.Item) {
	asksAStepQuestion(it)
	it.QKind, it.Asking = factory.QKindOutOfAttempts, nil
	it.Question = "write is not proven after 3 rounds · try again, send it back, or stop the job?"
	it.Stream.Phases[2].Round = 3
	it.Stream.Phases[2].Result = &factory.StageResult{Done: true}
}

func isHeld(it *factory.Item) {
	asksAStepQuestion(it)
	it.State, it.QKind, it.Question, it.Asking = factory.StateRunning, "", "", nil
	it.Stream.Phases[2].State = factory.PhaseRunning
	it.Stream.Paused = true
}

func failedAStep(it *factory.Item) {
	isHeld(it)
	it.Stream.Paused = false
	it.State = factory.StateNeedsYou
	it.Stream.Phases[2].State = factory.PhaseFailed
	it.Failure = &factory.Failure{Step: "write", Code: factory.CodeToolError, Summary: "the build broke", At: time.Now()}
}

func spentItsBudget(it *factory.Item) {
	failedAStep(it)
	it.Stream.Spent = 6
}

func hasChangesRequested(it *factory.Item) {
	hasMergeablePullRequest(it)
	it.PullRequest.ReviewDecision = "changes_requested"
}

func autoPausedOnAFinding(it *factory.Item) {
	waitsOnPublishApproval(it)
	it.Autonomy = factory.AutonomyAuto
	it.Policy = []factory.Claim{{Text: "FINDING: a vendored secret"}}
}

func failedToPublish(it *factory.Item) {
	waitsOnPublishApproval(it)
	it.PublishError = "the push was refused"
}

func hasProposedActs(it *factory.Item) {
	hasDraftPullRequest(it)
	it.Autonomy = factory.AutonomyManual
	if _, err := factory.AddActs(it, []factory.PublicAct{{Kind: factory.ActKindComment, Text: "Rebased on main."}}, factory.ActByMainAgent, "the review is addressed", time.Now()); err != nil {
		panic(err)
	}
}

// EACH PRESS SENDS THE VERB AND THE BODY ITS DOOR EXPECTS, as the person, for
// a job that offers it. The factory's handlers refuse an unknown field, so
// the body is the whole contract: an approval names the job's head commit, a
// merge the pull request's head, a plan sent back the document it was read
// at, a task who asked.
func TestEachFactoryManagerPressSendsItsVerbAndBody(t *testing.T) {
	st, web, profile := talkLab(t)
	sent := fakePress(t, nil)
	hand := foremanHand(t, st, web, profile)
	presses := []struct {
		name  string
		shape func(*factory.Item)
		press session.FloorPress
		verb  string
		// body may hold one %d, the job's document revision.
		body string
		says string
	}{
		{"publication", waitsOnPublishApproval, session.FloorPress{Action: factory.DoApprove}, "approve", `{"commit":"abc"}`, "Publication of {ref} approved, as the person."},
		{"plan", waitsOnPlanApproval, session.FloorPress{Action: factory.DoApprove, Words: "go ahead"}, "approve", `{"words":"go ahead","commit":""}`, "its plan is approved, as the person"},
		{"plan sent back", waitsOnPlanApproval, session.FloorPress{Action: factory.DoRejectPlan, Words: "Name the files the plan touches."}, "reject_plan",
			`{"words":"Name the files the plan touches.","expect":{"state":"needs_you","version":%d}}`, "plan is sent back with the person's words"},
		{"a step's question in words", asksAStepQuestion, session.FloorPress{Action: factory.DoAnswer, Words: "Half even, as the bank does."}, "answer",
			`{"yes":true,"words":"Half even, as the bank does."}`, "Answered {ref} for the person: Half even, as the bank does."},
		{"a step's question by its choice", asksAStepQuestion, session.FloorPress{Action: factory.DoAnswer, Choice: "half even", Words: "as the bank does"}, "answer",
			`{"yes":true,"words":"Half even: as the bank does"}`, "Half even: as the bank does"},
		{"out of rounds, once more", isOutOfRounds, session.FloorPress{Action: factory.DoAnswer, Choice: "try_once_more"}, "answer", `{"choice":"try_once_more"}`, "try_once_more"},
		{"out of rounds, sent back", isOutOfRounds, session.FloorPress{Action: factory.DoAnswer, Choice: "send_back", Words: "the plan misses refunds"}, "answer",
			`{"choice":"send_back","reason":"the plan misses refunds"}`, "send_back (the plan misses refunds)"},
		{"out of rounds, accepted", isOutOfRounds, session.FloorPress{Action: factory.DoAcceptStep}, "answer", `{"choice":"accept"}`, "accepted as it stands, as the person"},
		{"acts approved", hasProposedActs, session.FloorPress{Action: factory.DoActsApprove}, "acts_approve", `{}`, "are approved, as the person"},
		{"acts declined", hasProposedActs, session.FloorPress{Action: factory.DoActsDecline, Words: "not before the release"}, "acts_decline", `{"words":"not before the release"}`, "are declined. Nothing was sent."},
		{"merge", hasMergeablePullRequest, session.FloorPress{Action: factory.DoMerge}, "merge", `{"commit":"abc"}`, "Merge pressed for pull request #161 of {ref}, as the person."},
		{"ready", hasDraftPullRequest, session.FloorPress{Action: factory.DoMarkReady}, "pr-ready", `{}`, "Pull request #161 of {ref} is marked ready for review."},
		{"publish again", failedToPublish, session.FloorPress{Action: factory.DoRetryPublish}, "retry_publish", `{}`, "queued to be tried again, as the person"},
		{"a held job", isHeld, session.FloorPress{Action: factory.DoResume}, "resume", `{}`, "{ref} resumed, as the person."},
		{"the pipeline", hasChangesRequested, session.FloorPress{Action: factory.DoResumePipeline, Words: "take the base in first"}, "control",
			`{"mode":"resume_pipeline","words":"take the base in first"}`, "pipeline is resumed, as the person"},
		{"Auto past a finding", autoPausedOnAFinding, session.FloorPress{Action: factory.DoResumeAuto, Words: "carry on"}, "resume_auto", `{"words":"carry on"}`, "it carries on past repository policy: FINDING: a vendored secret"},
		{"a failed step", failedAStep, session.FloorPress{Action: factory.DoRerunStep}, "steps/write/rerun", `{}`, "{ref} runs its write step again, as the person"},
		{"a new job", func(*factory.Item) {}, session.FloorPress{Action: factory.DoStart}, "start_now", `{}`, "{ref} started"},
		{"a task", isHeld, session.FloorPress{Action: factory.DoTask, Words: "Rebase on main.\nRun the ledger tests."}, "task",
			`{"words":"Rebase on main.\nRun the ledger tests.","now":false,"by":"you"}`, "Task taken for {ref}, as the person"},
		{"the budget", spentItsBudget, session.FloorPress{Action: factory.DoRaiseBudget, Budget: 10}, "raise_budget", `{"budget":10}`, "{ref}'s budget is now $10.00, as the person."},
	}
	for _, p := range presses {
		id, _ := managedJob(t, st, p.shape)
		it, err := st.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if !factory.OffersPerson(it, p.press.Action) {
			t.Errorf("%s: the job does not offer %s, it offers %v (%s)", p.name, p.press.Action, factory.PersonOffers(it), factory.FloorWaitLine(it))
			continue
		}
		body := p.body
		if strings.Contains(body, "%d") {
			version, err := st.Revision(id)
			if err != nil {
				t.Fatal(err)
			}
			body = fmt.Sprintf(body, version)
		}
		*sent = nil
		line, err := hand.Press(context.Background(), id, p.press, "do that for me")
		if err != nil || len(*sent) != 1 {
			t.Errorf("%s: %q (%v), sent %+v", p.name, line, err, *sent)
			continue
		}
		if got := (*sent)[0]; got.job != id || got.verb != p.verb || got.body != body {
			t.Errorf("%s sent %s %s\nwant      %s %s", p.name, got.verb, got.body, p.verb, body)
		}
		says := strings.ReplaceAll(p.says, "{ref}", it.Ref())
		if !strings.Contains(line, says) {
			t.Errorf("%s answered %q, want it to say %q", p.name, line, says)
		}
		if strings.Contains(line, "factory_status") {
			t.Errorf("%s points at a tool the Factory manager cannot use: %q", p.name, line)
		}
	}
}

// THE PERSON'S OWN WORDS ARE THE AUTHORITY, AND THE DOOR HOLDS IT TOO: with
// none nothing is pressed, in Manual and in Auto, merge included. An action
// the job does not offer now is refused with the job's own wait, so the
// person can be told what it does accept; the two the job page keeps are
// named; and a factory's refusal comes back in its words.
func TestTheFactoryManagerPressesOnlyWhatAJobOffersAndOnlyOnThePersonsWord(t *testing.T) {
	st, web, profile := talkLab(t)
	sent := fakePress(t, nil)
	hand := foremanHand(t, st, web, profile)
	for _, mode := range []string{factory.AutonomyManual, factory.AutonomyAuto} {
		for action, shape := range map[string]func(*factory.Item){
			factory.DoMerge: hasMergeablePullRequest, factory.DoApprove: waitsOnPublishApproval, factory.DoMarkReady: hasDraftPullRequest, factory.DoTask: isHeld,
		} {
			id, _ := managedJob(t, st, func(it *factory.Item) {
				shape(it)
				it.Autonomy = mode
			})
			if _, err := hand.Press(context.Background(), id, session.FloorPress{Action: action, Words: "the person told me to"}, " "); err == nil || err.Error() != session.FloorPressNoWords {
				t.Errorf("%s in %s with no person words = %v", action, mode, err)
			}
		}
	}
	if len(*sent) != 0 {
		t.Fatalf("a turn the person did not speak in pressed %+v", *sent)
	}
	// NOT OFFERED NOW: refused with what the job waits on and accepts.
	id, _ := managedJob(t, st, hasDraftPullRequest)
	it, _ := st.Get(id)
	_, err := hand.Press(context.Background(), id, session.FloorPress{Action: factory.DoMerge}, "merge it")
	want := fmt.Sprintf("Nothing changed: %s does not accept merge now. It waits on GitHub (a review). It accepts from the person now: pr_ready", it.Ref())
	if err == nil || !strings.HasPrefix(err.Error(), want) {
		t.Errorf("a merge of a draft = %v\nwant it to begin %q", err, want)
	}
	for _, action := range session.FloorPressElsewhere {
		if _, err := hand.Press(context.Background(), id, session.FloorPress{Action: action}, "do it"); err == nil || !strings.Contains(err.Error(), action+" is a choice the person makes on the job page") {
			t.Errorf("%s = %v", action, err)
		}
	}
	if _, err := hand.Press(context.Background(), id, session.FloorPress{Action: "force_merge"}, "do it"); err == nil || !strings.Contains(err.Error(), "does not accept force_merge now") {
		t.Errorf("an unknown action = %v", err)
	}
	if len(*sent) != 0 {
		t.Fatalf("a refused press sent %+v", *sent)
	}
	// THE FACTORY'S OWN REFUSAL COMES BACK IN ITS WORDS, and a window with no
	// signed-in connection says so.
	fakePress(t, func(string) error { return errors.New("the pull request has no approving review") })
	merge, _ := managedJob(t, st, hasMergeablePullRequest)
	if _, err := hand.Press(context.Background(), merge, session.FloorPress{Action: factory.DoMerge}, "merge it"); err == nil || err.Error() != "Nothing changed: the pull request has no approving review" {
		t.Errorf("a merge the factory refused = %v", err)
	}
	prev := appLocalPress.Swap(nil)
	t.Cleanup(func() { appLocalPress.Store(prev) })
	if _, err := hand.Press(context.Background(), merge, session.FloorPress{Action: factory.DoMerge}, "merge it"); !errors.Is(err, errNoPress) {
		t.Errorf("a merge with no connection = %v", err)
	}
}

// AN ANSWER NEVER STOPS A JOB BY ACCIDENT. A step that used its rounds reads
// any words but its own choices as stop, so that question takes a choice and
// nothing else, stopping is never pressed from here, and an approval is not
// answered in words. A press that needs words or an amount says so. None of
// these sends anything.
func TestTheFactoryManagersAnswerTakesOnlyWhatTheQuestionTakes(t *testing.T) {
	st, web, profile := talkLab(t)
	sent := fakePress(t, nil)
	hand := foremanHand(t, st, web, profile)
	refused := []struct {
		shape func(*factory.Item)
		press session.FloorPress
		want  string
	}{
		{isOutOfRounds, session.FloorPress{Action: factory.DoAnswer, Words: "try it with the race detector on"}, "takes one of these choices and no other words: try_once_more, send_back, accept."},
		{isOutOfRounds, session.FloorPress{Action: factory.DoAnswer, Choice: "cancel_job"}, "Stopping a job is done on the job page"},
		{isOutOfRounds, session.FloorPress{Action: factory.DoAnswer, Choice: "send_back"}, "send_back needs the person's words for why"},
		{waitsOnPlanApproval, session.FloorPress{Action: factory.DoAnswer, Words: "yes"}, "waits on an approval, not an answer. Press approve when the person says yes"},
		{asksAStepQuestion, session.FloorPress{Action: factory.DoAnswer}, "an answer needs the person's words, or one of the question's choices"},
		{asksAStepQuestion, session.FloorPress{Action: factory.DoAnswer, Choice: "banker's"}, "lists these choices: Half up, Half even"},
		{waitsOnPlanApproval, session.FloorPress{Action: factory.DoRejectPlan}, "needs the person's words for what to change"},
		{isHeld, session.FloorPress{Action: factory.DoTask}, "a task needs the person's instructions in words"},
		{spentItsBudget, session.FloorPress{Action: factory.DoRaiseBudget}, "set budget to the amount the person named: 5, 10, 20 or 30 USD"},
		{spentItsBudget, session.FloorPress{Action: factory.DoRaiseBudget, Budget: 7}, "set budget to the amount the person named"},
		{spentItsBudget, session.FloorPress{Action: factory.DoRaiseBudget, Budget: 5}, "has already spent $6.00, so its budget must be above that"},
	}
	for _, r := range refused {
		id, _ := managedJob(t, st, r.shape)
		_, err := hand.Press(context.Background(), id, r.press, "do that")
		if err == nil || !strings.HasPrefix(err.Error(), "Nothing changed: ") || !strings.Contains(err.Error(), r.want) {
			t.Errorf("%+v = %v, want it to say %q", r.press, err, r.want)
		}
	}
	if len(*sent) != 0 {
		t.Fatalf("a refused press sent %+v", *sent)
	}
}

// THE HOLDS THE JOB'S OWN MANAGER KEEPS ARE THE FACTORY MANAGER'S TOO: an
// approval a rule asks the person for, and one that names someone else, are
// pressed on the job page, whatever was said in a chat.
func TestTheFactoryManagerLeavesAHeldApprovalToItsPerson(t *testing.T) {
	st, web, profile := talkLab(t)
	sent := fakePress(t, nil)
	hand := foremanHand(t, st, web, profile)
	ruled, _ := managedJob(t, st, func(it *factory.Item) {
		waitsOnPlanApproval(it)
		it.RuleHold = "ask first · touch auth/ · auth/keys.go"
	})
	if _, err := hand.Press(context.Background(), ruled, session.FloorPress{Action: factory.DoApprove}, "approve it"); err == nil || !strings.Contains(err.Error(), "a rule asks first: ask first · touch auth/ · auth/keys.go") {
		t.Errorf("a rule-held plan = %v", err)
	}
	named, _ := managedJob(t, st, func(it *factory.Item) {
		waitsOnPlanApproval(it)
		it.Stages = append([]factory.Stage(nil), planStages...)
		it.Stages[1].Who = "@rel"
	})
	if _, err := hand.Press(context.Background(), named, session.FloorPress{Action: factory.DoApprove}, "approve it"); err == nil || !strings.Contains(err.Error(), "waits on @rel") {
		t.Errorf("an approval that names someone else = %v", err)
	}
	if len(*sent) != 0 {
		t.Fatalf("a held approval was pressed: %+v", *sent)
	}
}

// THE PERSON'S YES IS FOR WHAT THEY HAVE READ, AND THE FACTORY MANAGER'S TOO.
// Approving the acts, or the publication that carries them, presses nothing
// when a job's main agent proposed an act after the person's words opened this
// turn: they have not read it. An act proposed before is theirs to approve,
// and in Auto the job's main agent carries its own.
func TestTheFactoryManagerMayNotApproveActsProposedSinceThePersonSpoke(t *testing.T) {
	st, web, profile := talkLab(t)
	sent := fakePress(t, nil)
	hand := foremanHand(t, st, web, profile)
	began := time.Now()
	for _, c := range []struct {
		name     string
		shape    func(*factory.Item)
		press    string
		verb     string
		mode     string
		proposed time.Time
		pressed  bool
	}{
		{"acts, proposed since the person spoke", hasDraftPullRequest, factory.DoActsApprove, "acts_approve", factory.AutonomyManual, began.Add(time.Second), false},
		{"acts, proposed before the person spoke", hasDraftPullRequest, factory.DoActsApprove, "acts_approve", factory.AutonomyManual, began.Add(-time.Minute), true},
		{"publication, acts proposed since the person spoke", waitsOnPublishApproval, factory.DoApprove, "approve", factory.AutonomyManual, began.Add(time.Second), false},
		{"publication, acts proposed before the person spoke", waitsOnPublishApproval, factory.DoApprove, "approve", factory.AutonomyManual, began.Add(-time.Minute), true},
		{"publication in Auto", waitsOnPublishApproval, factory.DoApprove, "approve", factory.AutonomyAuto, began.Add(time.Second), true},
	} {
		id, _ := managedJob(t, st, func(it *factory.Item) {
			c.shape(it)
			it.Autonomy = c.mode
			it.Acts = []factory.PublicAct{{ID: "a1", Kind: factory.ActKindReply, Text: "Fixed in the new commit.", By: factory.ActByMainAgent, State: factory.ActProposed, At: c.proposed}}
		})
		*sent = nil
		_, err := hand.Press(context.Background(), id, session.FloorPress{Action: c.press, TurnAt: began}, "yes, do it")
		switch {
		case c.pressed && (err != nil || len(*sent) != 1 || (*sent)[0].verb != c.verb || (*sent)[0].job != id):
			t.Errorf("%s: %v, sent %+v; want %s pressed", c.name, err, *sent, c.verb)
		case !c.pressed && (err == nil || !strings.HasPrefix(err.Error(), "Nothing changed: ") || !strings.Contains(err.Error(), "have not read them yet") || len(*sent) != 0):
			t.Errorf("%s: %v, sent %+v; want a refusal and nothing sent", c.name, err, *sent)
		}
		if got, err := st.Get(id); err != nil {
			t.Fatal(err)
		} else if len(got.Acts) != 1 || got.Acts[0].State != factory.ActProposed {
			t.Errorf("%s: the act is %+v, want it still proposed", c.name, got.Acts)
		}
	}
}

// A DECISION THE PERSON PUT AWAY IS TAKEN UP AGAIN through the door the job's
// own chat takes it up by: the factory's resume verb refuses such a job, so
// nothing is pressed, and the job goes on with the person's words on it.
func TestTheFactoryManagerTakesUpADecisionThePersonPutAway(t *testing.T) {
	st, web, profile := talkLab(t)
	sent := fakePress(t, nil)
	hand := foremanHand(t, st, web, profile)
	id, _ := managedJob(t, st, func(it *factory.Item) {
		hasChangesRequested(it)
		factory.PauseDecision(it, 3)
	})
	it, _ := st.Get(id)
	if !factory.RecoveryPaused(it) || !factory.OffersPerson(it, factory.DoResume) {
		t.Fatalf("the lab's job is not put away: %s", factory.FloorWaitLine(it))
	}
	line, err := hand.Press(context.Background(), id, session.FloorPress{Action: factory.DoResume, Words: "take the base in first"}, "resume it")
	if err != nil || !strings.Contains(line, "is taken up again, as the person") {
		t.Fatalf("resume = %q (%v)", line, err)
	}
	after, _ := st.Get(id)
	if factory.RecoveryPaused(after) || len(*sent) != 0 {
		t.Errorf("the decision is still put away (%v), or a verb was pressed: %+v", factory.RecoveryPaused(after), *sent)
	}
}

// A START THAT WOULD CARRY A JOB PAST AN APPROVE STEP IS THE PERSON'S. The
// Factory manager's start answers yes to a job held at one, so on a turn the
// person did not speak in it is refused, in Manual and in Auto: Auto's
// standing yes is the job's own manager's. With their words the start goes
// on to the floor's own rules.
func TestTheFactoryManagersStartDoesNotPassAnApproveStepByItself(t *testing.T) {
	st, web, profile := talkLab(t)
	sent := fakePress(t, nil)
	actor := floorDoor(st, web, profile).(session.FloorActor)
	for _, mode := range []string{factory.AutonomyManual, "", factory.AutonomyAuto} {
		id, _ := managedJob(t, st, func(it *factory.Item) {
			waitsOnPlanApproval(it)
			it.Autonomy = mode
			it.Stages = append([]factory.Stage(nil), planStages...)
			it.Stages[1].Who = "@rel"
		})
		_, err := actor.StartItem(context.Background(), id, "")
		if err == nil || !strings.HasPrefix(err.Error(), "Nothing changed: ") || !strings.Contains(err.Error(), "nothing in this turn came from them") {
			t.Errorf("a start past an approve step in %q with no person words = %v", mode, err)
		}
		if got, _ := st.Get(id); got.State != factory.StateNeedsYou || got.QKind != factory.QKindApprove {
			t.Errorf("in %q the held job moved to %s %q", mode, got.State, got.QKind)
		}
		// With the person's words the authority is theirs, and the floor's own
		// rule answers: this approval names someone else.
		if _, err := actor.StartItem(context.Background(), id, "start it"); err == nil || !strings.Contains(err.Error(), "waits on @rel") {
			t.Errorf("a start the person asked for in %q = %v", mode, err)
		}
	}
	if len(*sent) != 0 {
		t.Fatalf("a held start pressed %+v", *sent)
	}
}

// fakeTell hangs a turn-giver that records what the job's main agent was
// asked and answers says, or fails.
func fakeTell(t *testing.T, says string, fail error) *[]string {
	t.Helper()
	var asked []string
	tell := localJobTell(func(_ context.Context, it factory.Item, ask string) (string, error) {
		asked = append(asked, fmt.Sprintf("%d %s", it.ID, ask))
		return says, fail
	})
	prev := appLocalTell.Swap(&tell)
	t.Cleanup(func() { appLocalTell.Store(prev) })
	return &asked
}

// THE PERSON'S WORDS REACH A JOB'S MAIN AGENT AS THEIRS, AND AS PASSED ON:
// the note says who said them and how they came, and that the turn does not
// carry the person's own words. What the main agent said comes back for the
// Factory manager to relay, with what that turn could not do in the job's
// mode, so it promises nothing the factory would refuse.
func TestTheFactoryManagerPassesThePersonsWordsToAJobsMainAgent(t *testing.T) {
	st, web, profile := talkLab(t)
	hand := foremanHand(t, st, web, profile)
	asked := fakeTell(t, "  Rebased on main and sent a delegate to rerun the ledger tests. Publishing still needs your approval.  ", nil)
	id, _ := managedJob(t, st, func(it *factory.Item) {
		isHeld(it)
		it.Autonomy = factory.AutonomyManual
	})
	it, _ := st.Get(id)
	// ONLY ON THE PERSON'S WORD.
	if _, err := hand.Tell(context.Background(), id, "rebase on main", ""); err == nil || err.Error() != session.FloorTellNoWords || len(*asked) != 0 {
		t.Fatalf("words passed on with none of the person's = %v, asked %v", err, *asked)
	}
	line, err := hand.Tell(context.Background(), id, "  Rebase on main, then run the ledger tests.  ", "tell it to rebase and rerun the ledger tests")
	if err != nil || len(*asked) != 1 {
		t.Fatalf("tell = %q (%v), asked %v", line, err, *asked)
	}
	for _, want := range []string{
		fmt.Sprintf("%d The person told the Factory manager, for you: Rebase on main, then run the ledger tests.\n\n", id),
		"The factory started this turn to pass that on.",
		"the turn does not carry their own words",
		"merging, and outside Auto approving publication, marking the pull request ready and approving public acts, stay theirs",
		"The Factory manager reads your answer back to them.",
	} {
		if !strings.Contains((*asked)[0], want) {
			t.Errorf("the note does not say %q:\n%s", want, (*asked)[0])
		}
	}
	for _, want := range []string{
		"The words were passed to " + it.Ref() + "'s main agent, which answered: Rebased on main and sent a delegate to rerun the ledger tests. Publishing still needs your approval.",
		"This job is not in Auto, and that turn was the factory's, not the person speaking in the job's conversation: its main agent could not push, approve publication, mark the pull request ready, approve public acts or merge on it. Do not promise any of those",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("the answer does not say %q:\n%s", want, line)
		}
	}
	// IN AUTO THE MAIN AGENT MAY DO MORE BY ITSELF, AND STILL NEVER MERGES.
	auto, _ := managedJob(t, st, func(it *factory.Item) {
		isHeld(it)
		it.Stream.Paused = false
		it.Autonomy = factory.AutonomyAuto
	})
	line, err = hand.Tell(context.Background(), auto, "mark it ready when the checks pass", "tell it")
	if err != nil || !strings.Contains(line, "This job is in Auto") || !strings.Contains(line, "It cannot merge on that turn") || strings.Contains(line, "could not push") {
		t.Errorf("tell in Auto = %q (%v)", line, err)
	}
	// A main agent that said nothing is said to have said nothing.
	fakeTell(t, " ", nil)
	if line, err := hand.Tell(context.Background(), id, "rebase", "tell it"); err != nil || !strings.Contains(line, "It finished its turn and said nothing") {
		t.Errorf("a silent main agent = %q (%v)", line, err)
	}
}

// WHAT A RELAY REFUSES, PLAINLY, before any turn is given: a job with no
// main agent conversation yet, a finished one, one its person stopped, one
// whose budget is spent (the turn is charged to the job), and a window with
// nothing to give the turn with. A turn that could not be given or did not end says which.
func TestTheFactoryManagersRelayRefusesWhatItCannotPassOn(t *testing.T) {
	st, web, profile := talkLab(t)
	hand := foremanHand(t, st, web, profile)
	asked := fakeTell(t, "ok", nil)
	fresh, err := st.Create(factory.Item{Repo: "web", Title: "dark mode", Kind: factory.KindIssue, Tier: factory.TierOwner})
	if err != nil {
		t.Fatal(err)
	}
	done, _ := managedJob(t, st, func(it *factory.Item) {
		hasMergeablePullRequest(it)
		it.PullRequest.State = factory.PRMerged
	})
	spent, _ := managedJob(t, st, spentItsBudget)
	stopped, _ := managedJob(t, st, func(it *factory.Item) {
		isHeld(it)
		factory.MarkPersonStopped(it)
	})
	for id, want := range map[int]string{
		stopped:  "so the factory gives its main agent no turn. They can start it again, or say it in the job's own chat",
		fresh.ID: "has no main agent conversation yet. It gets one when the job starts, or when the person opens the job's chat",
		done:     "is finished, so its main agent takes no more words",
		spent:    "The turn is charged to the job, so its budget is raised first",
	} {
		if _, err := hand.Tell(context.Background(), id, "rebase on main", "tell it"); err == nil || !strings.HasPrefix(err.Error(), "Nothing changed: ") || !strings.Contains(err.Error(), want) {
			t.Errorf("job %d = %v, want it to say %q", id, err, want)
		}
	}
	if len(*asked) != 0 {
		t.Fatalf("a refused relay gave a turn: %v", *asked)
	}
	live, _ := managedJob(t, st, isHeld)
	it, _ := st.Get(live)
	for _, c := range []struct {
		fail error
		want string
	}{
		{fmt.Errorf("%w: its window is mid-turn", factoryrun.ErrManagerBusy), "Nothing changed: " + it.Ref() + "'s main agent is in the middle of a turn of its own"},
		{context.DeadlineExceeded, it.Ref() + "'s main agent did not finish within 3 minutes, so its turn was stopped. What it did before that stands"},
		{errors.New("the provider refused the request"), it.Ref() + "'s main agent did not finish its turn: the provider refused the request. Whether it did anything is not known"},
		{errTellNoModel, "Nothing changed: this machine has no model settings"},
	} {
		fakeTell(t, "", c.fail)
		if _, err := hand.Tell(context.Background(), live, "rebase", "tell it"); err == nil || !strings.HasPrefix(err.Error(), c.want) {
			t.Errorf("%v = %v, want it to begin %q", c.fail, err, c.want)
		}
	}
	prev := appLocalTell.Swap(nil)
	t.Cleanup(func() { appLocalTell.Store(prev) })
	if _, err := hand.Tell(context.Background(), live, "rebase", "tell it"); !errors.Is(err, errNoTell) {
		t.Errorf("a window with no host = %v", err)
	}
}

// THE RELAYED TURN IS ONE TURN OF THE MAIN AGENT'S OWN CONVERSATION, by the
// road the engine's own turns take: it carries the door it was handed (the
// manager's own, so its tools work under their rules), it is stopped after
// the relay's bound, and what the main agent said comes back.
func TestTheRelayedTurnIsTheMainAgentsOwnBoundedTurn(t *testing.T) {
	var asked []string
	var lent session.RunDoor
	var wait time.Duration
	s := &shapeTurns{
		dirs: func(string) string { return "" },
		open: func(ctx context.Context, _ factory.Item, door session.RunDoor, _ bool) (managerTurn, error) {
			lent = door
			if deadline, ok := ctx.Deadline(); ok {
				wait = time.Until(deadline)
			}
			return &fakeStallTurn{asked: &asked, says: "Rebased. Publishing needs your approval."}, nil
		},
	}
	it := factory.Item{ID: 3, Talk: "/x/transcript.jsonl", State: factory.StateRunning, Cap: 10}
	door := stallDoor{name: "the manager's own"}
	said, err := s.Told(context.Background(), it, foremanTellAskFor("rebase on main"), door)
	if err != nil || said != "Rebased. Publishing needs your approval." {
		t.Fatalf("said = %q (%v)", said, err)
	}
	if lent != session.RunDoor(door) {
		t.Errorf("the turn was lent %v, want the door it was handed", lent)
	}
	if wait <= 0 || wait > session.FloorTellWait {
		t.Errorf("the turn may take %v, want at most %v", wait, session.FloorTellWait)
	}
	if len(asked) != 1 || !strings.HasPrefix(asked[0], "The person told the Factory manager, for you: rebase on main") {
		t.Errorf("asked %v", asked)
	}
	// A turn that cannot be given is an error, and a job with no main agent
	// conversation cannot be given one.
	refused := errors.New("the conversation could not be opened")
	s.open = func(context.Context, factory.Item, session.RunDoor, bool) (managerTurn, error) { return nil, refused }
	if _, err := s.Told(context.Background(), it, "x", door); !errors.Is(err, refused) {
		t.Errorf("err = %v", err)
	}
}

// The Factory manager's brief says the three things it now does and the rule
// each is under, and keeps out of the words the journal escapes.
func TestTheFactoryManagersBriefSaysWhatItMayDoForThePerson(t *testing.T) {
	for _, want := range []string{
		"Each job's row says what the job waits on and what it accepts from the person now",
		"ask needs_you for the jobs that wait on the person",
		"press that one action for them with press; it is refused unless their own message in this turn asked for it and the job accepts it now",
		"Before you approve what a job would post or change, show the person exactly what it is",
		"pass them on with tell and report what it answers",
		"outside Auto the job's main agent cannot push, approve, mark ready or merge on it: never promise that it will",
		"Never stop or drop a job, and never press, ship, merge or post on your own judgment",
	} {
		if !strings.Contains(foremanOpening, want) {
			t.Errorf("the brief lacks %q", want)
		}
	}
	if strings.ContainsAny(foremanOpening, `<>"&`) {
		t.Errorf("the brief carries a character the journal escapes")
	}
}

// realPress hangs a press that posts each verb to a real factory service over
// st, as the app's own press does, and answers the service's refusal.
func realPress(t *testing.T, st *store.Store) {
	t.Helper()
	history, err := record.Open(st, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	seam := factory.LocalSeam(st, time.Now(), factoryRunnerDoors(st))
	seam.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "sam"}, nil }
	h := serve.New(seam, serve.Config{Mode: api.FactorySolo, AccessToken: testHostToken, OperatorLogin: "sam", Store: st, Record: history, Root: st.Root()})
	srv := httptest.NewServer(h)
	t.Cleanup(func() { srv.Close(); h.Close() })
	press := localJobPress(func(ctx context.Context, job int, verb string, body any) error {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+jobPath(job, verb), bytes.NewReader(raw))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+testHostToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := srv.Client().Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("%d %s", resp.StatusCode, strings.TrimSpace(string(data)))
		}
		return nil
	})
	prev := appLocalPress.Swap(&press)
	t.Cleanup(func() { appLocalPress.Store(prev) })
}

// THE FACTORY'S OWN DOORS READ EVERY BODY THE FACTORY MANAGER SENDS. The
// doors refuse a field they do not know, so a body spelled wrong would be
// refused for ever and no fake would show it: each press here goes to a real
// service over a real store. A door may still refuse for the job's state (no
// runner holds it, nobody is signed in to GitHub); none refuses the request
// itself.
func TestTheFactorysDoorsReadEveryBodyTheFactoryManagerSends(t *testing.T) {
	st, web, profile := talkLab(t)
	realPress(t, st)
	hand := foremanHand(t, st, web, profile)
	fullSHA := func(shape func(*factory.Item)) func(*factory.Item) {
		return func(it *factory.Item) {
			shape(it)
			it.HeadCommit = strings.Repeat("a", 40)
		}
	}
	presses := []struct {
		name  string
		shape func(*factory.Item)
		press session.FloorPress
		// taken is a press the door takes on this store as it stands; the
		// others are refused for the job's state, which the line names.
		taken   bool
		refused string
	}{
		{"publication", waitsOnPublishApproval, session.FloorPress{Action: factory.DoApprove}, true, ""},
		{"plan", waitsOnPlanApproval, session.FloorPress{Action: factory.DoApprove, Words: "go ahead"}, true, ""},
		{"plan sent back", waitsOnPlanApproval, session.FloorPress{Action: factory.DoRejectPlan, Words: "Name the files the plan touches."}, true, ""},
		{"a step's question", asksAStepQuestion, session.FloorPress{Action: factory.DoAnswer, Choice: "half even", Words: "as the bank does"}, true, ""},
		{"out of rounds, once more", isOutOfRounds, session.FloorPress{Action: factory.DoAnswer, Choice: "try_once_more"}, true, ""},
		{"out of rounds, sent back", isOutOfRounds, session.FloorPress{Action: factory.DoAnswer, Choice: "send_back", Words: "the plan misses refunds"}, true, ""},
		{"out of rounds, accepted", isOutOfRounds, session.FloorPress{Action: factory.DoAcceptStep}, true, ""},
		{"acts approved", hasProposedActs, session.FloorPress{Action: factory.DoActsApprove}, false, "Sign in to GitHub first"},
		{"acts declined", hasProposedActs, session.FloorPress{Action: factory.DoActsDecline, Words: "not before the release"}, true, ""},
		{"merge", hasMergeablePullRequest, session.FloorPress{Action: factory.DoMerge}, true, ""},
		{"ready", hasDraftPullRequest, session.FloorPress{Action: factory.DoMarkReady}, false, "Sign in to GitHub before marking this job ready"},
		{"publish again", failedToPublish, session.FloorPress{Action: factory.DoRetryPublish}, false, "job is not waiting for Retry publishing"},
		{"a held job", isHeld, session.FloorPress{Action: factory.DoResume}, true, ""},
		{"the pipeline", hasChangesRequested, session.FloorPress{Action: factory.DoResumePipeline, Words: "take the base in first"}, true, ""},
		{"Auto past a finding", fullSHA(autoPausedOnAFinding), session.FloorPress{Action: factory.DoResumeAuto, Words: "carry on"}, true, ""},
		{"a failed step", failedAStep, session.FloorPress{Action: factory.DoRerunStep}, true, ""},
		{"a new job", func(*factory.Item) {}, session.FloorPress{Action: factory.DoStart}, true, ""},
		{"a task", isHeld, session.FloorPress{Action: factory.DoTask, Words: "Rebase on main."}, true, ""},
		{"the budget", spentItsBudget, session.FloorPress{Action: factory.DoRaiseBudget, Budget: 10}, true, ""},
	}
	for _, p := range presses {
		id, _ := managedJob(t, st, p.shape)
		_, err := hand.Press(context.Background(), id, p.press, "do that for me")
		switch {
		case p.taken && err != nil:
			t.Errorf("%s: the door refused: %v", p.name, err)
		case !p.taken && (err == nil || !strings.Contains(err.Error(), p.refused)):
			t.Errorf("%s = %v, want the door's own refusal %q", p.name, err, p.refused)
		}
		if err == nil {
			continue
		}
		for _, malformed := range []string{"invalid JSON body", "unknown field", "body must be a JSON object", "requires expect", "step not found", "mode must be", "by must be", "choice must be", "answer requires"} {
			if strings.Contains(err.Error(), malformed) {
				t.Errorf("%s: the door could not read the request: %v", p.name, err)
			}
		}
	}
	// WHAT A FEW OF THEM LEFT ON THE JOB, read back from the store.
	budget, _ := managedJob(t, st, spentItsBudget)
	if _, err := hand.Press(context.Background(), budget, session.FloorPress{Action: factory.DoRaiseBudget, Budget: 20}, "give it twenty"); err != nil {
		t.Fatal(err)
	}
	if it, _ := st.Get(budget); it.Cap != 20 {
		t.Errorf("the budget is %v, want 20", it.Cap)
	}
	plan, _ := managedJob(t, st, waitsOnPlanApproval)
	if _, err := hand.Press(context.Background(), plan, session.FloorPress{Action: factory.DoApprove}, "approve the plan"); err != nil {
		t.Fatal(err)
	}
	if it, _ := st.Get(plan); it.Approval == nil || it.Approval.State != "approving" || it.Approval.DecidedBy != "sam" {
		t.Errorf("the plan's approval reads %+v, want it approving, by the person", it.Approval)
	}
	// A DECISION PUT AWAY IS NOT TAKEN UP BY THE RESUME VERB, which is why
	// the Factory manager takes it up by the job's own chat's door
	// ([seamFloorDoor.foremanResume]).
	away, _ := managedJob(t, st, func(it *factory.Item) {
		hasChangesRequested(it)
		factory.PauseDecision(it, 3)
	})
	if err := (*appLocalPress.Load())(context.Background(), away, factoryVerbResume, guardBody{}); err == nil {
		t.Error("the resume verb took up a decision that was put away; the Factory manager's own road for it is no longer needed")
	}
}
