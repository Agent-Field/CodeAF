package session

// The chat's door onto one item on the floor (tools_factory_item.go), held to
// its laws: absent without a door, refused before any card when the bounds or
// the arguments say no, a card whose head is one question and whose body is
// before and after for only what changes, nothing changed on a no, on words or
// on silence, and exactly one change on a yes.

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/manual"
)

// itemRecipe is the recipe these tests hold the card to: plan › approve ›
// write › test › review › an off security stage › proof, nothing fixed. IT IS
// SPELLED HERE, NOT READ OFF factory.DefaultRecipe: these tests pin how the
// card previews, applies and refuses a change, and an off stage is the one
// shape where think strong reaches a stage that does not run. The shipped
// default has neither an off stage nor unfixed approve and proof since
// 2026-10-20, and the card's mechanics do not move with it.
func itemRecipe() factory.Recipe {
	return factory.Recipe{
		DefaultBudget: factory.DefaultJobBudget,
		Stages: []factory.Stage{
			{Name: "plan", Ask: "read the issue and say how", Until: "done", On: true},
			{Name: factory.ApproveName, Kind: factory.StageGate, On: true},
			{Name: "write", Ask: "make the change in the checkout", Fanout: "per-file", Until: "done", On: true},
			{Name: "test", Ask: "run what the change implies", Until: "green", Max: 2, On: true},
			{Name: "review", Ask: "read it as a stranger would", Fanout: "per-finding", Until: "clean", Max: 1, On: true},
			{Name: "security", Ask: "secrets, injection and authz", Until: "clean", On: false},
			{Name: "proof", Ask: "show each claim in its own medium", Until: "proven", On: true},
		},
		ByKind: map[factory.Kind][]factory.Stage{},
	}
}

// fakeItemDoor holds one item, #1, under a recipe, and previews and applies
// through [ApplyItemChange] exactly as the store's door does. It records every
// change it applied.
type fakeItemDoor struct {
	mu      sync.Mutex
	item    factory.Item
	recipe  factory.Recipe
	applied []ItemChange
	refuse  error
}

func newFakeItemDoor() *fakeItemDoor {
	return &fakeItemDoor{
		item:   factory.Item{ID: 1, Repo: "factory-demo", Kind: factory.KindIssue, Title: "Total double-counts an entry added twice", State: factory.StateNew, Cap: 5},
		recipe: itemRecipe(),
	}
}

func (d *fakeItemDoor) Preview(_ context.Context, item int, change ItemChange) (factory.Item, factory.Item, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if item != d.item.ID {
		return factory.Item{}, factory.Item{}, errors.New("there is no item 7 on the factory floor")
	}
	after, err := ApplyItemChange(d.item, change, d.recipe)
	if err != nil {
		return factory.Item{}, factory.Item{}, err
	}
	return ItemStaged(d.item, d.recipe), after, nil
}

func (d *fakeItemDoor) Apply(_ context.Context, item int, change ItemChange) (factory.Item, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.refuse != nil {
		return factory.Item{}, d.refuse
	}
	next, err := ApplyItemChange(d.item, change, d.recipe)
	if err != nil {
		return factory.Item{}, err
	}
	d.item = next
	d.applied = append(d.applied, change)
	return next, nil
}

func (d *fakeItemDoor) changes() []ItemChange {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]ItemChange(nil), d.applied...)
}

func newItemAgent(t *testing.T, door ItemDoor, window time.Duration) *Agent {
	t.Helper()
	root := t.TempDir()
	config := Config{
		Workspace: root, Place: Place{Dir: root, Workspace: root},
		Model: "test/model", Interactive: true, AskConsent: true,
		factoryWindow: window,
	}
	if door != nil {
		config.FactoryItem = door
	}
	agent, err := newAgent(config, &scriptedCompleter{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	return agent
}

const itemArgs = `{"item":1,"cap":8,"why":"the stages need room for a second review"}`

func runFactoryItem(t *testing.T, agent *Agent, ctx context.Context, args string) <-chan string {
	t.Helper()
	results := make(chan string, 1)
	tool := agent.factoryItemTool()
	go func() {
		out, _, err := tool.Execute(ctx, json.RawMessage(args))
		if err != nil {
			out = "error: " + err.Error()
		}
		results <- out
	}()
	return results
}

func awaitItemQuestion(t *testing.T, events <-chan Event) Question {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case event := <-events:
			if event.Kind == EventQuestion && event.Question != nil && event.Question.Kind == QuestionItem {
				return *event.Question
			}
		case <-deadline.C:
			t.Fatal("no item question was raised")
		}
	}
}

func TestFactoryItemIsOnTheBeltOnlyWithItsDoor(t *testing.T) {
	floorOnly := &Agent{config: Config{Workspace: t.TempDir(), Factory: &fakeFactoryDoor{}}}
	floorOnly.tools = floorOnly.belt()
	for _, tool := range floorOnly.offeredTools() {
		if tool.Name == "factory_item" || tool.Name == "factory_stages" {
			t.Fatalf("a belt with no item door carries %s", tool.Name)
		}
	}
	with := &Agent{config: Config{Workspace: t.TempDir(), Factory: &fakeFactoryDoor{}, FactoryItem: newFakeItemDoor()}}
	with.tools = with.belt()
	found := false
	for _, tool := range with.offeredTools() {
		if tool.Name == "factory_stages" {
			t.Fatal("the retired factory_stages is still on the belt")
		}
		if tool.Name == "factory_item" {
			found = true
			for _, want := range []string{"only for the item this conversation was opened for", "only their `yes` changes anything",
				"leave notes here", "its stages report into this conversation"} {
				if !strings.Contains(tool.Description, want) {
					t.Errorf("the description does not say %q: %q", want, tool.Description)
				}
			}
		}
		if !manual.Chat().Mentions(tool.Name) {
			t.Errorf("no chat manual page mentions the %s tool — add it to internal/manual/chat/", tool.Name)
		}
	}
	if !found {
		t.Fatal("a belt with an item door does not carry factory_item")
	}
	if got := ActionCategoryForTool("factory_item"); got != ActionCreate {
		t.Errorf("factory_item's family is %q, want %q", got, ActionCreate)
	}
}

