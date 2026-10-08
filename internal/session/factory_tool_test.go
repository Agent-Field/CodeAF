package session

// The chat's door onto the factory floor (tools_factory.go), held to its laws:
// absent without a door, a card before anything is written, nothing written on
// a no, on words or on silence, and exactly one item on a yes.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/manual"
)

// fakeFactoryDoor records every item it is handed and answers a fixed id.
type fakeFactoryDoor struct {
	mu    sync.Mutex
	items []factory.Item
	id    int
	err   error
}

func (d *fakeFactoryDoor) Add(_ context.Context, it factory.Item) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err != nil {
		return 0, d.err
	}
	d.items = append(d.items, it)
	return d.id, nil
}

func (d *fakeFactoryDoor) written() []factory.Item {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]factory.Item(nil), d.items...)
}

func newFactoryAgent(t *testing.T, door FactoryDoor, window time.Duration) *Agent {
	t.Helper()
	root := t.TempDir()
	config := Config{
		Workspace: root, Place: Place{Dir: root, Workspace: root},
		Model: "test/model", Interactive: true, AskConsent: true,
		factoryWindow: window,
	}
	if door != nil {
		config.Factory = door
	}
	agent, err := newAgent(config, &scriptedCompleter{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	return agent
}

const factoryArgs = `{"title":"retry the flaky upload test","body":"the upload test fails one run in ten on CI; find why and make it deterministic","repo":"web","kind":"bug","size":"S","estimate_usd":0.4}`

// runFactoryAdd calls the tool on its own goroutine and hands back the result
// channel.
func runFactoryAdd(t *testing.T, agent *Agent, ctx context.Context) <-chan string {
	t.Helper()
	results := make(chan string, 1)
	tool := agent.factoryAddTool()
	go func() {
		out, _, err := tool.Execute(ctx, json.RawMessage(factoryArgs))
		if err != nil {
			out = "error: " + err.Error()
		}
		results <- out
	}()
	return results
}

// awaitFactoryQuestion waits for the card's question on the questions lane.
func awaitFactoryQuestion(t *testing.T, events <-chan Event) Question {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case event := <-events:
			if event.Kind == EventQuestion && event.Question != nil && event.Question.Kind == QuestionFactory {
				return *event.Question
			}
		case <-deadline.C:
			t.Fatal("no factory question was raised")
		}
	}
}

func awaitResult(t *testing.T, results <-chan string) string {
	t.Helper()
	select {
	case out := <-results:
		return out
	case <-time.After(5 * time.Second):
		t.Fatal("factory_add never returned")
	}
	return ""
}

func TestFactoryAddIsAbsentWithoutADoorAndPresentWithOne(t *testing.T) {
	without := &Agent{config: Config{Workspace: t.TempDir()}}
	without.tools = without.belt()
	for _, tool := range without.offeredTools() {
		if tool.Name == "factory_add" {
			t.Fatal("a belt with no factory door carries factory_add")
		}
	}
	if strings.Contains(promptWithBeltFacts(without.config), "factory_add") {
		t.Error("the page names factory_add on a belt with no door")
	}

	with := &Agent{config: Config{Workspace: t.TempDir(), Factory: &fakeFactoryDoor{}}}
	with.tools = with.belt()
	found := false
	for _, tool := range with.offeredTools() {
		if tool.Name == "factory_add" {
			found = true
		}
		if !manual.Chat().Mentions(tool.Name) {
			t.Errorf("no chat manual page mentions the %s tool — add it to internal/manual/chat/", tool.Name)
		}
	}
	if !found {
		t.Fatal("a belt with a factory door does not carry factory_add")
	}
	if !strings.Contains(promptWithBeltFacts(with.config), "AND THE FACTORY FLOOR: `factory_add`; a card asks, nothing starts.") {
		t.Error("the page does not say factory_add asks first on a belt that carries it")
	}
	if got := ActionCategoryForTool("factory_add"); got != ActionCreate {
		t.Errorf("factory_add's family is %q, want %q", got, ActionCreate)
	}
	if glossField["factory_add"] != "title" {
		t.Errorf("factory_add glosses by %q, want title", glossField["factory_add"])
	}
}

