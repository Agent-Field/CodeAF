package session

// `factory_floor`, the foreman's read of the floor, held to its laws: on the
// belt only with a door; each ask answers its own table; marks go through the
// door by ref and only onto new items; nothing is said for what is not known.

import (
	"context"
	"encoding/json"
	"strconv"
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

func itoaFloor(n int) string { return strconv.Itoa(n) }

// floorLab is a floor of six items a morning would choose among: three new
// bugs at three prices, a risky new feature, one waiting on the person and one
// running, plus a hidden one the foreman never sees.
func floorLab() *fakeFloorDoor {
	now := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	return &fakeFloorDoor{snap: factory.Snapshot{Now: now, Items: []factory.Item{
		{ID: 1, Num: 1, Repo: "acme/api", Kind: factory.KindIssue, Title: "ledger counts twice", State: factory.StateNew, Created: now.Add(-9 * day),
			Body: strings.Repeat("x", 3000), Author: "alice",
			Triage: factory.Triage{Type: "bug", Size: "S", SizeFrom: factory.SizeFromRead, TriagedAt: now, Rubric: floorRubric(3), Priority: 1, Reason: "money is wrong", Risk: []string{"touches money"}, Read: "A double count in the ledger."}},
		{ID: 2, Num: 4, Repo: "acme/api", Kind: factory.KindIssue, Title: "typo on the login page", State: factory.StateNew, Created: now.Add(-2 * day),
			Triage: factory.Triage{Type: "bug", Size: "S", SizeFrom: factory.SizeFromRead, TriagedAt: now, Rubric: floorRubric(1)}},
		{ID: 3, Num: 6, Repo: "acme/api", Kind: factory.KindIssue, Title: "meter rounds wrong", State: factory.StateNew, Created: now.Add(-5 * day),
			Triage: factory.Triage{Type: "bug", Size: "M", SizeFrom: factory.SizeFromRead, TriagedAt: now, Rubric: floorRubric(5)}},
		{ID: 4, Num: 7, Repo: "acme/api", Kind: factory.KindIssue, Title: "new auth flow", State: factory.StateNew, Created: now.Add(-1 * day),
			Triage: factory.Triage{Type: "feat", Size: "L", SizeFrom: factory.SizeFromRead, TriagedAt: now, Rubric: floorRubric(8)}},
		{ID: 5, Num: 3, Repo: "acme/api", Kind: factory.KindPR, Title: "bump the parser", State: factory.StateNeedsYou, Question: "ship it with one red check?", Created: now.Add(-3 * time.Hour)},
		{ID: 6, Num: 9, Repo: "acme/api", Kind: factory.KindIssue, Title: "cache warmup", State: factory.StateRunning, Created: now.Add(-40 * time.Minute)},
		{ID: 7, Num: 2, Repo: "acme/api", Kind: factory.KindIssue, Title: "hidden thing", State: factory.StateDismissed},
	}}}
}

// floorRubric is a read's rubric whose complexity is sum, spread over the
// factors in order.
func floorRubric(sum int) *factory.Rubric {
	r := &factory.Rubric{From: factory.RubricFromRead}
	for _, f := range r.Factors() {
		f.Score = min(sum, factory.FactorMost)
		sum -= f.Score
	}
	return r
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
		for _, want := range []string{"foreman", "only when the person asks", "nothing here ships, merges or posts"} {
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
		{"cheapest", []string{"least complex first"}, []string{"#3 ", "#9 "}, []string{"#4 ", "#1 ", "#6 ", "#7 "}},
		{"oldest", []string{"oldest first", "9d old"}, []string{"#2 "}, []string{"#1 ", "#6 ", "#4 ", "#7 ", "#3 ", "#9 "}},
		{"all", []string{"every open item", "6 items", "4 new in the factory", "1 waiting on the person", "#9 cache warmup", "40m old"}, []string{"hidden thing", "$0.00", "est $0.0000"}, nil},
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
	for _, want := range []string{"#1 ledger counts twice", "complexity 3/10", "p1 money is wrong", "read: A double count in the ledger.", "author alice", "body:"} {
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
	if text, _ := floorCall(t, floorLab(), `{"ask":"item","item":42}`); text != "there is no #42 in the factory" {
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
	for _, want := range []string{"#9 is running, and only a new item can be selected", "there is no #42 in the factory"} {
		if !strings.Contains(text, want) {
			t.Errorf("the mark does not say %q:\n%s", want, text)
		}
	}
	if got := strings.Join(door.asked, ","); got != "mark 1,mark 2,mark 3" {
		t.Errorf("the door was asked %q, want the three new items by their floor ids", got)
	}
	table, _ := floorCall(t, door, `{"ask":"all"}`)
	if !strings.Contains(table, "3 selected") || !strings.Contains(table, "#4 typo on the login page · issue · acme/api · read says S · complexity 1/10, small (read) · new, selected") {
		t.Errorf("the next read does not show the marks:\n%s", table)
	}
	text, _ = floorCall(t, door, `{"unmark":[4],"ask":"all"}`)
	if !strings.HasPrefix(text, "unselected #4") || !strings.Contains(text, "2 selected") {
		t.Errorf("unmark answered:\n%s", text)
	}
}

// THE FLOOR SAYS WHERE A SIZE CAME FROM, and says nothing when there is none:
// a label is "size S", a completed read "read says S", a pull request's
// measured lines "size S (diff)"; a legacy size and a chat guess are blank.
func TestFloorRowNamesTheSourceOfASize(t *testing.T) {
	now := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	read := factory.Triage{Size: "S", SizeFrom: factory.SizeFromRead, TriagedAt: now}
	for _, tc := range []struct {
		name string
		it   factory.Item
		want string
	}{
		{"a label", factory.Item{Labels: []string{"size:m"}, Triage: read}, "size M"},
		{"a read", factory.Item{Triage: read}, "read says S"},
		{"a diff", factory.Item{Kind: factory.KindPR, Triage: factory.Triage{Size: "M", SizeFrom: factory.SizeFromDiff}}, "size M (diff)"},
	} {
		tc.it.ID, tc.it.Num, tc.it.Title = 1, 1, "t"
		if row := FloorRow(tc.it, now); !strings.Contains(row, tc.want) {
			t.Errorf("%s: %q lacks %q", tc.name, row, tc.want)
		}
	}
	for name, tr := range map[string]factory.Triage{
		"a legacy size": {Size: "S", TriagedAt: now},
		"a chat guess":  {Size: "S", SizeFrom: factory.SizeFromChat, TriagedAt: now},
		"an unfinished": {Size: "S", SizeFrom: factory.SizeFromRead},
	} {
		it := factory.Item{ID: 1, Num: 1, Title: "t", Triage: tr}
		if row := FloorRow(it, now); strings.Contains(row, "size") || strings.Contains(row, "says") {
			t.Errorf("%s: %q says a size", name, row)
		}
	}
}

// actingFloorDoor is a floor that also starts and prioritises, recording each.
type actingFloorDoor struct {
	*fakeFloorDoor
	acted []string
}

func (d *actingFloorDoor) StartItem(_ context.Context, id int) (string, error) {
	d.acted = append(d.acted, "start "+itoaFloor(id))
	return "#" + itoaFloor(id) + " started", nil
}

func (d *actingFloorDoor) SetPriority(_ context.Context, id int, value string) (string, error) {
	d.acted = append(d.acted, "priority "+itoaFloor(id)+" "+value)
	return "set", nil
}

// THE FACTORY-WIDE MANAGER READS EVERY JOB AND ACTS ON ONE WHEN ASKED. The
// audit's "what's running?", "start the oldest p1" and "what did we ship
// today?" had no answer anywhere; running, queue and shipped answer them from
// the floor, start and priority reach the job by the ref the person read, and
// a floor that cannot act says so and changes nothing.
func TestTheForemanReadsRunningQueueAndShippedAndStartsByRef(t *testing.T) {
	door := floorLab()
	now := door.snap.Now
	door.snap.Items[5].Stream = &factory.Stream{Cur: 1, Phases: []factory.Phase{{Name: "plan", State: factory.PhaseDone}, {Name: "write", State: factory.PhaseRunning}, {Name: "proof", State: factory.PhasePending}}}
	door.snap.Items = append(door.snap.Items, factory.Item{ID: 8, Num: 11, Repo: "acme/api", Kind: factory.KindIssue, Title: "add Ledger.Largest", State: factory.StateShipped, DoneAt: now.Add(-time.Hour), DoneHow: "merged",
		PullRequest: &factory.PullRequest{Number: 161, URL: "https://github.com/acme/api/pull/161", State: factory.PRMerged}, Change: &factory.Change{Stat: &factory.ChangeStat{Files: 3, Added: 83, Removed: 1}}},
		factory.Item{ID: 9, Num: 12, Repo: "acme/api", Kind: factory.KindIssue, Title: "old shipped", State: factory.StateShipped, DoneAt: now.Add(-48 * time.Hour)})
	door.snap.Items[2].Priority = &factory.JobPriority{Value: "high"}
	door.snap.Items = append(door.snap.Items, factory.Item{ID: 10, Num: 13, Repo: "acme/api", Kind: factory.KindIssue, Title: "following its PR", State: factory.StateShipped,
		PullRequest: &factory.PullRequest{Number: 165, State: factory.PROpen, Draft: true}},
		// The live lane's job: its pull request's state not read yet, waiting
		// on Mark ready. It is still the factory's work.
		factory.Item{ID: 11, Num: 14, Repo: "acme/api", Kind: factory.KindIssue, Title: "pull request unread", State: factory.StateNeedsYou,
			PullRequest: &factory.PullRequest{Number: 166, Draft: true}},
		factory.Item{ID: 12, Num: 15, Repo: "acme/api", Kind: factory.KindIssue, Title: "closed pull request", State: factory.StateShipped,
			PullRequest: &factory.PullRequest{Number: 167, State: factory.PRClosed}})
	text, _ := floorCall(t, door, `{"ask":"running"}`)
	if !strings.Contains(text, "#9 cache warmup") || !strings.Contains(text, "steps 1 of 3 done, on write") || strings.Contains(text, "#1 ") || !strings.Contains(text, "#13 following its PR") || !strings.Contains(text, "#14 pull request unread") || strings.Contains(text, "#15 ") {
		t.Errorf("running:\n%s", text)
	}
	text, _ = floorCall(t, door, `{"ask":"queue"}`)
	if lines := strings.Split(text, "\n"); len(lines) < 2 || !strings.HasPrefix(lines[1], "#6 meter rounds wrong") || !strings.Contains(lines[1], "priority p1 (high)") {
		t.Errorf("queue does not lead with the p1:\n%s", text)
	}
	text, _ = floorCall(t, door, `{"ask":"shipped"}`)
	if !strings.Contains(text, "shipped today · 1 job") || !strings.Contains(text, "pull/161") || !strings.Contains(text, "3 files, +83 −1") || strings.Contains(text, "old shipped") {
		t.Errorf("shipped:\n%s", text)
	}
	if text, bad := floorCall(t, door, `{"start":6}`); !bad || !strings.Contains(text, "Nothing changed") {
		t.Errorf("a floor that cannot act started a job: %q", text)
	}
	acting := &actingFloorDoor{fakeFloorDoor: door}
	agent := &Agent{config: Config{Workspace: t.TempDir(), Floor: acting}}
	for _, args := range []string{`{"start":6}`, `{"item":4,"priority":"p0"}`} {
		if _, bad, err := agent.factoryFloorTool().Execute(context.Background(), json.RawMessage(args)); bad || err != nil {
			t.Fatalf("%s refused: %v", args, err)
		}
	}
	if strings.Join(acting.acted, ",") != "start 3,priority 2 urgent" {
		t.Fatalf("acted %v, want the jobs by their ids", acting.acted)
	}
}
