package tui3

// A run's task in the task room (planroom.go): what the room reads from the
// store, what it draws of it, and the laws #1420's page kept that the room
// keeps now.

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/session"
)

// openPlanRoomNow opens one store task's room and answers the read it asks for.
func openPlanRoomNow(t *testing.T, a *app, id string) {
	t.Helper()
	spend(t, a, a.openRailPlan(id, nil))
}

// planRoomText is the whole frame while a room is open, without its inks.
func planRoomText(t *testing.T, a *app) string {
	t.Helper()
	frame, _, _ := a.frame()
	return plain(frame)
}

// errRefusedForTest is a store refusal a test puts in front of the room.
var errRefusedForTest = errors.New("that task has ended and takes no more notes")

// planWorkFake is a plan agent that also reads the run's working copy.
type planWorkFake struct {
	*planFake
	work  session.PlanTaskWork
	reads int
}

func (f *planWorkFake) PlanTaskWork(string) (session.PlanTaskWork, bool) {
	f.reads++
	return f.work, true
}

func TestTheRunsTaskRoomDrawsItsStepsAsTheRoomsShellCalls(t *testing.T) {
	row := session.PlanTaskRow{ID: "t-6", Title: "fix the loader", Status: "running", Started: taskFixtureNow.Add(-3 * 60e9)}
	row.Live.Step, row.Live.Command = 3, "go test ./internal/config/..."
	page := session.PlanTaskPage{
		Row:         row,
		Description: "make the loader read the new key",
		Steps: []session.PlanStep{
			{Kind: "step", Step: 1, Command: "cat internal/config/load.go", Observation: "package config"},
			{Kind: "step", Step: 2, Command: "sed -i s/old/new/ internal/config/load.go"},
		},
		Live: row.Live,
	}
	a, _ := planAppWith(t, []session.PlanTaskRow{row}, map[string]session.PlanTaskPage{row.ID: page})
	a.height = 40
	openPlanRoomNow(t, a, row.ID)
	if a.room == nil || a.room.plan == nil {
		t.Fatal("the run's task opened no room")
	}
	text := planRoomText(t, a)
	if !strings.Contains(text, "make the loader read the new key") {
		t.Fatalf("the room does not open on the brief:\n%s", text)
	}
	want := []string{"cat internal/config/load.go", "sed -i s/old/new/ internal/config/load.go", "go test ./internal/config/..."}
	if got := roomCommands(a); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("the room's calls are %q, want %q", got, want)
	}
	// THE ROOM'S OWN HEAD: the state, the clock, the steps counted as calls.
	if !strings.Contains(text, "working") || !strings.Contains(text, "3m") {
		t.Fatalf("the room's head does not say the state and the clock:\n%s", text)
	}
}

// #1420's two laws: a live step with no command is no line, and a live step
// with no step number is not a live step.
func TestTheRunsTaskRoomDrawsNoLiveRowWithoutACommandOrAStep(t *testing.T) {
	for _, live := range []plandb.LiveStep{{Step: 4}, {Command: "go vet ./..."}} {
		row := session.PlanTaskRow{ID: "t-6", Title: "fix the loader", Status: "running", Live: live}
		page := session.PlanTaskPage{Row: row, Description: "the brief", Live: live}
		a, _ := planAppWith(t, []session.PlanTaskRow{row}, map[string]session.PlanTaskPage{row.ID: page})
		openPlanRoomNow(t, a, row.ID)
		for _, e := range a.room.entries {
			if e.kind == entryTool {
				t.Fatalf("a live step %+v drew a call row: %+v", live, e)
			}
		}
		if text := planRoomText(t, a); strings.Contains(text, "◑ $") || strings.Contains(text, "go vet") {
			t.Fatalf("a live step %+v drew a live row:\n%s", live, text)
		}
	}
}

