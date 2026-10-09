package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/session"
)

// THE MANAGER'S BRIEF is the marker, the owner's five blocks word for word, and
// then the item's facts: its read, its stages with their asks and loops, the
// recipe's policy and habits, ask me at, budget and thinking.
func TestTheManagersBriefIsTheFiveBlocksThenTheFacts(t *testing.T) {
	recipe := factory.DefaultRecipe()
	recipe.Policy = []string{"tests pass before anything posts"}
	recipe.Habits = []string{"keep the changelog"}
	it := factory.Item{ID: 12, Repo: "acme/web", Title: "fix the ledger double count", Body: "refunds count twice",
		Gate: factory.GatePlan, Cap: 5, Triage: factory.Triage{Read: "a small fix in the ledger", Size: "M"}}
	it.Stages = factory.CopyStages(recipe.Stages)
	for i := range it.Stages {
		it.Stages[i].Effort = "strong"
	}
	brief := talkBrief(it, recipe)
	lines := strings.Split(brief, "\n")
	want := []string{
		"[factory item #12]",
		"You are the manager of #12 in acme/web.",
		"You know: the issue and its comments, what codeaf read of it, the repository's recipe, policy and habits, the checkout, and what the person has said here.",
		"You do: shape the run, start it when asked, report each stage here, answer the person, and ask only when ask-me-at says so.",
		"Shape the run with factory_run: stages are one lowercase word each, at most nine. Each stage has an ask that says what done looks like, a loop (until, rounds, fanout) and one line of why. Keep the recipe's stages unless the item says otherwise; change asks before adding stages; add a stage only for work no existing stage covers. Never drop proof or a gate stage. Nothing posts outward before ask-me-at.",
		"Say what you set in three lines at most, then stop. Do not narrate.",
		talkRecipeLaw,
		"",
		"#12 · fix the ledger double count",
	}
	if len(lines) < len(want) {
		t.Fatalf("brief = %q", brief)
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("line %d = %q, want %q", i+1, lines[i], w)
		}
	}
	for _, fact := range []string{"ask me at plan · budget $5 · thinking strong", "refunds count twice", "codeaf read it: a small fix in the ledger",
		"stages:", "2. write · chat · make the change in the checkout · until done · fanout per file", "policy:\n- tests pass before anything posts",
		"habits:\n- keep the changelog", "Its item field is 12"} {
		if !strings.Contains(brief, fact) {
			t.Errorf("the facts lack %q:\n%s", fact, brief)
		}
	}
}

// fakeManagerTurn is one turn of a manager conversation: it calls
// `factory_run` through the door it was opened with, says one sentence, and
// ends.
type fakeManagerTurn struct {
	door   session.RunDoor
	edit   factory.RunEdit
	asked  *[]string
	closed bool
	refuse *error
}

func (f *fakeManagerTurn) SubmitRunnerNote(ctx context.Context, text string) (<-chan session.Event, error) {
	*f.asked = append(*f.asked, text)
	ch := make(chan session.Event, 4)
	_, _, err := f.door.EditRun(ctx, "", f.edit)
	if f.refuse != nil {
		*f.refuse = err
	}
	ch <- session.Event{Kind: session.EventTextDelta, Text: "set review to read for security."}
	close(ch)
	return ch, nil
}
func (f *fakeManagerTurn) Interrupt()   {}
func (f *fakeManagerTurn) Close() error { f.closed = true; return nil }

