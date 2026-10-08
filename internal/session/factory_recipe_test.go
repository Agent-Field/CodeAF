package session

// The chat's door onto a repository's recipe (tools_factory_recipe.go), held to
// its laws: absent without a door, a line the file cannot read refused before
// any card, a card before anything is written, nothing written on a no, on
// words or on silence, and exactly one line on a yes.

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

// fakeRecipeDoor records every line it is handed.
type fakeRecipeDoor struct {
	mu    sync.Mutex
	lines []string
}

func (d *fakeRecipeDoor) record(line string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lines = append(d.lines, line)
	return nil
}

func (d *fakeRecipeDoor) BankStage(_ context.Context, repo string, kind factory.Kind, line string) error {
	return d.record("stage " + repo + " " + string(kind) + " " + line)
}

func (d *fakeRecipeDoor) BankPolicy(_ context.Context, repo, sentence string) error {
	return d.record("policy " + repo + " " + sentence)
}

func (d *fakeRecipeDoor) BankHabit(_ context.Context, repo, sentence string) error {
	return d.record("habit " + repo + " " + sentence)
}

func (d *fakeRecipeDoor) written() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.lines...)
}

func newRecipeAgent(t *testing.T, door RecipeDoor, window time.Duration) *Agent {
	t.Helper()
	root := t.TempDir()
	config := Config{
		Workspace: root, Place: Place{Dir: root, Workspace: root},
		Model: "test/model", Interactive: true, AskConsent: true,
		factoryWindow: window,
	}
	if door != nil {
		config.Recipe = door
	}
	agent, err := newAgent(config, &scriptedCompleter{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	return agent
}

const recipeStageArgs = `{"repo":"web","kind":"issue","stage":"security · chat · read it for auth holes · when touches auth"}`

func runFactoryRecipe(t *testing.T, agent *Agent, ctx context.Context, args string) <-chan string {
	t.Helper()
	results := make(chan string, 1)
	tool := agent.factoryRecipeTool()
	go func() {
		out, _, err := tool.Execute(ctx, json.RawMessage(args))
		if err != nil {
			out = "error: " + err.Error()
		}
		results <- out
	}()
	return results
}

func awaitRecipeQuestion(t *testing.T, events <-chan Event) Question {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case event := <-events:
			if event.Kind == EventQuestion && event.Question != nil && event.Question.Kind == QuestionRecipe {
				return *event.Question
			}
		case <-deadline.C:
			t.Fatal("no recipe question was raised")
		}
	}
}

func TestFactoryRecipeIsOnTheBeltOnlyWithItsDoor(t *testing.T) {
	floorOnly := &Agent{config: Config{Workspace: t.TempDir(), Factory: &fakeFactoryDoor{}}}
	floorOnly.tools = floorOnly.belt()
	for _, tool := range floorOnly.offeredTools() {
		if tool.Name == "factory_recipe" {
			t.Fatal("a belt with no recipe door carries factory_recipe")
		}
	}
	if strings.Contains(promptWithBeltFacts(floorOnly.config), "factory_recipe") {
		t.Error("the page names factory_recipe on a belt with no recipe door")
	}

	both := &Agent{config: Config{Workspace: t.TempDir(), Factory: &fakeFactoryDoor{}, Recipe: &fakeRecipeDoor{}}}
	both.tools = both.belt()
	found := false
	for _, tool := range both.offeredTools() {
		if tool.Name == "factory_recipe" {
			found = true
		}
		if !manual.Chat().Mentions(tool.Name) {
			t.Errorf("no chat manual page mentions the %s tool — add it to internal/manual/chat/", tool.Name)
		}
	}
	if !found {
		t.Fatal("a belt with a recipe door does not carry factory_recipe")
	}
	if !strings.Contains(promptWithBeltFacts(both.config), "AND THE FACTORY: `factory_add`, `factory_recipe`; a card asks.") {
		t.Error("the page does not name factory_recipe on a belt that carries it")
	}
	if got := ActionCategoryForTool("factory_recipe"); got != ActionCreate {
		t.Errorf("factory_recipe's family is %q, want %q", got, ActionCreate)
	}
	if glossField["factory_recipe"] != "stage" {
		t.Errorf("factory_recipe glosses by %q, want stage", glossField["factory_recipe"])
	}
}

