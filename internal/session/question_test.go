package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ONE OBJECT, ONE DOOR, AND A RECORD THAT IS READ BEFORE ANYTHING IS ASKED.
//
// These tests hold question.go to the three things it promises. The gate refuses
// a question its asker had not finished thinking about, and every refusal says
// what to do instead. Every ask this engine already raises comes back out of
// [Agent.OpenQuestions] with the right kind and the right stakes. And an answer
// through [Agent.ResolveQuestion] reaches the resolver the card in the window
// would have called — which is the whole of answers.go's first law, now spread
// across eleven lanes.

// wellFormed is a question that passes the gate, for the refusal table to break
// one field at a time.
func wellFormed() Question {
	return Question{
		ID:      1,
		Kind:    QuestionConsent,
		Ask:     AskPermission,
		Head:    "may it overwrite build/notes.md?",
		Reason:  "bash always asks",
		Stakes:  StakesCostly,
		Options: AnswerOptions(QuestionConsent),
	}
}

func TestTheQuestionGateRefusesWhatAnAskerHasNotFinishedThinkingAbout(t *testing.T) {
	four := AnswerOptions(QuestionConsent)
	tooMany := append(append([]AnswerOption{}, four...),
		AnswerOption{Key: "4", Label: "four"},
		AnswerOption{Key: "5", Label: "five"})

	for _, tc := range []struct {
		name   string
		break_ func(*Question)
		says   string
	}{
		{"no head", func(q *Question) { q.Head = "  " }, "one sentence"},
		{"no reason", func(q *Question) { q.Reason = "" }, "why it is being asked now"},
		{"no stakes", func(q *Question) { q.Stakes = "" }, "reversible, costly or irreversible"},
		{"one answer", func(q *Question) { q.Options = q.Options[:1] }, "at least two answers"},
		{"too many answers", func(q *Question) { q.Options = tooMany }, "at most 4 answers"},
		{"a pick nobody offered", func(q *Question) { q.Pick = &Pick{Key: "9"} }, `"9"`},
		{"a clock on what cannot be undone", func(q *Question) {
			q.Stakes = StakesIrreversible
			q.Deadline = time.Now().Add(time.Minute)
		}, "never runs on a clock"},
		{"a policy on what cannot be undone", func(q *Question) {
			q.Stakes = StakesIrreversible
			q.Pick = &Pick{Key: "1"}
			q.Policy = Policy{Kind: PolicyRecommendThenAuto, After: time.Minute}
		}, "never answered by a policy"},
		{"a policy with nothing to take", func(q *Question) {
			q.Policy = Policy{Kind: PolicyRecommendThenAuto, After: time.Minute}
		}, "needs a pick to take"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			question := wellFormed()
			tc.break_(&question)
			err := question.Check(nil)
			if err == nil {
				t.Fatalf("the gate let %s through", tc.name)
			}
			if !strings.Contains(err.Error(), tc.says) {
				t.Fatalf("the refusal reads %q, and does not name %q", err, tc.says)
			}
		})
	}

	// AND A WELL-FORMED QUESTION PASSES, which is the half of this table that
	// catches a gate that has learned to refuse everything.
	if err := wellFormed().Check(nil); err != nil {
		t.Fatalf("the gate refused a whole question: %v", err)
	}
}

// EVERY REFUSAL ENDS IN SOMETHING THE ASKER CAN DO. The reader on the other side
// of this gate is a model that has to act on the refusal without asking again,
// and "invalid question" is a sentence it can only retry.
func TestEveryRefusalTellsTheAskerWhatToDoInstead(t *testing.T) {
	for _, err := range []error{
		errQuestionNoHead, errQuestionNoReason, errQuestionNoStakes,
		errQuestionTooFewOptions, errQuestionTooManyOptions,
		errQuestionClockOnIrreversible, errQuestionAutoOnIrreversible,
		errQuestionAutoWithoutPick,
	} {
		words := err.Error()
		if !strings.Contains(words, ":") && !strings.Contains(words, ",") {
			t.Fatalf("this refusal offers no way out: %q", words)
		}
		// The machinery vocabulary the design language bans, which is what a
		// refusal drifts into first.
		for _, banned := range []string{"invalid", "malformed", "schema", "field"} {
			if strings.Contains(strings.ToLower(words), banned) {
				t.Fatalf("the refusal %q uses machinery vocabulary: %q", words, banned)
			}
		}
	}
}