func TestTheRunsTaskRoomDrawsARefusedActionAsOneLineAndACorrectionAsNone(t *testing.T) {
	row := session.PlanTaskRow{ID: "t-6", Title: "fix the loader", Status: "running"}
	page := session.PlanTaskPage{Row: row, Description: "the brief", Steps: []session.PlanStep{
		{Kind: "step", Step: 1, Command: "rm -rf /", NotRun: true, Refused: true},
		{Kind: "step", Step: 2, Command: "reply in the form", NotRun: true},
	}}
	a, _ := planAppWith(t, []session.PlanTaskRow{row}, map[string]session.PlanTaskPage{row.ID: page})
	openPlanRoomNow(t, a, row.ID)
	text := planRoomText(t, a)
	if !strings.Contains(text, taskPlanRefusedWord+railSep+"rm -rf /") {
		t.Fatalf("the refused action is not one line:\n%s", text)
	}
	if strings.Contains(text, "reply in the form") {
		t.Fatalf("a correction about the form of a reply was drawn:\n%s", text)
	}
}

// THE HEAD SAYS TOKENS AND MODEL WHERE THE STORE RECORDS THEM, AND NOTHING
// WHERE IT DOES NOT — never a blank and never a zero.
func TestTheRunsTaskRoomHeadDropsTokensAndModelTheStoreHasNot(t *testing.T) {
	known := session.PlanTaskRow{ID: "t-6", Title: "fix the loader", Status: "done", USD: 0.04,
		Model: "deepseek/deepseek-v4-flash", Tokens: 12400,
		Started: taskFixtureNow.Add(-120e9), Ended: taskFixtureNow.Add(-60e9)}
	a, _ := planAppWith(t, []session.PlanTaskRow{known}, map[string]session.PlanTaskPage{known.ID: {Row: known, Description: "b"}})
	openPlanRoomNow(t, a, known.ID)
	head := strings.Join(a.roomHeadRows(a.width), "\n")
	for _, want := range []string{"$0.04", "12.4k tok", "deepseek-v4-flash", "1m"} {
		if !strings.Contains(plain(head), want) {
			t.Fatalf("the head does not say %q:\n%s", want, plain(head))
		}
	}
	unknown := session.PlanTaskRow{ID: "t-7", Title: "fix the loader", Status: "done"}
	a, _ = planAppWith(t, []session.PlanTaskRow{unknown}, map[string]session.PlanTaskPage{unknown.ID: {Row: unknown, Description: "b"}})
	openPlanRoomNow(t, a, unknown.ID)
	head = plain(strings.Join(a.roomHeadRows(a.width), "\n"))
	for _, never := range []string{"$0", "0 tok", " tok", "task "} {
		if strings.Contains(head, never) {
			t.Fatalf("the head draws %q for a figure the store has not got:\n%s", never, head)
		}
	}
}

// A TALL WINDOW DRAWS THE MODEL ON THE TITLE ROW. That is the header a person
// actually sits in front of, and it is the row that used to keep the price and
// the tokens and leave the model off.
func TestAnOrganizedRunsTaskRoomHeadNamesTheModel(t *testing.T) {
	row := session.PlanTaskRow{ID: "t-6", Title: "fix the loader", Status: "running", USD: 0.0017,
		Model: "z-ai/glm-5.3-flash", Tokens: 21000,
		Started: taskFixtureNow.Add(-20e9)}
	a, _ := planAppWith(t, []session.PlanTaskRow{row}, map[string]session.PlanTaskPage{row.ID: {Row: row, Description: "b"}})
	a.width, a.height = 120, 40
	openPlanRoomNow(t, a, row.ID)
	if !a.roomOrganized() {
		t.Fatal("120×40 must be the organized layout the title row is drawn for")
	}
	head := plain(strings.Join(a.roomHeadRows(a.width), "\n"))
	if !strings.Contains(head, "z-ai/glm-5.3-flash") {
		t.Fatalf("the organized head does not name the model:\n%s", head)
	}
	if strings.Contains(head, " tok") && !strings.Contains(head, "21k tok") && !strings.Contains(head, "21.0k tok") {
		t.Fatalf("the organized head lost the tokens beside the model:\n%s", head)
	}
}

