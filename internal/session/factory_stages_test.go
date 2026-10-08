package session

// The chat's door onto one item's stages (tools_factory_stages.go), held to
// its laws: absent without a door, a card before anything changes, nothing
// changed on a no, on words or on silence, the door's refusal said back, and
// exactly one change on a yes.

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

// fakeStagesDoor records every edit it is handed and answers the stages it
// was told to, or refuses with refuse.
type fakeStagesDoor struct {
	mu     sync.Mutex
	edits  []factory.PlanEdit
	now    []string
	refuse error
}

func (d *fakeStagesDoor) Ref(_ context.Context, item int) (string, error) {
	if item != 12 {
		return "", errors.New("there is no item on the factory floor by that id")
	}
	return "#12", nil
}

func (d *fakeStagesDoor) Apply(_ context.Context, item int, edit factory.PlanEdit) ([]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.refuse != nil {
		return nil, d.refuse
	}
	d.edits = append(d.edits, edit)
	return d.now, nil
}

func (d *fakeStagesDoor) applied() []factory.PlanEdit {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]factory.PlanEdit(nil), d.edits...)
}

func newStagesAgent(t *testing.T, door StagesDoor, window time.Duration) *Agent {
	t.Helper()
	root := t.TempDir()
	config := Config{
		Workspace: root, Place: Place{Dir: root, Workspace: root},
		Model: "test/model", Interactive: true, AskConsent: true,
		factoryWindow: window,
	}
	if door != nil {
		config.Stages = door
	}
	agent, err := newAgent(config, &scriptedCompleter{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	return agent
}

const stagesArgs = `{"item":12,"add":["after review, read it for auth holes"],"skip":["neaten"],"why":"touches billing"}`

func runFactoryStages(t *testing.T, agent *Agent, ctx context.Context, args string) <-chan string {
	t.Helper()
	results := make(chan string, 1)
	tool := agent.factoryStagesTool()
	go func() {
		out, _, err := tool.Execute(ctx, json.RawMessage(args))
		if err != nil {
			out = "error: " + err.Error()
		}
		results <- out
	}()
	return results
}

func awaitStagesQuestion(t *testing.T, events <-chan Event) Question {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case event := <-events:
			if event.Kind == EventQuestion && event.Question != nil && event.Question.Kind == QuestionStages {
				return *event.Question
			}
		case <-deadline.C:
			t.Fatal("no stages question was raised")
		}
	}
}

func TestFactoryStagesIsOnTheBeltOnlyWithItsDoor(t *testing.T) {
	floorOnly := &Agent{config: Config{Workspace: t.TempDir(), Factory: &fakeFactoryDoor{}}}
	floorOnly.tools = floorOnly.belt()
	for _, tool := range floorOnly.offeredTools() {
		if tool.Name == "factory_stages" {
			t.Fatal("a belt with no stages door carries factory_stages")
		}
	}
	with := &Agent{config: Config{Workspace: t.TempDir(), Factory: &fakeFactoryDoor{}, Stages: &fakeStagesDoor{}}}
	with.tools = with.belt()
	found := false
	for _, tool := range with.offeredTools() {
		if tool.Name == "factory_stages" {
			found = true
			if !strings.Contains(tool.Description, "only for the item this conversation was opened for") ||
				!strings.Contains(tool.Description, "only their key changes anything") {
				t.Errorf("the description does not hold the model to its item and the person's key: %q", tool.Description)
			}
		}
		if !manual.Chat().Mentions(tool.Name) {
			t.Errorf("no chat manual page mentions the %s tool — add it to internal/manual/chat/", tool.Name)
		}
	}
	if !found {
		t.Fatal("a belt with a stages door does not carry factory_stages")
	}
	if got := ActionCategoryForTool("factory_stages"); got != ActionCreate {
		t.Errorf("factory_stages' family is %q, want %q", got, ActionCreate)
	}
}

