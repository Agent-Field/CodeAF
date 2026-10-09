package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// openShapeRig is the Shape door (`shape steps`) over a real store, with a
// fake turn that answers edit, a maker that counts the conversations it made,
// and the lines said into conversations.
type openShapeRig struct {
	st      *store.Store
	door    *shapeDoor
	id      int
	edit    factory.RunEdit
	turnErr error
	turns   int
	inRun   []bool
	made    int
	said    []string
}

func newOpenShapeRig(t *testing.T) *openShapeRig {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	recipe := factory.DefaultRecipe()
	it, err := st.Create(factory.Item{Repo: "acme/web", Kind: factory.KindIssue, Title: "fix the ledger double count",
		State: factory.StateNew, Stages: factory.CopyStages(recipe.For(factory.KindIssue))})
	if err != nil {
		t.Fatal(err)
	}
	r := &openShapeRig{st: st, id: it.ID}
	r.door = &shapeDoor{
		st: st,
		turn: func(_ context.Context, it factory.Item, inRun bool) (factory.RunEdit, string, error) {
			r.turns++
			r.inRun = append(r.inRun, inRun)
			if it.Talk == "" {
				t.Error("the turn was given an item with no conversation")
			}
			return r.edit, "set review to read for security.", r.turnErr
		},
		make: func(_ context.Context, it factory.Item) (string, error) {
			r.made++
			return "/x/manager/transcript.jsonl", nil
		},
		say: func(transcript, line string) error {
			r.said = append(r.said, transcript+" ← "+line)
			return nil
		},
		recipe: func(factory.Item) factory.Recipe { return recipe },
		wait:   time.Second,
	}
	return r
}

// `shape steps` MAKES THE CONVERSATION, RUNS ONE TURN, APPLIES AND KEEPS THE
// EDIT, AND SAYS THE LINE: an item with no conversation gets one, the manager
// is given one turn bounded as before a run, the edit is on the item's stages
// marked the manager's with its record line, the line goes into the
// conversation, and nothing runs. ASKED AGAIN, NO TURN IS SPENT: the manager
// already shaped the steps, and the person changes them in its chat.
func TestOpenShapeDoorMakesTheConversationAndShapesOnce(t *testing.T) {
	r := newOpenShapeRig(t)
	r.edit = factory.RunEdit{By: factory.ByManager, Ask: map[string]string{"review": "read it for security"}, Why: "touches the ledger"}
	line, err := r.door.Shape(context.Background(), r.id)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line, "manager set review: read it for security") {
		t.Fatalf("line = %q", line)
	}
	if r.made != 1 || r.turns != 1 || r.inRun[0] {
		t.Fatalf("made %d conversations, %d turns, in a run %v", r.made, r.turns, r.inRun)
	}
	it, err := r.st.Get(r.id)
	if err != nil {
		t.Fatal(err)
	}
	if it.Talk != "/x/manager/transcript.jsonl" {
		t.Fatalf("the conversation was not kept on the item: %q", it.Talk)
	}
	at := factory.StageIndex(it.Stages, "review")
	if at < 0 || it.Stages[at].Ask != "read it for security" || it.Stages[at].By != factory.ByManager {
		t.Fatalf("the edit is not on the item: %+v", it.Stages)
	}
	if !factoryrun.ShapedByManager(it) || len(it.Adapted) == 0 || it.Stream != nil {
		t.Fatalf("the item is not shaped by the manager before its run: adapted %q, stream %v", it.Adapted, it.Stream)
	}
	if len(r.said) != 1 || r.said[0] != "/x/manager/transcript.jsonl ← "+line {
		t.Fatalf("said %q", r.said)
	}
	r.edit = factory.RunEdit{}
	again, err := r.door.Shape(context.Background(), r.id)
	if err != nil || again != sayShapedAlready || r.turns != 1 || r.made != 1 {
		t.Fatalf("the second ask answered %q, %v after %d turns and %d conversations", again, err, r.turns, r.made)
	}
}

