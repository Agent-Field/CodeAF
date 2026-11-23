package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE FACTORY MANAGER'S HAND: A PRESS FOR THE PERSON, THEIR WORDS PASSED ON ─
//
// The owner's answer 8 (2026-11-17): the person runs their factory by talking
// to its manager alone. `factory_floor` reads every job's stored wait
// (internal/session's tools_floor.go), and this file is the two things it
// does on the person's word:
//
//   - [seamFloorDoor.Press] presses ONE action the job's stored offers list
//     for its person now, as the job page's own button would;
//   - [seamFloorDoor.Tell] gives the job's main agent one turn of its own
//     conversation with the person's words, and answers what it said.
//
// THE PERSON'S OWN WORDS ARE THE AUTHORITY, AND THEY ARE CHECKED HERE. said is
// the person's own words in the Factory manager's running turn, read from the
// turn by the tool and never from the model's arguments. With none, nothing
// is pressed and nothing is passed on, in every mode: the Factory manager is
// not a job's main agent, and Auto's standing yes is not its to use.
//
// ONLY WHAT THE JOB OFFERS ITS PERSON IS PRESSED ([factory.OffersPerson]), by
// the verb and the body the job page sends for it, through the app's own
// press ([appLocalPress]). The factory's handler decides the rest, and its
// refusal comes back in its own words. An action the job does not offer now
// is refused with the job's wait, so the person is told what it does accept.
//
// THE RELAYED TURN IS THE FACTORY'S, NOT THE PERSON SPEAKING IN THE JOB'S
// CONVERSATION. It goes in as a runner's note by the road the engine's own
// turns take ([shapeTurns.through]), so it carries no words of the person's
// there, and the job's main agent is held to what it may do by itself
// ([factory.ManagerMay]): outside Auto it cannot approve publication, mark
// the pull request ready or approve public acts on it, and it never merges on
// it. [seamFloorDoor.Tell] says so in its answer, so the Factory manager does
// not promise otherwise.

// The Factory manager's refusals and the words of the relayed turn, said once
// so the tests and the manual quote the same ones.
const (
	foremanPressElsewhere = "Nothing changed: %s is a choice the person makes on the job page; you cannot press it from here."
	foremanNotOffered     = "Nothing changed: %s does not accept %s now. %s"
	// foremanUnreadActs is an approval that would carry acts the job's main
	// agent proposed after the person's words in this turn.
	foremanUnreadActs = "Nothing changed: %s's main agent proposed public acts after the person's words in this turn, so they have not read them yet. Read the job again, show them exactly what will be posted or changed, and press when they answer."
	// foremanPassStepRefusal is a start that would carry a job past an
	// approve step on a turn the person did not speak in.
	foremanPassStepRefusal = "Nothing changed: going past an approve step is the person's, and nothing in this turn came from them. They approve it on the job page, or by asking you here."

	foremanTellAsk  = "The person told the Factory manager, for you: %s"
	foremanTellTail = "The factory started this turn to pass that on. The person is not typing in this conversation now, so the turn does not carry their own words: merging, and outside Auto approving publication, marking the pull request ready and approving public acts, stay theirs, on the job page or through the Factory manager. Do not try those here. Do what they asked as far as you may by yourself, then answer in a few plain sentences: what you did, and what still needs the person. The Factory manager reads your answer back to them."

	foremanToldManual = "This job is not in Auto, and that turn was the factory's, not the person speaking in the job's conversation: its main agent could not push, approve publication, mark the pull request ready, approve public acts or merge on it. Do not promise any of those; press them here when the person asks and the job accepts them."
	foremanToldAuto   = "This job is in Auto, so its main agent may push, approve publication and mark the pull request ready by itself. It cannot merge on that turn: merging is the person's, pressed here when they ask and the job accepts it."
)

// errNoTell is words for a main agent asked where no factory host is to give
// it a turn.
var errNoTell = errors.New("Nothing changed: this window cannot give a job's main agent a turn; the person can say it in the job's own chat")