func TestFactoryCardAsksWithItsHeadAndTwoAnswers(t *testing.T) {
	door := &fakeFactoryDoor{id: 41}
	agent := newFactoryAgent(t, door, 0)
	questions, stop := agent.WatchQuestions()
	t.Cleanup(stop)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	results := runFactoryAdd(t, agent, ctx)

	q := awaitFactoryQuestion(t, questions)
	if q.Head != "wants to put this on the factory floor: retry the flaky upload test" {
		t.Errorf("head = %q", q.Head)
	}
	if q.Subject.Name != "web · bug · S" {
		t.Errorf("subject = %q", q.Subject.Name)
	}
	if !strings.Contains(q.Reason, "one run in ten") {
		t.Errorf("reason = %q, want the body", q.Reason)
	}
	if q.Ask != AskPermission || q.Form != FormCard || q.Stakes != StakesReversible || !q.Blocking.Turn {
		t.Errorf("shape = %+v", q)
	}
	if !q.Deadline.IsZero() || q.Pick != nil {
		t.Error("the factory card carries a clock or a pick; silence must add nothing")
	}
	if len(q.Options) != 2 || q.Options[0].Key != "1" || q.Options[0].Label != "add it" ||
		q.Options[1].Key != "2" || q.Options[1].Label != "not now" || !q.Options[1].Safe {
		t.Errorf("options = %+v", q.Options)
	}
	if q.Input.Kind != InputText || q.Input.Prompt != "say what to change… (enter sends it)" {
		t.Errorf("input = %+v", q.Input)
	}
	if err := q.Check(nil); err != nil {
		t.Errorf("the card is not a well-formed question: %v", err)
	}
	if len(door.written()) != 0 {
		t.Fatal("something was written before the person answered")
	}
	cancel()
	if out := awaitResult(t, results); out != "nothing was added: the card was never answered" {
		t.Errorf("interrupted result = %q", out)
	}
}

func TestFactoryApproveWritesExactlyOneItemAndTellsTheWatchers(t *testing.T) {
	door := &fakeFactoryDoor{id: 1207}
	agent := newFactoryAgent(t, door, 0)
	questions, stopQ := agent.WatchQuestions()
	t.Cleanup(stopQ)
	lane, stopT := agent.WatchTaskUpdates()
	t.Cleanup(stopT)
	results := runFactoryAdd(t, agent, context.Background())

	q := awaitFactoryQuestion(t, questions)
	if err := agent.ResolveQuestion(Answer{Kind: QuestionFactory, Ref: q.Ref, Key: "1"}); err != nil {
		t.Fatal(err)
	}
	if out := awaitResult(t, results); out != "#1207 retry the flaky upload test is on the factory floor" {
		t.Errorf("result = %q", out)
	}
	items := door.written()
	if len(items) != 1 {
		t.Fatalf("%d items written, want exactly one", len(items))
	}
	it := items[0]
	if it.State != factory.StateNew || it.Origin != factory.OriginChat || it.Tier != factory.TierOwner ||
		it.Author != "" || it.Repo != "web" || it.Kind != factory.KindIssue || it.Title != "retry the flaky upload test" ||
		it.Triage.Type != "bug" || it.Triage.Size != "S" || it.Triage.Est != 0.4 || len(it.Stages) != 0 || it.Created.IsZero() {
		t.Errorf("item = %+v", it)
	}

	var sawProposal, sawAdded bool
	deadline := time.After(5 * time.Second)
	for !sawAdded {
		select {
		case event := <-lane:
			switch event.Kind {
			case EventFactoryProposal:
				if event.Factory != nil && event.Factory.ID == q.Ref {
					sawProposal = true
				}
			case EventFactoryAdded:
				if event.Factory == nil || event.Factory.Item != 1207 {
					t.Fatalf("added news = %+v", event.Factory)
				}
				sawAdded = true
			}
		case <-deadline:
			t.Fatal("EventFactoryAdded never reached the task lane")
		}
	}
	if !sawProposal {
		t.Error("EventFactoryProposal never reached the task lane")
	}
}

func TestFactoryNotNowWritesNothing(t *testing.T) {
	door := &fakeFactoryDoor{id: 3}
	agent := newFactoryAgent(t, door, 0)
	questions, stop := agent.WatchQuestions()
	t.Cleanup(stop)
	results := runFactoryAdd(t, agent, context.Background())
	q := awaitFactoryQuestion(t, questions)
	if err := agent.ResolveQuestion(Answer{Kind: QuestionFactory, Ref: q.Ref, Key: "2"}); err != nil {
		t.Fatal(err)
	}
	if out := awaitResult(t, results); out != "nothing was added: the person said no." {
		t.Errorf("result = %q", out)
	}
	if n := len(door.written()); n != 0 {
		t.Errorf("%d items written on not now", n)
	}
}