// ONE TURN PER ITEM AT A TIME: an ask while a turn for the item is out is
// `already shaped`, and no second turn.
func TestOpenShapeDoorAsksOneTurnAtATime(t *testing.T) {
	r := newOpenShapeRig(t)
	if !r.door.begin(r.id) {
		t.Fatal("no turn was out and the door refused one")
	}
	defer r.door.end(r.id)
	if line, err := r.door.Shape(context.Background(), r.id); err != nil || line != factory.ShapeAlready || r.turns != 0 {
		t.Fatalf("line %q, err %v, %d turns", line, err, r.turns)
	}
}

// THE RECIPE STANDING IS A SHAPING TOO: a turn that changes nothing says `the
// recipe stands`, keeps the manager's record so the launch does not ask again,
// and leaves the stages as they were.
func TestOpenShapeDoorRecipeStandsIsKept(t *testing.T) {
	r := newOpenShapeRig(t)
	before, _ := r.st.Get(r.id)
	line, err := r.door.Shape(context.Background(), r.id)
	if err != nil || line != "the recipe stands" {
		t.Fatalf("line %q, err %v", line, err)
	}
	it, _ := r.st.Get(r.id)
	if !factoryrun.ShapedByManager(it) || factory.AdaptedLine(it) != "manager kept the recipe" {
		t.Fatalf("record %q", it.Adapted)
	}
	for i := range it.Stages {
		if it.Stages[i].Ask != before.Stages[i].Ask || it.Stages[i].By != "" {
			t.Fatalf("the stages changed: %+v", it.Stages[i])
		}
	}
	// A SECOND ASK IS NO SECOND TURN AND NO SECOND RECORD.
	if again, _ := r.door.Shape(context.Background(), r.id); again != sayShapedAlready || r.turns != 1 {
		t.Fatalf("second ask %q after %d turns", again, r.turns)
	}
	if it, _ := r.st.Get(r.id); len(it.Adapted) != 1 {
		t.Fatalf("records %q", it.Adapted)
	}
}

// THE CONVERSATION THE ITEM HAS IS THE ONE THE TURN RUNS IN: nothing is made.
func TestOpenShapeDoorUsesTheItemsConversation(t *testing.T) {
	r := newOpenShapeRig(t)
	chat := "/x/talked/transcript.jsonl"
	if err := r.st.Update(r.id, func(it *factory.Item) error { it.Talk = chat; return nil }); err != nil {
		t.Fatal(err)
	}
	line, err := r.door.Shape(context.Background(), r.id)
	if err != nil || line != "the recipe stands" || r.turns != 1 || r.made != 0 || len(r.said) != 1 || !strings.HasPrefix(r.said[0], chat) {
		t.Fatalf("line %q, err %v, %d turns, %d made, said %q", line, err, r.turns, r.made, r.said)
	}
}

// A TURN THAT DID NOT ANSWER IS NO SHAPING: the launch's line is said and
// answered as the failure, and nothing is recorded, so ▶ run asks again.
func TestOpenShapeDoorFailedTurnRecordsNothing(t *testing.T) {
	r := newOpenShapeRig(t)
	r.turnErr = errors.New("model unavailable")
	line, err := r.door.Shape(context.Background(), r.id)
	if err == nil || line != "the manager did not answer · the recipe stands" || err.Error() != line {
		t.Fatalf("line %q, err %v", line, err)
	}
	it, _ := r.st.Get(r.id)
	if factoryrun.ShapedByManager(it) {
		t.Fatalf("a failed turn left a record: %q", it.Adapted)
	}
	if len(r.said) != 1 || !strings.HasSuffix(r.said[0], line) {
		t.Fatalf("said %q", r.said)
	}
}

