package session

// The inbox (tools_factory_answer.go): a stage's `ask` goes to its item's
// manager through the stage door and waits for the words, and the manager's
// `factory_answer` replies through the door lent to its turn.

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/manual"
)

// askingStageDoor is a stage door that takes questions.
type askingStageDoor struct {
	fakeStageDoor
	mu    sync.Mutex
	asked []factory.Asked
	reply string
}

func (d *askingStageDoor) Ask(_ context.Context, q factory.Asked) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.asked = append(d.asked, q)
	return d.reply, nil
}

// A STAGE'S QUESTION NEVER REACHES THE PERSON'S SCREEN: it goes through the
// stage door, whole, and the call answers the manager's words.
func TestAStagesAskGoesToTheManagerThroughItsDoor(t *testing.T) {
	door := &askingStageDoor{reply: "the manager answered: sqlite"}
	agent, _ := questionSession(t, "inbx1111inbx1111", func(config *Config) {
		config.Interactive = false
		config.Unattended = true
		config.Stage = door
	})
	out, isErr, err := agent.executeAsk(context.Background(), json.RawMessage(`{
		"head":"which storage shape should this use?",
		"kind":"choice",
		"reason":"two shapes are viable",
		"stakes":"costly",
		"options":[{"key":"1","label":"sqlite"},{"key":"2","label":"jsonl"}],
		"pick":{"key":"2","reason":"it streams"}}`))
	if err != nil || isErr {
		t.Fatalf("ask = %q, %v, %v", out, isErr, err)
	}
	if out != "the manager answered: sqlite" {
		t.Errorf("ask answered %q", out)
	}
	door.mu.Lock()
	defer door.mu.Unlock()
	if len(door.asked) != 1 {
		t.Fatalf("the door was asked %d times", len(door.asked))
	}
	q := door.asked[0]
	if q.Question != "which storage shape should this use? (two shapes are viable)" ||
		strings.Join(q.Options, ",") != "sqlite,jsonl" || q.Pick != "jsonl, because it streams" {
		t.Errorf("the door was handed %+v", q)
	}
}

// A PERMISSION KEEPS ITS OWN ROAD: the manager never answers one.
func TestAStagesPermissionDoesNotGoToTheManager(t *testing.T) {
	door := &askingStageDoor{reply: "the manager answered: yes"}
	agent, _ := questionSession(t, "inbx2222inbx2222", func(config *Config) {
		config.Interactive = false
		config.Stage = door
	})
	out, _, _ := agent.executeAsk(context.Background(), json.RawMessage(`{
		"head":"delete the old table?","kind":"permission","reason":"it is in the way","stakes":"costly"}`))
	if strings.Contains(out, "the manager answered") {
		t.Errorf("a permission was answered by the manager: %q", out)
	}
	door.mu.Lock()
	defer door.mu.Unlock()
	if len(door.asked) != 0 {
		t.Errorf("a permission went to the manager: %+v", door.asked)
	}
}

// fakeInboxDoor is the door lent to a manager's turn on a step's question.
type fakeInboxDoor struct {
	fakeRunDoor
	answer, why string
}

func (d *fakeInboxDoor) AnswerStep(_ context.Context, _ string, answer string) (string, error) {
	d.answer = answer
	return "answered · plan goes on with it", nil
}

func (d *fakeInboxDoor) SendOn(_ context.Context, _ string, why string) (string, error) {
	d.why = why
	return "sent to the person · plan waits for their answer", nil
}

// `factory_answer` ANSWERS THROUGH THE INBOX DOOR, or sends the question on
// with the reason; it needs exactly one, and on a turn with no question it
// changes nothing.
func TestFactoryAnswerRepliesThroughTheInboxDoor(t *testing.T) {
	door := &fakeInboxDoor{}
	a := &Agent{config: Config{Workspace: t.TempDir(), FactoryRun: door}}
	call := func(args string) (string, bool) {
		out, isErr, err := a.factoryAnswerTool().Execute(context.Background(), json.RawMessage(args))
		if err != nil {
			t.Fatal(err)
		}
		return out, isErr
	}
	if out, isErr := call(`{"answer":"  sqlite,   the recipe says so "}`); isErr || out != "answered · plan goes on with it" || door.answer != "sqlite, the recipe says so" {
		t.Errorf("answer = %q %v, door holds %q", out, isErr, door.answer)
	}
	if out, isErr := call(`{"ask_person":"a taste call"}`); isErr || !strings.HasPrefix(out, "sent to the person") || door.why != "a taste call" {
		t.Errorf("ask_person = %q %v, door holds %q", out, isErr, door.why)
	}
	for _, args := range []string{`{}`, `{"answer":"a","ask_person":"b"}`} {
		if out, isErr := call(args); !isErr || out != answerNeedsOne {
			t.Errorf("%s answered %q %v", args, out, isErr)
		}
	}
	plain := &Agent{config: Config{Workspace: t.TempDir(), FactoryRun: &fakeRunDoor{}}}
	out, isErr, _ := plain.factoryAnswerTool().Execute(context.Background(), json.RawMessage(`{"answer":"x"}`))
	if !isErr || out != answerNothingWaits {
		t.Errorf("a turn with no question answered %q %v", out, isErr)
	}
}

// `factory_answer` RIDES WITH `factory_run`, the manual names it, and its
// family is an edit.
func TestFactoryAnswerIsOnTheManagersBelt(t *testing.T) {
	with := &Agent{config: Config{Workspace: t.TempDir(), FactoryRun: &fakeRunDoor{}}}
	with.tools = with.belt()
	found := false
	for _, tool := range with.offeredTools() {
		if tool.Name == "factory_answer" {
			found = true
		}
	}
	if !found {
		t.Fatal("the manager's belt does not carry factory_answer")
	}
	without := &Agent{config: Config{Workspace: t.TempDir()}}
	without.tools = without.belt()
	for _, tool := range without.offeredTools() {
		if tool.Name == "factory_answer" {
			t.Fatal("a belt with no run door carries factory_answer")
		}
	}
	if !manual.Chat().Mentions("factory_answer") {
		t.Error("no chat manual page mentions factory_answer")
	}
	if got := ActionCategoryForTool("factory_answer"); got != ActionEdit {
		t.Errorf("factory_answer's family is %q, want %q", got, ActionEdit)
	}
}
