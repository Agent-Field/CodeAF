package session

// `factory_floor`, the foreman's read of the floor, held to its laws: on the
// belt only with a door; each ask answers its own table; marks go through the
// door by ref and only onto new items; nothing is said for what is not known.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/manual"
)

// fakeFloorDoor is a floor in memory that keeps its marks the way the store
// does, and records every Mark asked of it.
type fakeFloorDoor struct {
	snap  factory.Snapshot
	marks map[int]bool
	asked []string
}

func (d *fakeFloorDoor) Snapshot(context.Context) (factory.Snapshot, error) {
	snap := d.snap
	snap.Items = append([]factory.Item(nil), d.snap.Items...)
	var ids []int
	for id, on := range d.marks {
		if on {
			ids = append(ids, id)
		}
	}
	factory.FoldMarks(&snap, ids)
	return snap, nil
}

func (d *fakeFloorDoor) Mark(_ context.Context, ids []int, on bool) error {
	if d.marks == nil {
		d.marks = map[int]bool{}
	}
	for _, id := range ids {
		d.marks[id] = on
		word := "mark"
		if !on {
			word = "unmark"
		}
		d.asked = append(d.asked, word+" "+itoaFloor(id))
	}
	return nil
}

func itoaFloor(n int) string { return strings.TrimPrefix(factory.Item{ID: n}.Ref(), "#") }

// floorLab is a floor of six items a morning would choose among: three new
// bugs at three prices, a risky new feature, one waiting on the person and one
// running, plus a hidden one the foreman never sees.
func floorLab() *fakeFloorDoor {
	now := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	return &fakeFloorDoor{snap: factory.Snapshot{Now: now, Items: []factory.Item{
		{ID: 1, Num: 1, Repo: "acme/api", Kind: factory.KindIssue, Title: "ledger counts twice", State: factory.StateNew, Created: now.Add(-9 * day),
			Body: strings.Repeat("x", 3000), Author: "alice",
			Triage: factory.Triage{Type: "bug", Size: "S", Est: 0.4, Priority: 1, Reason: "money is wrong", Risk: []string{"touches money"}, Read: "A double count in the ledger."}},
		{ID: 2, Num: 4, Repo: "acme/api", Kind: factory.KindIssue, Title: "typo on the login page", State: factory.StateNew, Created: now.Add(-2 * day),
			Triage: factory.Triage{Type: "bug", Size: "S", Est: 0.1}},
		{ID: 3, Num: 6, Repo: "acme/api", Kind: factory.KindIssue, Title: "meter rounds wrong", State: factory.StateNew, Created: now.Add(-5 * day),
			Triage: factory.Triage{Type: "bug", Size: "M", Est: 1.2}},
		{ID: 4, Num: 7, Repo: "acme/api", Kind: factory.KindIssue, Title: "new auth flow", State: factory.StateNew, Created: now.Add(-1 * day),
			Triage: factory.Triage{Type: "feat", Size: "L", Est: 6}},
		{ID: 5, Num: 3, Repo: "acme/api", Kind: factory.KindPR, Title: "bump the parser", State: factory.StateNeedsYou, Question: "ship it with one red check?", Created: now.Add(-3 * time.Hour)},
		{ID: 6, Num: 9, Repo: "acme/api", Kind: factory.KindIssue, Title: "cache warmup", State: factory.StateRunning, Created: now.Add(-40 * time.Minute)},
		{ID: 7, Num: 2, Repo: "acme/api", Kind: factory.KindIssue, Title: "hidden thing", State: factory.StateDismissed},
	}}}
}