// AN ENGINE THAT ANSWERS THE DOOR AND SAYS IT HAS NONE still draws the absence
// sentence. That is an older engine on the host road, not a copy that is gone.
func TestTheWorkTabDrawsTheAbsenceSentenceWhenTheEngineHasNoDoor(t *testing.T) {
	row := session.PlanTaskRow{ID: "t-6", Title: "fix the loader", Status: "running"}
	fake := &planWorkFake{work: session.PlanTaskWork{NoDoor: true}}
	a, plan := planAppWith(t, []session.PlanTaskRow{row}, map[string]session.PlanTaskPage{row.ID: {Row: row, Description: "b"}})
	fake.planFake = plan
	a.agent = fake
	openPlanRoomNow(t, a, row.ID)
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyTab})
	text := planRoomText(t, a)
	if !strings.Contains(text, planWorkNoDoorWord) {
		t.Fatalf("the work tab did not say the engine has no door:\n%s", text)
	}
}

func TestTheWorkTabOfARunsTaskDrawsTheRunsWorkingCopyDifference(t *testing.T) {
	row := session.PlanTaskRow{ID: "t-6", Title: "fix the loader", Status: "running"}
	fake := &planWorkFake{work: session.PlanTaskWork{
		Dir: "/work/copy", Read: true, Added: []string{"notes.txt"},
		Patch: "diff --git a/load.go b/load.go\nindex 1..2 100644\n--- a/load.go\n+++ b/load.go\n@@ -1 +1 @@\n-old line\n+new line\n",
	}}
	a, plan := planAppWith(t, []session.PlanTaskRow{row}, map[string]session.PlanTaskPage{row.ID: {Row: row, Description: "b"}})
	fake.planFake = plan
	a.agent = fake
	openPlanRoomNow(t, a, row.ID)
	if fake.reads != 0 {
		t.Fatalf("the transcript tab read the working copy %d times", fake.reads)
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyTab})
	if a.room.tab != roomTabWork || fake.reads != 1 {
		t.Fatalf("tab did not open the work tab and read the copy once: tab=%v reads=%d", a.room.tab, fake.reads)
	}
	text := planRoomText(t, a)
	for _, want := range []string{"load.go", "old line", "new line", "notes.txt"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the work tab does not draw %q:\n%s", want, text)
		}
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyTab})
	if a.room.tab != roomTabTranscript {
		t.Fatal("tab did not come back to the transcript")
	}
}

// `x` OVER AN EMPTY BOX STOPS A RUN'S TASK THROUGH THE PLAN'S DOOR, after the
// card; on a task that has ended it is the letter it is.
func TestStopInARunsTaskRoomGoesThroughThePlansDoor(t *testing.T) {
	row := session.PlanTaskRow{ID: "t-6", Title: "fix the loader", Status: "running"}
	a, fake := planAppWith(t, []session.PlanTaskRow{row}, map[string]session.PlanTaskPage{row.ID: {Row: row, Description: "b"}})
	openPlanRoomNow(t, a, row.ID)
	drive(t, a, key("x"))
	if !a.stopping() || a.stop.target.plan != row.ID {
		t.Fatalf("x did not raise the stop card for the store task: %+v", a.stop)
	}
	spend(t, a, a.stopTake(0))
	if len(fake.cancelled) != 1 || fake.cancelled[0] != row.ID {
		t.Fatalf("the card's stop did not reach the plan's door: %v", fake.cancelled)
	}

	ended := session.PlanTaskRow{ID: "t-7", Title: "done task", Status: "done"}
	a, fake = planAppWith(t, []session.PlanTaskRow{ended}, map[string]session.PlanTaskPage{ended.ID: {Row: ended, Description: "b"}})
	openPlanRoomNow(t, a, ended.ID)
	drive(t, a, key("x"))
	if a.stopping() || len(fake.cancelled) != 0 {
		t.Fatalf("x on an ended task raised a card or cancelled: %v", fake.cancelled)
	}
}