func TestTheGateRefusesAQuestionTheRecordAlreadyAnswers(t *testing.T) {
	question := wellFormed()
	question.Subject = SubjectRef{Kind: SubjectCall, CallID: "c1", Name: "bash"}

	records := []DecisionRecord{{
		Head:    "May it overwrite   build/notes.md?",
		Subject: question.Subject,
		Picked:  []string{"1"}, Labels: []string{"allow once"},
		By: DecidedByPerson, At: time.Now(),
	}}
	err := question.Check(records)
	if err == nil {
		t.Fatal("the gate asked a question the record already answers")
	}
	if !strings.Contains(err.Error(), "already decided") || !strings.Contains(err.Error(), "allow once") {
		t.Fatalf("the refusal does not read the decision back: %q", err)
	}

	// AND THE SUBJECT IS PART OF THE MATCH. The same sentence about a different
	// call is a different decision, and a record keyed on the head alone would
	// answer for every file a session ever touched.
	elsewhere := question
	elsewhere.Subject = SubjectRef{Kind: SubjectCall, CallID: "c2", Name: "bash"}
	if err := elsewhere.Check(records); err != nil {
		t.Fatalf("the gate read one file's decision as another's: %v", err)
	}
}

// THE APPROVAL GATE, AS A QUESTION. It is the lane with the most callers and the
// least state — a bare channel — so it is the one that proves the words are
// banked rather than thrown away with the event.
func TestTheApprovalGateComesBackOutAsAQuestion(t *testing.T) {
	agent, dir := questionSession(t, "qqqq1111qqqq1111", nil)
	hub := newEventHub()
	events := hub.subscribe()
	// THE QUESTION SPEAKS ON ITS OWN LANE and never on the turn's, so that a
	// caller reading a turn to its close reads exactly what it always did.
	asks, stopAsking := agent.WatchQuestions()
	defer stopAsking()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = agent.ask(context.Background(), hub,
			ai.ToolCall{ID: "c1", Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"rm -rf build/"}`}},
			approval.Decision{Action: approval.ActionPrompt, Rule: `bash pattern "rm -rf *"`})
	}()

	request := <-events
	if request.Kind != EventConsentRequest {
		t.Fatalf("the gate sent %v first", request.Kind)
	}
	// AND THE OBJECT ARRIVES ON THE QUESTIONS LANE, after the request that named
	// the row.
	object := waitForAsk(t, asks, EventQuestion)
	if object.Question.ID != request.ID {
		t.Fatalf("the object names question %d, the request names %d", object.Question.ID, request.ID)
	}

	open := agent.OpenQuestions()
	if len(open) != 1 {
		t.Fatalf("OpenQuestions() = %d questions, want the one the gate is stopped on", len(open))
	}
	asked := open[0]
	switch {
	case asked.Kind != QuestionConsent:
		t.Fatalf("the lane reads %q", asked.Kind)
	case asked.Ask != AskPermission:
		t.Fatalf("an approval reads as a %q", asked.Ask)
	case asked.Stakes != StakesCostly:
		t.Fatalf("the stakes read %q", asked.Stakes)
	case !asked.Blocking.Turn:
		t.Fatal("the question says the turn is not waiting on it, and the call is blocked")
	case asked.Head != "needs your ok to run bash":
		t.Fatalf("the head reads %q", asked.Head)
	case asked.Reason != `bash pattern "rm -rf *"`:
		t.Fatalf("the reason is not the policy's own words: %q", asked.Reason)
	case asked.Subject.CallID != "c1":
		t.Fatalf("the question does not point at the row it is about: %+v", asked.Subject)
	}
	if err := asked.Check(nil); err != nil {
		t.Fatalf("the gate would refuse its own question: %v", err)
	}

	// AND THE WHOLE OBJECT IS ON THE PRESENCE FILE, beside the four fields an
	// older window reads.
	presence := waitForQuestion(t, dir)
	if presence.Full == nil {
		t.Fatal("presence carries the line and not the question")
	}
	if presence.Full.Reason != asked.Reason || presence.Kind != QuestionConsent || presence.Text != asked.Head {
		t.Fatalf("presence and the object disagree: %+v vs %+v", presence, asked)
	}

	// AND THE ONE DOOR ANSWERS IT, reaching the resolver the card would have
	// called: `2 always` is the tool-wide memo.
	if err := agent.ResolveQuestion(Answer{Kind: QuestionConsent, ID: asked.ID, Picked: []string{"2"}}); err != nil {
		t.Fatalf("ResolveQuestion: %v", err)
	}
	<-done
	if remembered, known := agent.rememberedConsent("bash"); !known || !remembered {
		t.Fatal("the answer did not reach ResolveConsentRemember")
	}
	if open := agent.OpenQuestions(); len(open) != 0 {
		t.Fatalf("the answered question is still open: %+v", open)
	}

	// AND IT IS IN THE RECORD, which is what refuses the same question next time.
	records := agent.Decisions()
	if len(records) != 1 {
		t.Fatalf("the record holds %d decisions, want one", len(records))
	}
	if records[0].Head != asked.Head || records[0].Words() != "always" || records[0].By != DecidedByPerson {
		t.Fatalf("the record reads %+v", records[0])
	}
	if again := asked.Check(records); again == nil || !strings.Contains(again.Error(), "already decided") {
		t.Fatalf("the record does not refuse the same question: %v", again)
	}
	if line := records[0].Line(); !strings.Contains(line, "→ always") || !strings.Contains(line, "you") {
		t.Fatalf("the record's line reads %q", line)
	}
	if section := DecisionsSection(records); !strings.HasPrefix(section, "the record\n- ") {
		t.Fatalf("the section the model carries reads %q", section)
	}
	// THE EMPTINESS LAW: a session that has decided nothing carries no heading.
	if section := DecisionsSection(nil); section != "" {
		t.Fatalf("an empty record rendered %q", section)
	}
}

// THE PROPOSAL IS THE ONE QUESTION WITH A CLOCK, AND THE CLOCK APPROVES. A
// surface that could not say so would be showing a person a card whose silence
// meant something it never told them.
func TestTheTaskProposalComesBackOutWithItsClockAndItsPick(t *testing.T) {
	agent, _ := questionSession(t, "qqqq2222qqqq2222", func(config *Config) {
		config.TaskAutoApproveSeconds = 30
	})
	events := watched(agent)
	asks, stopAsking := agent.WatchQuestions()
	defer stopAsking()

	go func() { _, _ = agent.askTask(context.Background(), 7, taskSpec{title: "port the resume picker"}, "") }()

	if proposal := <-events; proposal.Kind != EventTaskProposal {
		t.Fatalf("the proposal lane sent %v", proposal.Kind)
	}
	waitForAsk(t, asks, EventQuestion)

	open := agent.OpenQuestions()
	if len(open) != 1 || open[0].Kind != QuestionTask {
		t.Fatalf("OpenQuestions() = %+v", open)
	}
	asked := open[0]
	switch {
	case asked.Ask != AskPermission:
		t.Fatalf("a proposal reads as a %q", asked.Ask)
	case asked.Deadline.IsZero():
		t.Fatal("the proposal carries no clock, and the clock is what starts it")
	case asked.Policy.Kind != PolicyRecommendThenAuto:
		t.Fatalf("the policy reads %q, and silence starts the work", asked.Policy.Kind)
	case asked.Pick == nil || asked.Pick.Key != "1":
		t.Fatalf("a question that answers itself has no pick to take: %+v", asked.Pick)
	case asked.Head != "wants to start a task: port the resume picker":
		t.Fatalf("the head reads %q", asked.Head)
	}

	agent.ResolveQuestion(Answer{Kind: QuestionTask, ID: 7, Picked: []string{"2"}})
	if open := agent.OpenQuestions(); len(open) != 0 {
		t.Fatalf("the answered proposal is still open: %+v", open)
	}
}

// A QUESTION IS NEVER SIMPLY GONE. The turn that raised this one is interrupted
// under it, so nothing answers it — and the person looking at it is owed the
// sentence saying why the count they were watching dropped.
func TestAQuestionWhoseSubjectWentAwayIsWithdrawnWithAReason(t *testing.T) {
	agent, _ := questionSession(t, "qqqq3333qqqq3333", nil)
	// THE WITHDRAWAL SPEAKS ON THE QUESTIONS LANE, which is the one a surface
	// holds for the life of the session — it happens after the lane that raised
	// the question has let go of it, when there may be no turn left at all.
	hub := newEventHub()
	asks, stopAsking := agent.WatchQuestions()
	defer stopAsking()

	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = agent.ask(ctx, hub,
			ai.ToolCall{ID: "c1", Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"ls"}`}},
			approval.Decision{Action: approval.ActionPrompt, Rule: "bash always asks"})
	}()
	waitForAsk(t, asks, EventQuestion)

	stop()
	<-done

	// The wait is gone, so nothing is open; the words are still banked, and the
	// sweep is what reconciles the two.
	if open := agent.OpenQuestions(); len(open) != 0 {
		t.Fatalf("the interrupted question is still open: %+v", open)
	}
	agent.sweepQuestions()

	withdrawn := waitForAsk(t, asks, EventQuestionWithdrawn)
	if withdrawn.Question == nil || withdrawn.Question.Withdrawn == nil {
		t.Fatalf("nothing said the question had gone: %+v", withdrawn)
	}
	if reason := withdrawn.Question.Withdrawn.Reason; reason != "the turn moved on without it" {
		t.Fatalf("the withdrawal reads %q", reason)
	}
	// AND IT IS WITHDRAWN ONCE. A second sweep has nothing left to say.
	agent.sweepQuestions()
	select {
	case event := <-asks:
		if event.Kind == EventQuestionWithdrawn {
			t.Fatal("the question was withdrawn twice")
		}
	case <-time.After(200 * time.Millisecond):
	}
}

