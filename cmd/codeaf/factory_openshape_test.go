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

// openShapeRig is the Shape door over a real store, with a fake turn that
// answers edit, a maker that counts the conversations it made, the lines said
// into conversations, and the conversations the person spoke in.
type openShapeRig struct {
	st      *store.Store
	door    *shapeDoor
	id      int
	edit    factory.RunEdit
	turnErr error
	turns   int
	asked   [][]string
	made    int
	said    []string
	spoken  map[string]bool
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
	r := &openShapeRig{st: st, id: it.ID, spoken: map[string]bool{}}
	r.door = &shapeDoor{
		st: st,
		turn: func(_ context.Context, it factory.Item, said []string) (factory.RunEdit, string, error) {
			r.turns++
			r.asked = append(r.asked, said)
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
		spoke:  func(transcript string) bool { return r.spoken[transcript] },
		recipe: func(factory.Item) factory.Recipe { return recipe },
		wait:   time.Second,
	}
	return r
}

// THE DOOR MAKES THE CONVERSATION, RUNS ONE TURN, APPLIES AND KEEPS THE EDIT,
// AND SAYS THE LINE: an item with no conversation gets one, the manager is
// given one turn with nothing said, the edit is on the item's stages marked
// the manager's with its record line, the line goes into the conversation,
// and the second ask is `already shaped` with no second turn.
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
	if r.made != 1 || r.turns != 1 || len(r.asked[0]) != 0 {
		t.Fatalf("made %d conversations, %d turns, said %q", r.made, r.turns, r.asked)
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
	again, err := r.door.Shape(context.Background(), r.id)
	if err != nil || again != factory.ShapeAlready || r.turns != 1 || r.made != 1 {
		t.Fatalf("the second ask answered %q, %v after %d turns and %d conversations", again, err, r.turns, r.made)
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
	if again, _ := r.door.Shape(context.Background(), r.id); again != factory.ShapeAlready || r.turns != 1 {
		t.Fatalf("second ask %q after %d turns", again, r.turns)
	}
}

// THE PERSON SPOKE FIRST: an item whose conversation holds a line the person
// typed is left to that conversation. No turn, nothing written.
func TestOpenShapeDoorLeavesAnItemThePersonTalkedTo(t *testing.T) {
	r := newOpenShapeRig(t)
	chat := "/x/talked/transcript.jsonl"
	if err := r.st.Update(r.id, func(it *factory.Item) error { it.Talk = chat; return nil }); err != nil {
		t.Fatal(err)
	}
	r.spoken[chat] = true
	line, err := r.door.Shape(context.Background(), r.id)
	if err != nil || line != factory.ShapeAlready || r.turns != 0 || r.made != 0 || len(r.said) != 0 {
		t.Fatalf("line %q, err %v, %d turns, %d made, said %q", line, err, r.turns, r.made, r.said)
	}
}

// A TURN THAT DID NOT ANSWER IS NO SHAPING: the launch's line is said and
// answered as the failure, and nothing is recorded, so `r` asks again.
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

// AN ITEM THAT RAN IS NEVER SHAPED AT OPEN.
func TestOpenShapeDoorLeavesAnItemThatRan(t *testing.T) {
	r := newOpenShapeRig(t)
	if err := r.st.Update(r.id, func(it *factory.Item) error {
		it.Stream = &factory.Stream{Started: time.Now()}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if line, _ := r.door.Shape(context.Background(), r.id); line != factory.ShapeAlready || r.turns != 0 {
		t.Fatalf("line %q after %d turns", line, r.turns)
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