func TestFactoryItemRefusesBeforeAnyCard(t *testing.T) {
	door := newFakeItemDoor()
	agent := newItemAgent(t, door, 0)
	tool := agent.factoryItemTool()
	for _, c := range []struct{ args, want string }{
		{`{"item":1}`, "needs something to change"},
		{`{"cap":8}`, "needs the factory id"},
		{`{"item":7,"skip":["review"]}`, "nothing changed: there is no item 7"},
		{`{"item":1,"add":["after review"]}`, "needs to say what it does"},
		{`{"item":1,"effort":"huge"}`, "effort is one of cheap, strong, default"},
		{`{"item":1,"cap":-3}`, "cap is a number of dollars"},
		{`{"item":1,"skip":["proof"]}`, "nothing changed: plan may not skip proof"},
		{`{"item":1,"skip":["approve"]}`, "only you may choose automatic approval for approve"},
	} {
		out, isErr, err := tool.Execute(context.Background(), json.RawMessage(c.args))
		if err != nil || !isErr || !strings.Contains(out, c.want) {
			t.Errorf("%s: %q, %v, %v; want a refusal containing %q", c.args, out, isErr, err, c.want)
		}
	}
	// A change that leaves the item exactly as it is asks nothing.
	if out, _, _ := tool.Execute(context.Background(), json.RawMessage(`{"item":1,"cap":5}`)); out != "nothing changed: the item already runs that way" {
		t.Errorf("a no-op change = %q", out)
	}
	// AND UNDER A FIXED RECIPE A STAGE CHANGE IS REFUSED IN ADAPT'S OWN WORDS,
	// before any card.
	door.recipe.Adapt = map[factory.Kind]factory.AdaptMode{factory.KindIssue: factory.AdaptFixed}
	out, isErr, _ := tool.Execute(context.Background(), json.RawMessage(`{"item":1,"skip":["review"],"cap":8}`))
	if !isErr || out != "nothing changed: the recipe for issue is fixed; plan may not change the stages" {
		t.Errorf("fixed = %q, %v", out, isErr)
	}
	if n := len(door.changes()); n != 0 {
		t.Errorf("%d changes applied by refused calls", n)
	}
	for _, q := range agent.OpenQuestions() {
		if q.Kind == QuestionItem {
			t.Error("a refused call raised a card")
		}
	}
}

// itemCard is the notice a change to the fake door's item would raise.
func itemCard(t *testing.T, change ItemChange, why string) ItemNotice {
	t.Helper()
	door := newFakeItemDoor()
	before, after, err := door.Preview(context.Background(), 1, change)
	if err != nil {
		t.Fatal(err)
	}
	return ItemNotice{Item: 1, Ref: before.Ref(), Note: change.Note, Why: why, Before: ItemFactsOf(before), After: ItemFactsOf(after)}
}

// EVERY CARD IS ONE QUESTION, AND ITS BODY IS BEFORE AND AFTER FOR ONLY WHAT
// CHANGES.
func TestFactoryItemHeadAndRowsForEachChange(t *testing.T) {
	security := factory.ParseStage("after test, read it for auth holes").Name
	for _, c := range []struct {
		name   string
		change ItemChange
		why    string
		head   string
		rows   []string
	}{
		{"skip", ItemChange{Edit: factory.PlanEdit{Skip: []string{"review"}}}, "", "#1 · skip the review stage?",
			[]string{"now: plan · approve · write · test · review · proof", "after: plan · approve · write · test · proof"}},
		{"on", ItemChange{Edit: factory.PlanEdit{On: []string{"security"}}}, "touches auth", "#1 · add a security stage?",
			[]string{"now: plan · approve · write · test · review · proof", "after: plan · approve · write · test · review · +security · proof", "why: touches auth"}},
		{"add", ItemChange{Edit: factory.PlanEdit{Add: []factory.Stage{{Ask: "after test, read it for auth holes"}}}}, "", "#1 · add a " + security + " stage?",
			[]string{"now: plan · approve · write · test · review · proof", "after: plan · approve · write · test · +" + security + " · review · proof"}},
		// WHERE THE RUN HOLDS FOR THE PERSON IS AN APPROVE STEP, added or
		// skipped like any stage.
		{"approve", ItemChange{Edit: factory.PlanEdit{Add: []factory.Stage{{Ask: "after test, approve"}}}}, "", "#1 · add an approve step?",
			[]string{"now: plan · approve · write · test · review · proof", "after: plan · approve · write · test · +approve2 · review · proof"}},
		{"cap up", ItemChange{Cap: 12.5}, "", "#1 · raise the budget to $12.50?", []string{"budget  $5 → $12.50"}},
		{"cap down", ItemChange{Cap: 3}, "", "#1 · lower the budget to $3?", []string{"budget  $5 → $3"}},
		{"effort", ItemChange{Effort: "strong"}, "", "#1 · think strong?", []string{"thinking  plan — → strong · write — → strong · test — → strong · review — → strong · proof — → strong · security(off) — → strong"}},
		{"note", ItemChange{Note: "the fixture in testdata is flaky"}, "", "#1 · add a note for the stages?",
			[]string{"note: the fixture in testdata is flaky"}},
		{"several", ItemChange{Edit: factory.PlanEdit{Skip: []string{"review"}}, Cap: 8, Note: "keep it small"}, "small change", "#1 · change the plan?",
			[]string{"now: plan · approve · write · test · review · proof", "after: plan · approve · write · test · proof", "budget  $5 → $8", "note: keep it small", "why: small change"}},
	} {
		n := itemCard(t, c.change, c.why)
		if got := ItemHead(n); got != c.head {
			t.Errorf("%s: head = %q, want %q", c.name, got, c.head)
		}
		if got := ItemRows(n); strings.Join(got, "\n") != strings.Join(c.rows, "\n") {
			t.Errorf("%s: rows =\n%s\nwant\n%s", c.name, strings.Join(got, "\n"), strings.Join(c.rows, "\n"))
		}
	}
}

// THE CHANGE ITSELF, APPLIED: each field, the note appended, effort only on
// the conversation stages that have not run, and a fixed recipe refusing a
// stage change WHOLE — no chip changes either.
func TestFactoryItemChangeAppliesEachFieldAndRefusesUnderFixed(t *testing.T) {
	recipe := itemRecipe()
	it := factory.Item{ID: 1, Kind: factory.KindIssue, Cap: 5, Notes: []string{"first"},
		Stream: &factory.Stream{Phases: []factory.Phase{{Name: "plan", State: factory.PhaseDone}}}}
	next, err := ApplyItemChange(it, ItemChange{Edit: factory.PlanEdit{Skip: []string{"review"}}, Cap: 8, Effort: "strong", Note: "second"}, recipe)
	if err != nil {
		t.Fatal(err)
	}
	if next.Cap != 8 {
		t.Errorf("cap = %v", next.Cap)
	}
	if i := factory.StageIndex(next.Stages, "review"); i < 0 || next.Stages[i].On {
		t.Errorf("review still runs: %+v", next.Stages)
	}
	for _, st := range next.Stages {
		want := "strong"
		if st.Name == "plan" || st.Kind == factory.StageGate {
			want = "" // it has run, or is a person: effort means nothing to it
		}
		if st.Effort != want {
			t.Errorf("%s effort = %q, want %q", st.Name, st.Effort, want)
		}
	}
	if strings.Join(next.Notes, "|") != "first|second" {
		t.Errorf("notes = %v", next.Notes)
	}
	// THE SAVED VALUES ARE EACH STEP'S OWN, never one scalar for the job.
	if got := ItemFactsOf(next).Thinking; got["write"] != "strong" || got["plan"] != "" || got["security"] != "strong" {
		t.Errorf("saved thinking = %v", got)
	}
	back, err := ApplyItemChange(next, ItemChange{Effort: "default"}, recipe)
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range ItemFactsOf(back).Thinking {
		if got != "" {
			t.Errorf("default left %s at %q", name, got)
		}
	}

	recipe.Adapt = map[factory.Kind]factory.AdaptMode{factory.KindIssue: factory.AdaptFixed}
	same, err := ApplyItemChange(it, ItemChange{Edit: factory.PlanEdit{Skip: []string{"review"}}, Cap: 8}, recipe)
	if err == nil || !strings.Contains(err.Error(), "is fixed") {
		t.Fatalf("fixed = %v", err)
	}
	if same.Cap != 5 {
		t.Errorf("a refused change still moved the cap to %v", same.Cap)
	}
	// A fixed recipe still lets the person's chips and notes through: only
	// the stages are the recipe's.
	chips, err := ApplyItemChange(it, ItemChange{Cap: 8, Note: "fine"}, recipe)
	if err != nil || chips.Cap != 8 {
		t.Errorf("chips under fixed = %v, %v", chips.Cap, err)
	}
}

