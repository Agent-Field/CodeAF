package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/manual"
)

// fakeRunDoor keeps the edits it was handed and answers lines or a refusal.
type fakeRunDoor struct {
	mu     sync.Mutex
	asked  []string
	edits  []factory.RunEdit
	lines  []string
	refuse error
}

func (d *fakeRunDoor) EditRun(_ context.Context, conversation string, e factory.RunEdit) (factory.Item, []string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.asked = append(d.asked, conversation)
	d.edits = append(d.edits, e)
	if d.refuse != nil {
		return factory.Item{}, nil, d.refuse
	}
	return factory.Item{ID: 3}, d.lines, nil
}

func runToolCall(t *testing.T, a *Agent, args string) (string, bool) {
	t.Helper()
	out, isErr, err := a.factoryRunTool().Execute(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	return out, isErr
}

// THE TOOL BUILDS THE MANAGER'S EDIT, asks the door with its own conversation,
// and answers the one line the item's page shows.
func TestFactoryRunBuildsTheEditAndAnswersTheLine(t *testing.T) {
	door := &fakeRunDoor{lines: []string{
		"manager set review: read it for security, code and architecture",
		"manager added arch after review",
		"why: the person asked for a thorough review",
	}}
	a := &Agent{config: Config{FactoryRun: door, SessionFile: "/tmp/manager/transcript.jsonl"}}
	out, isErr := runToolCall(t, a, `{
		"set":[{"stage":"Review","ask":"read it for security,  code and architecture"},{"stage":"test","thinking":"strong"}],
		"add":[{"name":"arch","ask":"say whether the shape holds","after":"review","why":"no stage reads the architecture"}],
		"skip":["neaten"],
		"why":"the person asked for a thorough review"}`)
	if isErr {
		t.Fatalf("refused: %s", out)
	}
	if want := "manager set review: read it for security, code and architecture · added arch after review · why: the person asked for a thorough review"; out != want {
		t.Fatalf("answered %q, want %q", out, want)
	}
	if len(door.edits) != 1 || door.asked[0] != "/tmp/manager/transcript.jsonl" {
		t.Fatalf("the door was asked %v by %v", door.edits, door.asked)
	}
	e := door.edits[0]
	if e.By != "manager" || e.Why != "the person asked for a thorough review" {
		t.Errorf("by %q why %q", e.By, e.Why)
	}
	if e.Ask["review"] != "read it for security, code and architecture" || e.Thinking["test"] != "strong" || len(e.Ask) != 1 {
		t.Errorf("asks %v thinking %v", e.Ask, e.Thinking)
	}
	if len(e.Add) != 1 || e.Add[0].Stage.Name != "arch" || e.Add[0].After != "review" || e.Add[0].Stage.Ask != "say whether the shape holds" || !e.Add[0].Stage.On || e.Add[0].Stage.Why != "no stage reads the architecture" {
		t.Errorf("added %+v", e.Add)
	}
	if len(e.Skip) != 1 || e.Skip[0] != "neaten" {
		t.Errorf("skip %v", e.Skip)
	}
}

// A REFUSAL IS THE DOOR'S OWN SENTENCE, and nothing changed; a name that is
// not one word, or a change of nothing, never reaches the door.
func TestFactoryRunRefusesInTheDoorsWordsAndChecksItsArguments(t *testing.T) {
	door := &fakeRunDoor{refuse: errors.New("proof is never skipped")}
	a := &Agent{config: Config{FactoryRun: door}}
	if out, isErr := runToolCall(t, a, `{"skip":["proof"]}`); !isErr || out != "nothing changed: proof is never skipped" {
		t.Fatalf("a refusal answered %q (%v)", out, isErr)
	}
	door.refuse = nil
	for _, args := range []string{`{}`, `{"add":[{"name":"two words","ask":"x","after":"review"}]}`, `{"set":[{"stage":"review"}]}`, `{"add":[{"name":"arch","ask":" ","after":"review"}]}`} {
		before := len(door.edits)
		if out, isErr := runToolCall(t, a, args); !isErr || !strings.HasPrefix(out, "Invalid arguments: ") || len(door.edits) != before {
			t.Errorf("%s answered %q (%v) and reached the door %d times", args, out, isErr, len(door.edits)-before)
		}
	}
	if out, _ := runToolCall(t, a, `{"set":[{"stage":"review","ask":"same as before"}]}`); out != runAlready {
		t.Errorf("an edit that changed nothing answered %q", out)
	}
}

// `factory_run` IS ON THE BELT ONLY WITH ITS DOOR, its description says no card
// is shown before a run, and the manual names it.
func TestFactoryRunIsOnTheBeltOnlyWithItsDoor(t *testing.T) {
	without := &Agent{config: Config{Workspace: t.TempDir(), Factory: &fakeFactoryDoor{}}}
	without.tools = without.belt()
	for _, tool := range without.offeredTools() {
		if tool.Name == "factory_run" {
			t.Fatal("a belt with no run door carries factory_run")
		}
	}
	with := &Agent{config: Config{Workspace: t.TempDir(), FactoryRun: &fakeRunDoor{}}}
	with.tools = with.belt()
	found := false
	for _, tool := range with.offeredTools() {
		if tool.Name != "factory_run" {
			continue
		}
		found = true
		for _, want := range []string{"NO CARD IS SHOWN", "one lowercase word each, at most nine", "only the stages that have not started", "Proof and a gate stage are never skipped"} {
			if !strings.Contains(tool.Description, want) {
				t.Errorf("the description does not say %q", want)
			}
		}
	}
	if !found {
		t.Fatal("a belt with a run door does not carry factory_run")
	}
	if !manual.Chat().Mentions("factory_run") {
		t.Error("no chat manual page mentions factory_run")
	}
	if got := ActionCategoryForTool("factory_run"); got != ActionEdit {
		t.Errorf("factory_run's family is %q, want %q", got, ActionEdit)
	}
}

// THE LINE says who once: `manager set … · added … · why: …`.
func TestRunEditLineSaysWhoOnce(t *testing.T) {
	got := RunEditLine([]string{"manager set review: x", "manager added arch after review", "manager skipped neaten", "why: y"})
	if got != "manager set review: x · added arch after review · skipped neaten · why: y" {
		t.Fatalf("line = %q", got)
	}
	if RunEditLine(nil) != "" {
		t.Fatal("no lines is a line")
	}
}

// A DOOR LENT TO THE SHAPING TURN answers that turn's `factory_run` and no
// other: the next turn, and a conversation between turns, get the floor's.
func TestFactoryRunLentDoorIsTheTurnsAlone(t *testing.T) {
	floor := &fakeRunDoor{lines: []string{"manager set review: by the floor"}}
	lent := &fakeRunDoor{lines: []string{"manager set review: collected"}}
	a := &Agent{config: Config{FactoryRun: floor, SessionFile: "/tmp/manager/transcript.jsonl"}}
	a.running, a.teamTurnSerial = true, 7
	turnRunDoors.Store(a, turnRunDoor{door: lent, serial: 7})
	t.Cleanup(func() { turnRunDoors.Delete(a) })
	if out, _ := runToolCall(t, a, `{"set":[{"stage":"review","ask":"x"}]}`); out != "manager set review: collected" {
		t.Fatalf("the lent turn answered %q", out)
	}
	a.teamTurnSerial = 8
	if out, _ := runToolCall(t, a, `{"set":[{"stage":"review","ask":"x"}]}`); out != "manager set review: by the floor" {
		t.Fatalf("the next turn answered %q", out)
	}
	if len(lent.edits) != 1 || len(floor.edits) != 1 {
		t.Fatalf("lent %d, floor %d", len(lent.edits), len(floor.edits))
	}
	// A CONVERSATION MID-TURN refuses the runner's ask; it never steers it.
	if _, err := a.SubmitRunnerNoteThrough(context.Background(), "Shape the run for this item now.", lent); !errors.Is(err, ErrConversationBusy) {
		t.Fatalf("a running conversation took the ask: %v", err)
	}
}

// LIVE IS A CONVERSATION OPEN HERE ON THAT JOURNAL, and not once it closed.
func TestLiveAgentForIsTheOpenConversation(t *testing.T) {
	path := t.TempDir() + "/transcript.jsonl"
	if _, ok := LiveAgentFor(path); ok {
		t.Fatal("a journal nobody holds read as live")
	}
	a := &Agent{}
	rememberLiveJournal(path, a)
	t.Cleanup(func() { forgetLiveJournal(a) })
	if got, ok := LiveAgentFor(path); !ok || got != a {
		t.Fatalf("live %v %v", got, ok)
	}
	a.closed = true
	if _, ok := LiveAgentFor(path); ok {
		t.Fatal("a closed conversation read as live")
	}
}