// A RUNNING ITEM IS SHAPED IN ITS TAIL: the turn is bounded to the steps not
// yet started. AN ITEM WHOSE RUN IS OVER HAS NOTHING TO SHAPE, and says so.
func TestOpenShapeDoorShapesARunningItemsTailAndRefusesALandedOne(t *testing.T) {
	r := newOpenShapeRig(t)
	if err := r.st.Update(r.id, func(it *factory.Item) error {
		it.State = factory.StateRunning
		it.Stream = &factory.Stream{Started: time.Now()}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.door.Shape(context.Background(), r.id); err != nil || r.turns != 1 || !r.inRun[0] {
		t.Fatalf("err %v after %d turns, in a run %v", err, r.turns, r.inRun)
	}
	if err := r.st.Update(r.id, func(it *factory.Item) error { it.State = factory.StateLanded; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := r.door.Shape(context.Background(), r.id); !errors.Is(err, errNothingToShape) || r.turns != 1 {
		t.Fatalf("a landed item: err %v after %d turns", err, r.turns)
	}
}

// THE MAILBOX CARRIES A SHAPING BESIDE THE DRAIN: a window that does not run
// the floor asks the owner, whose door answers the line back.
func TestOpenShapeThroughTheMailbox(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seam := factory.LocalSeam(st, time.Now(), factory.WithMailbox(st.Mailbox()))
	if !seam.Has("shape") {
		t.Fatal("a window with a mailbox has no Shape door")
	}
	asked := make(chan int, 1)
	shape := func(_ context.Context, id int) (string, error) {
		asked <- id
		return "manager set review: read it for security", nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go drainFactoryMailbox(ctx, st.Mailbox(), buildFactoryRunner(st, t.TempDir(), t.TempDir(), nil, nil), shape, 5*time.Millisecond)
	line, err := seam.Shape(context.Background(), 4)
	if err != nil || line != "manager set review: read it for security" || <-asked != 4 {
		t.Fatalf("line %q, err %v", line, err)
	}
}

// SKIP THEN SWITCH ON IN ONE TURN IS ON: the edit the turn collected, applied
// to the item the turn started from, leaves every stage as the manager's last
// call left it, and the line the item records says what was applied. (The
// owner's run of 2026-10-09 recorded `skipped write · skipped review · why:
// Turn write and review back on`, and both stages ended off.)
func TestASkipThenSwitchOnInOneTurnLeavesTheStagesOn(t *testing.T) {
	recipe := factory.DefaultRecipe()
	it := factory.Item{ID: 7, Kind: factory.KindIssue, Talk: "/x/t.jsonl", Stages: factory.CopyStages(recipe.For(factory.KindIssue))}
	door := &collectRunDoor{it: it, recipe: recipe}
	ctx := context.Background()
	if _, lines, err := door.EditRun(ctx, "", factory.RunEdit{By: factory.ByManager, Skip: []string{"write", "review"}, Why: "rebuild them"}); err != nil || len(lines) == 0 {
		t.Fatalf("skip: %v %q", err, lines)
	}
	_, lines, err := door.EditRun(ctx, "", factory.RunEdit{By: factory.ByManager, On: []string{"write", "review"}, Why: "turn write and review back on"})
	if err != nil || !strings.Contains(strings.Join(lines, " "), "switched on write") {
		t.Fatalf("on: %v %q", err, lines)
	}
	got := door.collected()
	shaped := it
	line, err := factoryrun.ApplyShape(&shaped, got, recipe, false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"write", "review"} {
		i := factory.StageIndex(shaped.Stages, name)
		if i < 0 || !shaped.Stages[i].On {
			t.Fatalf("%s is off after skip then on: %+v", name, shaped.Stages)
		}
	}
	if strings.Contains(line, "skipped") {
		t.Fatalf("the line says what was not applied: %q", line)
	}
	// AND THE OTHER WAY: on then skip is off, and the line says skipped.
	door = &collectRunDoor{it: it, recipe: recipe}
	if _, _, err := door.EditRun(ctx, "", factory.RunEdit{By: factory.ByManager, On: []string{"security"}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := door.EditRun(ctx, "", factory.RunEdit{By: factory.ByManager, Skip: []string{"security", "write"}}); err != nil {
		t.Fatal(err)
	}
	shaped = it
	line, err = factoryrun.ApplyShape(&shaped, door.collected(), recipe, false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if i := factory.StageIndex(shaped.Stages, "security"); i < 0 || shaped.Stages[i].On {
		t.Fatalf("security is on after on then skip: %+v", shaped.Stages)
	}
	if !strings.Contains(line, "skipped write") || strings.Contains(line, "switched on") {
		t.Fatalf("line %q", line)
	}
}