func TestFactoryRecipeRefusesWhatTheFileCannotRead(t *testing.T) {
	door := &fakeRecipeDoor{}
	agent := newRecipeAgent(t, door, 0)
	tool := agent.factoryRecipeTool()
	for _, c := range []struct{ args, want string }{
		{`{"repo":"web","kind":"issue","stage":"security · chat · when sometimes"}`, "when is one of"},
		{`{"repo":"web","stage":"security · chat"}`, "a stage needs kind"},
		{`{"repo":"web","kind":"issue","stage":"x · chat","policy":"no"}`, "exactly one of"},
		{`{"repo":"web"}`, "exactly one of"},
		{`{"kind":"issue","stage":"security"}`, "needs the repo"},
	} {
		out, isErr, err := tool.Execute(context.Background(), json.RawMessage(c.args))
		if err != nil || !isErr || !strings.Contains(out, c.want) {
			t.Errorf("%s: %q, %v, %v; want a refusal containing %q", c.args, out, isErr, err, c.want)
		}
	}
	if n := len(door.written()); n != 0 {
		t.Errorf("%d lines written by refused calls", n)
	}
	for _, q := range agent.OpenQuestions() {
		if q.Kind == QuestionRecipe {
			t.Error("a refused call raised a card")
		}
	}
}

func TestFactoryRecipeCardAsksWithItsHeadAndTwoAnswers(t *testing.T) {
	door := &fakeRecipeDoor{}
	agent := newRecipeAgent(t, door, 0)
	questions, stop := agent.WatchQuestions()
	t.Cleanup(stop)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	results := runFactoryRecipe(t, agent, ctx, recipeStageArgs)

	q := awaitRecipeQuestion(t, questions)
	if q.Head != "add this to web's recipe for issue?" {
		t.Errorf("head = %q", q.Head)
	}
	if q.Subject.Name != "recipe · web · issue" {
		t.Errorf("subject = %q", q.Subject.Name)
	}
	if !q.Deadline.IsZero() || q.Pick != nil {
		t.Error("the recipe card carries a clock or a pick; silence must bank nothing")
	}
	if len(q.Options) != 2 || q.Options[0].Key != "1" || q.Options[0].Label != "bank it" ||
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
	if out := awaitResult(t, results); out != "nothing was banked: the card was never answered" {
		t.Errorf("interrupted result = %q", out)
	}
}

func TestFactoryRecipeYesWritesExactlyOneLine(t *testing.T) {
	for _, c := range []struct{ args, head, want, line string }{
		{recipeStageArgs,
			"add this to web's recipe for issue?",
			"security · chat · read it for auth holes · when touches auth is in web's recipe for issue",
			"stage web issue security · chat · read it for auth holes · when touches auth"},
		{`{"repo":"web","policy":"never post without green tests"}`,
			"add this to web's policy?",
			"never post without green tests is in web's policy",
			"policy web never post without green tests"},
		{`{"repo":"web","habit":"PRs from my own issues ship when proof is green"}`,
			"add this to web's habits?",
			"PRs from my own issues ship when proof is green is in web's habits",
			"habit web PRs from my own issues ship when proof is green"},
	} {
		door := &fakeRecipeDoor{}
		agent := newRecipeAgent(t, door, 0)
		questions, stopQ := agent.WatchQuestions()
		lane, stopT := agent.WatchTaskUpdates()
		results := runFactoryRecipe(t, agent, context.Background(), c.args)
		q := awaitRecipeQuestion(t, questions)
		if q.Head != c.head {
			t.Errorf("head = %q, want %q", q.Head, c.head)
		}
		if err := agent.ResolveQuestion(Answer{Kind: QuestionRecipe, Ref: q.Ref, Key: "1"}); err != nil {
			t.Fatal(err)
		}
		if out := awaitResult(t, results); out != c.want {
			t.Errorf("result = %q, want %q", out, c.want)
		}
		if got := door.written(); len(got) != 1 || got[0] != c.line {
			t.Errorf("written = %q, want exactly %q", got, c.line)
		}
		var sawBanked bool
		deadline := time.After(5 * time.Second)
		for !sawBanked {
			select {
			case event := <-lane:
				if event.Kind == EventRecipeBanked {
					if event.Recipe == nil || event.Recipe.ID != q.Ref {
						t.Fatalf("banked news = %+v", event.Recipe)
					}
					sawBanked = true
				}
			case <-deadline:
				t.Fatal("EventRecipeBanked never reached the task lane")
			}
		}
		stopQ()
		stopT()
	}
}

