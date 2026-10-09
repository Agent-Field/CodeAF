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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/manual"
)

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
		recipe: factory.DefaultRecipe(),
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
		{`{"cap":8}`, "needs the floor id"},
		{`{"item":7,"skip":["review"]}`, "nothing changed: there is no item 7"},
		{`{"item":1,"add":["after review"]}`, "needs to say what it does"},
		{`{"item":1,"effort":"huge"}`, "effort is one of cheap, strong, default"},
		{`{"item":1,"cap":-3}`, "cap is a number of dollars"},
		{`{"item":1,"skip":["proof"]}`, "nothing changed: plan may not skip proof"},
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
		{"no approve", ItemChange{Edit: factory.PlanEdit{Skip: []string{"approve"}}}, "", "#1 · skip the approve step?",
			[]string{"now: plan · approve · write · test · review · proof", "after: plan · write · test · review · proof"}},
		{"cap up", ItemChange{Cap: 12.5}, "", "#1 · raise the budget to $12.50?", []string{"budget  $5 → $12.50"}},
		{"cap down", ItemChange{Cap: 3}, "", "#1 · lower the budget to $3?", []string{"budget  $5 → $3"}},
		{"effort", ItemChange{Effort: "strong"}, "", "#1 · think strong?", []string{"thinking  — → strong"}},
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
	recipe := factory.DefaultRecipe()
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
	if ItemEffort(next) != "strong" {
		t.Errorf("ItemEffort = %q", ItemEffort(next))
	}
	back, err := ApplyItemChange(next, ItemChange{Effort: "default"}, recipe)
	if err != nil || ItemEffort(back) != "" {
		t.Errorf("default effort = %q, %v", ItemEffort(back), err)
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
		"The person said yes and the floor has the change; there is nothing left to answer. The note is kept on the item for its stages."
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
