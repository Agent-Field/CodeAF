package tui3

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

func selectHomeTask(t *testing.T, a *app, id string) session.SessionRow {
	t.Helper()
	a.showPage(pageTasks)
	for _, at := range a.taskSheet.stops(a) {
		a.taskSheet.cursor = at
		if item, ok := a.taskSheetCurrent(); ok && item.entry.ID == id {
			return item.row
		}
	}
	t.Fatalf("home has no task %s", id)
	return session.SessionRow{}
}

type recordActionAgent struct {
	*fakeAgent
	rows  []session.TaskIndexEntry
	calls []string
	fail  error
}

func (a *recordActionAgent) TaskIndex() []session.TaskIndexEntry { return a.rows }
func (a *recordActionAgent) Cancel(id string) (string, error) {
	a.calls = append(a.calls, id)
	if a.fail != nil {
		return "", a.fail
	}
	for i := range a.rows {
		if "task:"+a.rows[i].ID == id {
			a.rows[i].Status = string(session.TaskFailed)
		}
	}
	return "stopped", nil
}

func TestTaskStopTargetsItsOwnerAndNeverDeletesAfterCompletion(t *testing.T) {
	for _, settled := range []bool{false, true} {
		t.Run(fmt.Sprint(settled), func(t *testing.T) {
			lab := newSwitchLab(t)
			a := lab.open(180, 40)
			owner := selectHomeTask(t, a, "t1")
			item, _ := a.taskSheetCurrent()
			agent := &recordActionAgent{fakeAgent: &fakeAgent{model: "m"}, rows: []session.TaskIndexEntry{item.entry}}
			var asked string
			a.open = func(workspace, file string) (Conversation, error) {
				asked = file
				return Conversation{Agent: agent, SessionFile: file, Workspace: workspace}, nil
			}
			before := a.file
			drive(t, a, key("right"))
			for _, word := range []string{"x stop", "n new in project", "o open folder", "p copy project"} {
				if !strings.Contains(taskSheetText(a), word) {
					t.Fatalf("missing %q", word)
				}
			}
			if settled {
				agent.rows[0].Status = string(session.TaskDone)
			}
			drive(t, a, key("x"))
			if asked != owner.Transcript || a.file != before {
				t.Fatal("stop used or changed the wrong conversation")
			}
			if len(agent.calls) != map[bool]int{false: 1, true: 0}[settled] {
				t.Fatalf("stop calls=%v", agent.calls)
			}
			meta, _ := session.LoadMeta(owner.Dir)
			if meta.ArchivedTasks["t1"] || meta.DeletedTasks["t1"] || a.strip.prompt != "" {
				t.Fatal("stop archived or deleted a record")
			}
		})
	}
}

func TestFinishedTasksOfferConfirmedDeleteAndCancelRestoresActions(t *testing.T) {
	lab := newSwitchLab(t)
	lab.task("-beta", session.TaskIndexEntry{ID: "t1", SessionID: "bbbb000000000001", Title: "read 40 filings", Status: string(session.TaskDone), EndedAt: lab.now})
	a := lab.open(180, 40)
	owner := selectHomeTask(t, a, "t1")
	drive(t, a, key("right"), key("x"))
	if !strings.Contains(a.strip.prompt, deletePermanentWord) {
		t.Fatal("delete did not ask")
	}
	drive(t, a, key("enter"))
	meta, _ := session.LoadMeta(owner.Dir)
	if meta.DeletedTasks["t1"] {
		t.Fatal("Enter confirmed deletion")
	}
	drive(t, a, key("n"))
	if !a.strip.open || len(a.strip.verbs) != 4 || a.strip.verbs[0].word != "delete" {
		t.Fatal("cancel did not restore four actions")
	}
}

func TestTaskStopFailureKeepsTheRecordAndConversation(t *testing.T) {
	lab := newSwitchLab(t)
	a := lab.open(180, 40)
	owner := selectHomeTask(t, a, "t1")
	item, _ := a.taskSheetCurrent()
	agent := &recordActionAgent{fakeAgent: &fakeAgent{model: "m"}, rows: []session.TaskIndexEntry{item.entry}, fail: errors.New("stop refused")}
	a.open = func(workspace, file string) (Conversation, error) {
		return Conversation{Agent: agent, SessionFile: file, Workspace: workspace}, nil
	}
	drive(t, a, key("right"), key("x"))
	meta, _ := session.LoadMeta(owner.Dir)
	if meta.DeletedTasks["t1"] || !strings.Contains(a.taskSheet.actionNote, "stop refused") {
		t.Fatal("stop failure was hidden or deleted its record")
	}
}

