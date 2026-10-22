package session

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/factory"
	"reflect"
	"strings"
	"testing"
	"time"
)

type savedManagerDoor struct {
	fakeRunDoor
	answers   int
	seen      string
	statusErr error
}

func (d *savedManagerDoor) ManagerStatus(context.Context, string) (factory.ManagerStatus, error) {
	if d.statusErr != nil {
		return factory.ManagerStatus{}, d.statusErr
	}
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

	eventsGot := collect(t, events)
	assertFreeStatus(t, eventsGot)
	if completer.requests() != 0 || d.answers != 0 {
		t.Fatal("status used a provider or answered the decision")
	}
}
func assertFreeStatus(t *testing.T, events []Event) {
	t.Helper()
	if len(events) != 1 || events[0].Kind != EventNotice || !events[0].Unrecorded || !strings.Contains(events[0].Text, "Plan is waiting") || strings.Contains(events[0].Text, "saved-id") {
		t.Fatal("status did not return one independent, human-only read")
	}
}
func TestFactoryStatusDoesNotDrainRetainedFollowUps(t *testing.T) {
	d := &savedManagerDoor{}
	completer := &scriptedCompleter{}
	a, _ := newTestAgent(t, completer, func(c *Config) { c.FactoryRun = d })
	retained := followUp{message: userText("retained instruction"), stream: newEventStream()}
	a.mu.Lock()
	a.followups = append(a.followups, retained)
	a.mu.Unlock()
	identity, usage := a.ModelIdentity(), a.Usage()
	events, err := a.Submit(t.Context(), "status")
	if err != nil {
		t.Fatal(err)
	}
	assertFreeStatus(t, collect(t, events))
	a.mu.Lock()
	same := !a.running && len(a.followups) == 1 && a.followups[0].stream == retained.stream && len(a.steering) == 0
	a.mu.Unlock()
	if !same || completer.requests() != 0 || !reflect.DeepEqual(identity, a.ModelIdentity()) || !reflect.DeepEqual(usage, a.Usage()) || d.answers != 0 {
		t.Fatal("status entered completion cleanup, spent, or altered retained work")
	}
}
func TestBusyFactoryStatusDoesNotSteerOrCancelTheTurn(t *testing.T) {
	entered, cancelled := make(chan struct{}), make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			close(entered)
			<-ctx.Done()
			close(cancelled)
			return nil, ctx.Err()
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("ordinary correction received"), nil
		},
	}}
	d := &savedManagerDoor{}
	a, _ := newTestAgent(t, completer, func(c *Config) { c.FactoryRun = d })
	foreground, err := a.Submit(t.Context(), "Investigate the implementation")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not start")
	}
	identity := a.ModelIdentity()
	for _, read := range []func() (<-chan Event, error){func() (<-chan Event, error) { return a.Submit(t.Context(), "are we stuck?") }, func() (<-chan Event, error) { return a.Steer("what's next?") }} {
		stream, err := read()
		if err != nil {
			t.Fatal(err)
		}
		assertFreeStatus(t, collect(t, stream))
	}
	a.mu.Lock()
	unchanged := a.running && len(a.steering) == 0
	a.mu.Unlock()
	select {
	case <-cancelled:
		t.Fatal("free status cancelled provider work")
	default:
	}
	if !unchanged || !reflect.DeepEqual(identity, a.ModelIdentity()) || completer.requests() != 1 || d.answers != 0 {
		t.Fatal("status changed running work, identity, or decision")
	}
	correction, err := a.Steer("Use the existing helper instead")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, correction)
	collect(t, foreground)
	select {
	case <-cancelled:
	default:
		t.Fatal("ordinary non-status steering lost its interrupt behavior")
	}
	if completer.requests() != 2 {
		t.Fatal("ordinary steering did not continue with the provider")
	}
}

func TestFactoryStatusReadFailureNeverFallsThroughToPaidWork(t *testing.T) {
	d := &savedManagerDoor{statusErr: errors.New("saved status could not be read")}
	completer := &scriptedCompleter{}
	a, _ := newTestAgent(t, completer, func(c *Config) { c.FactoryRun = d })
	if _, err := a.Submit(t.Context(), "status"); err == nil {
		t.Fatal("status read failure became a paid turn")
	}
	if completer.requests() != 0 {
		t.Fatal("status failure used the provider")
	}
	d.statusErr = factory.ErrNotManager
	if _, handled, err := a.ReadFactoryStatus(t.Context(), "status"); err != nil || handled {
		t.Fatal("an ordinary non-manager conversation lost its normal path")
	}
}