// A LANDED `YOUR CALL` IS A QUESTION LIKE ANY OTHER, which for a long time it
// was not: it lived in a registry of its own that the one predicate never folded
// in, so a session sitting on one said `idle` to home, the switcher and the tab
// signal (ideation/questions-audit.md, finding 4).
func TestALandedYourCallIsAQuestionAndCountsAsNeedingSomebody(t *testing.T) {
	agent, _, child := unverifiedFamily(t)

	if !agent.NeedsPerson() {
		t.Fatal("a session holding work that waits on somebody's word says nobody is needed")
	}
	if reason := agent.WaitingOn(); !strings.HasPrefix(reason, "your call on ") {
		t.Fatalf("the line reads %q", reason)
	}

	var landing *Question
	for _, question := range agent.OpenQuestions() {
		if question.ID == child.id {
			found := question
			landing = &found
		}
	}
	if landing == nil {
		t.Fatalf("the landed task is on no question list: %+v", agent.OpenQuestions())
	}
	switch {
	case landing.Kind != QuestionLanding && landing.Kind != QuestionConflict:
		t.Fatalf("the lane reads %q", landing.Kind)
	case landing.Ask != AskLanding:
		t.Fatalf("a landing reads as a %q", landing.Ask)
	case landing.Stakes != StakesCostly:
		t.Fatalf("the stakes read %q", landing.Stakes)
	case landing.Blocking.Blocks():
		t.Fatal("the landing says something is waiting on it, and the work has already finished")
	}
	// THE THREE KEYS ARE TASK-STATES' OWN, letter for letter.
	keys := make([]string, 0, len(landing.Options))
	for _, option := range landing.Options {
		keys = append(keys, option.Key)
	}
	if len(keys) != 3 || keys[0] != LandingYesKey || keys[1] != LandingNoKey || keys[2] != LandingTellKey {
		t.Fatalf("the landing's keys are %v, want a · n · s", keys)
	}
}

