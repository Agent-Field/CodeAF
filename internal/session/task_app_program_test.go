package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// The two launches the fixtures speak of: the run on the record, and the run
// before it on the same task.
const (
	fixtureLaunch = "0123456789abcdef0123456789abcdef"
	earlierLaunch = "fedcba9876543210fedcba9876543210"
)

// appendLines writes lines to a task's action log exactly as given.
func appendLines(t *testing.T, folder string, lines ...delegate.Action) {
	t.Helper()
	for _, line := range lines {
		if err := delegate.AppendAction(folder, line); err != nil {
			t.Fatal(err)
		}
	}
}

// stamped is a line as the producer of launch writes it.
func stamped(launch string, action delegate.Action) delegate.Action {
	action.Launch = launch
	return action
}

// programAppFixture is a senior-dev task on disk with a hello of four stages,
// the launch's start line, a stage line, a step and any tail, every line
// stamped with the record's launch, read through the real AppTask door.
func programAppFixture(t *testing.T, tail ...delegate.Action) (*Agent, string) {
	t.Helper()
	agent, dir := programPageFixture(t)
	folder := plandb.TaskDir(dir, "alpha")
	record, _ := delegate.ReadProgram(folder)
	record.Stages = []string{"bootstrap", "intake", "mystery", "implement"}
	record.StartedAt = time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	record.Launch = fixtureLaunch
	appendLines(t, folder, delegate.StartAction(record.StartedAt, fixtureLaunch))
	if err := delegate.WriteProgram(folder, record); err != nil {
		t.Fatal(err)
	}
	agent.config.Delegates = []delegate.Delegate{{Name: "senior-dev", Listens: true,
		StageWords: map[string]string{"bootstrap": "starting", "intake": "reading the brief", "implement": "working"}}}
	at := time.Date(2026, 10, 16, 9, 1, 0, 0, time.UTC)
	for i, action := range append([]delegate.Action{
		delegate.StageAction(at, delegate.StageRecord{Stage: "intake", Status: "captured"}),
		delegate.StepAction(at.Add(time.Second), delegate.StepRecord{Tool: "read", Step: "explore", Command: "read internal/auth/middleware.go"}),
	}, tail...) {
		if action.At.IsZero() {
			action.At = at.Add(time.Duration(i) * time.Minute)
		}
		appendLines(t, folder, stamped(fixtureLaunch, action))
	}
	return agent, dir
}

func appTaskOf(t *testing.T, agent *Agent) AppTaskView {
	t.Helper()
	rows, err := agent.AppTasks()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if view, found, err := agent.AppTask(row.ID); err == nil && found && view.Program != nil {
			return view
		}
	}
	t.Fatalf("no program task among %d rows", len(rows))
	return AppTaskView{}
}

// THE APP'S TASK READ CARRIES THE PROGRAM THE PAGE READ ALREADY HELD.
func TestAppTaskCarriesTheProgramTheStageWordsAndTheLastStage(t *testing.T) {
	agent, _ := programAppFixture(t)
	view := appTaskOf(t, agent)
	p := view.Program
	if p.Name != "senior-dev" || p.CeilingUSD != 5 || p.Calls != 2 || p.Effort != "high" || !p.Listens || p.LastStage != "intake" {
		t.Fatalf("program = %+v", p)
	}
	// A wordless stage keeps the word standing before it, as the live step does.
	want := []AppProgramStage{{"bootstrap", "starting"}, {"intake", "reading the brief"}, {"mystery", "reading the brief"}, {"implement", "working"}}
	if len(p.Stages) != 4 || p.Stages[2] != want[2] || p.Stages[3] != want[3] || p.Stages[0] != want[0] {
		t.Fatalf("stages = %+v, want %+v", p.Stages, want)
	}
	if p.Read != AppProgramReadComplete || len(p.Actions) != 2 || p.Ending != nil || p.StartedAt.IsZero() {
		t.Fatalf("read %q actions %d ending %v started %v", p.Read, len(p.Actions), p.Ending, p.StartedAt)
	}
	// It crosses the remote wire as the field it is.
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	var back AppTaskView
	if err := json.Unmarshal(raw, &back); err != nil || back.Program == nil || back.Program.LastStage != "intake" || len(back.Program.Stages) != 4 {
		t.Fatalf("remote round trip: %v %+v", err, back.Program)
	}
}

func TestAProgramWithNoWordsIsShownByItsStageNames(t *testing.T) {
	if got := AppProgramStageWord(delegate.Delegate{Name: "x"}, "recon", "starting"); got != "recon" {
		t.Fatalf("word = %q", got)
	}
	words := delegate.Delegate{StageWords: map[string]string{}}
	if got := AppProgramStageWord(words, "mystery", ""); got != "" {
		t.Fatalf("a first wordless stage has no word, got %q", got)
	}
}