func TestFactoryRecipeNoWordsAndSilenceWriteNothing(t *testing.T) {
	door := &fakeRecipeDoor{}
	agent := newRecipeAgent(t, door, 0)
	questions, stop := agent.WatchQuestions()
	t.Cleanup(stop)

	results := runFactoryRecipe(t, agent, context.Background(), recipeStageArgs)
	q := awaitRecipeQuestion(t, questions)
	if err := agent.ResolveQuestion(Answer{Kind: QuestionRecipe, Ref: q.Ref, Key: "2"}); err != nil {
		t.Fatal(err)
	}
	if out := awaitResult(t, results); out != "nothing was banked: the person said no." {
		t.Errorf("no = %q", out)
	}

	results = runFactoryRecipe(t, agent, context.Background(), recipeStageArgs)
	q = awaitRecipeQuestion(t, questions)
	if err := agent.ResolveQuestion(Answer{Kind: QuestionRecipe, Ref: q.Ref, Change: "only for pull requests"}); err != nil {
		t.Fatal(err)
	}
	want := "the person changed it: only for pull requests\nNothing is banked. Propose it again with that"
	if out := awaitResult(t, results); out != want {
		t.Errorf("change = %q, want %q", out, want)
	}

	quiet := newRecipeAgent(t, door, 20*time.Millisecond)
	if out := awaitResult(t, runFactoryRecipe(t, quiet, context.Background(), recipeStageArgs)); out != "nothing was banked: the card was never answered" {
		t.Errorf("window = %q", out)
	}
	for _, q := range quiet.OpenQuestions() {
		if q.Kind == QuestionRecipe {
			t.Error("the card is still open after its window ended")
		}
	}
	if n := len(door.written()); n != 0 {
		t.Errorf("%d lines written on no, words or silence", n)
	}
}

func TestFactoryRecipeAnswerKeysAndQuestionAsked(t *testing.T) {
	yes, ok := AnswerFromKey(QuestionRecipe, "1")
	if !ok || !yes.Recipe.Approved {
		t.Errorf("1 = %+v, %v", yes, ok)
	}
	no, ok := AnswerFromKey(QuestionRecipe, "2")
	if !ok || no.Recipe.Approved {
		t.Errorf("2 = %+v, %v", no, ok)
	}
	if got := questionGoneReason(Question{Kind: QuestionRecipe}); got != "nothing was banked in the recipe" {
		t.Errorf("gone reason = %q", got)
	}
	key, ok := questionAsked(Event{Kind: EventRecipeProposal, Recipe: &RecipeNotice{ID: "r1"}})
	if !ok || key != questionToken(QuestionRecipe, "r1") {
		t.Errorf("questionAsked = %q, %v", key, ok)
	}
	if _, ok := questionAsked(Event{Kind: EventRecipeProposal, Recipe: &RecipeNotice{ID: "r1", Withdrawn: "x"}}); ok {
		t.Error("a withdrawn card still reads as asking")
	}
}