// AN OLDER WINDOW STILL ANSWERS. A presence file and an answers file are read
// and written by builds of other ages, so the four fields answers.jsonl started
// with have to go on meaning exactly what they meant.
func TestAnAnswerFileWrittenByAnOlderWindowStillReads(t *testing.T) {
	dir := t.TempDir()
	// The exact four fields a build from before question.go wrote.
	line := `{"at":"2026-09-09T10:00:00Z","kind":"consent","id":7,"key":"1","from":"home"}` + "\n"
	if err := os.WriteFile(AnswersPath(dir), []byte(line), 0o600); err != nil {
		t.Fatalf("writing the old line: %v", err)
	}
	answers, err := DrainAnswers(dir)
	if err != nil || len(answers) != 1 {
		t.Fatalf("DrainAnswers = %+v, %v", answers, err)
	}
	answer := answers[0]
	if answer.Kind != QuestionConsent || answer.ID != 7 || answer.Key != "1" {
		t.Fatalf("the old line reads %+v", answer)
	}
	// AND ITS BARE KEY IS READ BY THE ONE READER OF EITHER SHAPE.
	if keys := answer.Keys(); len(keys) != 1 || keys[0] != "1" {
		t.Fatalf("a bare key reads as %v", keys)
	}
	action, ok := AnswerFromKey(answer.Kind, answer.Key)
	if !ok || !action.Allow || action.Scope != ConsentOnce {
		t.Fatalf("AnswerFromKey no longer answers from a bare key: %+v %v", action, ok)
	}

	// AND A WHOLE ANSWER ROUND-TRIPS, keeping the four fields filled for whoever
	// reads it next.
	whole := Answer{
		At: time.Now().UTC(), Kind: QuestionStanding, ID: 9, Key: StandingOnceKey,
		Picked: []string{StandingOnceKey}, Change: "but not on Sundays",
		Blanks: map[string]string{"when": "six"}, DecidedBy: DecidedByPerson,
		Scope: ScopeProject, Why: "I only want it on weekdays",
	}
	raw, err := json.Marshal(whole)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	var back Answer
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshalling: %v", err)
	}
	if back.Key != StandingOnceKey || back.Change != whole.Change ||
		back.Blanks["when"] != "six" || back.Scope != ScopeProject || back.Why != whole.Why {
		t.Fatalf("the whole answer did not round-trip: %+v", back)
	}
}