func floorCall(t *testing.T, door *fakeFloorDoor, args string) (string, bool) {
	t.Helper()
	agent := &Agent{config: Config{Workspace: t.TempDir(), Floor: door}}
	text, isErr, err := agent.factoryFloorTool().Execute(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	return text, isErr
}

func TestFactoryFloorIsOnTheBeltOnlyWithItsDoor(t *testing.T) {
	without := &Agent{config: Config{Workspace: t.TempDir(), Factory: &fakeFactoryDoor{}}}
	without.tools = without.belt()
	for _, tool := range without.offeredTools() {
		if tool.Name == "factory_floor" {
			t.Fatal("a belt with no floor door carries factory_floor")
		}
	}
	with := &Agent{config: Config{Workspace: t.TempDir(), Factory: &fakeFactoryDoor{}, Floor: floorLab()}}
	with.tools = with.belt()
	found := false
	for _, tool := range with.offeredTools() {
		if tool.Name != "factory_floor" {
			continue
		}
		found = true
		for _, want := range []string{"foreman", "the person runs what is selected with L", "nothing you do here runs, ships or posts"} {
			if !strings.Contains(tool.Description, want) {
				t.Errorf("the description lacks %q: %q", want, tool.Description)
			}
		}
		if !manual.Chat().Mentions(tool.Name) {
			t.Errorf("no chat manual page mentions the %s tool", tool.Name)
		}
	}
	if !found {
		t.Fatal("a belt with a floor door does not carry factory_floor")
	}
	if got := ActionCategoryForTool("factory_floor"); got != ActionRead {
		t.Errorf("factory_floor's family is %q, want %q", got, ActionRead)
	}
}

// EACH ASK ANSWERS ITS OWN TABLE, by ref, over the floor as it is: the hidden
// item never shows, the ranked asks keep their order, and a fact nobody knows
// is not written.
func TestFactoryFloorEachAskShapesItsTable(t *testing.T) {
	for _, c := range []struct {
		ask        string
		want, not  []string
		firstOrder []string
	}{
		{"waiting", []string{"waiting on the person", "#3 bump the parser"}, []string{"#1 ", "#9 "}, nil},
		{"risky", []string{"#1 ledger counts twice", "#7 new auth flow", "risk: touches money"}, []string{"#4 ", "#3 "}, []string{"#1 ", "#7 "}},
		{"cheapest", []string{"cheapest first"}, []string{"#3 ", "#9 "}, []string{"#4 ", "#1 ", "#6 ", "#7 "}},
		{"oldest", []string{"oldest first", "9d old"}, []string{"#2 "}, []string{"#1 ", "#6 ", "#4 ", "#7 ", "#3 ", "#9 "}},
		{"all", []string{"every open item", "6 items", "4 new on the floor", "1 waiting on the person", "#9 cache warmup", "40m old"}, []string{"hidden thing", "$0.00", "est $0.0000"}, nil},
	} {
		text, isErr := floorCall(t, floorLab(), `{"ask":"`+c.ask+`"}`)
		if isErr {
			t.Fatalf("%s: refused: %s", c.ask, text)
		}
		for _, w := range c.want {
			if !strings.Contains(text, w) {
				t.Errorf("%s: the answer lacks %q:\n%s", c.ask, w, text)
			}
		}
		for _, w := range c.not {
			if strings.Contains(text, w) {
				t.Errorf("%s: the answer carries %q:\n%s", c.ask, w, text)
			}
		}
		at := -1
		for _, ref := range c.firstOrder {
			i := strings.Index(text, "\n"+ref)
			if i <= at {
				t.Errorf("%s: %s is out of order:\n%s", c.ask, ref, text)
			}
			at = i
		}
	}
}

// `item` IS ONE ITEM IN FULL: its row, its read, its facts, its question and
// its body cut to two thousand characters.
func TestFactoryFloorItemAskIsTheItemInFull(t *testing.T) {
	text, isErr := floorCall(t, floorLab(), `{"ask":"item","item":1}`)
	if isErr {
		t.Fatal(text)
	}
	for _, want := range []string{"#1 ledger counts twice", "est $0.40", "p1 money is wrong", "read: A double count in the ledger.", "author alice", "body:"} {
		if !strings.Contains(text, want) {
			t.Errorf("the item lacks %q:\n%s", want, text)
		}
	}
	if n := strings.Count(text, "x"); n > floorBodyCut+1 || n < floorBodyCut-1 {
		t.Errorf("the body carries %d characters, want it cut to %d", n, floorBodyCut)
	}
	text, _ = floorCall(t, floorLab(), `{"ask":"item","item":3}`)
	if !strings.Contains(text, "waiting on the person for: ship it with one red check?") {
		t.Errorf("a waiting item does not say its question:\n%s", text)
	}
	if text, _ := floorCall(t, floorLab(), `{"ask":"item","item":42}`); text != "there is no #42 on the floor" {
		t.Errorf("an unknown ref answered %q", text)
	}
	if _, isErr := floorCall(t, floorLab(), `{"ask":"item"}`); !isErr {
		t.Error("ask item with no item was not refused")
	}
}

// MARKS GO THROUGH THE DOOR, BY REF, ONTO NEW ITEMS ONLY, and the answer is
// the handoff sentence; the next read shows them marked, and unmark takes
// them off.
func TestFactoryFloorMarksThroughTheDoor(t *testing.T) {
	door := floorLab()
	text, isErr := floorCall(t, door, `{"mark":[1,4,6,9,42]}`)
	if isErr {
		t.Fatal(text)
	}
	if !strings.HasPrefix(text, "selected #1 #4 #6"+FloorMarkedTail) {
		t.Errorf("the mark answered %q", text)
	}
	for _, want := range []string{"#9 is running, and only a new item can be selected", "there is no #42 on the floor"} {
		if !strings.Contains(text, want) {
			t.Errorf("the mark does not say %q:\n%s", want, text)
		}
	}
	if got := strings.Join(door.asked, ","); got != "mark 1,mark 2,mark 3" {
		t.Errorf("the door was asked %q, want the three new items by their floor ids", got)
	}
	table, _ := floorCall(t, door, `{"ask":"all"}`)
	if !strings.Contains(table, "3 selected") || !strings.Contains(table, "#4 typo on the login page · issue · acme/api · size S · est $0.10 · new, selected") {
		t.Errorf("the next read does not show the marks:\n%s", table)
	}
	text, _ = floorCall(t, door, `{"unmark":[4],"ask":"all"}`)
	if !strings.HasPrefix(text, "unselected #4") || !strings.Contains(text, "2 selected") {
		t.Errorf("unmark answered:\n%s", text)
	}
}
