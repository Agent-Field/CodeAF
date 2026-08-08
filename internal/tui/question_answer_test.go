package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// headQuestionBody is the shape the head asks in when no job is behind the
// question: a prompt and numbered option keys, and nothing that names a node or
// a command for the card derivation to hang the question on.
const headQuestionBody = `Ship the source inline, or keep it the way I just did it?

` + "```" + `
{"kind":"choose","prompt":"Ship the source inline, or keep it the way I just did it?","options":[{"key":"1","label":"keep it this way"},{"key":"2","label":"this is what I meant"}],"allowFree":true}
` + "```"

var errAnswerRefused = errors.New("the store refused the answer")

// headQuestionModel seats a model in the reported state: one agent question in
// the thread, with options, unanswered, owned by no job.
func headQuestionModel(t *testing.T, backend *fakeBackend, session string) *Model {
	t.Helper()
	model := New(backend, session)
	backend.nextSeq = 3342
	model.messages = []store.Message{{
		Seq: 3341, Time: time.Now(), SessionID: session,
		Role: store.RoleAgent, QuestionSeq: 3337, Body: headQuestionBody,
	}}
	model.lastSeq = 3341
	model.rebuildCards()
	model.setSize(110, 30)
	if len(model.cards) != 0 {
		t.Fatalf("head question derived job cards = %#v", model.cards)
	}
	if model.questionCardWithOptions() == nil {
		t.Fatal("head question has no answer target")
	}
	return model
}

func requirePosted(t *testing.T, model *Model, backend *fakeBackend, command tea.Cmd) store.Message {
	t.Helper()
	if command == nil {
		t.Fatal("answer produced no command")
	}
	_, _ = model.Update(command())
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.posted) != 1 {
		t.Fatalf("posted = %#v, want exactly one answer", backend.posted)
	}
	return backend.posted[0]
}

// The reported bug: the thread drew the choices, and every way of taking one
// died inside the TUI. A digit is the shortest of those ways.
func TestHeadQuestionDigitAnswersInTheThread(t *testing.T) {
	backend := &fakeBackend{}
	model := headQuestionModel(t, backend, "head-question-digit")
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	posted := requirePosted(t, model, backend, command)
	if posted.Role != store.RoleUser || posted.Body != "2" {
		t.Fatalf("posted answer = %+v", posted)
	}
	if model.input.Value() != "" {
		t.Fatalf("digit leaked into the composer: %q", model.input.Value())
	}
}

// Typing the number into the composer and pressing enter is the path that was
// working earlier the same night; it must keep answering rather than sending a
// bare "2" the head has to guess about.
func TestHeadQuestionNumberPlusEnterAnswers(t *testing.T) {
	backend := &fakeBackend{}
	model := headQuestionModel(t, backend, "head-question-enter")
	model.input.SetValue("1")
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	posted := requirePosted(t, model, backend, command)
	if posted.Body != "1" {
		t.Fatalf("posted answer = %q", posted.Body)
	}
}

// The band in the thread must be a selection, not decoration: the arrows move
// it and enter submits whatever it is sitting on.
func TestHeadQuestionArrowsAndEnterAnswer(t *testing.T) {
	backend := &fakeBackend{}
	model := headQuestionModel(t, backend, "head-question-arrows")
	cardID := model.questionCardWithOptions().ID
	if _, handled := model.updateKey(tea.KeyMsg{Type: tea.KeyDown}); !handled {
		t.Fatal("down did not move the option selection")
	}
	if model.questionSelection[cardID] != 1 {
		t.Fatalf("selection after down = %d", model.questionSelection[cardID])
	}
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	posted := requirePosted(t, model, backend, command)
	if posted.Body != "2" {
		t.Fatalf("enter submitted %q, want the banded option", posted.Body)
	}
}

// The thread zone's own traversal reaches the option rows, and enter there is
// the same answer a click on the row would be.
func TestHeadQuestionOptionRowIsAThreadTarget(t *testing.T) {
	backend := &fakeBackend{}
	model := headQuestionModel(t, backend, "head-question-thread")
	_ = model.View()
	rows := optionRowLines(model)
	if len(rows) != 2 {
		t.Fatalf("thread option rows = %#v", model.cardOptionRows)
	}
	focusable := model.chatFocusLines()
	for _, line := range rows {
		if !containsInt(focusable, line) {
			t.Fatalf("option row %d is not a thread focus target: %v", line, focusable)
		}
	}
	model.focus = focusChat
	model.inputFocused = false
	model.input.Blur()
	model.chatFocusIndex = indexOfInt(focusable, rows[1])
	command, ok := model.activateChatFocus()
	if !ok {
		t.Fatal("enter on the option row activated nothing")
	}
	if posted := requirePosted(t, model, backend, command); posted.Body != "2" {
		t.Fatalf("thread enter posted %q", posted.Body)
	}
}