func TestFactoryStagesRefusesBeforeAnyCard(t *testing.T) {
	door := &fakeStagesDoor{}
	agent := newStagesAgent(t, door, 0)
	tool := agent.factoryStagesTool()
	for _, c := range []struct{ args, want string }{
		{`{"item":12}`, "at least one of add, skip or on"},
		{`{"skip":["review"]}`, "needs the floor id"},
		{`{"item":7,"skip":["review"]}`, "nothing changed: there is no item"},
		{`{"item":12,"add":["after review"]}`, "needs to say what it does"},
	} {
		out, isErr, err := tool.Execute(context.Background(), json.RawMessage(c.args))
		if err != nil || !isErr || !strings.Contains(out, c.want) {
			t.Errorf("%s: %q, %v, %v; want a refusal containing %q", c.args, out, isErr, err, c.want)
		}
	}
	if n := len(door.applied()); n != 0 {
		t.Errorf("%d edits applied by refused calls", n)
	}
	for _, q := range agent.OpenQuestions() {
		if q.Kind == QuestionStages {
			t.Error("a refused call raised a card")
		}
	}
}

func TestFactoryStagesCardAsksWithItsHeadAndTwoAnswers(t *testing.T) {
	door := &fakeStagesDoor{}
	agent := newStagesAgent(t, door, 0)
	questions, stop := agent.WatchQuestions()
	t.Cleanup(stop)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	results := runFactoryStages(t, agent, ctx, stagesArgs)

	q := awaitStagesQuestion(t, questions)
	added := factory.ParseStage("after review, read it for auth holes").Name
	if added == "" || q.Head != "wants to change #12's stages: +"+added+" · −neaten" {
		t.Errorf("head = %q", q.Head)
	}
	if q.Subject.Name != "stages · #12 · touches billing" {
		t.Errorf("subject = %q", q.Subject.Name)
	}
	if !q.Deadline.IsZero() || q.Pick != nil {
		t.Error("the stages card carries a clock or a pick; silence must change nothing")
	}
	if len(q.Options) != 2 || q.Options[0].Key != "1" || q.Options[0].Label != "change it" ||
		q.Options[1].Key != "2" || q.Options[1].Label != "not now" || !q.Options[1].Safe {
		t.Errorf("options = %+v", q.Options)
	}
	if q.Input.Kind != InputText || q.Input.Prompt != "say what to change… (enter sends it)" {
		t.Errorf("input = %+v", q.Input)
	}
	if err := q.Check(nil); err != nil {
		t.Errorf("the card is not a well-formed question: %v", err)
	}
	if len(door.applied()) != 0 {
		t.Fatal("the item changed before the person answered")
	}
	cancel()
	if out := awaitResult(t, results); out != "nothing changed: the card was never answered" {
		t.Errorf("interrupted result = %q", out)
	}
}

func TestFactoryStagesYesAppliesTheEditOnce(t *testing.T) {
	door := &fakeStagesDoor{now: []string{"plan", "write", "test", "review", "security", "proof"}}
	agent := newStagesAgent(t, door, 0)
	questions, stopQ := agent.WatchQuestions()
	t.Cleanup(stopQ)
	lane, stopT := agent.WatchTaskUpdates()
	t.Cleanup(stopT)
	results := runFactoryStages(t, agent, context.Background(), stagesArgs)
	q := awaitStagesQuestion(t, questions)
	if err := agent.ResolveQuestion(Answer{Kind: QuestionStages, Ref: q.Ref, Key: "1"}); err != nil {
		t.Fatal(err)
	}
	if out := awaitResult(t, results); out != "#12's stages are now: plan · write · test · review · security · proof" {
		t.Errorf("result = %q", out)
	}
	got := door.applied()
	if len(got) != 1 || len(got[0].Add) != 1 || got[0].Add[0].Ask != "after review, read it for auth holes" ||
		len(got[0].Skip) != 1 || got[0].Skip[0] != "neaten" || got[0].Why != "touches billing" {
		t.Fatalf("applied = %+v", got)
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event := <-lane:
			if event.Kind == EventStagesChanged {
				if event.Stages == nil || event.Stages.ID != q.Ref || len(event.Stages.Now) != 6 {
					t.Fatalf("changed news = %+v", event.Stages)
				}
				return
			}
		case <-deadline:
			t.Fatal("EventStagesChanged never reached the task lane")
		}
	}
}