// THE DOOR REACHES THE LANES NOTHING COULD REACH. The audit found three
// resolvers with no caller anywhere in the product; an answer naming one of them
// has to arrive at it.
func TestTheOneDoorReachesTheLanesThatHadNoCaller(t *testing.T) {
	agent, _ := questionSession(t, "qqqq4444qqqq4444", nil)

	// A running sub-harness's own question: answered in words, never with a key.
	replies := make(chan subharnessReply, 1)
	agent.mu.Lock()
	agent.subharnessAsks = map[uint64]*subharnessQuestion{
		4: {replies: replies, question: "which branch should I cut from?", name: "release", asked: time.Now()},
	}
	agent.mu.Unlock()

	open := agent.OpenQuestions()
	if len(open) != 1 || open[0].Kind != QuestionSubharnessAsk {
		t.Fatalf("OpenQuestions() = %+v, want the run's own question", open)
	}
	if open[0].Head != "which branch should I cut from?" {
		t.Fatalf("the head reads %q — the words were thrown away with the event", open[0].Head)
	}
	if open[0].Input.Kind != InputText || len(open[0].Options) != 0 {
		t.Fatalf("a question the run wrote no answers for offers %+v", open[0].Options)
	}

	if err := agent.ResolveQuestion(Answer{Kind: QuestionSubharnessAsk, ID: 4, Change: "cut from dev"}); err != nil {
		t.Fatalf("ResolveQuestion: %v", err)
	}
	select {
	case reply := <-replies:
		if reply.text != "cut from dev" {
			t.Fatalf("the run was handed %q", reply.text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the answer never reached AnswerSubharness")
	}

	// AND A LANE NOTHING IN THIS SESSION IS ASKING IS REFUSED RATHER THAN
	// SILENTLY DROPPED — the caller is the one that can still say something.
	if err := agent.ResolveQuestion(Answer{Kind: "nothing-like-this", ID: 1, Picked: []string{"1"}}); !errors.Is(err, errAnswerUnknownLane) {
		t.Fatalf("an answer to no lane came back %v", err)
	}
}

// A STEER NEVER RESOLVES A TASK BY ITSELF (docs/design/task-states/DESIGN.md).
// "Looks good" typed on a landing card must not silently become accept, so the
// key that sends words leaves the question exactly where it was.
func TestTellingTheWorkSomethingLeavesTheQuestionOpen(t *testing.T) {
	if resolvesQuestion(Answer{Kind: QuestionLanding, Picked: []string{LandingTellKey}}) {
		t.Fatal("telling the work something closed the question")
	}
	for _, key := range []string{LandingYesKey, LandingNoKey, LandingAgainKey} {
		if !resolvesQuestion(Answer{Kind: QuestionLanding, Picked: []string{key}}) {
			t.Fatalf("%q left the question open", key)
		}
	}
}

// THREE OPEN QUESTIONS ABOUT ONE PIECE OF WORK IS A PIECE OF WORK THAT HAS
// STOPPED. The cap is counted against the SUBJECT and not against the session,
// so three questions about three tasks is three tasks that need something and
// three about one is a sheet.
func TestTheCapIsCountedAgainstOnePieceOfWorkAndNotTheSession(t *testing.T) {
	agent, _ := questionSession(t, "qqqq5555qqqq5555", nil)
	subject := SubjectRef{Kind: SubjectNode, ID: 3, Name: "Port the parser"}

	ask := func(id uint64, about SubjectRef) error {
		_, err := agent.AskQuestion(Question{
			ID: id, Kind: QuestionSubharnessAsk, Ask: AskChoice,
			Head:    "which of these for " + about.Name + "? " + itoa64(id),
			Reason:  "the shape of the work depends on it",
			Subject: about, Stakes: StakesReversible,
			Options: []AnswerOption{{Key: "1", Label: "one"}, {Key: "2", Label: "two"}},
		})
		return err
	}

	// A question with a subject needs a WAIT behind it to be open, so the three
	// that fill the cap are registered as the run's own questions.
	agent.mu.Lock()
	agent.subharnessAsks = map[uint64]*subharnessQuestion{}
	for id := uint64(1); id <= QuestionCap; id++ {
		agent.subharnessAsks[id] = &subharnessQuestion{
			replies: make(chan subharnessReply, 1), question: "x", name: "Port the parser", asked: time.Now(),
		}
	}
	agent.mu.Unlock()
	for id := uint64(1); id <= QuestionCap; id++ {
		if err := ask(id, subject); err != nil {
			t.Fatalf("question %d was refused under the cap: %v", id, err)
		}
	}

	// The next one about the SAME work is refused, and told what to do instead.
	err := ask(QuestionCap+1, subject)
	if err == nil {
		t.Fatalf("a %dth question about one piece of work was allowed", QuestionCap+1)
	}
	if !strings.Contains(err.Error(), "one question with several parts") {
		t.Fatalf("the refusal does not say what to do instead: %q", err)
	}

	// AND ONE ABOUT DIFFERENT WORK IS NOT.
	elsewhere := SubjectRef{Kind: SubjectNode, ID: 9, Name: "Write the notes"}
	agent.mu.Lock()
	agent.subharnessAsks[9] = &subharnessQuestion{
		replies: make(chan subharnessReply, 1), question: "y", name: "Write the notes", asked: time.Now(),
	}
	agent.mu.Unlock()
	if err := ask(9, elsewhere); err != nil {
		t.Fatalf("a question about other work was refused: %v", err)
	}
}

// waitForAsk is the next event of one kind off the questions lane, and a
// failure with the kinds it did see when none arrives.
func waitForAsk(t *testing.T, asks <-chan Event, want EventKind) Event {
	t.Helper()
	var seen []EventKind
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event := <-asks:
			if event.Kind == want {
				if event.Question == nil {
					t.Fatalf("the questions lane sent a %v with no question on it", want)
				}
				return event
			}
			seen = append(seen, event.Kind)
		case <-deadline:
			t.Fatalf("no %v reached the questions lane; it sent %v", want, seen)
			return Event{}
		}
	}
}