// A DROPPED JOB REFUSES EVERY CHANGE, ITS CHIPS AND NOTES AS MUCH AS ITS
// STAGES, with the start's own sentence: its conversations are gone and
// nothing of it runs, so a card would offer a change nobody ever reads. The
// job comes back exactly as it went in.
func TestFactoryItemChangeRefusesADroppedJob(t *testing.T) {
	recipe := itemRecipe()
	it := factory.Item{ID: 1, Num: 12, Kind: factory.KindIssue, Cap: 5, State: factory.StateDismissed, DoneHow: "dropped"}
	want := "#12 " + factory.DroppedRefusal
	for _, change := range []ItemChange{
		{Edit: factory.PlanEdit{Skip: []string{"review"}}},
		{Effort: "strong"},
		{Cap: 8},
		{Note: "the fixture is flaky"},
	} {
		got, err := ApplyItemChange(it, change, recipe)
		if err == nil || err.Error() != want {
			t.Errorf("%+v on a dropped job = %v, want %q", change, err, want)
		}
		if got.Cap != 5 || len(got.Notes) != 0 || len(got.Stages) != 0 {
			t.Errorf("%+v changed the dropped job: %+v", change, got)
		}
	}
}

func TestFactoryItemCardAsksWithItsHeadAndTwoAnswers(t *testing.T) {
	door := newFakeItemDoor()
	agent := newItemAgent(t, door, 0)
	questions, stop := agent.WatchQuestions()
	t.Cleanup(stop)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	results := runFactoryItem(t, agent, ctx, itemArgs)

	q := awaitItemQuestion(t, questions)
	if q.Head != "#1 · raise the budget to $8?" {
		t.Errorf("head = %q", q.Head)
	}
	if q.Reason != "budget  $5 → $8; why: the stages need room for a second review" {
		t.Errorf("reason = %q", q.Reason)
	}
	if !q.Deadline.IsZero() || q.Pick != nil {
		t.Error("the item card carries a clock or a pick; silence must change nothing")
	}
	if len(q.Options) != 2 || q.Options[0].Key != "1" || q.Options[0].Label != "yes" ||
		q.Options[1].Key != "2" || q.Options[1].Label != "keep it" || !q.Options[1].Safe {
		t.Errorf("options = %+v", q.Options)
	}
	if q.Input.Kind != InputText || q.Input.Prompt != "say what to change… (enter sends it)" {
		t.Errorf("input = %+v", q.Input)
	}
	if err := q.Check(nil); err != nil {
		t.Errorf("the card is not a well-formed question: %v", err)
	}
	if len(door.changes()) != 0 {
		t.Fatal("the item changed before the person answered")
	}
	cancel()
	if out := awaitResult(t, results); out != "nothing changed: the card was never answered" {
		t.Errorf("interrupted result = %q", out)
	}
}

func TestFactoryItemYesAppliesTheChangeOnce(t *testing.T) {
	door := newFakeItemDoor()
	agent := newItemAgent(t, door, 0)
	questions, stopQ := agent.WatchQuestions()
	t.Cleanup(stopQ)
	lane, stopT := agent.WatchTaskUpdates()
	t.Cleanup(stopT)
	results := runFactoryItem(t, agent, context.Background(), `{"item":1,"skip":["review"],"cap":8,"note":"the fixture is flaky"}`)
	q := awaitItemQuestion(t, questions)
	if err := agent.ResolveQuestion(Answer{Kind: QuestionItem, Ref: q.Ref, Key: "1"}); err != nil {
		t.Fatal(err)
	}
	want := "#1 now: plan · approve · write · test · proof · budget $8\n" +
		"The person said yes and the factory has the change; there is nothing left to answer. The note is kept on the item for its stages."
	if out := awaitResult(t, results); out != want {
		t.Errorf("result = %q, want %q", out, want)
	}
	if got := door.changes(); len(got) != 1 || got[0].Cap != 8 || got[0].Note != "the fixture is flaky" {
		t.Fatalf("applied = %+v", got)
	}
	if strings.Join(door.item.Notes, "|") != "the fixture is flaky" {
		t.Errorf("the item's notes = %v", door.item.Notes)
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event := <-lane:
			if event.Kind == EventItemChanged {
				n := event.FactoryItem
				if n == nil || n.ID != q.Ref || n.Now == nil || n.Now.Cap != 8 || strings.Join(n.After.Stages, " ") != "plan approve write test proof" {
					t.Fatalf("changed news = %+v", n)
				}
				return
			}
		case <-deadline:
			t.Fatal("EventItemChanged never reached the task lane")
		}
	}
}

func TestFactoryItemNoWordsSilenceAndARefusalChangeNothing(t *testing.T) {
	door := newFakeItemDoor()
	agent := newItemAgent(t, door, 0)
	questions, stop := agent.WatchQuestions()
	t.Cleanup(stop)

	results := runFactoryItem(t, agent, context.Background(), itemArgs)
	q := awaitItemQuestion(t, questions)
	if err := agent.ResolveQuestion(Answer{Kind: QuestionItem, Ref: q.Ref, Key: "2"}); err != nil {
		t.Fatal(err)
	}
	if out := awaitResult(t, results); out != "nothing changed: the person said no." {
		t.Errorf("no = %q", out)
	}

	results = runFactoryItem(t, agent, context.Background(), itemArgs)
	q = awaitItemQuestion(t, questions)
	if err := agent.ResolveQuestion(Answer{Kind: QuestionItem, Ref: q.Ref, Change: "make it $10"}); err != nil {
		t.Fatal(err)
	}
	want := "the person changed it: make it $10\nNothing changed. Propose it again with that"
	if out := awaitResult(t, results); out != want {
		t.Errorf("change = %q, want %q", out, want)
	}

	quiet := newItemAgent(t, door, 20*time.Millisecond)
	if out := awaitResult(t, runFactoryItem(t, quiet, context.Background(), itemArgs)); out != "nothing changed: the card was never answered" {
		t.Errorf("window = %q", out)
	}
	for _, q := range quiet.OpenQuestions() {
		if q.Kind == QuestionItem {
			t.Error("the card is still open after its window ended")
		}
	}
	if n := len(door.changes()); n != 0 {
		t.Errorf("%d changes applied on no, words or silence", n)
	}

	refusing := newFakeItemDoor()
	refusing.refuse = errors.New("plan may not skip review, which has already run")
	strict := newItemAgent(t, refusing, 0)
	sq, stopS := strict.WatchQuestions()
	t.Cleanup(stopS)
	results = runFactoryItem(t, strict, context.Background(), itemArgs)
	q = awaitItemQuestion(t, sq)
	if err := strict.ResolveQuestion(Answer{Kind: QuestionItem, Ref: q.Ref, Key: "1"}); err != nil {
		t.Fatal(err)
	}
	if out := awaitResult(t, results); out != "nothing changed: plan may not skip review, which has already run" {
		t.Errorf("refused yes = %q", out)
	}
}

