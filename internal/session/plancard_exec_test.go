package session

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// fakePlanEffects records every call so a test can say exactly what ran, and
// fails a call by naming its id in gone (a deleted target) or broken.
type fakePlanEffects struct {
	calls  []string
	gone   map[string]bool
	broken map[string]bool
}

func (f *fakePlanEffects) do(call, id string) error {
	f.calls = append(f.calls, call+" "+id)
	switch {
	case f.gone[id]:
		return noSuchPlanTask{id: id}
	case f.broken[id]:
		return errors.New("the store said no")
	}
	return nil
}
func (f *fakePlanEffects) NoteTask(id, text string) error { return f.do("note-task", id) }
func (f *fakePlanEffects) PauseTask(id string) error      { return f.do("pause", id) }
func (f *fakePlanEffects) ResumeTask(id string) error     { return f.do("resume", id) }
func (f *fakePlanEffects) CancelTask(id string) error     { return f.do("cancel", id) }
func (f *fakePlanEffects) NoteChat(chat, text string) error {
	if f.gone[chat] {
		return ErrPlanTargetGone
	}
	return f.do("note-chat", chat)
}
func (f *fakePlanEffects) AskPlace(_ context.Context, place, text string) (string, error) {
	return "answer for " + place, f.do("ask", place)
}
func (f *fakePlanEffects) Remember(text string) (string, error) {
	return "tok-1", f.do("remember", text)
}
func (f *fakePlanEffects) Forget(token string) error { return f.do("forget", token) }

func oneOfEach() []PlanCardStep {
	return []PlanCardStep{
		{Kind: PlanStepHold, Target: PlanTarget{Task: "t1"}},
		{Kind: PlanStepStart, Target: PlanTarget{Task: "t2"}},
		{Kind: PlanStepStop, Target: PlanTarget{Task: "t3"}},
		{Kind: PlanStepSteer, Target: PlanTarget{Task: "t4"}, Text: "use the v2 fixture"},
		{Kind: PlanStepSteer, Target: PlanTarget{Chat: "c1"}, Text: "heads up"},
		{Kind: PlanStepAskPlace, Target: PlanTarget{Place: "software"}, Text: "is v1 still supported?"},
		{Kind: PlanStepRemember, Text: "the suite needs docker"},
	}
}

func TestEveryStepKindRunsThroughItsDoorAndReturnsItsUndo(t *testing.T) {
	fx := &fakePlanEffects{}
	book := NewPlanBook(fx, nil)
	receipt, err := book.Propose(context.Background(), Plan{Steps: oneOfEach()})
	if err != nil || receipt == nil {
		t.Fatalf("an in-chat plan runs at once: %v %v", receipt, err)
	}
	want := []string{"pause t1", "resume t2", "cancel t3", "note-task t4", "note-chat c1", "ask software", "remember the suite needs docker"}
	if !reflect.DeepEqual(fx.calls, want) {
		t.Fatalf("calls = %v, want %v", fx.calls, want)
	}
	undo := func(i int) string {
		if u := receipt.Results[i].Undo; u != nil {
			return u.Kind
		}
		return ""
	}
	for i, kind := range []string{"resume", "hold", "", "", "", "", "forget"} {
		if got := undo(i); got != kind {
			t.Errorf("step %d undo = %q, want %q", i, got, kind)
		}
	}
	if receipt.Results[5].Answer != "answer for software" {
		t.Errorf("the place's answer was lost: %+v", receipt.Results[5])
	}
	// One receipt carries every line, so the surface draws one group.
	if len(receipt.Results) != 7 {
		t.Fatalf("receipt has %d lines", len(receipt.Results))
	}
	// And each undo really takes its step back.
	for _, i := range []int{0, 1, 6} {
		if err := book.Undo(*receipt.Results[i].Undo); err != nil {
			t.Errorf("undo %d: %v", i, err)
		}
	}
	if got := fx.calls[len(fx.calls)-3:]; !reflect.DeepEqual(got, []string{"resume t1", "pause t2", "forget tok-1"}) {
		t.Errorf("undo calls = %v", got)
	}
}

func TestADeletedTargetIsASkippedLineAndTheRestStillRun(t *testing.T) {
	fx := &fakePlanEffects{gone: map[string]bool{"t1": true, "c1": true}, broken: map[string]bool{"t3": true}}
	receipt, err := NewPlanBook(fx, nil).Propose(context.Background(), Plan{Steps: oneOfEach()})
	if err != nil {
		t.Fatal(err)
	}
	got := map[int]PlanStatus{}
	for i, r := range receipt.Results {
		got[i] = r.Status
	}
	want := map[int]PlanStatus{0: PlanStepSkipped, 1: PlanStepDone, 2: PlanStepFailed, 3: PlanStepDone, 4: PlanStepSkipped, 5: PlanStepDone, 6: PlanStepDone}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("statuses = %v, want %v", got, want)
	}
	if receipt.Results[0].Line != "Skipped: t1 is gone" || receipt.Results[0].Undo != nil {
		t.Errorf("skipped line = %+v", receipt.Results[0])
	}
}