// AN ENDING IS CURRENT ONLY WHILE IT IS THE LOG'S LAST LINE.
func TestTheEndingIsCurrentOnlyWhileItIsTheLastLine(t *testing.T) {
	end := delegate.EndAction(time.Date(2026, 10, 16, 9, 30, 0, 0, time.UTC), delegate.Terminal{Status: delegate.StatusPass, Message: "handed in",
		Data: map[string]json.RawMessage{"submitted": json.RawMessage(`true`), "findings": json.RawMessage(`null`)}})
	agent, dir := programAppFixture(t, end)
	p := appTaskOf(t, agent).Program
	if p.Ending == nil || p.Ending.Text != "handed in" || p.EndStatus != delegate.StatusPass ||
		p.Ending.Ending == nil || p.Ending.Ending.Submitted == nil || !*p.Ending.Ending.Submitted || p.Ending.Ending.Findings != nil {
		t.Fatalf("ending = %+v status %q", p.Ending, p.EndStatus)
	}
	later := delegate.StageAction(time.Date(2026, 10, 16, 9, 40, 0, 0, time.UTC), delegate.StageRecord{Stage: "implement", Status: "running"})
	appendLines(t, plandb.TaskDir(dir, "alpha"), stamped(fixtureLaunch, later))
	p = appTaskOf(t, agent).Program
	if p.Ending != nil || p.LastStage != "implement" {
		t.Fatalf("a run begun again still shows its old ending: %+v last %q", p.Ending, p.LastStage)
	}
}

func TestAnOrdinaryTaskAppReadCarriesNoProgram(t *testing.T) {
	agent, dir := programPageFixture(t)
	seedPlanStore(t, filepath.Join(dir, planStoreFilename), "chat-a", plandb.TaskSpec{ID: "beta", Title: "Beta", Description: "plain"})
	rows, _ := agent.AppTasks()
	seen := false
	for _, row := range rows {
		if view, found, _ := agent.AppTask(row.ID); found && view.Title == "Beta" {
			seen = true
			if view.Program != nil {
				t.Fatalf("an ordinary task carries a program: %+v", view.Program)
			}
		}
	}
	if !seen {
		t.Fatalf("the ordinary task was not among %d rows", len(rows))
	}
}

// relaunched is the fixture's task after a run of the EARLIER launch that
// ended with "handed in" at 09:30, handed off again: the record now names the
// fixture's launch started at started, and the log holds its start line.
func relaunched(t *testing.T, started time.Time) (*Agent, string) {
	t.Helper()
	agent, dir := programPageFixture(t)
	folder := plandb.TaskDir(dir, "alpha")
	at := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	appendLines(t, folder,
		delegate.StartAction(at, earlierLaunch),
		stamped(earlierLaunch, delegate.StageAction(at.Add(time.Minute), delegate.StageRecord{Stage: "intake", Status: "captured"})),
		stamped(earlierLaunch, delegate.EndAction(time.Date(2026, 10, 16, 9, 30, 0, 0, time.UTC), delegate.Terminal{Status: delegate.StatusFail, Message: "handed in"})),
		delegate.StartAction(started, fixtureLaunch))
	record, _ := delegate.ReadProgram(folder)
	record.Launch, record.StartedAt = fixtureLaunch, started
	if err := delegate.WriteProgram(folder, record); err != nil {
		t.Fatal(err)
	}
	return agent, dir
}

// A RELAUNCH BEGINS A NEW PROCESS ON AN OLD LOG, and what ties a line to the
// process is the launch the producer stamped on it — never a time. A clock that
// stepped back, a start equal to the old ending's time, an old line written
// late: the previous run's ending, stage and actions are never the new run's.
func TestARelaunchedProgramNeverWearsTheLastRunsWork(t *testing.T) {
	for _, tc := range []struct {
		name    string
		started time.Time
		tail    []delegate.Action
		actions int
		last    string
	}{
		{name: "the clock stepped back before the old ending", started: time.Date(2026, 10, 16, 9, 25, 0, 0, time.UTC)},
		{name: "the start equals the old ending's time", started: time.Date(2026, 10, 16, 9, 30, 0, 0, time.UTC)},
		{name: "an old line written after the new start", started: time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC),
			tail: []delegate.Action{
				stamped(fixtureLaunch, delegate.StageAction(time.Date(2026, 10, 16, 10, 1, 0, 0, time.UTC), delegate.StageRecord{Stage: "implement", Status: "running"})),
				stamped(earlierLaunch, delegate.EndAction(time.Date(2026, 10, 16, 10, 2, 0, 0, time.UTC), delegate.Terminal{Status: delegate.StatusPass, Message: "a late old ending"})),
			}, actions: 1, last: "implement"},
		{name: "the rerun has logged its own work", started: time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC),
			tail: []delegate.Action{
				stamped(fixtureLaunch, delegate.StageAction(time.Date(2026, 10, 16, 10, 1, 0, 0, time.UTC), delegate.StageRecord{Stage: "implement", Status: "running"})),
				stamped(fixtureLaunch, delegate.StepAction(time.Date(2026, 10, 16, 10, 2, 0, 0, time.UTC), delegate.StepRecord{Tool: "read", Step: "explore", Command: "read b.go"})),
			}, actions: 2, last: "implement"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent, dir := relaunched(t, tc.started)
			appendLines(t, plandb.TaskDir(dir, "alpha"), tc.tail...)
			p := appTaskOf(t, agent).Program
			if p.Read != AppProgramReadComplete || p.Ending != nil || p.EndStatus != "" || p.LastStage != tc.last || len(p.Actions) != tc.actions || p.EarlierActions != 0 {
				t.Fatalf("read %q ending %+v last %q actions %+v earlier %d", p.Read, p.Ending, p.LastStage, p.Actions, p.EarlierActions)
			}
			for _, action := range p.Actions {
				if strings.Contains(action.Text, "handed in") || strings.Contains(action.Text, "late old") {
					t.Fatalf("an earlier run's action is the new run's: %+v", action)
				}
			}
		})
	}
}

