package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

// jobQuestionBody is a job's askback: a prompt and two numbered keys, the same
// shape every surface reads.
func jobQuestionBody(prompt, first, second string) string {
	return prompt + "\n\n```\n" +
		`{"kind":"choose","prompt":"` + prompt + `","options":[{"key":"1","label":"` + first +
		`"},{"key":"2","label":"` + second + `"}],"allowFree":true}` + "\n```"
}

// twoQuestionModel seats the reported state: two jobs are both waiting on an
// answer, both drew their choices in the thread, and the newest one is the
// answer target the digit keys aim at.
func twoQuestionModel(t *testing.T, backend *fakeBackend, session string) *Model {
	t.Helper()
	model := New(backend, session)
	older := store.Command{
		Seq: 41, Time: time.Now().Add(-10 * time.Minute), SessionID: session,
		Kind: store.CommandSplice, Instruction: "patch the auth token refresh",
		Status: store.CommandRejected,
	}
	newer := store.Command{
		Seq: 43, Time: time.Now().Add(-2 * time.Minute), SessionID: session,
		Kind: store.CommandSplice, Instruction: "run the deploy migration",
		Status: store.CommandRejected,
	}
	model.commands[older.Seq] = older
	model.commands[newer.Seq] = newer
	model.messages = []store.Message{
		{
			Seq: 210, Time: time.Now().Add(-9 * time.Minute), SessionID: session,
			Role: store.RoleAgent, CommandSeq: older.Seq, QuestionSeq: 205,
			Body: jobQuestionBody("Which branch should the fix target?", "main", "release/2.4"),
		},
		{
			Seq: 214, Time: time.Now().Add(-time.Minute), SessionID: session,
			Role: store.RoleAgent, CommandSeq: newer.Seq, QuestionSeq: 212,
			Body: jobQuestionBody("Start the migration plan at $4.10?", "start it", "hold it"),
		},
	}
	model.lastSeq = 214
	backend.nextSeq = 300
	model.rebuildCards()
	model.setSize(110, 40)
	if len(model.cards) != 2 {
		t.Fatalf("two open job questions derived %d cards", len(model.cards))
	}
	return model
}

// The blocker, exactly: the eyes pointed at one card and the pointing was
// discarded at the seam, so the store resolved the reply against the newest
// question and approved a plan the user never read.
func TestClickingTheOlderQuestionAnswersTheOlderQuestion(t *testing.T) {
	backend := &fakeBackend{}
	model := twoQuestionModel(t, backend, "two-questions")
	_ = model.View()
	rows := optionRowLines(model)
	if len(rows) != 4 {
		t.Fatalf("thread option rows = %#v", model.cardOptionRows)
	}
	// The first two rows belong to the older question, drawn with the message
	// that asked it.
	y := model.chatBounds.y + rows[0] - model.chat.YOffset
	_, click := model.Update(tea.MouseMsg{
		X: model.chatBounds.x + 3, Y: y,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	posted := requirePosted(t, model, backend, click)
	if posted.Body != "1" {
		t.Fatalf("click posted %q", posted.Body)
	}
	if posted.QuestionSeq != 205 {
		t.Fatalf("answer carried question %d, want the one that was clicked (205)", posted.QuestionSeq)
	}
}

// The digit keys aim at the answer target on screen, and that identity rides
// the reply too — a bare "1" that reaches the store with no question named is
// what made "newest wins" the only available rule.
func TestDigitAnswerCarriesTheQuestionOnScreen(t *testing.T) {
	backend := &fakeBackend{}
	model := twoQuestionModel(t, backend, "two-questions-digit")
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	posted := requirePosted(t, model, backend, command)
	if posted.Body != "2" || posted.QuestionSeq != 212 {
		t.Fatalf("digit answer = %+v, want option 2 of question 212", posted)
	}
}

// A head askback with no job behind it is the thread's own question, and it
// carries its identity the same way.
func TestThreadQuestionAnswerCarriesItsSeq(t *testing.T) {
	backend := &fakeBackend{}
	model := headQuestionModel(t, backend, "thread-question-seq")
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	posted := requirePosted(t, model, backend, command)
	if posted.QuestionSeq != 3337 {
		t.Fatalf("thread answer carried question %d, want 3337", posted.QuestionSeq)
	}
}

// The dock is the one surface that always knew which question it was showing.
// It still does, and now every other surface agrees with it.
func TestDockAnswerCarriesItsSeq(t *testing.T) {
	backend := &fakeBackend{agentQuestions: []store.AgentQuestion{
		{Seq: 77, SessionID: "dock-seq", Text: "Use a table or bullets?",
			Urgency: store.QuestionWhenever, Status: store.QuestionPending},
	}}
	model := New(backend, "dock-seq")
	model.setSize(110, 30)
	model.applyPoll(model.poll()().(pollResultMsg))
	model.questionDockExpanded = true
	command := model.surfaceSelectedAgentQuestion()
	if command == nil {
		t.Fatal("dock selection surfaced nothing")
	}
	_, _ = model.Update(command())
	if model.answeringQuestionSeq != 77 {
		t.Fatalf("dock aim = %d, want 77", model.answeringQuestionSeq)
	}
	_, _ = model.Update(model.postUserMessage("bullets")())
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.posted) != 1 || backend.posted[0].QuestionSeq != 77 {
		t.Fatalf("dock answer = %#v", backend.posted)
	}
}

// The dock read the unsurfaced set, and a blocking question is surfaced the
// instant it is asked — so the questions a running job is stuck behind were the
// exact questions the dock could never show.
func TestDockListsQuestionsThatAreAlreadyInTheThread(t *testing.T) {
	backend := &fakeBackend{agentQuestions: []store.AgentQuestion{
		{Seq: 90, SessionID: "dock-open", Text: "Which branch should the fix target?",
			Urgency: store.QuestionBlocking, Status: store.QuestionAsked},
		{Seq: 91, SessionID: "dock-open", Text: "Keep the appendix?",
			Urgency: store.QuestionNextNaturalMoment, Status: store.QuestionPending},
	}}
	model := New(backend, "dock-open")
	model.setSize(110, 30)
	model.applyPoll(model.poll()().(pollResultMsg))
	if len(model.agentQuestions) != 2 {
		t.Fatalf("dock questions = %#v", model.agentQuestions)
	}
	dock := model.renderActivityBar()
	if !strings.Contains(dock, "2 questions waiting") {
		t.Fatalf("dock did not count the open questions:\n%s", dock)
	}
	// Choosing the one that is already in the thread says nothing twice: it
	// aims the next answer at it.
	model.questionDockExpanded = true
	model.questionDockSelection = 0
	if command := model.surfaceSelectedAgentQuestion(); command != nil {
		t.Fatal("an already-asked question was surfaced again")
	}
	if model.answeringQuestionSeq != 90 {
		t.Fatalf("dock aim = %d, want 90", model.answeringQuestionSeq)
	}
	if !strings.Contains(model.status, "Which branch") {
		t.Fatalf("aiming at a question said nothing: %q", model.status)
	}
	_, _ = model.Update(model.postUserMessage("main")())
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.posted) != 1 || backend.posted[0].QuestionSeq != 90 {
		t.Fatalf("answer to a re-aimed question = %#v", backend.posted)
	}
}