func TestFactoryItemAnswerKeysAndQuestionAsked(t *testing.T) {
	yes, ok := AnswerFromKey(QuestionItem, "1")
	if !ok || !yes.Item.Approved {
		t.Errorf("1 = %+v, %v", yes, ok)
	}
	no, ok := AnswerFromKey(QuestionItem, "2")
	if !ok || no.Item.Approved {
		t.Errorf("2 = %+v, %v", no, ok)
	}
	if got := questionGoneReason(Question{Kind: QuestionItem}); got != "nothing changed on the item" {
		t.Errorf("gone reason = %q", got)
	}
	key, ok := questionAsked(Event{Kind: EventItemProposal, FactoryItem: &ItemNotice{ID: "g1"}})
	if !ok || key != questionToken(QuestionItem, "g1") {
		t.Errorf("questionAsked = %q, %v", key, ok)
	}
	if _, ok := questionAsked(Event{Kind: EventItemProposal, FactoryItem: &ItemNotice{ID: "g1", Withdrawn: "x"}}); ok {
		t.Error("a withdrawn card still reads as asking")
	}
}

// A WINDOW THAT OPENS WHILE AN ITEM CARD STANDS IS HANDED IT, and a settled
// card is never replayed as standing.
func TestFactoryItemCardIsReplayedToALateTaskLane(t *testing.T) {
	agent := newItemAgent(t, newFakeItemDoor(), 0)
	questions, stopQ := agent.WatchQuestions()
	t.Cleanup(stopQ)
	results := runFactoryItem(t, agent, context.Background(), itemArgs)
	q := awaitItemQuestion(t, questions)

	late, stopLate := agent.WatchTaskUpdates()
	t.Cleanup(stopLate)
	select {
	case event := <-late:
		n := event.FactoryItem
		if event.Kind != EventItemProposal || n == nil || n.ID != q.Ref || n.Decided != nil || n.Before.Cap != 5 || n.After.Cap != 8 {
			t.Fatalf("the late lane was handed %v / %+v, want the standing card", event.Kind, n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a lane opened while the card stood was not handed it")
	}
	agent.ResolveItem(q.Ref, ItemAnswer{})
	awaitResult(t, results)
	agent.mu.Lock()
	standing := agent.standingItemCardsLocked()
	agent.mu.Unlock()
	if len(standing) != 0 {
		t.Fatalf("a settled card is still replayed as standing: %+v", standing)
	}
}

// readingFactoryDoor is a floor door that can read back what it wrote, as the
// store can.
type readingFactoryDoor struct{ fakeFactoryDoor }

func (d *readingFactoryDoor) Get(id int) (factory.Item, error) {
	return factory.Item{ID: id, Title: "retry the flaky upload test", Repo: "web", State: factory.StateNew}, nil
}

// THE ADDED NEWS CARRIES THE ITEM AS THE FLOOR WROTE IT when the door can read
// it back, so a window with no floor of its own draws the live card from it.
func TestFactoryAddedNewsCarriesTheItem(t *testing.T) {
	door := &readingFactoryDoor{fakeFactoryDoor{id: 41}}
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
	awaitResult(t, results)
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event := <-lane:
			if event.Kind == EventFactoryAdded {
				if event.Factory == nil || event.Factory.Now == nil || event.Factory.Now.ID != 41 {
					t.Fatalf("the added news = %+v", event.Factory)
				}
				return
			}
		case <-deadline:
			t.Fatal("EventFactoryAdded never reached the task lane")
		}
	}
}

// THE CARD AND THE TOOL SPEAK THE FLOOR'S WORDS (`budget`, `thinking`), while
// the schema keeps its field names (cap, effort) and tells the model which
// word the person uses for each. `ask me at` is gone: where the run holds for
// the person is an approve step among the stages.
func TestFactoryItemCardUsesTheFloorsWords(t *testing.T) {
	rows := strings.Join(ItemRows(ItemNotice{Before: ItemFacts{Cap: 5}, After: ItemFacts{Cap: 8, Effort: "strong"}}), "\n")
	for _, want := range []string{"budget  $5 → $8", "thinking  — → strong"} {
		if !strings.Contains(rows, want) {
			t.Errorf("rows lack %q:\n%s", want, rows)
		}
	}
	for _, old := range []string{"plan first", "sign-off", "sign off", "gate  ", "cap  ", "effort  ", "ask me at"} {
		if strings.Contains(rows, old) {
			t.Errorf("the card still says %q", old)
		}
	}
	schema := factoryItemSchemaJSON()
	for _, field := range []string{`"cap"`, `"effort"`} {
		if !strings.Contains(schema, field) {
			t.Errorf("the schema lost the field %s", field)
		}
	}
	if strings.Contains(schema, `"gate"`) || strings.Contains(schema, "ask me at") || strings.Contains(factoryItemDescription, "ask me at") {
		t.Error("the tool still offers ask me at")
	}
	if !strings.Contains(factoryItemDescription, "approve step") {
		t.Error("the description does not tell the model where the run holds for the person")
	}
	for _, word := range []string{"budget", "thinking"} {
		if !strings.Contains(factoryItemDescription, `"`+word+`"`) {
			t.Errorf("the description does not tell the model the person's word %q", word)
		}
		if !strings.Contains(schema, `\"`+word+`\"`) {
			t.Errorf("the schema does not tell the model the person's word %q", word)
		}
	}
}

// itemWithPhases is an [itemRecipe] item whose stream holds exactly the phases given.
func itemWithPhases(phases ...factory.Phase) factory.Item {
	return factory.Item{ID: 1, Kind: factory.KindIssue, Cap: 5, Notes: []string{"first"}, Stream: &factory.Stream{Phases: phases}}
}

// THINKING IS THE SHARED EDIT'S, SO ITS REFUSALS HOLD: a refused change comes
// back as it went in, the cap and the note included.
func TestItemThinkingRefusalLeavesTheItemAsItWas(t *testing.T) {
	recipe := itemRecipe()
	landed := itemWithPhases()
	landed.State = factory.StateLanded
	proofRan := itemWithPhases(
		factory.Phase{Name: "plan", State: factory.PhaseDone},
		factory.Phase{Name: "write", State: factory.PhaseDone},
		factory.Phase{Name: "test", State: factory.PhaseDone},
		factory.Phase{Name: "review", State: factory.PhaseDone},
		factory.Phase{Name: "proof", State: factory.PhaseDone})
	for _, c := range []struct {
		name string
		it   factory.Item
		want string
	}{
		{"landed", landed, "has landed"},
		{"proof started", proofRan, factory.ProofFrozenRefusal},
	} {
		staged := ItemStaged(c.it, recipe)
		got, err := ApplyItemChange(c.it, ItemChange{Effort: "strong", Cap: 9, Note: "ignored"}, recipe)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
		if !reflect.DeepEqual(got, staged) {
			t.Errorf("%s: a refused change moved the item:\n%+v\nwas\n%+v", c.name, got, staged)
		}
	}
}

// A change that moves no thinking stays a no-op even on a landed job.
func TestItemThinkingNoOpStaysANoOpWhenLanded(t *testing.T) {
	recipe := itemRecipe()
	it := itemWithPhases()
	it.State = factory.StateLanded
	got, err := ApplyItemChange(it, ItemChange{Effort: "default"}, recipe)
	if err != nil {
		t.Fatalf("a no-op thinking change was refused: %v", err)
	}
	if !reflect.DeepEqual(got.Stages, ItemStaged(it, recipe).Stages) {
		t.Error("a no-op thinking change moved a stage")
	}
}