// A WINDOW THAT ARRIVES AFTER THE CARD WAS RAISED IS STILL HANDED IT, as the
// factory card is.
func TestFactoryRecipeCardIsReplayedToALateTaskLane(t *testing.T) {
	agent := newRecipeAgent(t, &fakeRecipeDoor{}, 0)
	questions, stopQ := agent.WatchQuestions()
	t.Cleanup(stopQ)
	results := runFactoryRecipe(t, agent, context.Background(), recipeStageArgs)
	q := awaitRecipeQuestion(t, questions)

	late, stopLate := agent.WatchTaskUpdates()
	t.Cleanup(stopLate)
	select {
	case event := <-late:
		if event.Kind != EventRecipeProposal || event.Recipe == nil || event.Recipe.ID != q.Ref || event.Recipe.Decided != nil {
			t.Fatalf("the late lane was handed %v / %+v, want the standing card", event.Kind, event.Recipe)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a lane opened while the card stood was not handed it")
	}
	agent.ResolveRecipe(q.Ref, RecipeAnswer{})
	awaitResult(t, results)
	agent.mu.Lock()
	standing := agent.standingRecipeCardsLocked()
	agent.mu.Unlock()
	if len(standing) != 0 {
		t.Fatalf("a settled card is still replayed as standing: %+v", standing)
	}
}

// fakeStagerDoor is a recipe door that can also say the stages a kind runs
// today, as the file door does (cmd/codeaf's fileRecipeDoor.Stages), or
// refuse to.
type fakeStagerDoor struct {
	fakeRecipeDoor
	now    []string
	refuse error
}

func (d *fakeStagerDoor) Stages(_ context.Context, repo string, kind factory.Kind) ([]string, error) {
	if d.refuse != nil {
		return nil, d.refuse
	}
	return d.now, nil
}

// THE RECIPE CARD SAYS THE KIND'S STAGES BEFORE AND AFTER, the new one marked
// `+`, then the line and the reason; and a recipe the door cannot read refuses
// the line before any card, in the door's own words.
func TestFactoryRecipeCardSaysNowAndAfter(t *testing.T) {
	door := &fakeStagerDoor{now: []string{"plan", "write", "test", "review", "proof"}}
	agent := newRecipeAgent(t, door, 0)
	questions, stop := agent.WatchQuestions()
	t.Cleanup(stop)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	results := runFactoryRecipe(t, agent, ctx, `{"repo":"web","kind":"issue","stage":"security · chat · read it for auth holes · when touches auth","why":"you said auth always gets a second look"}`)
	q := awaitRecipeQuestion(t, questions)
	want := "now: plan · write · test · review · proof; after: plan · write · test · review · proof · +security; " +
		"security · chat · read it for auth holes · when touches auth; why: you said auth always gets a second look"
	if q.Reason != want {
		t.Errorf("reason =\n%q\nwant\n%q", q.Reason, want)
	}
	cancel()
	awaitResult(t, results)

	policy := RecipeRows(RecipeNotice{Repo: "web", Policy: "never post without green tests", Why: "a red post cost a day"})
	if strings.Join(policy, "|") != "never post without green tests|why: a red post cost a day" {
		t.Errorf("policy rows = %q", policy)
	}
	replaced := recipeAfter([]string{"plan", "review", "proof"}, "review")
	if strings.Join(replaced, " ") != "plan +review proof" {
		t.Errorf("a replaced stage = %q", replaced)
	}

	lost := &fakeStagerDoor{refuse: errors.New("codeaf does not know where web is checked out; open codeaf there once")}
	out, isErr, _ := newRecipeAgent(t, lost, 0).factoryRecipeTool().Execute(context.Background(), json.RawMessage(recipeStageArgs))
	if !isErr || out != "nothing was banked: codeaf does not know where web is checked out; open codeaf there once" {
		t.Errorf("an unreadable recipe = %q, %v", out, isErr)
	}
	if len(lost.written()) != 0 {
		t.Error("an unreadable recipe still banked")
	}
}
