package session

import (
	"context"
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/factory"
	"strings"
	"testing"
	"time"
)

type savedManagerDoor struct {
	fakeRunDoor
	answers int
	seen    string
}

func (d *savedManagerDoor) ManagerStatus(context.Context, string) (factory.ManagerStatus, error) {
	return factory.ManagerStatus{Text: "Plan is waiting for a saved decision.\nPending question ID: saved-id.", QuestionID: "saved-id", At: time.Now()}, nil
}
func (d *savedManagerDoor) AnswerPending(_ context.Context, _ string, id, answer string) (string, error) {
	d.answers++
	d.seen = id + ":" + answer
	return "Decision saved", nil
}
func TestFactoryAnswerUsesThePersistedDecisionDoor(t *testing.T) {
	d := &savedManagerDoor{}
	a := &Agent{config: Config{FactoryRun: d}}
	out, bad, err := a.factoryAnswerTool().Execute(context.Background(), json.RawMessage(`{"question_id":"saved-id","answer":"Merge both"}`))
	if err != nil || bad || out != "Decision saved" || d.seen != "saved-id:Merge both" {
		t.Fatalf("ordinary turn could not answer: %q %v %v", out, bad, err)
	}
}
func TestFactoryStatusDirectReplyUsesNoProvider(t *testing.T) {
	d := &savedManagerDoor{}
	completer := &scriptedCompleter{}
	a, _ := newTestAgent(t, completer, func(c *Config) { c.FactoryRun = d; c.SpendRailUSD = 1 })
	spend(a, 2)
	events, err := a.Submit(context.Background(), "Where do we stand here? What's next?")
	if err != nil {
		t.Fatal(err)
	}
	var text string
	done := false
	for event := range events {
		if event.Kind == EventError {
			t.Fatal(event.Err)
		}
		if event.Kind == EventTextDelta {
			text += event.Text
		}
		if event.Kind == EventTurnDone {
			done = true
			if event.Usage.CostUSD != 0 || event.Usage.Calls != 0 {
				t.Fatal("status spent money")
			}
		}
	}
	if completer.requests() != 0 {
		t.Fatal("status used a provider request")
	}
	if !done || !strings.Contains(text, "Plan is waiting") || strings.Contains(text, "saved-id") || d.answers != 0 {
		t.Fatalf("bad local status %q", text)
	}
}