func TestHeadQuestionOptionRowIsClickable(t *testing.T) {
	backend := &fakeBackend{}
	model := headQuestionModel(t, backend, "head-question-click")
	_ = model.View()
	rows := optionRowLines(model)
	if len(rows) != 2 {
		t.Fatalf("thread option rows = %#v", model.cardOptionRows)
	}
	y := model.chatBounds.y + rows[0] - model.chat.YOffset
	_, command := model.Update(tea.MouseMsg{
		X: model.chatBounds.x + 3, Y: y,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	if posted := requirePosted(t, model, backend, command); posted.Body != "1" {
		t.Fatalf("click posted %q", posted.Body)
	}
}

// Silence is the one unacceptable outcome: a refused answer says so.
func TestFailedAnswerSaysSo(t *testing.T) {
	backend := &fakeBackend{postErr: errAnswerRefused}
	model := headQuestionModel(t, backend, "head-question-error")
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	if command == nil {
		t.Fatal("digit produced no command")
	}
	_, _ = model.Update(command())
	if model.err == nil || !strings.Contains(model.err.Error(), "refused") {
		t.Fatalf("failed answer reported %v", model.err)
	}
	if !strings.Contains(ansi.Strip(model.View()), "refused") {
		t.Fatal("failed answer is not on screen")
	}
}

// The wave-4 key gate is what keeps a letter a letter. A question on screen
// must not turn the composer back into a trap.
func TestKeyGateHoldsWhileAQuestionIsOnScreen(t *testing.T) {
	backend := &fakeBackend{}
	model := headQuestionModel(t, backend, "head-question-gate")
	model.input.SetValue("k")
	for _, key := range []string{"j", "k", "y", "c", "v", ",", "[", "]"} {
		_, handled := model.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		if handled {
			t.Fatalf("%q acted while the composer had focus", key)
		}
	}
	backend.mu.Lock()
	posted := len(backend.posted)
	backend.mu.Unlock()
	if posted != 0 {
		t.Fatalf("gated keys posted %d messages", posted)
	}
}

func TestFooterAnnouncesASelectableQuestion(t *testing.T) {
	backend := &fakeBackend{}
	model := headQuestionModel(t, backend, "head-question-footer")
	want := "↑/↓ choose · enter answer"
	if line := model.contextHelpLine(); !strings.Contains(line, want) {
		t.Fatalf("footer with a question on screen = %q", line)
	}
	if !strings.Contains(ansi.Strip(model.View()), want) {
		t.Fatal("footer help is not on screen")
	}
	// A draft in progress is a sentence, not a selection.
	model.input.SetValue("actually")
	if line := model.contextHelpLine(); strings.Contains(line, want) {
		t.Fatalf("footer claimed a selection over a draft: %q", line)
	}
}

// A job's askback shares the thread's option rows, so the same click and the
// same enter answer it — the card only decides which selection they move.
func TestJobQuestionOptionRowIsAnswerableInTheThread(t *testing.T) {
	backend := &fakeBackend{nextSeq: 50}
	model := New(backend, "job-question")
	command := store.Command{
		Seq: 41, Time: time.Now().Add(-time.Minute), SessionID: "job-question",
		Kind: store.CommandSplice, Instruction: "ship it", Status: store.CommandRejected,
	}
	model.messages = []store.Message{{
		Seq: 42, Time: time.Now(), SessionID: "job-question", Role: store.RoleAgent,
		CommandSeq: command.Seq, Body: headQuestionBody,
	}}
	model.lastSeq = 42
	model.commands[command.Seq] = command
	model.rebuildCards()
	model.setSize(110, 30)
	card := model.questionCardWithOptions()
	if card == nil || card.CommandSeq != command.Seq {
		t.Fatalf("job question card = %#v", card)
	}
	_ = model.View()
	rows := optionRowLines(model)
	if len(rows) != 2 {
		t.Fatalf("thread option rows = %#v", model.cardOptionRows)
	}
	y := model.chatBounds.y + rows[1] - model.chat.YOffset
	_, click := model.Update(tea.MouseMsg{
		X: model.chatBounds.x + 3, Y: y,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	if posted := requirePosted(t, model, backend, click); posted.Body != "2" {
		t.Fatalf("job question click posted %q", posted.Body)
	}
}

func optionRowLines(model *Model) []int {
	lines := make([]int, 0, len(model.cardOptionRows))
	for _, row := range model.cardOptionRows {
		if !row.dock {
			lines = append(lines, row.line)
		}
	}
	return lines
}

func containsInt(values []int, want int) bool {
	return indexOfInt(values, want) >= 0
}

func indexOfInt(values []int, want int) int {
	for index, value := range values {
		if value == want {
			return index
		}
	}
	return -1
}