// TestABankedRuleAnswersAsARuleAndNotAsAToolWideMemo is the one thing the
// widening answer's scope has to get right.
//
// The session memo this engine writes for a [ConsentToolSession] answer is keyed
// by the tool's NAME alone, so on `bash` it means every command for the rest of
// the conversation. A person who reads `git status*` and presses a key must not
// buy silence for `rm -rf` — so when the SURFACE has already written the rule
// down, the answer says so ([AnswerBanked]) and this door applies it as a
// [ConsentRule], which is the scope [Agent.askAnswer] writes nothing beside.
func TestABankedRuleAnswersAsARuleAndNotAsAToolWideMemo(t *testing.T) {
	widening := AnswerAction{Kind: QuestionConsent, Allow: true, Scope: ConsentToolSession}
	banked := Answer{Kind: QuestionConsent, Key: "2", Comments: map[string]string{AnswerBanked: "git status*"}}
	if got := ConsentScopeOf(widening, banked); got != ConsentRule {
		t.Fatalf("a banked shape answered as %q, want %q", got, ConsentRule)
	}
	// AND EVERY OTHER ANSWER IS UNTOUCHED. A widening yes with nothing written
	// behind it is still the memo it always was — that is what stops the asking
	// for a plain tool — and neither the narrow yes nor the no is widened by a
	// comment that happens to be on them.
	if got := ConsentScopeOf(widening, Answer{Kind: QuestionConsent, Key: "2"}); got != ConsentToolSession {
		t.Fatalf("a widening yes with nothing banked answered as %q", got)
	}
	once := AnswerAction{Kind: QuestionConsent, Allow: true, Scope: ConsentOnce}
	if got := ConsentScopeOf(once, banked); got != ConsentOnce {
		t.Fatalf("the narrow yes was widened to %q by a comment", got)
	}
	deny := AnswerAction{Kind: QuestionConsent, Scope: ConsentOnce}
	if got := ConsentScopeOf(deny, banked); got != ConsentOnce {
		t.Fatalf("a refusal answered as %q", got)
	}
	// A comment with nothing in it is a claim with nothing behind it.
	empty := Answer{Kind: QuestionConsent, Key: "2", Comments: map[string]string{AnswerBanked: "  "}}
	if got := ConsentScopeOf(widening, empty); got != ConsentToolSession {
		t.Fatalf("an empty banked comment claimed a rule: %q", got)
	}
}