// A PROCESS NOBODY CAN TIE A LINE TO IS UNKNOWN, NOT EMPTY AND NOT THE LAST
// RUN'S: before a relaunch's hello the worker has removed the record, and a log
// written before launches were carries none.
func TestAProgramWhoseProcessCannotBeTiedToALineIsUnknown(t *testing.T) {
	t.Run("before the hello", func(t *testing.T) {
		agent, dir := relaunched(t, time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC))
		if err := os.Remove(filepath.Join(plandb.TaskDir(dir, "alpha"), delegate.ProgramFile)); err != nil {
			t.Fatal(err)
		}
		assertUnknownProgram(t, appTaskOf(t, agent).Program)
	})
	t.Run("a log from before launches", func(t *testing.T) {
		agent, dir := programPageFixture(t)
		appendLines(t, plandb.TaskDir(dir, "alpha"),
			delegate.StageAction(time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC), delegate.StageRecord{Stage: "intake", Status: "captured"}),
			delegate.EndAction(time.Date(2026, 10, 16, 9, 30, 0, 0, time.UTC), delegate.Terminal{Status: delegate.StatusPass, Message: "handed in"}))
		assertUnknownProgram(t, appTaskOf(t, agent).Program)
	})
	t.Run("a launch with no line yet is known and empty", func(t *testing.T) {
		agent, _ := relaunched(t, time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC))
		if p := appTaskOf(t, agent).Program; p.Read != AppProgramReadComplete || len(p.Actions) != 0 || p.Ending != nil {
			t.Fatalf("program = %+v", p)
		}
	})
}

func assertUnknownProgram(t *testing.T, p *AppProgramView) {
	t.Helper()
	if p.Read != AppProgramReadUnknown || p.Actions != nil || p.EarlierActions != 0 || p.LastStage != "" || p.Ending != nil || p.EndStatus != "" {
		t.Fatalf("an untied process says %+v", p)
	}
}

// AN ENDING THE READER DROPPED IS NOT THE LAST SHOWN ACTION'S.
func TestAnUnshownEndingIsNotReportedAsTheLastAction(t *testing.T) {
	at := time.Date(2026, 10, 16, 9, 1, 0, 0, time.UTC)
	logged := []delegate.Action{
		{At: at, Kind: delegate.ActionStep, Step: "explore", Command: "read a.go"},
		{At: at, Kind: delegate.ActionEnd, Status: delegate.StatusPass},
	}
	current := planProgramLaunchOf(logged, planProgramOf(nil, "x"), planRunCopies{})
	if len(current.Actions) != 1 || current.Ending != nil {
		t.Fatalf("the step before an unshown ending was reported as the ending: %+v", current)
	}
}

func TestTheAppReadKeepsTheNewestActionsWithoutDetailAndDropsOverlongStages(t *testing.T) {
	agent := &Agent{}
	page := PlanTaskPage{Program: &PlanProgram{Name: "x", Stages: []string{"a", string(make([]byte, 200))}, Current: &PlanProgramLaunch{EarlierActions: 3}}}
	for i := 0; i < AppProgramActionsMax+10; i++ {
		page.Program.Current.Actions = append(page.Program.Current.Actions, delegate.Shown{Text: "t", Detail: "big"})
	}
	view := agent.appProgramView(page)
	if len(view.Actions) != AppProgramActionsMax || view.EarlierActions != 13 || view.Actions[0].Detail != "" || len(view.Stages) != 0 {
		t.Fatalf("view = %d actions, %d earlier, %d stages", len(view.Actions), view.EarlierActions, len(view.Stages))
	}
}