// errTellNoModel is a turn asked on a machine with no model settings to open
// the job's main agent with: nothing was started.
var errTellNoModel = errors.New("Nothing changed: this machine has no model settings to open the job's main agent with")

// localJobTell gives one job's main agent one turn of its own conversation on
// ask, and answers what it said.
type localJobTell func(ctx context.Context, it factory.Item, ask string) (string, error)

// appLocalTell is the app's own, nil outside `codeaf app`.
var appLocalTell atomic.Pointer[localJobTell]

// installAppLocalTell hangs b's turn-giver for this process, and answers the
// function that takes it down again.
func installAppLocalTell(b *appBackend) func() {
	tell := localJobTell(func(ctx context.Context, it factory.Item, ask string) (string, error) {
		// This machine's own factory, while this engine serves it
		// ([appBackend.localHostOf]).
		var host *appFactoryHost
		func() {
			l := &b.local
			l.mu.Lock()
			defer l.mu.Unlock()
			if !l.closed && l.running {
				host = l.host
			}
		}()
		if host == nil {
			return "", errNoTell
		}
		return host.told(ctx, it, ask)
	})
	appLocalTell.Store(&tell)
	return func() { appLocalTell.CompareAndSwap(&tell, nil) }
}

// told is one turn of a job's main agent on ask, on the factory this host
// serves, by the road the watchdog's turn takes ([appFactoryHost.stalled]):
// through the conversation when this process holds it open, else opened for
// the turn, WITH THE MANAGER'S OWN DOOR, so every tool it has when the person
// talks to it works here under the same rules.
//
// THE TURN IS CHARGED TO THE JOB: a conversation opened for the turn is
// opened from the home's own config, which carries no job chat door, so it is
// hung here, as the watchdog's turn hangs it.
func (h *appFactoryHost) told(ctx context.Context, it factory.Item, ask string) (string, error) {
	source, raw, ok := h.parentSource()
	if !ok {
		return "", errTellNoModel
	}
	current := func() session.Config {
		cfg := source.Config()
		if cfg.JobChat == nil {
			cfg.JobChat = jobChatDoorOf(h.floor)
		}
		return cfg
	}
	turns := factoryShapeTurnsFrom(h.floor, h.workspace, h.profile, current)
	turns.refusal = source.Refusal
	turns.liveOK = factoryLiveOK(raw, h.profile)
	return turns.Told(ctx, it, ask, runDoor(h.floor, h.workspace, h.profile))
}

// Told is the main agent's one turn on words passed to it, with door as its
// tools for the turn, stopped after [session.FloorTellWait]. It answers what
// the main agent said.
func (s *shapeTurns) Told(ctx context.Context, it factory.Item, ask string, door session.RunDoor) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, session.FloorTellWait)
	defer cancel()
	return s.through(ctx, it, ask, door)
}

// foremanTellAskFor is the note the job's main agent reads: the person's
// words, said to be theirs and how they came, then what this turn may do.
func foremanTellAskFor(words string) string {
	return fmt.Sprintf(foremanTellAsk, strings.TrimSpace(words)) + "\n\n" + foremanTellTail
}