func TestFactoryStagesNoWordsSilenceAndARefusalChangeNothing(t *testing.T) {
	door := &fakeStagesDoor{}
	agent := newStagesAgent(t, door, 0)
	questions, stop := agent.WatchQuestions()
	t.Cleanup(stop)

	results := runFactoryStages(t, agent, context.Background(), stagesArgs)
	q := awaitStagesQuestion(t, questions)
	if err := agent.ResolveQuestion(Answer{Kind: QuestionStages, Ref: q.Ref, Key: "2"}); err != nil {
		t.Fatal(err)
	}
	if out := awaitResult(t, results); out != "nothing changed: the person said no." {
		t.Errorf("no = %q", out)
	}

	results = runFactoryStages(t, agent, context.Background(), stagesArgs)
	q = awaitStagesQuestion(t, questions)
	if err := agent.ResolveQuestion(Answer{Kind: QuestionStages, Ref: q.Ref, Change: "keep neaten, it is cheap"}); err != nil {
		t.Fatal(err)
	}
	want := "the person changed it: keep neaten, it is cheap\nNothing changed. Propose it again with that"
	if out := awaitResult(t, results); out != want {
		t.Errorf("change = %q, want %q", out, want)
	}

	quiet := newStagesAgent(t, door, 20*time.Millisecond)
	if out := awaitResult(t, runFactoryStages(t, quiet, context.Background(), stagesArgs)); out != "nothing changed: the card was never answered" {
		t.Errorf("window = %q", out)
	}
	for _, q := range quiet.OpenQuestions() {
		if q.Kind == QuestionStages {
			t.Error("the card is still open after its window ended")
		}
	}
	if n := len(door.applied()); n != 0 {
		t.Errorf("%d edits applied on no, words or silence", n)
	}

	fixed := &fakeStagesDoor{refuse: errors.New("the recipe for issue is fixed; plan may not change the stages")}
	strict := newStagesAgent(t, fixed, 0)
	sq, stopS := strict.WatchQuestions()
	t.Cleanup(stopS)
	results = runFactoryStages(t, strict, context.Background(), stagesArgs)
	q = awaitStagesQuestion(t, sq)
	if err := strict.ResolveQuestion(Answer{Kind: QuestionStages, Ref: q.Ref, Key: "1"}); err != nil {
		t.Fatal(err)
	}
	if out := awaitResult(t, results); out != "nothing changed: the recipe for issue is fixed; plan may not change the stages" {
		t.Errorf("refused yes = %q", out)
	}
}

func TestFactoryStagesAnswerKeysAndQuestionAsked(t *testing.T) {
	yes, ok := AnswerFromKey(QuestionStages, "1")
	if !ok || !yes.Stages.Approved {
		t.Errorf("1 = %+v, %v", yes, ok)
	}
	no, ok := AnswerFromKey(QuestionStages, "2")
	if !ok || no.Stages.Approved {
		t.Errorf("2 = %+v, %v", no, ok)
	}
	if got := questionGoneReason(Question{Kind: QuestionStages}); got != "nothing changed in the item's stages" {
		t.Errorf("gone reason = %q", got)
	}
	key, ok := questionAsked(Event{Kind: EventStagesProposal, Stages: &StagesNotice{ID: "g1"}})
	if !ok || key != questionToken(QuestionStages, "g1") {
		t.Errorf("questionAsked = %q, %v", key, ok)
	}
	if _, ok := questionAsked(Event{Kind: EventStagesProposal, Stages: &StagesNotice{ID: "g1", Withdrawn: "x"}}); ok {
		t.Error("a withdrawn card still reads as asking")
	}
}