func TestAPlanBeyondTheChatWaitsAndCancelHasNoSideEffects(t *testing.T) {
	fx := &fakePlanEffects{}
	var announced []*Plan
	book := NewPlanBook(fx, func(e Event) { announced = append(announced, e.Plan) })
	receipt, err := book.Propose(context.Background(), Plan{ReachesBeyond: true, Steps: oneOfEach()})
	if err != nil || receipt != nil {
		t.Fatalf("a plan beyond the chat must wait: %v %v", receipt, err)
	}
	if len(announced) != 1 || announced[0].ID == "" || !announced[0].ReachesBeyond {
		t.Fatalf("announced = %+v", announced)
	}
	id := announced[0].ID
	if err := book.Cancel(id); err != nil {
		t.Fatal(err)
	}
	if len(fx.calls) != 0 {
		t.Fatalf("cancel touched the world: %v", fx.calls)
	}
	if _, err := book.Go(context.Background(), id); !errors.Is(err, ErrPlanCancelled) {
		t.Fatalf("go after cancel = %v", err)
	}
	if err := book.Edit(id, oneOfEach()); !errors.Is(err, ErrPlanCancelled) {
		t.Fatalf("edit after cancel = %v", err)
	}
	if err := book.Cancel(id); err != nil {
		t.Fatalf("a second cancel is the same cancel: %v", err)
	}
	if len(fx.calls) != 0 {
		t.Fatalf("something ran after cancel: %v", fx.calls)
	}
	if err := book.Cancel("nope"); !errors.Is(err, ErrPlanUnknown) {
		t.Fatalf("unknown cancel = %v", err)
	}
}

func TestEditReplacesTheStepsAndGoRunsThemOnceOnly(t *testing.T) {
	fx := &fakePlanEffects{}
	var announced []*Plan
	book := NewPlanBook(fx, func(e Event) { announced = append(announced, e.Plan) })
	if _, err := book.Propose(context.Background(), Plan{ReachesBeyond: true, Steps: oneOfEach()}); err != nil {
		t.Fatal(err)
	}
	id := announced[0].ID
	edited := []PlanCardStep{{Kind: PlanStepHold, Target: PlanTarget{Task: "only"}}}
	if err := book.Edit(id, edited); err != nil {
		t.Fatal(err)
	}
	if len(announced) != 2 || !reflect.DeepEqual(announced[1].Steps, edited) {
		t.Fatalf("the card was not redrawn with the edit: %+v", announced)
	}
	if err := book.Edit(id, []PlanCardStep{{Kind: PlanStepHold}}); err == nil {
		t.Fatal("an edit that cannot run must be refused")
	}
	first, err := book.Go(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	again, err := book.Go(context.Background(), id)
	if err != nil || again != first {
		t.Fatalf("a second Go must return the same receipt: %v %v", again, err)
	}
	if !reflect.DeepEqual(fx.calls, []string{"pause only"}) {
		t.Fatalf("calls = %v", fx.calls)
	}
	if err := book.Cancel(id); !errors.Is(err, ErrPlanUnknown) {
		t.Fatalf("a ran plan cannot be cancelled: %v", err)
	}
}

func TestAMalformedPlanRunsNothing(t *testing.T) {
	bad := map[string]PlanCardStep{
		"hold without a task":    {Kind: PlanStepHold, Target: PlanTarget{Chat: "c"}},
		"two targets":            {Kind: PlanStepStop, Target: PlanTarget{Task: "t", Chat: "c"}},
		"steer without words":    {Kind: PlanStepSteer, Target: PlanTarget{Task: "t"}},
		"ask without a place":    {Kind: PlanStepAskPlace, Target: PlanTarget{Task: "t"}, Text: "?"},
		"remember with a target": {Kind: PlanStepRemember, Target: PlanTarget{Task: "t"}, Text: "x"},
		"unknown action":         {Kind: "explode", Target: PlanTarget{Task: "t"}},
	}
	for name, step := range bad {
		fx := &fakePlanEffects{}
		good := PlanCardStep{Kind: PlanStepHold, Target: PlanTarget{Task: "ok"}}
		if _, err := NewPlanBook(fx, nil).Propose(context.Background(), Plan{Steps: []PlanCardStep{good, step}}); err == nil || len(fx.calls) != 0 {
			t.Errorf("%s: err=%v calls=%v", name, err, fx.calls)
		}
	}
	if _, err := NewPlanBook(&fakePlanEffects{}, nil).Propose(context.Background(), Plan{}); err == nil {
		t.Error("an empty plan must be refused")
	}
}

func TestThePlanToolIsAbsentUnlessASurfaceAsksForIt(t *testing.T) {
	a := &Agent{}
	if tools := a.planCardTools(); len(tools) != 0 {
		t.Fatalf("plan is on the belt with no card to draw it: %v", tools)
	}
	a.config.PlanCards = true
	tools := a.planCardTools()
	if len(tools) != 1 || tools[0].Name != "plan" {
		t.Fatalf("tools = %v", tools)
	}
	out, isErr, _ := tools[0].Execute(context.Background(), []byte(`{"steps":[{"kind":"hold"}]}`))
	if !isErr {
		t.Fatalf("a malformed call must be a tool error: %s", out)
	}
	waiting, isErr, _ := tools[0].Execute(context.Background(), []byte(`{"reachesBeyond":true,"steps":[{"kind":"remember","text":"x"}]}`))
	if isErr || waiting == "" {
		t.Fatalf("a plan beyond the chat waits: %q %v", waiting, isErr)
	}
}