// Tell passes the person's words to the main agent of job id and answers what
// it said, for the Factory manager to relay.
func (d seamFloorDoor) Tell(ctx context.Context, id int, words, said string) (string, error) {
	if strings.TrimSpace(said) == "" {
		return "", errors.New(session.FloorTellNoWords)
	}
	words = strings.TrimSpace(words)
	if words == "" {
		return "", errors.New("Nothing changed: there were no words to pass on")
	}
	it, err := d.st.Get(id)
	if err != nil {
		return "", pressRefused(err)
	}
	ref := it.Ref()
	switch {
	case factory.JobState(it) == "done":
		return "", fmt.Errorf("Nothing changed: %s is finished, so its main agent takes no more words", ref)
	case strings.TrimSpace(it.Talk) == "":
		return "", fmt.Errorf("Nothing changed: %s has no main agent conversation yet. It gets one when the job starts, or when the person opens the job's chat", ref)
	case factory.StoppedByPerson(it):
		// The factory starts no turn on a job its person stopped
		// ([factory.CheckPersonStop]); their own chat with it is still theirs.
		return "", fmt.Errorf("Nothing changed: the person stopped %s, so the factory gives its main agent no turn. They can start it again, or say it in the job's own chat", ref)
	}
	if err := factoryrun.CheckWriteAccess(it, "chat"); err != nil {
		return "", pressRefused(err)
	}
	// THE TURN IS CHARGED TO THE JOB, so a job whose budget is spent takes
	// none: the budget is read where the job's own chat reads it.
	if door := jobChatDoorOf(d.st); door != nil {
		if chat := door.Chat(it.Talk); chat != nil {
			if refusal := chat.Refusal(); refusal != "" {
				return "", fmt.Errorf("Nothing changed: %s's main agent cannot take a turn: %s. The turn is charged to the job, so its budget is raised first", ref, refusal)
			}
		}
	}
	tell := appLocalTell.Load()
	if tell == nil {
		return "", errNoTell
	}
	answer, err := (*tell)(ctx, it, foremanTellAskFor(words))
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "", errors.New(session.FloorTellLate(ref))
	case errors.Is(err, factoryrun.ErrManagerBusy), errors.Is(err, factoryrun.ErrManagerAway):
		return "", fmt.Errorf("Nothing changed: %s's main agent is in the middle of a turn of its own, so the words were not passed on. Try again in a moment", ref)
	case errors.Is(err, errNoTell), errors.Is(err, errTellNoModel):
		return "", err
	case err != nil:
		return "", fmt.Errorf("%s's main agent did not finish its turn: %s. Whether it did anything is not known: read %s again", ref, oneLineWords(err.Error()), ref)
	}
	mode := foremanToldManual
	if factory.Automatic(it) {
		mode = foremanToldAuto
	}
	if answer = strings.TrimSpace(answer); answer == "" {
		return fmt.Sprintf("The words were passed to %s's main agent. It finished its turn and said nothing: read %s again for what it did. %s", ref, ref, mode), nil
	}
	return fmt.Sprintf("The words were passed to %s's main agent, which answered: %s\n\nTell the person that in your own plain words. %s", ref, answer, mode), nil
}