func TestProjectMenuShortcutsUseTheSelectedItemsProject(t *testing.T) {
	for _, task := range []bool{false, true} {
		name := "thread"
		if task {
			name = "task"
		}
		t.Run(name, func(t *testing.T) {
			var a *app
			var owner session.SessionRow
			if task {
				a = newSwitchLab(t).open(180, 40)
				owner = selectHomeTask(t, a, "t1")
			} else {
				a = placeAppOneColumn(t)
				placeFrameText(a)
				line, ok := a.home.focusedLine()
				if !ok || line.kind != homeSession {
					t.Fatal("fixture did not select a thread")
				}
				owner = line.row
			}
			var started string
			a.start = func(workspace string) (Conversation, error) {
				started = workspace
				return Conversation{}, errors.New("test captured the requested project")
			}
			drive(t, a, key("right"), key("t"), key("c"))
			if !a.strip.open || started != "" {
				t.Fatal("a retired menu shortcut still acted")
			}
			cmd, handled := a.stripKey(key("p"))
			if !handled || cmd == nil || !reflect.DeepEqual(cmd(), tea.Raw(osc52(owner.Workspace, a.tmux))()) {
				t.Fatal("p copy project did not copy the selected item's project")
			}
			drive(t, a, key("right"), key("n"))
			if started != owner.Workspace {
				t.Fatalf("n new in project started in %q, want %q", started, owner.Workspace)
			}
		})
	}
}

func TestTaskOptionsWrapWithoutDroppingActiveShortcuts(t *testing.T) {
	a, _ := tasksFootApp(t)
	drive(t, a, key("right"))
	rows := a.verbStripRow(32)
	for _, v := range a.strip.verbs {
		if !strings.Contains(plain(strings.Join(rows, "\n")), string(v.key)+" "+v.word) {
			t.Fatalf("narrow task options dropped %c %s", v.key, v.word)
		}
	}
}

func TestTaskOptionsSuspendTheLandingAnswerHints(t *testing.T) {
	a, _ := paneLandingLab(t, taskPaneFloor+12)
	item, ok := a.taskSheetCurrent()
	if !ok || len(a.taskPaneVerbs(item)) < 2 {
		t.Fatal("fixture has no landing answers")
	}
	drive(t, a, key("right"))
	if !a.strip.open {
		t.Fatal("task options did not open")
	}
	verbs := a.taskPaneVerbs(item)
	if len(verbs) != 1 || verbs[0].key != questionEnterKey {
		t.Fatal("the pane advertised answer keys while task options owned them")
	}
	drive(t, a, key("left"))
	if len(a.taskPaneVerbs(item)) < 2 {
		t.Fatal("closing options did not restore the landing answers")
	}
}

func TestTaskDeleteRechecksNewlyActiveDescendantsOnYes(t *testing.T) {
	lab := newSwitchLab(t)
	lab.task("-beta", session.TaskIndexEntry{ID: "t1", SessionID: "bbbb000000000001", Title: "read 40 filings", Status: string(session.TaskDone), EndedAt: lab.now})
	a := lab.open(180, 40)
	owner := selectHomeTask(t, a, "t1")
	item, _ := a.taskSheetCurrent()
	agent := &recordActionAgent{fakeAgent: &fakeAgent{model: "m"}, rows: []session.TaskIndexEntry{item.entry}}
	a.file, a.agent = owner.Transcript, agent
	drive(t, a, key("right"), key("x"))
	if a.strip.prompt == "" {
		t.Fatal("delete did not confirm")
	}
	agent.rows = append(agent.rows, session.TaskIndexEntry{ID: "child", Parent: "t1", SessionID: owner.ID, Title: "child", Status: string(session.TaskRunning)})
	drive(t, a, key("y"))
	meta, _ := session.LoadMeta(owner.Dir)
	if meta.DeletedTasks["t1"] || len(agent.calls) > 0 || !strings.Contains(a.taskSheet.actionNote, "still active") {
		t.Fatalf("confirmation deleted or stopped new work: %s", a.taskSheet.actionNote)
	}
}