// TestAProposalAsksAboutItsModelOnlyWhenThereIsSomethingToAsk is
// [TaskModelShape]'s whole bound, and it is the emptiness law said about a
// question: a hole offering the one model the work was already going to run on
// is a question that has answered itself.
func TestAProposalAsksAboutItsModelOnlyWhenThereIsSomethingToAsk(t *testing.T) {
	options := []string{"anthropic/claude-opus-5", "anthropic/claude-opus-4.8"}
	shape := TaskModelShape(TaskNotice{Model: options[0], ModelOptions: options})
	if shape.Kind != InputBlanks || len(shape.Blanks) != 1 {
		t.Fatalf("a shortlist of two did not become one blank: %+v", shape)
	}
	blank := shape.Blanks[0]
	if blank.Label != TaskModelBlank || blank.Kind != BlankChoice {
		t.Fatalf("the hole is not a choice called %q: %+v", TaskModelBlank, blank)
	}
	// THE DEFAULT IS AN ANSWER ALREADY GIVEN: the leading option is what the
	// card shows and what the clock takes, so somebody who changes nothing has
	// confirmed the model the work was always going to run on.
	if blank.Default != options[0] {
		t.Fatalf("the hole opens on %q, not on the closest match", blank.Default)
	}
	if !strings.Contains(shape.Prompt, "{"+TaskModelBlank+"}") {
		t.Fatalf("the sentence has no hole in it: %q", shape.Prompt)
	}
	for _, none := range []TaskNotice{
		{},
		{Model: options[0]},
		{Model: options[0], ModelOptions: options[:1]},
	} {
		if shape := TaskModelShape(none); shape.Kind != InputNone {
			t.Fatalf("a proposal with nothing to ask carried %+v", shape)
		}
	}
}