// Press presses one action job id offers its person, as the person.
func (d seamFloorDoor) Press(ctx context.Context, id int, p session.FloorPress, said string) (string, error) {
	if strings.TrimSpace(said) == "" {
		return "", errors.New(session.FloorPressNoWords)
	}
	it, err := d.st.Get(id)
	if err != nil {
		return "", pressRefused(err)
	}
	ref := it.Ref()
	if slices.Contains(session.FloorPressElsewhere, p.Action) {
		return "", fmt.Errorf(foremanPressElsewhere, p.Action)
	}
	if !factory.OffersPerson(it, p.Action) {
		return "", fmt.Errorf(foremanNotOffered, ref, p.Action, factory.FloorWaitLine(it))
	}
	// THE PERSON'S YES IS FOR WHAT THEY HAVE READ. Approving the acts, or the
	// publication that carries them (serve's approveActsWithPush), is held to
	// the rule the job's manager is held to: outside Auto, acts proposed since
	// the person's words opened this turn are not theirs to have approved.
	if (p.Action == factory.DoActsApprove || (p.Action == factory.DoApprove && factory.OffersPublishApproval(it))) && actsUnread(it, p.TurnAt) {
		return "", fmt.Errorf(foremanUnreadActs, ref)
	}
	press := appLocalPress.Load()
	words := strings.TrimSpace(p.Words)
	again := "read " + ref + " again for the outcome"
	switch p.Action {
	case factory.DoApprove:
		return foremanApprove(ctx, press, it, words)
	case factory.DoAnswer:
		body, answered, err := foremanAnswer(it, p)
		if err != nil {
			return "", err
		}
		if err := pressed(ctx, press, it.ID, "answer", body); err != nil {
			return "", err
		}
		return fmt.Sprintf("Answered %s for the person: %s. This confirms the answer, not what follows; %s.", ref, answered, again), nil
	case factory.DoRejectPlan:
		if words == "" || utf8.RuneCountInString(words) > api.MaxRejectPlanWords {
			return "", fmt.Errorf("Nothing changed: sending a plan back needs the person's words for what to change, at most %d characters", api.MaxRejectPlanWords)
		}
		version, err := d.st.Revision(it.ID)
		if err != nil {
			return "", pressRefused(err)
		}
		if err := pressed(ctx, press, it.ID, "reject_plan", api.RejectPlan{Words: words, Expect: &api.Expect{Version: &version, State: api.StateNeedsYou}}); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s's plan is sent back with the person's words. The step before it runs again with them, and approval is asked again afterwards.", ref), nil
	case factory.DoAcceptStep:
		if err := pressed(ctx, press, it.ID, "answer", foremanAnswerBody{Choice: factory.ChoiceAccept}); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s's step is accepted as it stands, as the person. The job carries on; %s.", ref, again), nil
	case factory.DoActsApprove:
		if err := pressed(ctx, press, it.ID, factoryVerbActsApprove, guardBody{}); err != nil {
			return "", err
		}
		return fmt.Sprintf("The public acts that waited on %s are approved, as the person. The factory carries them out and writes each to the job's log. This confirms the approval, not the acts; %s.", ref, again), nil
	case factory.DoActsDecline:
		if err := pressed(ctx, press, it.ID, factoryVerbActsDecline, wordsBody{Words: words}); err != nil {
			return "", err
		}
		return fmt.Sprintf("The public acts that waited on %s are declined. Nothing was sent.", ref), nil
	case factory.DoMerge:
		if err := sendPRDecision(ctx, press, it, session.ActionMerge); err != nil {
			return "", err
		}
		return fmt.Sprintf("Merge pressed for pull request #%d of %s, as the person. It waits out its Undo window (%s) on the job page, then the factory checks the gate again and merges the approved commit. This confirms the press, not the merge; %s.", it.PullRequest.Number, ref, factory.MergeUndo, again), nil
	case factory.DoMarkReady:
		if err := sendPRDecision(ctx, press, it, session.ActionMarkReady); err != nil {
			return "", err
		}
		return fmt.Sprintf("Pull request #%d of %s is marked ready for review. Reviewers are notified; nothing was pushed or merged.", it.PullRequest.Number, ref), nil
	case factory.DoRetryPublish:
		if err := pressed(ctx, press, it.ID, factoryVerbRetryPublish, guardBody{}); err != nil {
			return "", err
		}
		return fmt.Sprintf("Publishing %s is queued to be tried again, as the person. This confirms the retry, not the publication; %s.", ref, again), nil
	case factory.DoResume:
		return d.foremanResume(ctx, press, it, words)
	case factory.DoResumePipeline:
		if workingChatRunning(it) {
			return "", errors.New("Nothing changed: " + workingChatBusy)
		}
		if err := pressed(ctx, press, it.ID, "control", struct {
			Mode  string `json:"mode"`
			Words string `json:"words,omitempty"`
		}{"resume_pipeline", words}); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s's pipeline is resumed, as the person. This confirms the press, not what follows; %s.", ref, again), nil
	case factory.DoResumeAuto:
		found, err := sendAutoResume(ctx, press, it, words)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Auto resumed on %s, as the person: it carries on past %s, and the job's history records the override. A new failing check pauses it again. This confirms the override, not the publication; %s.", ref, found, again), nil
	case factory.DoRerunStep:
		step := foremanRerunStep(it)
		if step == "" {
			return "", fmt.Errorf("Nothing changed: %s names no step to run again", ref)
		}
		if err := pressed(ctx, press, it.ID, "steps/"+stepSegment(step)+"/rerun", struct {
			Words string `json:"words,omitempty"`
		}{words}); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s runs its %s step again, as the person; %s.", ref, step, again), nil
	case factory.DoStart:
		if err := pressed(ctx, press, it.ID, factoryVerbStart, guardBody{}); err != nil {
			return "", err
		}
		return fmt.Sprintf(sayStartLaunched, ref), nil
	case factory.DoTask:
		if words == "" {
			return "", errors.New("Nothing changed: a task needs the person's instructions in words")
		}
		if err := pressed(ctx, press, it.ID, factoryVerbTask, taskBody{Words: words, By: api.TaskByYou}); err != nil {
			return "", err
		}
		return fmt.Sprintf("Task taken for %s, as the person: a delegate does it in the job's checkout when the round in progress ends, or at once when nothing is running. If it changes files, the checks that had passed run again; %s.", ref, again), nil
	case factory.DoRaiseBudget:
		return foremanRaiseBudget(ctx, press, it, p.Budget)
	}
	return "", errors.New("Nothing changed: unknown action")
}