// A NOTE THE STORE REFUSED SAYS SO ON ITS OWN ROW, in the store's words.
func TestANoteTheStoreRefusesSaysSoWhereItWasTyped(t *testing.T) {
	row := session.PlanTaskRow{ID: "t-6", Title: "fix the loader", Status: "running"}
	a, fake := planAppWith(t, []session.PlanTaskRow{row}, map[string]session.PlanTaskPage{row.ID: {Row: row, Description: "b"}})
	fake.refuse = errRefusedForTest
	openPlanRoomNow(t, a, row.ID)
	typeText(t, a, "keep it small")
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if text := planRoomText(t, a); !strings.Contains(text, errRefusedForTest.Error()) {
		t.Fatalf("the refusal is not on the page:\n%s", text)
	}
}

// KEYS TYPED WHILE THE ROOM IS ON ITS WAY GO INTO ITS BOX AND NOWHERE ELSE: a
// sentence that starts with the stop key raises no card, and enter sends
// nothing until the person has read the room.
func TestKeysTypedWhileARunsTaskRoomOpensAreTheBoxAndNothingElse(t *testing.T) {
	row := session.PlanTaskRow{ID: "t-6", Title: "fix the loader", Status: "running"}
	a, fake := planAppWith(t, []session.PlanTaskRow{row}, map[string]session.PlanTaskPage{row.ID: {Row: row, Description: "b"}})
	cmd := a.openRailPlan(row.ID, nil)
	for _, r := range "x-axis" {
		drive(t, a, key(string(r)))
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	spend(t, a, cmd)
	if a.stopping() || len(fake.cancelled) != 0 || len(fake.noted) != 0 {
		t.Fatalf("keys typed in the gap acted: card=%v cancelled=%v noted=%v", a.stopping(), fake.cancelled, fake.noted)
	}
	if !a.roomOpen() || string(a.input.value) != "x-axis" {
		t.Fatalf("the gap's keys are not in the room's box: room=%v box=%q", a.roomOpen(), string(a.input.value))
	}
}

// roomCallText is the room as a reader could open it: the frame, and every
// call row's command and output, which a folded chip holds until it is opened.
func roomCallText(t *testing.T, a *app) string {
	t.Helper()
	parts := []string{planRoomText(t, a)}
	if a.room != nil {
		for _, e := range a.room.entries {
			if e.kind == entryTool {
				parts = append(parts, e.detail.Args, e.detail.Output)
			}
		}
	}
	return strings.Join(parts, "\n")
}

// roomStepsWord is the step count the room's facts carry for its task.
func roomStepsWord(a *app) string {
	if node := a.roomNode(); node != nil {
		return a.roomFactsOf(node).calls.full
	}
	return ""
}

// roomCommands is every call row in the open room, in order, by the command
// it carries. A call row can fold into a chip on screen, so a test that asks
// which steps the room holds reads its entries rather than its frame.
func roomCommands(a *app) []string {
	var out []string
	if a.room == nil {
		return nil
	}
	for _, e := range a.room.entries {
		if e.kind != entryTool {
			continue
		}
		var args struct{ Command string }
		_ = json.Unmarshal([]byte(e.detail.Args), &args)
		out = append(out, args.Command)
	}
	return out
}

// THE ROOM FOLLOWS A RUNNING TASK ON ITS OWN BEAT: a step the store records
// after the room opened is a row after the next beat, once.
func TestTheRunsTaskRoomFollowsAStepTheStoreRecordsLater(t *testing.T) {
	row := session.PlanTaskRow{ID: "t-6", Title: "fix the loader", Status: "running"}
	page := session.PlanTaskPage{Row: row, Description: "b", Steps: []session.PlanStep{
		{Kind: "step", Step: 1, Command: "cat load.go"},
	}}
	a, fake := planAppWith(t, []session.PlanTaskRow{row}, map[string]session.PlanTaskPage{row.ID: page})
	openPlanRoomNow(t, a, row.ID)
	if got := roomCommands(a); len(got) != 1 || got[0] != "cat load.go" {
		t.Fatalf("the room opened with calls %q", got)
	}
	page.Steps = append(page.Steps, session.PlanStep{Kind: "step", Step: 2, Command: "go test ./..."})
	fake.pages[row.ID] = page
	drive(t, a, planRoomTickMsg{gen: a.room.gen})
	if got := roomCommands(a); len(got) != 2 || got[1] != "go test ./..." {
		t.Fatalf("the beat did not bring the new step in once: %q", got)
	}
}

// A LIVE STEP LEAVES WHEN ITS TASK ENDS. The room of a task that has landed
// draws no call in flight, and says it is done.
func TestTheLiveStepLeavesTheRoomWhenTheTaskEnds(t *testing.T) {
	row := session.PlanTaskRow{ID: "t-6", Title: "fix the loader", Status: "running"}
	row.Live.Step, row.Live.Command = 2, "go test ./..."
	page := session.PlanTaskPage{Row: row, Description: "b", Live: row.Live}
	a, fake := planAppWith(t, []session.PlanTaskRow{row}, map[string]session.PlanTaskPage{row.ID: page})
	openPlanRoomNow(t, a, row.ID)
	if got := roomCommands(a); len(got) != 1 {
		t.Fatalf("the running task's live step is not a call: %q", got)
	}
	row.Status, row.Live = "done", plandb.LiveStep{}
	page.Row, page.Live = row, row.Live
	page.Steps = []session.PlanStep{{Kind: "step", Step: 2, Command: "go test ./...", Observation: "ok"}}
	fake.pages[row.ID] = page
	drive(t, a, planRoomTickMsg{gen: a.room.gen})
	if !a.room.done {
		t.Fatal("the room of a task that ended is not done")
	}
	for _, e := range a.room.entries {
		if e.kind == entryTool && e.status == toolRunning {
			t.Fatalf("an ended task's room still draws a call in flight: %+v", e)
		}
	}
}

// A NOTE TYPED INTO A PART'S ROOM REACHES THAT PART, not the run above it.
func TestANoteInAPartsRoomReachesThePart(t *testing.T) {
	root := session.PlanTaskRow{ID: "t-root", Title: "Root", Status: "running"}
	part := session.PlanTaskRow{ID: "t-part", Parent: "t-root", Title: "Part", Status: "running"}
	a, fake := planAppWith(t, []session.PlanTaskRow{root, part}, map[string]session.PlanTaskPage{
		root.ID: {Row: root, Description: "b", Children: []session.PlanTaskRow{part}},
		part.ID: {Row: part, Description: "the part's order"},
	})
	openPlanRoomNow(t, a, part.ID)
	typeText(t, a, "check the edge case")
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(fake.noted) != 1 || fake.noted[0] != (planCall{id: part.ID, text: "check the edge case"}) {
		t.Fatalf("the part's note went to %+v", fake.noted)
	}
}

// A NOTE THE STORE TOOK SAYS WHEN IT IS READ, and on a task that has ended it
// does not, because that task takes no next step.
func TestANoteInARunningTasksRoomSaysWhenTheWorkerReadsIt(t *testing.T) {
	row := session.PlanTaskRow{ID: "t-6", Title: "fix the loader", Status: "running"}
	a, _ := planAppWith(t, []session.PlanTaskRow{row}, map[string]session.PlanTaskPage{row.ID: {Row: row, Description: "b"}})
	openPlanRoomNow(t, a, row.ID)
	typeText(t, a, "keep it small")
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if text := planRoomText(t, a); !strings.Contains(text, taskPlanPickupWord) {
		t.Fatalf("the room does not say when the note is read:\n%s", text)
	}
}

// A PART UNDER A RUN'S TASK IS A DOOR: a press on its row in the room opens
// that part's room, the way its row on the side list does.
func TestAPressOnAPartRowInTheRoomOpensThePartsRoom(t *testing.T) {
	root := session.PlanTaskRow{ID: "t-root", Title: "Root", Status: "running"}
	part := session.PlanTaskRow{ID: "t-part", Parent: "t-root", Title: "Check the flaky parser", Status: "running"}
	a, _ := planAppWith(t, []session.PlanTaskRow{root, part}, map[string]session.PlanTaskPage{
		root.ID: {Row: root, Description: "b", Children: []session.PlanTaskRow{part}},
		part.ID: {Row: part, Description: "the part's order"},
	})
	a.height = 40
	openPlanRoomNow(t, a, root.ID)
	x, y := -1, -1
	for i, line := range strings.Split(planRoomText(t, a), "\n") {
		if at := strings.LastIndex(line, railSeam); at >= 0 {
			line = line[:at]
		}
		if col := strings.Index(line, part.Title); col >= 0 {
			x, y = len([]rune(line[:col]))+1, i
		}
	}
	if y < 0 {
		t.Fatalf("the room does not draw its part:\n%s", planRoomText(t, a))
	}
	drive(t, a, clickAt(x, y), releaseAt(x, y))
	if plan := a.roomPlan(); plan == nil || plan.id != part.ID {
		t.Fatalf("a press on the part's row did not open its room: room %v", a.roomOpen())
	}
}

// A TASK THAT HAS ENDED TAKES NO NOTE: the room says what its box already says,
// asks the store nothing, and leaves the words in the box.
func TestAnEndedTasksRoomRefusesANoteInItsOwnWords(t *testing.T) {
	row := session.PlanTaskRow{ID: "t-6", Title: "fix the loader", Status: "done"}
	a, fake := planAppWith(t, []session.PlanTaskRow{row}, map[string]session.PlanTaskPage{row.ID: {Row: row, Description: "b"}})
	openPlanRoomNow(t, a, row.ID)
	typeText(t, a, "one more thing")
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(fake.noted) != 0 {
		t.Fatalf("an ended task's room sent a note: %v", fake.noted)
	}
	if got := string(a.input.value); got != "one more thing" {
		t.Fatalf("the box holds %q, want the words kept", got)
	}
	if text := planRoomText(t, a); !strings.Contains(text, roomFinishedRefusal.what) {
		t.Fatalf("the room does not say the task has finished:\n%s", text)
	}
}

// THE WORK TAB DRAWS SOMEBODY ELSE'S BYTES, and they reach no frame unparsed. A
// file the work changed is whatever a worker wrote into it: a carriage return
// from a file with Windows line ends, a title escape, a screen clear. Drawn raw,
// each one repaints rows this surface owns; the transcript draws a call's
// output through [drawableLine] for the same reason, and so does this tab.
func TestTheWorkTabDrawsNoControlBytesFromTheDifference(t *testing.T) {
	row := session.PlanTaskRow{ID: "t-6", Title: "fix the loader", Status: "running"}
	fake := &planWorkFake{work: session.PlanTaskWork{
		Dir: "/work/copy", Read: true, Added: []string{"odd\x1b]0;named\x07.txt"},
		Patch: "diff --git a/load.go b/load.go\n--- a/load.go\n+++ b/load.go\n@@ -1 +1 @@\n" +
			"-old line\r\n+new\x1b]0;title\x07 line\x1b[2J\r\n",
	}}
	a, plan := planAppWith(t, []session.PlanTaskRow{row}, map[string]session.PlanTaskPage{row.ID: {Row: row, Description: "b"}})
	fake.planFake = plan
	a.agent = fake
	openPlanRoomNow(t, a, row.ID)
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyTab})
	rows := a.roomWorkRows(80)
	var drawn strings.Builder
	for _, r := range rows {
		drawn.WriteString(r.text)
		drawn.WriteByte('\n')
	}
	text := drawn.String()
	for _, raw := range []string{"\r", "\a", "\x1b]", "\x1b[2J"} {
		if strings.Contains(text, raw) {
			t.Fatalf("the work tab drew the control bytes %q from the difference:\n%q", raw, text)
		}
	}
	for _, want := range []string{"old line", "new", "line", "odd"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the work tab lost the words %q:\n%q", want, text)
		}
	}
}