func TestTaskParentStopsActiveDescendantsAndOffersDeleteAfterTheySettle(t *testing.T) {
	lab := newSwitchLab(t)
	a := lab.open(180, 40)
	owner := selectHomeTask(t, a, "t1")
	rows := []session.TaskIndexEntry{
		{ID: "t1", SessionID: owner.ID, Title: "Parent", Status: string(session.TaskDone)},
		{ID: "child", Parent: "t1", SessionID: owner.ID, Title: "Child", Status: string(session.TaskQueued)},
		{ID: "grandchild", Parent: "child", SessionID: owner.ID, Title: "Grandchild", Status: string(session.TaskRunning)},
		{ID: "sibling", SessionID: owner.ID, Title: "Sibling", Status: string(session.TaskRunning)},
	}
	agent := &recordActionAgent{fakeAgent: &fakeAgent{model: "m"}, rows: rows}
	a.file, a.agent = owner.Transcript, agent
	a.taskSheet.reading.items = nil
	owner.Tasks.Rows = rows
	verbs := a.taskRowVerbs(owner, rows[0])
	if verbs[0].word != "stop" {
		t.Fatal("settled parent offered delete over active children")
	}
	drain(t, a, verbs[0].do())
	if len(agent.calls) != 2 {
		t.Fatalf("calls=%v", agent.calls)
	}
	for _, id := range agent.calls {
		if id == "task:t1" || id == "task:sibling" {
			t.Fatalf("stopped unrelated task: %s", id)
		}
	}
	a.taskSheet.reading.items = nil
	owner.Tasks.Rows = agent.rows
	if got := a.taskRowVerbs(owner, agent.rows[0])[0].word; got != "delete" {
		t.Fatalf("settled subtree offers %s", got)
	}
}

func TestFormerlyClosedTasksRemainVisible(t *testing.T) {
	lab := newSwitchLab(t)
	a := lab.open(180, 40)
	owner := selectHomeTask(t, a, "t1")
	if err := session.SetTaskArchived(owner.Dir, owner.ID, "t1", true); err != nil {
		t.Fatal(err)
	}
	fresh := lab.open(180, 40)
	selectHomeTask(t, fresh, "t1")
}

func TestHomeTaskOptionsUseStopInsteadOfClose(t *testing.T) {
	lab := newSwitchLab(t)
	a := lab.open(180, 40)
	owner := selectHomeTask(t, a, "t1")
	item, _ := a.taskSheetCurrent()
	line := homeLine{cell: &homeCell{row: &switcherRow{session: owner, task: &item.entry}}}
	verbs := a.runningVerbs(line)
	if len(verbs) != 4 || verbs[0].key != 'x' || verbs[0].word != "stop" {
		t.Fatalf("home task actions=%v", verbs)
	}
}

func TestTaskStopUsesPlanDoorForWaitingSubtree(t *testing.T) {
	lab := newSwitchLab(t)
	a := lab.open(180, 40)
	owner := selectHomeTask(t, a, "t1")
	agent := &planFake{taskFake: &taskFake{fakeAgent: &fakeAgent{model: "m"}}, plan: []session.PlanTaskRow{
		{ID: "t-1", Title: "Root", Status: "done"},
		{ID: "t-child", Parent: "t-1", Title: "Child", Status: "paused"},
		{ID: "t-sibling", Title: "Sibling", Status: "running"},
	}}
	root := session.TaskIndexEntry{ID: "1", Title: "Root", SessionID: owner.ID, Status: string(session.TaskDone)}
	a.file, a.agent = owner.Transcript, agent
	a.planRows = agent.plan
	a.taskSheet.reading.items = nil
	owner.Tasks.Rows = []session.TaskIndexEntry{root}
	verbs := a.taskRowVerbs(owner, root)
	if verbs[0].word != "stop" {
		t.Fatal("waiting plan child offered Delete")
	}
	drain(t, a, verbs[0].do())
	if !reflect.DeepEqual(agent.cancelled, []string{"t-child"}) {
		t.Fatalf("wrong plan work stopped: %v", agent.cancelled)
	}
}