// foremanApprove presses Approve on whatever approval the job waits on its
// person for: the start, the plan, the next step, or publishing its checked
// work.
//
// THE HOLDS THE JOB'S OWN MANAGER KEEPS ARE KEPT HERE TOO (factory_start.go,
// [sendPRDecision]): an approval a rule asks the person for, one that names
// someone else, and an approve step only a person passes are each theirs to
// press on the job page, whatever they said to a manager.
func foremanApprove(ctx context.Context, press *localJobPress, it factory.Item, words string) (string, error) {
	ref := it.Ref()
	if it.RuleHold != "" {
		return "", fmt.Errorf("Nothing changed: "+sayStartRuleHold, ref, it.RuleHold)
	}
	if factory.NamesOtherApprover(it) {
		return "", fmt.Errorf("Nothing changed: "+sayStartApprover, ref, factory.WaitingApprover(it))
	}
	if st, _, ok := factory.WaitingGateStage(it); ok && factory.PersonOnly(st) {
		return "", errors.New(personOnlyApproval)
	}
	if factory.OffersPublishApproval(it) {
		if err := sendPRDecision(ctx, press, it, session.ActionApprove); err != nil {
			return "", err
		}
		return fmt.Sprintf("Publication of %s approved, as the person. It waits out its Undo window (%s) on the job page, then the factory publishes the checked work. This confirms the approval, not the publication; read %s again for the outcome.", ref, factory.ApprovalUndo, ref), nil
	}
	if err := pressed(ctx, press, it.ID, factoryVerbApprove, api.Approve{Commit: it.HeadCommit, Words: words}); err != nil {
		return "", err
	}
	what := "what it asked"
	if w, yours := factory.WaitsOnPerson(it); yours {
		what = firstNonEmptyString(foremanApproved[w.Action], what)
	}
	return fmt.Sprintf("%s: %s is approved, as the person. It waits out its Undo window (%s) on the job page, then the job carries on. This confirms the approval, not what follows; read %s again for the outcome.", ref, what, factory.ApprovalUndo, ref), nil
}

// foremanApproved says what an approval was of, by the action the job waited
// on its person for.
var foremanApproved = map[string]string{
	factory.ActApproveStart:   "its start",
	factory.ActApprovePlan:    "its plan",
	factory.ActApproveStep:    "its next step",
	factory.ActApprovePublish: "publishing its checked work",
	factory.ActApproveReady:   "marking its pull request ready",
}

