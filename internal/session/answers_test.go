package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A QUESTION TRAVELS OUT IN PRESENCE AND THE ANSWER COMES BACK IN A FILE.
//
// These tests drive the three lanes' own ask functions rather than whole turns:
// what is under test is the pair of files this wave added — the question landing
// in presence.json, and an answer left beside it reaching the resolver the card
// in the window would have called. The turns those asks live inside are already
// tested by consent_test.go, task_test.go and standing_test.go.

// questionSession is [newPresenceSession] with the config left open, because
// every ask here needs a watcher and two of them need one of their own settings.
func questionSession(t *testing.T, id string, tune func(*Config)) (*Agent, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("session folder: %v", err)
	}
	workspace := t.TempDir()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = workspace
		config.Place = Place{Dir: dir, Workspace: workspace}
		config.SessionFile = filepath.Join(dir, placeTranscript)
		config.AskConsent = true
		if tune != nil {
			tune(config)
		}
	})
	return agent, dir
}

// watched attaches a hub, which is what makes a session one somebody is sitting
// in front of — the condition every one of the three asks tests before it waits.
func watched(agent *Agent) <-chan Event {
	hub := newEventHub()
	agent.mu.Lock()
	agent.hub = hub
	agent.mu.Unlock()
	return hub.subscribe()
}