func TestFactoryChangeWordsWriteNothingAndComeBack(t *testing.T) {
	door := &fakeFactoryDoor{id: 3}
	agent := newFactoryAgent(t, door, 0)
	questions, stop := agent.WatchQuestions()
	t.Cleanup(stop)
	results := runFactoryAdd(t, agent, context.Background())
	q := awaitFactoryQuestion(t, questions)
	if err := agent.ResolveQuestion(Answer{Kind: QuestionFactory, Ref: q.Ref, Change: "it belongs on the api repo"}); err != nil {
		t.Fatal(err)
	}
	want := "the person changed it: it belongs on the api repo\nNothing is on the floor yet. Propose it again with that"
	if out := awaitResult(t, results); out != want {
		t.Errorf("result = %q, want %q", out, want)
	}
	if n := len(door.written()); n != 0 {
		t.Errorf("%d items written on a change", n)
	}
}

func TestFactoryWindowEndingWritesNothing(t *testing.T) {
	door := &fakeFactoryDoor{id: 3}
	agent := newFactoryAgent(t, door, 20*time.Millisecond)
	results := runFactoryAdd(t, agent, context.Background())
	if out := awaitResult(t, results); out != "nothing was added: the card was never answered" {
		t.Errorf("result = %q", out)
	}
	if n := len(door.written()); n != 0 {
		t.Errorf("%d items written when nobody answered", n)
	}
	for _, q := range agent.OpenQuestions() {
		if q.Kind == QuestionFactory {
			t.Error("the card is still open after its window ended")
		}
	}
}

func TestFactoryAnswerFromKeyMapsBothKeys(t *testing.T) {
	yes, ok := AnswerFromKey(QuestionFactory, "1")
	if !ok || !yes.Factory.Approved {
		t.Errorf("1 = %+v, %v", yes, ok)
	}
	no, ok := AnswerFromKey(QuestionFactory, "2")
	if !ok || no.Factory.Approved {
		t.Errorf("2 = %+v, %v", no, ok)
	}
	if _, ok := AnswerFromKey(QuestionFactory, "3"); ok {
		t.Error("a key the card does not offer was taken")
	}
	if got := questionGoneReason(Question{Kind: QuestionFactory}); got != "nothing was added to the factory floor" {
		t.Errorf("gone reason = %q", got)
	}
	key, ok := questionAsked(Event{Kind: EventFactoryProposal, Factory: &FactoryNotice{ID: "f1"}})
	if !ok || key != questionToken(QuestionFactory, "f1") {
		t.Errorf("questionAsked = %q, %v", key, ok)
	}
	if _, ok := questionAsked(Event{Kind: EventFactoryProposal, Factory: &FactoryNotice{ID: "f1", Decided: &FactoryAnswer{}}}); ok {
		t.Error("a settled card still reads as asking")
	}
}

func TestFactoryRefusedByTheDoorSaysSo(t *testing.T) {
	door := &fakeFactoryDoor{err: errors.New("the floor is closed")}
	agent := newFactoryAgent(t, door, 0)
	questions, stop := agent.WatchQuestions()
	t.Cleanup(stop)
	results := runFactoryAdd(t, agent, context.Background())
	q := awaitFactoryQuestion(t, questions)
	agent.ResolveFactory(q.Ref, FactoryAnswer{Approved: true})
	if out := awaitResult(t, results); !strings.HasPrefix(out, "nothing was added: the factory floor would not take it") {
		t.Errorf("result = %q", out)
	}
}

// A WINDOW THAT ARRIVES AFTER THE CARD WAS RAISED IS STILL HANDED IT. The card
// goes out on the task lane once; a lane opened while the call is parked gets
// the standing card replayed, and a lane opened after it was answered gets
// nothing, so a settled card never comes back as a standing one.
func TestFactoryCardIsReplayedToALateTaskLaneUntilItSettles(t *testing.T) {
	door := &fakeFactoryDoor{id: 9}
	agent := newFactoryAgent(t, door, 0)
	questions, stopQ := agent.WatchQuestions()
	t.Cleanup(stopQ)
	results := runFactoryAdd(t, agent, context.Background())
	q := awaitFactoryQuestion(t, questions)

	late, stopLate := agent.WatchTaskUpdates()
	t.Cleanup(stopLate)
	select {
	case event := <-late:
		if event.Kind != EventFactoryProposal || event.Factory == nil || event.Factory.ID != q.Ref ||
			event.Factory.Title != "retry the flaky upload test" || event.Factory.Decided != nil || event.Factory.Withdrawn != "" {
			t.Fatalf("the late lane was handed %v / %+v, want the standing card", event.Kind, event.Factory)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a lane opened while the card stood was not handed it")
	}

	if err := agent.ResolveQuestion(Answer{Kind: QuestionFactory, Ref: q.Ref, Key: "1"}); err != nil {
		t.Fatal(err)
	}
	awaitResult(t, results)

	agent.mu.Lock()
	standing := agent.standingFactoryCardsLocked()
	agent.mu.Unlock()
	if len(standing) != 0 {
		t.Fatalf("a settled card is still replayed as standing: %+v", standing)
	}
}