// A fixed stage keeps its own thinking; the rest of the card goes through.
func TestItemThinkingLeavesAFixedStageAlone(t *testing.T) {
	recipe := itemRecipe()
	it := ItemStaged(itemWithPhases(), recipe)
	it.Stages = factory.CopyStages(it.Stages)
	i := factory.StageIndex(it.Stages, "review")
	it.Stages[i].Fixed = true
	it.Stages[i].Effort = "cheap"
	next, err := ApplyItemChange(it, ItemChange{Effort: "strong"}, recipe)
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range next.Stages {
		switch {
		case st.Name == "review" && st.Effort != "cheap":
			t.Errorf("fixed review now thinks %q", st.Effort)
		case st.Name == "write" && st.Effort != "strong":
			t.Errorf("write thinks %q, want strong", st.Effort)
		}
	}
}

// A phase a rerun rewound to pending has still begun, as the shared Edit counts it.
func TestItemThinkingSkipsAPendingPhaseWithARound(t *testing.T) {
	recipe := itemRecipe()
	it := itemWithPhases(factory.Phase{Name: "plan", State: factory.PhaseDone},
		factory.Phase{Name: "write", State: factory.PhasePending, Round: 1})
	next, err := ApplyItemChange(it, ItemChange{Effort: "strong"}, recipe)
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range next.Stages {
		if st.Name == "write" && st.Effort != "" {
			t.Errorf("a stage with a round behind it was changed to %q", st.Effort)
		}
	}
	got := ItemFactsOf(next).Thinking
	if got["write"] != "" || got["test"] != "strong" || got["review"] != "strong" {
		t.Errorf("saved thinking = %v, want write untouched and the rest strong", got)
	}
}

// A bad level is refused before the edit, the cap or the note is looked at.
func TestItemThinkingBadLevelRefusesWholeChange(t *testing.T) {
	recipe := itemRecipe()
	it := itemWithPhases()
	got, err := ApplyItemChange(it, ItemChange{Edit: factory.PlanEdit{Skip: []string{"review"}}, Effort: "wild", Cap: 9, Note: "x"}, recipe)
	if err == nil || !strings.Contains(err.Error(), "not a thinking level") {
		t.Fatalf("err = %v", err)
	}
	if !reflect.DeepEqual(got, ItemStaged(it, recipe)) {
		t.Error("a bad level moved the item")
	}
}

// fixedPlanDoor is a door whose plan is FIXED at cheap, the first stage the
// old scalar read, so a card that says the job thinks cheap is the lie.
func fixedPlanDoor() *fakeItemDoor {
	door := newFakeItemDoor()
	door.item = ItemStaged(door.item, door.recipe)
	door.item.Stages = factory.CopyStages(door.item.Stages)
	plan := factory.StageIndex(door.item.Stages, "plan")
	door.item.Stages[plan].Fixed = true
	door.item.Stages[plan].Effort = "cheap"
	return door
}

// yesItem raises the card for args against door, answers yes, and returns what
// the model is told.
func yesItem(t *testing.T, door *fakeItemDoor, args string) string {
	t.Helper()
	agent := newItemAgent(t, door, 0)
	questions, stopQ := agent.WatchQuestions()
	t.Cleanup(stopQ)
	results := runFactoryItem(t, agent, context.Background(), args)
	q := awaitItemQuestion(t, questions)
	if err := agent.ResolveQuestion(Answer{Kind: QuestionItem, Ref: q.Ref, Key: "1"}); err != nil {
		t.Fatal(err)
	}
	return awaitResult(t, results)
}

// savedThinking is each conversation step's own thinking as the door holds it.
func savedThinking(door *fakeItemDoor) map[string]string {
	door.mu.Lock()
	defer door.mu.Unlock()
	return ItemFactsOf(door.item).Thinking
}