// ONE SHAPING TURN: the ask goes in as the runner's note, the turn's
// `factory_run` is collected and not written, and the thinking is cheap
// unless the item reads as large.
func TestOneShapingTurnCollectsTheManagersEdit(t *testing.T) {
	var asked []string
	var cheapness []bool
	var turn *fakeManagerTurn
	var refused error
	edit := factory.RunEdit{By: "manager", Ask: map[string]string{"review": "read it for security"}}
	s := &shapeTurns{
		dirs: func(string) string { return "" },
		open: func(_ context.Context, it factory.Item, door session.RunDoor, cheap bool) (managerTurn, error) {
			cheapness = append(cheapness, cheap)
			turn = &fakeManagerTurn{door: door, edit: edit, asked: &asked, refuse: &refused}
			return turn, nil
		},
	}
	it := factory.Item{ID: 3, Talk: "/x/transcript.jsonl", Stages: factory.CopyStages(factory.DefaultRecipe().Stages), Triage: factory.Triage{Size: "M"}}
	got, reply, err := s.Shape(context.Background(), it, []string{"keep the old API"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Ask["review"] != "read it for security" || reply != "set review to read for security." {
		t.Fatalf("collected %+v, reply %q", got, reply)
	}
	if len(asked) != 1 || asked[0] != "Shape the run for this item now. What the person said: keep the old API" {
		t.Fatalf("asked %q", asked)
	}
	if !turn.closed || refused != nil {
		t.Fatalf("closed %v, refused %v", turn.closed, refused)
	}
	if it.Stages[3].Ask != "read it as a stranger would" {
		t.Fatal("the shaping turn wrote the item")
	}
	it.Triage.Size = "L"
	if _, _, err := s.Reshape(context.Background(), it, "do a thorough review"); err != nil {
		t.Fatal(err)
	}
	if asked[1] != "The person said: do a thorough review · reshape the stages not yet started if that is what they mean, else leave them" {
		t.Fatalf("asked %q", asked[1])
	}
	if len(cheapness) != 2 || !cheapness[0] || cheapness[1] {
		t.Fatalf("cheap %v, want cheap for M and not for L", cheapness)
	}
	// A CALL THE BOUNDS REFUSE IS NOT COLLECTED: the manager is told why, and
	// the recipe stands.
	edit = factory.RunEdit{By: "manager", Ask: map[string]string{"nosuch": "x"}}
	got, _, err = s.Shape(context.Background(), it, nil)
	if err != nil || len(got.Ask) != 0 || refused == nil || asked[2] != shapeAsk {
		t.Fatalf("a refused call collected %+v (%v), refused %v, asked %q", got, err, refused, asked[2])
	}
	// A CONVERSATION THAT CANNOT BE OPENED is the turn's error.
	s.open = func(context.Context, factory.Item, session.RunDoor, bool) (managerTurn, error) {
		return nil, errors.New("held by another window")
	}
	if _, _, err := s.Shape(context.Background(), it, nil); err == nil {
		t.Fatal("a turn that could not open answered no error")
	}
}

// `factory_run` IN THE PERSON'S CONVERSATION edits the one item it manages,
// through the floor's edit door, and refuses a conversation that manages none.
func TestTheRunDoorEditsTheItemThisConversationManages(t *testing.T) {
	st, web, _ := talkLab(t)
	talk := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(talk, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	id, err := st.Add(context.Background(), factory.Item{Repo: "web", Title: "fix the ledger", Talk: talk})
	if err != nil {
		t.Fatal(err)
	}
	door := runDoor(st, web)
	if door == nil {
		t.Fatal("no run door over a store")
	}
	it, lines, err := door.EditRun(context.Background(), talk, factory.RunEdit{By: "manager", Ask: map[string]string{"review": "read it for security"}, Why: "the person asked"})
	if err != nil {
		t.Fatal(err)
	}
	if got := session.RunEditLine(lines); got != "manager set review: read it for security · why: the person asked" {
		t.Fatalf("line = %q", got)
	}
	saved, _ := st.Get(id)
	if i := factory.StageIndex(saved.Stages, "review"); i < 0 || saved.Stages[i].Ask != "read it for security" || it.ID != id {
		t.Fatalf("saved stages %+v", saved.Stages)
	}
	if _, _, err := door.EditRun(context.Background(), filepath.Join(t.TempDir(), "other.jsonl"), factory.RunEdit{Skip: []string{"review"}}); !errors.Is(err, errNotAManager) {
		t.Fatalf("another conversation edited the item: %v", err)
	}
	if runDoor(nil, web) != nil {
		t.Fatal("a nil store has a run door")
	}
}

// fakeLiveTurn is the window's own conversation as the live road sees it: it
// may be mid-turn for busy submits, calls `factory_run` through the door its
// turn was lent, and counts the waits for it to be between turns.
type fakeLiveTurn struct {
	door  session.RunDoor
	edit  factory.RunEdit
	busy  int
	asked []string
	waits int
}

func (f *fakeLiveTurn) SubmitRunnerNote(ctx context.Context, text string) (<-chan session.Event, error) {
	f.asked = append(f.asked, text)
	if f.busy > 0 {
		f.busy--
		return nil, session.ErrConversationBusy
	}
	ch := make(chan session.Event, 2)
	if _, _, err := f.door.EditRun(ctx, "", f.edit); err != nil {
		return nil, err
	}
	ch <- session.Event{Kind: session.EventTextDelta, Text: "kept it tiny; review reads it thoroughly."}
	close(ch)
	return ch, nil
}
func (f *fakeLiveTurn) Interrupt()                     {}
func (f *fakeLiveTurn) Close() error                   { return nil }
func (f *fakeLiveTurn) WaitIdle(context.Context) error { f.waits++; return nil }

// liveShapeRig is shaping turns whose item conversation is live in this
// process at talk, and whose open fails the test: no second open may happen.
func liveShapeRig(t *testing.T, talk string, live *fakeLiveTurn) *shapeTurns {
	t.Helper()
	return &shapeTurns{
		dirs: func(string) string { return "" },
		live: func(transcript string, door session.RunDoor) (managerTurn, bool) {
			if transcript != talk {
				return nil, false
			}
			live.door = door
			return live, true
		},
		open: func(context.Context, factory.Item, session.RunDoor, bool) (managerTurn, error) {
			t.Error("the conversation was opened a second time while the window held it")
			return nil, errors.New("held")
		},
	}
}

// THE PERSON TALKED IN THE WINDOW AND PRESSED `r` (the 2026-10-08 19:33
// recording): the shaping turn runs through the window's own conversation, no
// second open happens, the person's words are in the ask, the edit is
// collected and not written, and the window's conversation is not closed.
func TestALiveConversationShapesThroughTheWindow(t *testing.T) {
	talk := "/x/manager/transcript.jsonl"
	live := &fakeLiveTurn{edit: factory.RunEdit{By: "manager", Ask: map[string]string{"review": "review it thoroughly"}}}
	s := liveShapeRig(t, talk, live)
	it := factory.Item{ID: 1, Talk: talk, Stages: factory.CopyStages(factory.DefaultRecipe().Stages), Triage: factory.Triage{Size: "S"}}
	said := "keep the fix tiny and prove it at the CLI, review it thoroughly"
	got, reply, err := s.Shape(context.Background(), it, []string{said})
	if err != nil {
		t.Fatal(err)
	}
	if got.Ask["review"] != "review it thoroughly" || reply == "" {
		t.Fatalf("collected %+v, reply %q", got, reply)
	}
	if len(live.asked) != 1 || live.asked[0] != shapeAsk+" What the person said: "+said {
		t.Fatalf("asked %q", live.asked)
	}
	if it.Stages[factory.StageIndex(it.Stages, "review")].Ask == "review it thoroughly" {
		t.Fatal("the shaping turn wrote the item")
	}
	if live.waits != 0 {
		t.Fatalf("an idle conversation was waited on %d times", live.waits)
	}
	// CLOSING THE LIVE TURN closes nothing of the window's: the real live
	// turn's Close never reaches its agent (a nil one would panic here).
	if err := (liveManagerTurn{}).Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := liveManagerTurnFor(filepath.Join(t.TempDir(), "nobody.jsonl"), nil); ok {
		t.Fatal("a conversation nobody holds read as live")
	}
}

// A WINDOW MID-TURN is waited on and asked once more; busy again is the busy
// line's error, after exactly two asks.
func TestABusyLiveConversationIsAskedOnceMoreThenNamed(t *testing.T) {
	talk := "/x/manager/transcript.jsonl"
	it := factory.Item{ID: 1, Talk: talk, Stages: factory.CopyStages(factory.DefaultRecipe().Stages)}
	edit := factory.RunEdit{By: "manager", Ask: map[string]string{"review": "review it thoroughly"}}

	once := &fakeLiveTurn{edit: edit, busy: 1}
	got, _, err := liveShapeRig(t, talk, once).Shape(context.Background(), it, nil)
	if err != nil || got.Ask["review"] != "review it thoroughly" || len(once.asked) != 2 || once.waits != 1 {
		t.Fatalf("busy once: %+v, %v, asked %d, waited %d", got, err, len(once.asked), once.waits)
	}

	twice := &fakeLiveTurn{edit: edit, busy: 2}
	_, _, err = liveShapeRig(t, talk, twice).Shape(context.Background(), it, nil)
	if !errors.Is(err, factoryrun.ErrManagerBusy) || len(twice.asked) != 2 || twice.waits != 1 {
		t.Fatalf("busy twice: %v, asked %d, waited %d", err, len(twice.asked), twice.waits)
	}
}

// ANOTHER PROCESS HOLDS THE CONVERSATION: the open's lock refusal is the
// other-window line's error, with no retry.
func TestAConversationAnotherProcessHoldsIsNamed(t *testing.T) {
	opens := 0
	s := &shapeTurns{
		dirs: func(string) string { return "" },
		open: func(context.Context, factory.Item, session.RunDoor, bool) (managerTurn, error) {
			opens++
			return nil, &session.SessionLockedError{Path: "/x/t.jsonl"}
		},
	}
	_, _, err := s.Shape(context.Background(), factory.Item{ID: 1, Talk: "/x/t.jsonl"}, nil)
	if !errors.Is(err, factoryrun.ErrManagerAway) || opens != 1 {
		t.Fatalf("err %v after %d opens", err, opens)
	}
}