// waitForQuestion polls the presence file until it carries one, because the
// heartbeat writes on a goroutine of its own.
func waitForQuestion(t *testing.T, dir string) PresenceQuestion {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if presence, ok := ReadSessionPresence(dir, time.Now()); ok && presence.Question.Answerable() {
			return presence.Question
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no question reached %s", filepath.Join(dir, presenceName))
	return PresenceQuestion{}
}

// waitForNoQuestion is the other end: the card was answered, so the session
// stops saying it is stopped on one.
func waitForNoQuestion(t *testing.T, dir string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		presence, ok := ReadSessionPresence(dir, time.Now())
		if !ok || !presence.Question.Answerable() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the question never left %s", filepath.Join(dir, presenceName))
}

func TestTheConsentQuestionTravelsInPresenceAndAnAnswerComesBack(t *testing.T) {
	agent, dir := questionSession(t, "aaaa1111aaaa2222", nil)
	hub := newEventHub()
	events := hub.subscribe()

	allowed := make(chan bool, 1)
	go func() {
		answer, err := agent.ask(context.Background(),
			hub,
			ai.ToolCall{ID: "c1", Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"rm -rf build/"}`}},
			approval.Decision{Action: approval.ActionPrompt, Rule: "bash always asks"})
		if err != nil {
			t.Errorf("the gate ended in an error: %v", err)
		}
		allowed <- answer
	}()

	request := <-events
	if request.Kind != EventConsentRequest {
		t.Fatalf("the gate sent %v first", request.Kind)
	}
	question := waitForQuestion(t, dir)
	if question.Kind != QuestionConsent {
		t.Fatalf("the question says it is a %q", question.Kind)
	}
	if question.ID != request.ID {
		t.Fatalf("presence names question %d, the event names %d — an answer would reach nobody",
			question.ID, request.ID)
	}
	if question.Text != "needs your ok to run bash" {
		t.Fatalf("the line is %q", question.Text)
	}
	if question.Asked.IsZero() {
		t.Fatal("the question carries no stamp")
	}
	// THE OPTIONS ARE THE WRITER'S OWN ACCOUNT of what it will take, so a
	// surface can draw them without knowing anything about the gate.
	keys := make([]string, 0, len(question.Options))
	for _, option := range question.Options {
		keys = append(keys, option.Key+" "+option.Label)
	}
	want := []string{"1 allow once", "2 always", "3 deny"}
	if len(keys) != len(want) {
		t.Fatalf("the chips are %v, want %v", keys, want)
	}
	for at := range want {
		if keys[at] != want[at] {
			t.Fatalf("the chips are %v, want %v", keys, want)
		}
	}

	// AND THE ANSWER COMES BACK ON THE DOORSTEP, from a window that never
	// touched this process.
	if err := WriteAnswer(dir, QuestionConsent, question.ID, "2"); err != nil {
		t.Fatalf("leaving the answer: %v", err)
	}
	agent.drainAnswers()
	if !<-allowed {
		t.Fatal("`2 always` did not allow the call")
	}
	// The always was the tool-wide memo, exactly as the window's own always
	// sends it — the next bash question is answered without anybody being asked.
	if remembered, known := agent.rememberedConsent("bash"); !known || !remembered {
		t.Fatalf("the always left no memo: remembered=%v known=%v", remembered, known)
	}
	waitForNoQuestion(t, dir)
	if _, err := os.Stat(AnswersPath(dir)); !os.IsNotExist(err) {
		t.Fatalf("the drain left the doorstep behind: %v", err)
	}
}

func TestTheTaskProposalTravelsInPresenceAndAnAnswerComesBack(t *testing.T) {
	// A countdown of zero with somebody watching is a proposal that waits for an
	// answer and nothing else (task.go), which is the shape a person can walk to
	// another window and answer.
	agent, dir := questionSession(t, "bbbb1111bbbb2222", func(config *Config) {
		config.TaskAutoApproveSeconds = 0
	})
	events := watched(agent)

	answers := make(chan TaskAnswer, 1)
	go func() {
		answer, err := agent.askTask(context.Background(), 7, taskSpec{title: "port the resume picker"})
		if err != nil {
			t.Errorf("the proposal ended in an error: %v", err)
		}
		answers <- answer
	}()

	if proposal := <-events; proposal.Kind != EventTaskProposal {
		t.Fatalf("the proposal lane sent %v", proposal.Kind)
	}
	question := waitForQuestion(t, dir)
	if question.Kind != QuestionTask || question.ID != 7 {
		t.Fatalf("the question reads %+v", question)
	}
	if question.Text != "wants to start a task: port the resume picker" {
		t.Fatalf("the line is %q", question.Text)
	}

	if err := WriteAnswer(dir, QuestionTask, 7, "2"); err != nil {
		t.Fatalf("leaving the answer: %v", err)
	}
	agent.drainAnswers()
	if answer := <-answers; answer.Approved {
		t.Fatal("`2 no` approved the task")
	}
	waitForNoQuestion(t, dir)
}

func TestTheStandingCardTravelsInPresenceAndAnAnswerComesBack(t *testing.T) {
	agent, dir := questionSession(t, "cccc1111cccc2222", nil)
	events := watched(agent)

	answers := make(chan StandingAnswer, 1)
	go func() {
		notice := &StandingNotice{Item: standing.Item{Words: "tell me when ci goes red"}}
		answer, err := agent.askStanding(context.Background(), notice)
		if err != nil {
			t.Errorf("the card ended in an error: %v", err)
		}
		answers <- answer
	}()

	proposal := <-events
	if proposal.Kind != EventStandingProposal || proposal.Standing == nil {
		t.Fatalf("the standing lane sent %v", proposal.Kind)
	}
	question := waitForQuestion(t, dir)
	if question.Kind != QuestionStanding || question.ID != proposal.Standing.ID {
		t.Fatalf("the question reads %+v, the card is %d", question, proposal.Standing.ID)
	}
	if question.Text != "wants to keep an eye on: tell me when ci goes red" {
		t.Fatalf("the line is %q", question.Text)
	}
	// `2 change when` is not offered from another window: it is a request for a
	// text box, and there is no box on a card in a column.
	for _, option := range question.Options {
		if option.Key == "2" {
			t.Fatalf("the card offered `2 %s`, which cannot be answered from home", option.Label)
		}
	}
	// AND THE OUTRIGHT NO IS OFFERED, which is the one answer another window
	// used to have no way to give: `esc` is the no in the conversation, and esc
	// on home closes home.
	if AnswerLabel(QuestionStanding, StandingNoKey) == "" {
		t.Fatal("the standing card takes no decline at all")
	}
	var decline bool
	for _, option := range question.Options {
		decline = decline || option.Key == StandingNoKey
	}
	if !decline {
		t.Fatalf("the card offered %v, with nothing on it that says no", question.Options)
	}

	if err := WriteAnswer(dir, QuestionStanding, question.ID, "3"); err != nil {
		t.Fatalf("leaving the answer: %v", err)
	}
	agent.drainAnswers()
	answer := <-answers
	if !answer.Once || answer.Approved {
		t.Fatalf("`3 once, not standing` came out as %+v", answer)
	}
	waitForNoQuestion(t, dir)
}

// THE OUTRIGHT NO TRAVELS TOO, AND IT IS ONE KEYSTROKE.
//
// This is the answer the standing card was missing from every window but its
// own: `esc` is the no in the conversation, and `esc` on home closes home. A
// person who did not want the thing had to walk to the window or leave the
// question standing — which is the walk the whole answer band exists to save.
func TestAStandingCardIsDeclinedFromAnotherWindowWithOneKey(t *testing.T) {
	agent, dir := questionSession(t, "ffff3333ffff4444", nil)
	events := watched(agent)

	answers := make(chan StandingAnswer, 1)
	go func() {
		notice := &StandingNotice{Item: standing.Item{Words: "check the deploy every morning"}}
		answer, err := agent.askStanding(context.Background(), notice)
		if err != nil {
			t.Errorf("the card ended in an error: %v", err)
		}
		answers <- answer
	}()

	proposal := <-events
	if proposal.Kind != EventStandingProposal || proposal.Standing == nil {
		t.Fatalf("the standing lane sent %v", proposal.Kind)
	}
	question := waitForQuestion(t, dir)
	if err := WriteAnswer(dir, QuestionStanding, question.ID, StandingNoKey); err != nil {
		t.Fatalf("leaving the answer: %v", err)
	}
	agent.drainAnswers()
	// NOTHING WAS SET UP, AND NOTHING WAS RUN. The decline is the zero answer:
	// not approved, not once, no correction to re-propose from, and nobody was
	// asked about the OS timer.
	if answer := <-answers; answer != (StandingAnswer{}) {
		t.Fatalf("`%s not set up` came out as %+v", StandingNoKey, answer)
	}
	waitForNoQuestion(t, dir)
}

// THE DECLINE IS ON EVERY STANDING CARD THERE IS. The `once` chip is the only
// one that is ever missing (a one-off reminder's), because "do it now" is not a
// smaller version of "do it at six" — but "set nothing up" answers every
// standing question ever asked, and a person who learned the key on a watch
// must find it under the same key on a reminder.
func TestEveryStandingCardOffersTheSameDecline(t *testing.T) {
	reminder := standing.Item{
		When: standing.When{Kind: standing.WhenAt},
		Does: standing.Action{Kind: standing.ActionSay},
	}
	watch := standing.Item{
		When: standing.When{Kind: standing.WhenProbe},
		Does: standing.Action{Kind: standing.ActionSay},
	}
	for _, item := range []standing.Item{reminder, watch} {
		var found bool
		for _, option := range StandingOptions(item) {
			found = found || (option.Key == StandingNoKey && option.Label == "not set up")
		}
		if !found {
			t.Fatalf("a %s card offers %v, with no decline on it", item.When.Kind, StandingOptions(item))
		}
	}
	// AND IT IS NOT A DIGIT ANY CHIP OWNS. The card in a conversation numbers
	// its chips by position — 1, 2, 3 — so a decline sharing one of those would
	// move under the hand the day a card drew one chip fewer.
	var wearsIt int
	for _, option := range AnswerOptions(QuestionStanding) {
		if option.Key == StandingNoKey {
			wearsIt++
		}
	}
	if wearsIt != 1 {
		t.Fatalf("%q names %d answers on a standing card, want exactly one", StandingNoKey, wearsIt)
	}
	if StandingNoKey == StandingOnceKey || StandingNoKey == "1" || StandingNoKey == "2" {
		t.Fatalf("the decline is %q, which is a chip's own digit", StandingNoKey)
	}
}

// A STALE ID IS IGNORED, AS EVERY LATE ANSWER IS. Nothing is reported, because
// there is nobody left to report it to — the question is over.
func TestAnAnswerToAQuestionNobodyIsWaitingOnIsDropped(t *testing.T) {
	agent, dir := questionSession(t, "dddd1111dddd2222", nil)
	for _, kind := range []QuestionKind{QuestionConsent, QuestionTask, QuestionStanding} {
		if err := WriteAnswer(dir, kind, 99, "1"); err != nil {
			t.Fatalf("leaving the answer: %v", err)
		}
	}
	// A key the question does not take never reaches the doorstep at all.
	if err := WriteAnswer(dir, QuestionStanding, 99, "2"); err == nil {
		t.Fatal("`2` was written for a standing card, which does not take it")
	}
	agent.drainAnswers()
	if _, err := os.Stat(AnswersPath(dir)); !os.IsNotExist(err) {
		t.Fatalf("the drain left the doorstep behind: %v", err)
	}
}

// THE HEARTBEAT IS THE CADENCE. Nothing in the session polls for an answer on a
// clock of its own: the beat that puts the question on disk is the beat that
// looks for the answer.
func TestTheHeartbeatPicksTheAnswerUpOnItsOwn(t *testing.T) {
	agent, dir := questionSession(t, "eeee1111eeee2222", nil)
	// The agent's own desk beats every five seconds, which is right for a
	// machine and wrong here ([TestPresenceHeartbeatMovesTheStamp] makes the
	// same trade).
	agent.stopPresence()
	desk := &presenceDesk{
		agent: agent,
		path:  filepath.Join(dir, presenceName),
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
		nudge: make(chan struct{}, 1),
		every: 10 * time.Millisecond,
	}
	agent.presence = desk
	go desk.beat()
	// The desk is taken down through [Agent.stopPresence] and never by closing
	// its channel here: [Agent.Close] runs at the end of the test too, and two
	// closers of one channel is a panic rather than a tidy-up.
	defer agent.stopPresence()

	hub := newEventHub()
	events := hub.subscribe()
	allowed := make(chan bool, 1)
	go func() {
		answer, err := agent.ask(context.Background(), hub,
			ai.ToolCall{ID: "c1", Function: ai.ToolCallFunction{Name: "read", Arguments: "{}"}},
			approval.Decision{Action: approval.ActionPrompt, Rule: "ask about everything"})
		if err != nil {
			t.Errorf("the gate ended in an error: %v", err)
		}
		allowed <- answer
	}()
	request := <-events
	question := waitForQuestion(t, dir)

	if err := WriteAnswer(dir, QuestionConsent, request.ID, "3"); err != nil {
		t.Fatalf("leaving the answer: %v", err)
	}
	select {
	case answer := <-allowed:
		if answer {
			t.Fatal("`3 deny` allowed the call")
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("the heartbeat never picked the answer up (question %+v)", question)
	}
}

// The file is one line per answer, and it says what it is: a small enough shape
// that another program could write one.
func TestTheAnswerFileIsOneLinePerAnswer(t *testing.T) {
	dir := t.TempDir()
	if err := WriteAnswer(dir, QuestionConsent, 3, "1"); err != nil {
		t.Fatal(err)
	}
	if err := WriteAnswer(dir, QuestionTask, 4, "2"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(AnswersPath(dir))
	if err != nil {
		t.Fatalf("the answers file is not where it says it is: %v", err)
	}
	var first Answer
	if err := json.Unmarshal([]byte(splitFirstLine(string(raw))), &first); err != nil {
		t.Fatalf("the first line is not whole JSON: %v\n%s", err, raw)
	}
	if first.Kind != QuestionConsent || first.ID != 3 || first.Key != "1" || first.From != "home" {
		t.Fatalf("the first line reads %+v", first)
	}
	if first.At.IsZero() {
		t.Fatal("the answer carries no stamp")
	}

	answers, err := DrainAnswers(dir)
	if err != nil {
		t.Fatalf("draining: %v", err)
	}
	if len(answers) != 2 || answers[0].ID != 3 || answers[1].ID != 4 {
		t.Fatalf("the drain answered %+v", answers)
	}
	// And a folder with nothing on its doorstep is an empty answer, not a fault.
	again, err := DrainAnswers(dir)
	if err != nil || len(again) != 0 {
		t.Fatalf("a second drain answered %+v, %v", again, err)
	}
}

// splitFirstLine is the first line of a file, without its newline.
func splitFirstLine(text string) string {
	for at := 0; at < len(text); at++ {
		if text[at] == '\n' {
			return text[:at]
		}
	}
	return text
}

// THE MAPPING IS THE ONE THING BOTH SIDES READ. A key that means one thing to
// the surface drawing it and another to the session applying it is the fault
// this table exists to make impossible.
func TestTheKeysMeanWhatTheChipsSay(t *testing.T) {
	for _, want := range []struct {
		kind  QuestionKind
		key   string
		label string
		check func(AnswerAction) bool
	}{
		{QuestionConsent, "1", "allow once", func(a AnswerAction) bool { return a.Allow && a.Scope == ConsentOnce }},
		{QuestionConsent, "2", "always", func(a AnswerAction) bool { return a.Allow && a.Scope == ConsentToolSession }},
		{QuestionConsent, "3", "deny", func(a AnswerAction) bool { return !a.Allow }},
		{QuestionTask, "1", "yes", func(a AnswerAction) bool { return a.Task.Approved }},
		{QuestionTask, "2", "no", func(a AnswerAction) bool { return !a.Task.Approved }},
		{QuestionStanding, "1", "yes", func(a AnswerAction) bool { return a.Standing.Approved && !a.Standing.Once }},
		{QuestionStanding, "3", "once, not standing", func(a AnswerAction) bool { return a.Standing.Once && !a.Standing.Approved }},
		{QuestionStanding, "0", "not set up", func(a AnswerAction) bool {
			return a.Standing == (StandingAnswer{})
		}},
	} {
		if label := AnswerLabel(want.kind, want.key); label != want.label {
			t.Errorf("%s %s is called %q, want %q", want.kind, want.key, label, want.label)
		}
		action, ok := AnswerFromKey(want.kind, want.key)
		if !ok {
			t.Errorf("%s does not take %s", want.kind, want.key)
			continue
		}
		if action.Kind != want.kind || !want.check(action) {
			t.Errorf("%s %s comes out as %+v", want.kind, want.key, action)
		}
	}
	// Everything else is nothing at all — never a yes.
	for _, key := range []string{"", "4", "y", "9", "2 "} {
		if action, ok := AnswerFromKey(QuestionStanding, key); ok && key != "2 " {
			t.Errorf("a standing card took %q as %+v", key, action)
		}
	}
	if _, ok := AnswerFromKey("nonsense", "1"); ok {
		t.Error("a question kind nothing raises took an answer")
	}
}