// A FIXED FIRST STAGE MUST NOT HIDE THE REST, AND MUST NOT SPEAK FOR THE JOB:
// with plan fixed at cheap, "think strong" moves the eligible stages, the card
// names them, and the yes receipt the model relays says the same, with the
// fixed step left as it was and no "thinking cheap" for the job.
func TestFactoryItemFixedFirstStageStillRaisesTheThinkingCard(t *testing.T) {
	door := fixedPlanDoor()
	agent := newItemAgent(t, door, 0)
	questions, stopQ := agent.WatchQuestions()
	t.Cleanup(stopQ)
	results := runFactoryItem(t, agent, context.Background(), `{"item":1,"effort":"strong"}`)
	q := awaitItemQuestion(t, questions)
	if q.Head != "#1"+DecisionSep+"think strong?" {
		t.Errorf("head = %q", q.Head)
	}
	if !strings.Contains(q.Reason, "write — → strong") || strings.Contains(q.Reason, "plan") {
		t.Errorf("rows do not name the steps that change: %q", q.Reason)
	}
	if err := agent.ResolveQuestion(Answer{Kind: QuestionItem, Ref: q.Ref, Key: "1"}); err != nil {
		t.Fatal(err)
	}
	out := awaitResult(t, results)
	for _, want := range []string{"thinking write strong", "security(off) strong", "left as they were: plan cheap"} {
		if !strings.Contains(out, want) {
			t.Errorf("the receipt lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "thinking cheap") || strings.Contains(out, "thinking strong") {
		t.Errorf("the receipt says one thinking for a job whose steps differ:\n%s", out)
	}
	want := map[string]string{"plan": "cheap", "write": "strong", "test": "strong", "review": "strong", "proof": "strong", "security": "strong"}
	if got := savedThinking(door); !reflect.DeepEqual(got, want) {
		t.Errorf("saved = %v, want %v", got, want)
	}
	if st := door.item.Stages[factory.StageIndex(door.item.Stages, "security")]; st.On {
		t.Error("the yes switched security on")
	}
}

// THE RECEIPT IS THE SAVED ITEM'S, NOT THE PREVIEW'S: a step that began between
// the card and the yes keeps its thinking, the yes does not claim it moved, and
// the receipt says it was left.
func TestFactoryItemReceiptIsWhatWasSavedNotWhatWasPreviewed(t *testing.T) {
	door := newFakeItemDoor()
	agent := newItemAgent(t, door, 0)
	questions, stopQ := agent.WatchQuestions()
	t.Cleanup(stopQ)
	results := runFactoryItem(t, agent, context.Background(), `{"item":1,"effort":"strong"}`)
	q := awaitItemQuestion(t, questions)
	if !strings.Contains(q.Reason, "write — → strong") {
		t.Fatalf("the card does not offer write: %q", q.Reason)
	}
	door.mu.Lock()
	door.item.Stream = &factory.Stream{Phases: []factory.Phase{{Name: "plan", State: factory.PhaseDone}, {Name: "write", State: factory.PhaseRunning}}}
	door.mu.Unlock()
	if err := agent.ResolveQuestion(Answer{Kind: QuestionItem, Ref: q.Ref, Key: "1"}); err != nil {
		t.Fatal(err)
	}
	out := awaitResult(t, results)
	if !strings.Contains(out, "left as they were: plan usual, write usual") {
		t.Errorf("the receipt does not name the steps that began:\n%s", out)
	}
	if strings.Contains(out, "write strong") || !strings.Contains(out, "test strong") {
		t.Errorf("the receipt claims what the preview offered rather than what was saved:\n%s", out)
	}
	if got := savedThinking(door); got["write"] != "" || got["test"] != "strong" {
		t.Errorf("saved = %v", got)
	}
}

// A BUDGET OR A NOTE IS NOT A THINKING CHANGE: the yes receipt of a card that
// asked for neither says nothing about thinking, however the steps think.
func TestFactoryItemCapOrNoteOnlyReceiptNeverSpeaksOfThinking(t *testing.T) {
	for _, c := range []struct{ name, args string }{
		{"cap", `{"item":1,"cap":9}`},
		{"note", `{"item":1,"note":"the fixture is flaky"}`},
		{"skip", `{"item":1,"skip":["review"]}`},
	} {
		door := fixedPlanDoor()
		for i := range door.item.Stages {
			if itemChatStage(door.item.Stages[i]) && door.item.Stages[i].Name != "plan" {
				door.item.Stages[i].Effort = "strong"
			}
		}
		out := yesItem(t, door, c.args)
		if strings.Contains(out, "thinking") || strings.Contains(out, "left as they were") {
			t.Errorf("%s: a change that asked for no thinking speaks of it:\n%s", c.name, out)
		}
	}
}

// A stage after proof refuses the whole card through the shared edit; the item
// keeps its cap and note.
func TestItemThinkingAConversationStageAfterProofRefusesTheWholeCard(t *testing.T) {
	recipe := itemRecipe()
	it := ItemStaged(itemWithPhases(), recipe)
	it.Stages = append(factory.CopyStages(it.Stages), factory.ParseStage("announce"))
	got, err := ApplyItemChange(it, ItemChange{Effort: "strong", Cap: 9, Note: "x"}, recipe)
	if err == nil {
		t.Fatal("a thinking change reaching a stage after proof was not refused")
	}
	if got.Cap != it.Cap || len(got.Notes) != len(it.Notes) {
		t.Error("a refused card moved the cap or the note")
	}
}

// offSecurityDoor is a door whose item thinks strong on every stage except an
// OFF security stage at cheap: the one place "think strong" has something to do.
func offSecurityDoor(t *testing.T) *fakeItemDoor {
	t.Helper()
	door := newFakeItemDoor()
	door.item = ItemStaged(door.item, door.recipe)
	door.item.Stages = factory.CopyStages(door.item.Stages)
	for i := range door.item.Stages {
		if st := &door.item.Stages[i]; itemChatStage(*st) {
			st.Effort = "strong"
			if st.Name == "security" {
				st.Effort = "cheap"
			}
		}
	}
	return door
}

func offSecurityCard(t *testing.T, door *fakeItemDoor, change ItemChange) *ItemNotice {
	t.Helper()
	before, after, err := door.Preview(context.Background(), 1, change)
	if err != nil {
		t.Fatal(err)
	}
	return &ItemNotice{Item: 1, Ref: before.Ref(), Before: ItemFactsOf(before), After: ItemFactsOf(after)}
}

// A CHANGE THAT MOVES ONLY A STAGE THAT IS OFF IS STILL A CHANGE: the card
// names it, marked off, a yes saves it while it stays off, and the receipt
// says so without calling the job strong.
func TestFactoryItemOffStageThinkingRaisesTheCardAndStaysOff(t *testing.T) {
	door := offSecurityDoor(t)
	n := offSecurityCard(t, door, ItemChange{Effort: "strong"})
	rows := strings.Join(ItemRows(*n), "\n")
	if rows != "thinking  security(off) cheap → strong" {
		t.Errorf("rows = %q", rows)
	}
	if head := ItemHead(*n); head != "#1"+DecisionSep+"think strong?" {
		t.Errorf("head = %q", head)
	}
	out := yesItem(t, door, `{"item":1,"effort":"strong"}`)
	if strings.Contains(out, itemAlready) {
		t.Errorf("an off-stage change was called a no-op: %q", out)
	}
	if !strings.Contains(out, "thinking security(off) strong") || strings.Contains(out, "left as they were") {
		t.Errorf("the receipt = %q", out)
	}
	st := door.item.Stages[factory.StageIndex(door.item.Stages, "security")]
	if st.Effort != "strong" || st.On {
		t.Errorf("security = effort %q on %v, want strong and off", st.Effort, st.On)
	}
	for name, got := range savedThinking(door) {
		if got != "strong" {
			t.Errorf("%s = %q, want strong", name, got)
		}
	}
}

// An unrelated cap does not hide the off stage: the card says both, and so does
// the receipt.
func TestFactoryItemCapAndEffortPreviewNamesTheOffStage(t *testing.T) {
	door := offSecurityDoor(t)
	n := offSecurityCard(t, door, ItemChange{Effort: "strong", Cap: 9})
	rows := strings.Join(ItemRows(*n), "\n")
	if !strings.Contains(rows, "budget  $5 → $9") || !strings.Contains(rows, "security(off) cheap → strong") {
		t.Errorf("rows = %q", rows)
	}
	if head := ItemHead(*n); head != "#1"+DecisionSep+"change the plan?" {
		t.Errorf("head = %q", head)
	}
	out := yesItem(t, door, `{"item":1,"effort":"strong","cap":9}`)
	if !strings.Contains(out, "budget $9") || !strings.Contains(out, "thinking security(off) strong") {
		t.Errorf("receipt = %q", out)
	}
}

// Default resets the off stage too, and the receipt says "usual" in words where
// the card draws its dash; a change nothing moves stays a no-op.
func TestFactoryItemOffStageDefaultResetAndNoOp(t *testing.T) {
	door := offSecurityDoor(t)
	n := offSecurityCard(t, door, ItemChange{Effort: "default"})
	if rows := strings.Join(ItemRows(*n), "\n"); rows != "thinking  plan strong → — · write strong → — · test strong → — · review strong → — · proof strong → — · security(off) cheap → —" {
		t.Errorf("default reset rows = %q", rows)
	}
	if head := ItemHead(*n); head != "#1"+DecisionSep+"think the usual amount?" {
		t.Errorf("head = %q", head)
	}
	out := yesItem(t, door, `{"item":1,"effort":"default"}`)
	if !strings.Contains(out, "thinking plan usual, write usual, test usual, review usual, proof usual, security(off) usual") || strings.Contains(out, "—") {
		t.Errorf("default receipt = %q", out)
	}
	for name, got := range savedThinking(door) {
		if got != "" {
			t.Errorf("%s = %q after default", name, got)
		}
	}

	door = offSecurityDoor(t)
	door.item.Stages[factory.StageIndex(door.item.Stages, "security")].Effort = "strong"
	n = offSecurityCard(t, door, ItemChange{Effort: "strong"})
	if itemEffortMoved(*n) || len(ItemRows(*n)) != 0 {
		t.Errorf("a change nothing moves was seen as moving: %v", ItemRows(*n))
	}
	agent := newItemAgent(t, door, 0)
	if out := awaitResult(t, runFactoryItem(t, agent, context.Background(), `{"item":1,"effort":"strong"}`)); out != itemAlready {
		t.Errorf("no-op = %q", out)
	}
}

// ONLY OFF STEPS CHANGE UNDER A CAP: a cap-and-off-only card ends with the cap
// and the off step, nothing about the steps that stayed.
func TestFactoryItemUsualResetOfAlreadyUsualIsANoOp(t *testing.T) {
	door := newFakeItemDoor()
	agent := newItemAgent(t, door, 0)
	if out := awaitResult(t, runFactoryItem(t, agent, context.Background(), `{"item":1,"effort":"default"}`)); out != itemAlready {
		t.Errorf("resetting a job that already thinks the usual amount = %q", out)
	}
}

// ONE TABLE PINS HEAD, ROWS AND RECEIPT IN LOCKSTEP for every shape a thinking
// change takes, so a fourth surface cannot drift from the first three.
func TestFactoryItemHeadRowsAndReceiptAgreeOnEveryShape(t *testing.T) {
	// withoutSecurity is the recipe's issue stages with no off stage, the one
	// shape where every conversation step is on and a change can be uniform.
	withoutSecurity := func(d *fakeItemDoor) {
		var keep []factory.Stage
		for _, st := range d.item.Stages {
			if st.Name != "security" {
				keep = append(keep, st)
			}
		}
		d.item.Stages = keep
	}
	allStrong := func(d *fakeItemDoor) {
		for i := range d.item.Stages {
			if itemChatStage(d.item.Stages[i]) {
				d.item.Stages[i].Effort = "strong"
			}
		}
	}
	for _, c := range []struct {
		name    string
		seed    func(*fakeItemDoor)
		args    string
		head    string
		rows    string
		receipt string // the thinking part of the first line, exactly
	}{
		{"uniform", withoutSecurity, `{"item":1,"effort":"strong"}`,
			"think strong?", "thinking  — → strong", "thinking strong"},
		{"uniform reset", func(d *fakeItemDoor) { withoutSecurity(d); allStrong(d) }, `{"item":1,"effort":"default"}`,
			"think the usual amount?", "thinking  strong → —", "thinking usual"},
		{"fixed plan", func(d *fakeItemDoor) {
			d.item.Stages[factory.StageIndex(d.item.Stages, "plan")].Fixed = true
			d.item.Stages[factory.StageIndex(d.item.Stages, "plan")].Effort = "cheap"
		}, `{"item":1,"effort":"strong"}`,
			"think strong?", "thinking  write — → strong · test — → strong · review — → strong · proof — → strong · security(off) — → strong",
			"thinking write strong, test strong, review strong, proof strong, security(off) strong · left as they were: plan cheap"},
		{"began", func(d *fakeItemDoor) {
			d.item.Stream = &factory.Stream{Phases: []factory.Phase{{Name: "plan", State: factory.PhaseDone}, {Name: "write", State: factory.PhasePending, Round: 1}}}
		}, `{"item":1,"effort":"strong"}`,
			"think strong?", "thinking  test — → strong · review — → strong · proof — → strong · security(off) — → strong",
			"thinking test strong, review strong, proof strong, security(off) strong · left as they were: plan usual, write usual"},
		{"off only", func(d *fakeItemDoor) {
			allStrong(d)
			d.item.Stages[factory.StageIndex(d.item.Stages, "security")].Effort = "cheap"
		}, `{"item":1,"effort":"strong"}`,
			"think strong?", "thinking  security(off) cheap → strong", "thinking security(off) strong"},
		{"mixed targets", func(d *fakeItemDoor) {
			d.item.Stages[factory.StageIndex(d.item.Stages, "write")].Effort = "cheap"
			d.item.Stages[factory.StageIndex(d.item.Stages, "test")].Effort = "strong"
		}, `{"item":1,"effort":"strong"}`,
			"think strong?", "thinking  plan — → strong · write cheap → strong · review — → strong · proof — → strong · security(off) — → strong",
			"thinking plan strong, write strong, review strong, proof strong, security(off) strong"},
	} {
		door := newFakeItemDoor()
		door.item = ItemStaged(door.item, door.recipe)
		door.item.Stages = factory.CopyStages(door.item.Stages)
		c.seed(door)
		var args struct{ Effort string }
		_ = json.Unmarshal([]byte(c.args), &args)
		n := offSecurityCard(t, door, ItemChange{Effort: args.Effort})
		n.Effort = args.Effort
		if got := ItemHead(*n); got != "#1"+DecisionSep+c.head {
			t.Errorf("%s: head = %q, want %q", c.name, got, c.head)
		}
		if got := strings.Join(ItemRows(*n), "\n"); got != c.rows {
			t.Errorf("%s: rows = %q, want %q", c.name, got, c.rows)
		}
		first, _, _ := strings.Cut(yesItem(t, door, c.args), "\n")
		if !strings.HasSuffix(first, " · "+c.receipt) {
			t.Errorf("%s: receipt = %q, want it to end %q", c.name, first, c.receipt)
		}
	}
}

// A NOTICE PERSISTED OR REPLAYED THROUGH JSON DRAWS THE SAME CARD AND RECEIPT:
// the per-step facts survive the trip, and a notice from before them (no map)
// keeps its scalar words and carries no new key.
func TestFactoryItemNoticeJSONReplayKeepsHeadRowsAndReceipt(t *testing.T) {
	door := fixedPlanDoor()
	n := offSecurityCard(t, door, ItemChange{Effort: "strong"})
	n.Effort = "strong"
	raw, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	var back ItemNotice
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if ItemHead(back) != ItemHead(*n) || strings.Join(ItemRows(back), "|") != strings.Join(ItemRows(*n), "|") || itemNow(back) != itemNow(*n) {
		t.Errorf("a replayed notice draws differently:\n%s\n%s", itemNow(back), itemNow(*n))
	}
	if !strings.Contains(string(raw), `"Thinking"`) || back.After.Effort != "cheap" {
		t.Errorf("the new notice lost its per-step facts or its legacy scalar: %s", raw)
	}

	legacy := ItemNotice{Item: 1, Effort: "strong", Before: ItemFacts{Stages: []string{"plan"}, Cap: 5}, After: ItemFacts{Stages: []string{"plan"}, Cap: 5, Effort: "strong"}}
	raw, err = json.Marshal(legacy)
	if err != nil || strings.Contains(string(raw), "Thinking") {
		t.Fatalf("a notice without per-step facts grew a key: %s, %v", raw, err)
	}
	back = ItemNotice{}
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if got := ItemHead(back); got != "#1"+DecisionSep+"think strong?" {
		t.Errorf("legacy head = %q", got)
	}
	if got := strings.Join(ItemRows(back), "|"); got != "thinking  — → strong" {
		t.Errorf("legacy rows = %q", got)
	}
	if got := itemNow(back); !strings.Contains(got, " · thinking strong\n") {
		t.Errorf("legacy receipt = %q", got)
	}
	legacy.Effort = ""
	if got := itemNow(legacy); strings.Contains(got, "thinking") {
		t.Errorf("a legacy receipt of a change that asked for no thinking says %q", got)
	}
}

// THE SCHEMA AND THE DESCRIPTIONS PROMISE ONLY WHAT THE WRITER DOES: not every
// step, and not a fixed or begun one.
func TestFactoryItemEffortWordsDoNotPromiseEveryStep(t *testing.T) {
	schema := factoryItemSchemaJSON()
	for _, want := range []string{"have not begun", "fixed", "after proof"} {
		if !strings.Contains(schema, want) {
			t.Errorf("the effort description omits %q:\n%s", want, schema)
		}
	}
	if strings.Contains(schema, "How hard the stages that have not run yet think") {
		t.Error("the effort description still says the stages that have not run, with no limit")
	}
}

// legacyNamedItem is an item document as an older build could have stored it,
// read back through the same decoder the store uses: the first conversation
// stages keep the names given, byte for byte.
func legacyNamedItem(t *testing.T, names ...string) factory.Item {
	t.Helper()
	base := factory.Item{ID: 1, Kind: factory.KindIssue, Cap: 5, Notes: []string{"first"}}
	for i := range names {
		base.Stages = append(base.Stages, factory.Stage{Name: "s" + string(rune('a'+i)), Kind: factory.StageChat, Ask: "do it", On: true, Until: factory.UntilDone})
	}
	raw, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	for i, n := range names {
		raw = []byte(strings.Replace(string(raw), `"Name":"s`+string(rune('a'+i))+`"`, `"Name":"`+n+`"`, 1))
	}
	var it factory.Item
	if err := json.Unmarshal(raw, &it); err != nil {
		t.Fatal(err)
	}
	return it
}

// A LEGACY `Tidy` SURVIVES THE STORE AND TAKES THE PERSON'S THINKING, under its
// own spelling: the name lookup is without case, the stored bytes are not touched.
func TestItemThinkingReachesALegacyCasedStage(t *testing.T) {
	recipe := itemRecipe()
	it := legacyNamedItem(t, "plan", "Tidy")
	if it.Stages[1].Name != "Tidy" {
		t.Fatalf("the store renamed the stage: %+v", it.Stages)
	}
	next, err := ApplyItemChange(it, ItemChange{Effort: "strong", Cap: 9, Note: "sweep"}, recipe)
	if err != nil {
		t.Fatal(err)
	}
	if next.Stages[0].Effort != "strong" || next.Stages[1].Effort != "strong" || next.Stages[1].Name != "Tidy" {
		t.Fatalf("stages = %+v", next.Stages)
	}
	if next.Cap != 9 || len(next.Notes) != 2 {
		t.Fatalf("cap %v notes %v", next.Cap, next.Notes)
	}
	if got := ItemFactsOf(next).Thinking["Tidy"]; got != "strong" {
		t.Fatalf("the card reads Tidy as %q", got)
	}
}

// TWO STAGES OF ONE NAME REFUSE THE WHOLE CARD, cap and note included, for every
// mix of efforts and spellings, rather than setting one and saying all.
func TestItemThinkingRefusesAnAmbiguousStageNameWholly(t *testing.T) {
	recipe := itemRecipe()
	for _, names := range [][]string{{"tidy", "tidy"}, {"Tidy", "tidy"}} {
		for _, efforts := range [][2]string{{"", ""}, {"cheap", "strong"}, {"strong", "strong"}} {
			it := legacyNamedItem(t, append([]string{"plan"}, names...)...)
			it.Stages[1].Effort, it.Stages[2].Effort = efforts[0], efforts[1]
			staged := ItemStaged(it, recipe)
			got, err := ApplyItemChange(it, ItemChange{Effort: "strong", Cap: 9, Note: "ignored"}, recipe)
			if efforts == [2]string{"strong", "strong"} && err == nil {
				// Only plan moves, and neither tidy is asked for.
				if got.Stages[0].Effort != "strong" {
					t.Fatalf("%v %v: plan = %+v", names, efforts, got.Stages[0])
				}
				continue
			}
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), "more than one stage is named tidy") {
				t.Fatalf("%v %v: err = %v", names, efforts, err)
			}
			if !reflect.DeepEqual(got, staged) {
				t.Fatalf("%v %v: a refused card moved the item:\n%+v\nwas\n%+v", names, efforts, got, staged)
			}
		}
	}
}