// foremanAnswerBody is the body of `answer` as the Factory manager sends it:
// the fields the factory's answer door reads, each left out when it is not
// part of this answer.
type foremanAnswerBody struct {
	Commit string `json:"commit,omitempty"`
	Yes    *bool  `json:"yes,omitempty"`
	Words  string `json:"words,omitempty"`
	Choice string `json:"choice,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// foremanAnswer is the body that answers the question job it waits on, and
// the answer in words for the line read back.
//
// A QUESTION WITH LISTED CHOICES TAKES ONLY A CHOICE. A step that used its
// rounds asks "try again, send it back, or stop?", and the factory reads any
// other words there as stop ([factory.OutOfAttemptsChoice]): a person's "try
// it with the race detector on" passed as words would have ended the job. So
// those questions are answered by the choice alone, send_back with its
// reason, and stopping a job is never pressed from here.
func foremanAnswer(it factory.Item, p session.FloorPress) (foremanAnswerBody, string, error) {
	ref := it.Ref()
	choice, words := strings.TrimSpace(p.Choice), strings.TrimSpace(p.Words)
	yes := true
	var fixed []string
	switch it.QKind {
	case factory.QKindAgentField:
		return foremanAnswerBody{}, "", fmt.Errorf("Nothing changed: %s waits on the approval of a run on AgentField, which the person gives on the job page", ref)
	case factory.QKindApprove, factory.QKindAskFirst:
		return foremanAnswerBody{}, "", fmt.Errorf("Nothing changed: %s waits on an approval, not an answer. Press approve when the person says yes; a no is theirs to give on the job page", ref)
	case factory.QKindSpendStopped:
		if !strings.EqualFold(choice, "yes") {
			return foremanAnswerBody{}, "", fmt.Errorf("Nothing changed: %s asks whether to carry on after its spending stop. Press answer with choice yes when the person says to carry on; anything else is theirs to choose on the job page", ref)
		}
		return foremanAnswerBody{Commit: it.HeadCommit, Yes: &yes}, "yes, carry on", nil
	case factory.QKindOutOfAttempts:
		fixed = factory.OutOfAttemptChoices(it)
	case factory.NeedsRunnerOffline:
		fixed = []string{factory.ChoiceTryOnceMore}
	case "cap":
		fixed = []string{factory.ChoiceContinue}
	}
	if fixed != nil {
		fixed = slices.DeleteFunc(slices.Clone(fixed), func(c string) bool { return c == factory.ChoiceCancel })
		choice = strings.ToLower(choice)
		if !slices.Contains(fixed, choice) {
			return foremanAnswerBody{}, "", fmt.Errorf("Nothing changed: %s's question takes one of these choices and no other words: %s. Set choice to the one the person chose. Stopping a job is done on the job page", ref, strings.Join(fixed, ", "))
		}
		body := foremanAnswerBody{Commit: it.HeadCommit, Choice: choice}
		if choice == factory.ChoiceSendBack {
			if words == "" {
				return foremanAnswerBody{}, "", errors.New("Nothing changed: send_back needs the person's words for why")
			}
			body.Reason = words
			return body, choice + " (" + oneLineWords(words) + ")", nil
		}
		return body, choice, nil
	}
	// A STEP'S QUESTION, OR A FREE ONE: the person's words, one of its own
	// options, or both, as the job page's answer sends them.
	answer := words
	if choice != "" {
		var options []string
		if it.Asking != nil {
			options = it.Asking.Options
		}
		i := slices.IndexFunc(options, func(o string) bool { return strings.EqualFold(strings.TrimSpace(o), choice) })
		if i < 0 {
			if len(options) == 0 {
				return foremanAnswerBody{}, "", fmt.Errorf("Nothing changed: %s's question lists no choices; answer it in words", ref)
			}
			return foremanAnswerBody{}, "", fmt.Errorf("Nothing changed: %s's question lists these choices: %s", ref, strings.Join(options, ", "))
		}
		answer = options[i]
		if words != "" {
			answer += ": " + words
		}
	}
	if answer == "" {
		return foremanAnswerBody{}, "", errors.New("Nothing changed: an answer needs the person's words, or one of the question's choices")
	}
	return foremanAnswerBody{Commit: it.HeadCommit, Yes: &yes, Words: answer}, oneLineWords(answer), nil
}

// foremanResume lifts a hold. A job its person held goes on through the job
// page's Resume. A decision they put away is taken up again through the door
// the job's own chat takes it up by ([lease.Doors.RetryPausedRecovery]): the
// factory's `resume` verb refuses a job whose decision is put away, as it
// refuses every other decision on it.
func (d seamFloorDoor) foremanResume(ctx context.Context, press *localJobPress, it factory.Item, words string) (string, error) {
	ref := it.Ref()
	if !factory.RecoveryPaused(it) {
		if err := pressed(ctx, press, it.ID, factoryVerbResume, guardBody{}); err != nil {
			return "", err
		}
		return fmt.Sprintf(sayStartResumed+", as the person.", ref), nil
	}
	if utf8.RuneCountInString(words) > api.MaxRejectPlanWords {
		return "", fmt.Errorf("Nothing changed: instructions for a resume are at most %d characters", api.MaxRejectPlanWords)
	}
	desk, err := lease.Open(d.st, lease.Options{})
	if err != nil {
		return "", pressRefused(err)
	}
	if err := lease.NewDoors(desk).RetryPausedRecovery(it.ID, it.Talk, it.PausedRecoveryQuestion, words, rand.Text()); err != nil {
		return "", pressRefused(err)
	}
	return fmt.Sprintf("%s is taken up again, as the person: the decision they had put away is lifted. Read %s again for what it does next.", ref, ref), nil
}

// foremanRerunStep is the step a rerun names: the one that failed, else the
// one the job is on.
func foremanRerunStep(it factory.Item) string {
	if f := it.Failure; f != nil && strings.TrimSpace(f.Step) != "" {
		return f.Step
	}
	if s := it.Stream; s != nil && s.Cur >= 0 && s.Cur < len(s.Phases) {
		return s.Phases[s.Cur].Name
	}
	return ""
}

// foremanRaiseBudget sets the job's budget to the amount the person named,
// one the job page's own budget control offers.
func foremanRaiseBudget(ctx context.Context, press *localJobPress, it factory.Item, budget float64) (string, error) {
	ref := it.Ref()
	next, spent := factory.NextJobCap(it), factory.JobLifetimeSpent(it)
	if !slices.Contains(factory.JobBudgetSteps(), budget) && budget != next {
		return "", fmt.Errorf("Nothing changed: set budget to the amount the person named: 5, 10, 20 or 30 USD, or %s's next budget, %s. It has spent %s of %s", ref, appUSD(next), appUSD(spent), appUSD(it.Cap))
	}
	if budget <= spent {
		return "", fmt.Errorf("Nothing changed: %s has already spent %s, so its budget must be above that", ref, appUSD(spent))
	}
	if err := pressed(ctx, press, it.ID, factoryVerbBudget, struct {
		Budget float64 `json:"budget"`
	}{budget}); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s's budget is now %s, as the person. Read %s again for what it does next.", ref, appUSD(budget), ref), nil
}

// workingChatBusy is Resume pipeline asked while a step's own chat is in the
// middle of a turn in a window here, and workingChatRunning says it is. The
// job page's Resume pipeline refuses the same way
// ([appBackend.FactoryJobControl]).
const workingChatBusy = "The working chat is still running. Stop it or wait for it to finish before resuming."

func workingChatRunning(it factory.Item) bool {
	if it.Stream == nil {
		return false
	}
	for _, ph := range it.Stream.Phases {
		if ph.Chat == "" {
			continue
		}
		if a, ok := session.LiveAgentFor(ph.Chat); ok && a.TurnRunning() {
			return true
		}
	}
	return false
}
