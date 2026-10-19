package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/session"
)

// THE MANAGER'S BRIEF is the marker, the owner's five blocks word for word, and
// then the item's facts: its read, its stages with their asks and loops, the
// recipe's policy and habits, budget and thinking.
func TestTheManagersBriefIsTheFiveBlocksThenTheFacts(t *testing.T) {
	recipe := factory.DefaultRecipe()
	recipe.Policy = []string{"tests pass before anything posts"}
	recipe.Habits = []string{"keep the changelog"}
	it := factory.Item{ID: 12, Repo: "acme/web", Title: "fix the ledger double count", Body: "refunds count twice",
		Cap: 5, Triage: factory.Triage{Read: "a small fix in the ledger", Size: "M"}}
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
		talkBlockDo,
		"Shape the run with factory_run: stages are one lowercase word each, at most nine. Each stage has an ask that says what done looks like, a loop (until, rounds, fanout) and one line of why. Keep the recipe's stages unless the item says otherwise; change asks before adding stages; add a stage only for work no existing stage covers. Never drop proof. An approve step is where the run holds until the person says continue: keep the recipe's, add one (named approve) after a stage the person wants to read first, and skip one only when the person asks. Nothing posts outward before an approve step.",
		talkBlockInbox,
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
	for _, fact := range []string{"budget $5 · thinking strong", "refunds count twice", "codeaf read it: a small fix in the ledger",
		"stages:", "3. write · chat · make the change in the checkout · until done · fanout per file", "policy:\n- tests pass before anything posts",
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

// ONE SHAPING TURN (`shape steps`): the ask goes in as the runner's note, the
// turn's `factory_run` is collected and not written, and the thinking is cheap
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
	got, reply, err := s.run(context.Background(), it, shapeAsk, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Ask["review"] != "read it for security" || reply != "set review to read for security." {
		t.Fatalf("collected %+v, reply %q", got, reply)
	}
	if len(asked) != 1 || asked[0] != shapeAsk {
		t.Fatalf("asked %q", asked)
	}
	if !turn.closed || refused != nil {
		t.Fatalf("closed %v, refused %v", turn.closed, refused)
	}
	if it.Stages[4].Ask != "read it as a stranger would" {
		t.Fatal("the shaping turn wrote the item")
	}
	it.Triage.Size, it.Triage.SizeFrom, it.Triage.TriagedAt = "L", factory.SizeFromRead, time.Unix(100, 0)
	if _, _, err := s.run(context.Background(), it, shapeAsk, false); err != nil {
		t.Fatal(err)
	}
	if asked[1] != shapeAsk {
		t.Fatalf("asked %q", asked[1])
	}
	if len(cheapness) != 2 || !cheapness[0] || cheapness[1] {
		t.Fatalf("cheap %v, want cheap for M and not for L", cheapness)
	}
	// A CALL THE BOUNDS REFUSE IS NOT COLLECTED: the manager is told why, and
	// the recipe stands.
	edit = factory.RunEdit{By: "manager", Ask: map[string]string{"nosuch": "x"}}
	got, _, err = s.run(context.Background(), it, shapeAsk, false)
	if err != nil || len(got.Ask) != 0 || refused == nil || asked[2] != shapeAsk {
		t.Fatalf("a refused call collected %+v (%v), refused %v, asked %q", got, err, refused, asked[2])
	}
	// A CONVERSATION THAT CANNOT BE OPENED is the turn's error.
	s.open = func(context.Context, factory.Item, session.RunDoor, bool) (managerTurn, error) {
		return nil, errors.New("held by another window")
	}
	if _, _, err := s.run(context.Background(), it, shapeAsk, false); err == nil {
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

// THE PERSON HAS THE MANAGER OPEN IN A WINDOW AND ASKS FOR `shape steps`: the
// shaping turn runs through the window's own conversation, no second open
// happens, the edit is collected and not written, and the window's
// conversation is not closed.
func TestALiveConversationShapesThroughTheWindow(t *testing.T) {
	talk := "/x/manager/transcript.jsonl"
	live := &fakeLiveTurn{edit: factory.RunEdit{By: "manager", Ask: map[string]string{"review": "review it thoroughly"}}}
	s := liveShapeRig(t, talk, live)
	it := factory.Item{ID: 1, Talk: talk, Stages: factory.CopyStages(factory.DefaultRecipe().Stages), Triage: factory.Triage{Size: "S"}}
	got, reply, err := s.run(context.Background(), it, shapeAsk, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Ask["review"] != "review it thoroughly" || reply == "" {
		t.Fatalf("collected %+v, reply %q", got, reply)
	}
	if len(live.asked) != 1 || live.asked[0] != shapeAsk {
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
	got, _, err := liveShapeRig(t, talk, once).run(context.Background(), it, shapeAsk, false)
	if err != nil || got.Ask["review"] != "review it thoroughly" || len(once.asked) != 2 || once.waits != 1 {
		t.Fatalf("busy once: %+v, %v, asked %d, waited %d", got, err, len(once.asked), once.waits)
	}

	twice := &fakeLiveTurn{edit: edit, busy: 2}
	_, _, err = liveShapeRig(t, talk, twice).run(context.Background(), it, shapeAsk, false)
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
	_, _, err := s.run(context.Background(), factory.Item{ID: 1, Talk: "/x/t.jsonl"}, shapeAsk, false)
	if !errors.Is(err, factoryrun.ErrManagerAway) || opens != 1 {
		t.Fatalf("err %v after %d opens", err, opens)
	}
}

// The shaping turn thinks cheap unless the SHOWN size is large: a label, a
// measured diff or a completed read. A bare L (a legacy document) or a chat
// model's guess is no size.
func TestShapeCheapReadsTheShownSize(t *testing.T) {
	when := time.Unix(100, 0)
	for _, tc := range []struct {
		name  string
		it    factory.Item
		cheap bool
	}{
		{"a read L", factory.Item{Triage: factory.Triage{Size: "L", SizeFrom: factory.SizeFromRead, TriagedAt: when}}, false},
		{"a diff L", factory.Item{Kind: factory.KindPR, Triage: factory.Triage{Size: "L", SizeFrom: factory.SizeFromDiff}}, false},
		{"an issue claiming a PR diff", factory.Item{Kind: factory.KindIssue, Triage: factory.Triage{Size: "L", SizeFrom: factory.SizeFromDiff}}, true},
		{"a label L over a read S", factory.Item{Labels: []string{"size:l"}, Triage: factory.Triage{Size: "S", SizeFrom: factory.SizeFromRead, TriagedAt: when}}, false},
		{"a label S over a read L", factory.Item{Labels: []string{"size:s"}, Triage: factory.Triage{Size: "L", SizeFrom: factory.SizeFromRead, TriagedAt: when}}, true},
		{"a legacy L", factory.Item{Triage: factory.Triage{Size: "L", TriagedAt: when}}, true},
		{"a chat L", factory.Item{Triage: factory.Triage{Size: "L", SizeFrom: factory.SizeFromChat}}, true},
		{"nothing", factory.Item{}, true},
	} {
		if got := shapeCheap(tc.it); got != tc.cheap {
			t.Errorf("%s: shapeCheap = %v, want %v", tc.name, got, tc.cheap)
		}
	}
}

// THE BRIEF'S THINKING IS PER STEP, ONLY OVER THE STEPS THAT RUN: the same
// scalar for every running step, each step's own when they differ, nothing when
// nobody set any, and never the thinking of a step that is switched off as the
// job's.
func TestTheManagersBriefSaysThinkingPerStepAndNeverOneForAMixedJob(t *testing.T) {
	recipe := factory.DefaultRecipe()
	for _, c := range []struct {
		name  string
		think map[string]string
		off   map[string]bool
		want  string // the whole facts line; "" = no thinking anywhere on it
		stage string // a stages: line that must still say the step's own
	}{
		{"mixed", map[string]string{"plan": "cheap", "write": "strong", "test": "strong", "review": "strong", "proof": "strong"}, nil,
			"budget $5 · thinking plan cheap, write strong, test strong, review strong, proof strong", "effort cheap"},
		{"mixed with usual", map[string]string{"plan": "cheap", "write": "strong"}, nil,
			"budget $5 · thinking plan cheap, write strong, test usual, review usual, proof usual", ""},
		{"uniform", map[string]string{"plan": "strong", "write": "strong", "test": "strong", "review": "strong", "proof": "strong"}, nil,
			"budget $5 · thinking strong", ""},
		{"all usual", nil, nil, "budget $5", ""},
		// A STEP THAT IS OFF IS NOT THE JOB'S THINKING: the others are all usual.
		{"only an off step", map[string]string{"security": "cheap"}, nil, "budget $5", "effort cheap · off"},
		// AND AN OFF STEP DOES NOT BREAK A UNIFORM: the running ones agree.
		{"uniform beside an off step", map[string]string{"plan": "strong", "write": "strong", "test": "strong", "review": "strong", "proof": "strong", "security": "cheap"}, nil,
			"budget $5 · thinking strong", "effort cheap · off"},
	} {
		it := factory.Item{ID: 12, Repo: "acme/web", Title: "fix the ledger", Cap: 5, Stages: factory.CopyStages(recipe.For(factory.KindIssue))}
		for i, st := range it.Stages {
			it.Stages[i].Effort = c.think[st.Name]
		}
		brief := talkBrief(it, recipe)
		var facts string
		for _, line := range strings.Split(brief, "\n") {
			if strings.HasPrefix(line, "budget") {
				facts = line
			}
			if c.want == "" && strings.HasPrefix(line, "thinking") {
				t.Errorf("%s: a thinking line with nothing set: %q", c.name, line)
			}
		}
		if facts != c.want {
			t.Errorf("%s: facts line = %q, want %q", c.name, facts, c.want)
		}
		if c.stage != "" && !strings.Contains(brief, c.stage) {
			t.Errorf("%s: the stages block lost %q:\n%s", c.name, c.stage, brief)
		}
		if strings.Contains(brief, "thinking cheap") && c.name != "mixed" {
			t.Errorf("%s: the brief says the job thinks cheap:\n%s", c.name, brief)
		}
	}
}