// The shared edit's own verbs refuse the same ambiguity, and a legacy name keeps
// every bound the lowercase name has.
func TestItemThinkingKeepsEveryBoundOnALegacyName(t *testing.T) {
	recipe := itemRecipe()
	dup := legacyNamedItem(t, "plan", "Tidy", "tidy")
	for kind, edit := range map[string]factory.PlanEdit{
		"skip": {Skip: []string{"tidy"}}, "on": {On: []string{"Tidy"}},
	} {
		if _, err := ApplyItemChange(dup, ItemChange{Edit: edit}, recipe); err == nil || !strings.Contains(err.Error(), "more than one stage is named") {
			t.Errorf("%s of an ambiguous name = %v", kind, err)
		}
	}
	for kind, mutate := range map[string]func(*factory.Item){
		"landed": func(it *factory.Item) { it.State = factory.StateLanded },
		"begun": func(it *factory.Item) {
			it.Stream = &factory.Stream{Phases: []factory.Phase{{Name: "Tidy", State: factory.PhasePending, Round: 1}}}
		},
	} {
		it := legacyNamedItem(t, "plan", "Tidy")
		mutate(&it)
		next, err := ApplyItemChange(it, ItemChange{Effort: "strong"}, recipe)
		if kind == "landed" {
			if err == nil || !strings.Contains(err.Error(), "has landed") {
				t.Errorf("landed legacy job = %v", err)
			}
			continue
		}
		// A begun Tidy keeps its thinking; the stage that has not begun takes it.
		if err != nil || next.Stages[1].Effort != "" || next.Stages[0].Effort != "strong" {
			t.Errorf("begun legacy stage: %v, %+v", err, next.Stages)
		}
	}
	proof := legacyNamedItem(t, "Tidy", "proof")
	proof.Stream = &factory.Stream{Phases: []factory.Phase{{Name: "proof", State: factory.PhaseDone}}}
	if _, err := ApplyItemChange(proof, ItemChange{Effort: "strong"}, recipe); err == nil || !strings.Contains(err.Error(), factory.ProofFrozenRefusal) {
		t.Errorf("proof-started legacy job = %v", err)
	}
}

// THE CARD HONOURS A RECIPE-FIXED STAGE UNDER A LEGACY SPELLING: "think strong"
// leaves a fixed `Review` at its own thinking and moves the rest, and the shared
// edit's verbs refuse it with the fixed sentence.
func TestItemThinkingKeepsARecipeFixedStageUnderALegacyName(t *testing.T) {
	recipe := itemRecipe()
	for i := range recipe.Stages {
		if recipe.Stages[i].Name == "review" {
			recipe.Stages[i].Fixed = true
		}
	}
	it := legacyNamedItem(t, "plan", "Review")
	next, err := ApplyItemChange(it, ItemChange{Effort: "strong"}, recipe)
	if err != nil {
		t.Fatal(err)
	}
	if next.Stages[0].Effort != "strong" || next.Stages[1].Effort != "" || next.Stages[1].Name != "Review" {
		t.Fatalf("stages = %+v", next.Stages)
	}
	if _, err := ApplyItemChange(it, ItemChange{Edit: factory.PlanEdit{Skip: []string{"REVIEW"}}}, recipe); err == nil || !strings.Contains(err.Error(), "is fixed by the recipe") {
		t.Errorf("skip of a fixed legacy stage = %v", err)
	}
}