// THE HARNESS LANE ASKS TWO QUESTIONS AND THEY DO NOT SHARE A ROW. An offer is
// a permission with a free no; a finished design is a judgement about a page
// somebody spent minutes writing, and drawing it with two answers would leave a
// person with no way to ask for the third thing they always want — that it be
// different.
func TestAFinishedDesignAsksThreeThingsAndAnOfferAsksTwo(t *testing.T) {
	agent, _ := questionSession(t, "qqqq7777qqqq7777", nil)

	offer := agent.harnessQuestion(3, Event{Kind: EventHarnessOffer, Text: `run harness "research"?`, Hint: "finds an answer across sources"})
	if offer.Ask != AskPermission {
		t.Fatalf("an offer asks %q, want a permission", offer.Ask)
	}
	if got := optionKeys(offer.Options); !sameStrings(got, []string{HarnessRunKey, HarnessNotNowKey}) {
		t.Fatalf("an offer offers %v, want run it and not now", got)
	}

	// THE FINISHED PAGE IS WHAT MAKES IT A DESIGN, and the fixture carries one
	// for that reason: it is the fact both roads agree on ([HarnessQuestion]),
	// and the surface refuses a design event without one (tui3's
	// askHarnessDesign).
	page := subharness.Harness{Id: subharness.Id{Name: "weekly-digest", Desc: "reads the log and writes it up"}}
	design := agent.harnessQuestion(4, Event{Kind: EventHarnessDesignDone, Text: "write up the week", Harness: &page})
	if design.Ask != AskJudgement {
		t.Fatalf("a design asks %q, want a judgement", design.Ask)
	}
	if design.Head != harnessDesignLead+"weekly-digest" {
		t.Fatalf("a design's head is %q — it names the request rather than the page", design.Head)
	}
	// AND IT STOPS ITS OWN NODE AND NOT THE CONVERSATION, which is what keeps the
	// box a person's while they read it (tui3's [questionOwnsBox]).
	if design.Blocking.Turn {
		t.Fatal("a finished page stops the turn, and the box under it stops being the person's")
	}
	if !design.Blocking.Blocks() {
		t.Fatal("a finished page stops nothing at all, and its row would say so")
	}
	if got := optionKeys(design.Options); !sameStrings(got, []string{HarnessSaveKey, HarnessChangeKey, HarnessDropKey}) {
		t.Fatalf("a design offers %v, want save, change and drop", got)
	}
	if design.Stakes != StakesCostly {
		t.Fatalf("a design's stakes are %q — a dropped page cannot be got back", design.Stakes)
	}
	// AND THE GATE TAKES IT. A question this engine builds and no surface can be
	// refused by [Question.Check] is a question that would never be asked.
	if err := design.Check(nil); err != nil {
		t.Fatalf("the gate refused the design's own question: %v", err)
	}
	for _, option := range design.Options {
		if strings.TrimSpace(option.Label) == "" || strings.TrimSpace(option.Consequence) == "" {
			t.Fatalf("an answer with nothing beside it: %+v", option)
		}
	}
}

// ASKING FOR A DESIGN TO BE DIFFERENT RESOLVES NOTHING. `change it` used to be
// spelled `improve` and it DROPPED the page (tui3's harnesscard.go tells that
// story); on the one door it must leave the lane exactly as it found it, or the
// page a person asked to have rewritten is a page that is already gone.
func TestAskingForADesignToBeDifferentLeavesItWaiting(t *testing.T) {
	agent, _ := questionSession(t, "qqqq8888qqqq8888", nil)
	answers := make(chan harnessAnswer, 1)
	agent.mu.Lock()
	agent.harnessAsks = map[uint64]harnessAsk{7: {answers: answers, card: Event{
		Kind: EventHarnessDesignDone, ID: 7, Text: "weekly-digest", Hint: "reads the log and writes it up",
	}}}
	agent.mu.Unlock()

	change := Answer{Kind: QuestionHarness, Ask: AskJudgement, ID: 7, Picked: []string{HarnessChangeKey}}
	if AnswerResolves(change) {
		t.Fatal("AnswerResolves says `change it` ends the question — the page would go with it")
	}
	if err := agent.ResolveQuestion(change); err != nil {
		t.Fatalf("ResolveQuestion: %v", err)
	}
	select {
	case got := <-answers:
		t.Fatalf("the lane was answered %+v — nothing was decided", got)
	default:
	}
	open := agent.OpenQuestions()
	if len(open) != 1 || open[0].Kind != QuestionHarness {
		t.Fatalf("the design is no longer waiting: %+v", open)
	}

	// AND THE SAME DIGIT ON AN OFFER STILL ENDS IT: `2` is `not now` there, and
	// one key means two things only because the shape tells them apart.
	notNow := Answer{Kind: QuestionHarness, Ask: AskPermission, ID: 7, Picked: []string{HarnessNotNowKey}}
	if !AnswerResolves(notNow) {
		t.Fatal("`not now` on an offer left the question open")
	}
	if err := agent.ResolveQuestion(notNow); err != nil {
		t.Fatalf("ResolveQuestion: %v", err)
	}
	select {
	case got := <-answers:
		if got.run {
			t.Fatalf("`not now` ran it: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the decline never reached the lane")
	}
}

// optionKeys is the keys of an answer list, in the order they are drawn.
func optionKeys(options []AnswerOption) []string {
	out := make([]string, 0, len(options))
	for _, option := range options {
		out = append(out, option.Key)
	}
	return out
}

func sameStrings(one, two []string) bool {
	if len(one) != len(two) {
		return false
	}
	for i := range one {
		if one[i] != two[i] {
			return false
		}
	}
	return true
}
