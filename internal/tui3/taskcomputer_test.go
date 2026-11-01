package tui3

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

const surfaceComputerB = "dev_11111111111111111111111111111111"
const surfaceComputerC = "dev_22222222222222222222222222222222"
const surfaceComputerWindows = "dev_33333333333333333333333333333333"
const surfaceComputerRemoved = "dev_44444444444444444444444444444444"

type surfaceTaskComputers []session.TaskComputer

func (c surfaceTaskComputers) List(context.Context) ([]session.TaskComputer, error) { return c, nil }
func (surfaceTaskComputers) ThisComputer() string                                   { return "home computer" }
func (surfaceTaskComputers) Engine(context.Context, string) (session.TaskRunEngine, error) {
	return nil, nil
}

type surfaceTaskComputerAgent struct {
	*taskFake
	computerAnswers     []session.Answer
	here                []uint64
	computer, brief     string
	effort              string
	solo                bool
	starts, localStarts int
	startErr            error
	startNote           string
}

type olderTaskComputerSurface struct{ *surfaceTaskComputerAgent }

func (*olderTaskComputerSurface) TaskComputersSupported() bool { return false }

// The wire compatibility law requires the welcome's missing capability to
// hide the new controls even though the client now has their Go methods.
func TestAnOlderEngineHasNoTaskComputerFlagOrRecoveryVerb(t *testing.T) {
	a, f, _ := computerRecoveryApp(t, session.TaskInterrupted, &session.TaskRemoteRecord{StartFailed: true}, "work computer is not taking tasks right now — run codeaf serve --take-tasks on it")
	a.agent = &olderTaskComputerSurface{f}
	if strings.Contains(a.taskCommandUsage(), "--on") {
		t.Fatal("an older engine advertises --on")
	}
	rows, _, _, _ := a.taskCardFrame(120, 40)
	if text := plain(strings.Join(rows, "\n")); strings.Contains(text, "run it here") {
		t.Fatalf("an older engine advertises recovery:\n%s", text)
	}
	if cmd := a.taskCardKey("h"); cmd != nil || len(f.here) != 0 {
		t.Fatal("an absent recovery control reached the engine")
	}
	a.taskSheet.detailOn = false
	a.proposeTask(proposal(a, 8, 0))
	if text := plain(strings.Join(a.taskCardRows(a.task, 120, false), "\n")); strings.Contains(text, "ctrl+d computer") || strings.Contains(text, "runs on") {
		t.Fatalf("an older engine advertises proposal placement:\n%s", text)
	}
	if cmd, took := a.taskKey(key("ctrl+d")); cmd != nil || took {
		t.Fatal("an older engine takes the computer-cycle key")
	}
}

func (f *surfaceTaskComputerAgent) ResolveQuestion(answer session.Answer) error {
	f.computerAnswers = append(f.computerAnswers, answer)
	return f.taskFake.ResolveQuestion(answer)
}

// DESIGN contracts 1 and 6 require both prefix orders to carry the same
// computer and one-task effort through the named placement operation.
func TestTaskOnCarriesEffortInEitherOrder(t *testing.T) {
	for _, tc := range []struct{ arg, effort string }{
		{`--best --on "Abir's Mac" repair export`, "best"},
		{`--on "Abir's Mac" --best repair export`, "best"},
		{`--cheap --on "Abir's Mac" repair export`, "cheap"},
		{`--on "Abir's Mac" --cheap repair export`, "cheap"},
	} {
		t.Run(tc.arg, func(t *testing.T) {
			a, base, _ := taskApp(t)
			f := &surfaceTaskComputerAgent{taskFake: base}
			a.agent, a.taskComputers = f, surfaceTaskComputers{}
			drive(t, a, taskMsg(a.slash("/task "+tc.arg)))
			if f.starts != 1 || f.localStarts != 0 || f.computer != "Abir's Mac" || f.effort != tc.effort || f.brief != "repair export" {
				t.Fatalf("computer or effort was lost: %+v", f)
			}
		})
	}
}

// DESIGN contract 1 requires a proposal's explicit choice to win over its
// default. Choosing a computer answers nothing until the person accepts.
func TestTheProposalCardCyclesWhereTheTaskRuns(t *testing.T) {
	a, base, _ := taskApp(t)
	f := &surfaceTaskComputerAgent{taskFake: base}
	f.pending = []uint64{7}
	a.agent = f
	a.taskComputers = surfaceTaskComputers{
		{Device: surfaceComputerB, Name: "work computer", CanTakeTasks: true},
		{Device: surfaceComputerC, Name: "Abir's Mac", CanTakeTasks: true},
		{Device: surfaceComputerWindows, Name: "Windows computer"},
	}
	if err := config.WriteTaskRunsOn(a.profileDir, surfaceComputerB); err != nil {
		t.Fatal(err)
	}
	drive(t, a, taskMsg(a.readTaskComputers()))
	ev := proposal(a, 7, 0)
	ev.Task.Model, ev.Task.ModelOptions = "test/one", []string{"test/one", "test/two"}
	drive(t, a, streamEventMsg{gen: a.gen, ev: ev})
	taskText(a)
	settleAsk(a)
	text := plain(strings.Join(a.taskCardRows(a.task, 120, false), "\n"))
	if !strings.Contains(text, "runs on work computer") || !strings.Contains(text, "ctrl+d computer") {
		t.Fatalf("the proposal hides its choice or key:\n%s", text)
	}
	a.input.setText("keep my draft")
	drive(t, a, key("ctrl+d"))
	if a.input.String() != "keep my draft" || !strings.Contains(plain(strings.Join(a.taskCardRows(a.task, 120, false), "\n")), "runs on work computer") {
		t.Fatal("the computer key changed a nonempty draft or its placement")
	}
	a.input.setText("")
	drive(t, a, key("right"))
	for _, want := range []string{"Abir's Mac", "this computer", "work computer", "Abir's Mac", "this computer"} {
		drive(t, a, key("ctrl+d"))
		if text := plain(strings.Join(a.taskCardRows(a.task, 120, false), "\n")); !strings.Contains(text, "runs on "+want) || strings.Contains(text, "Windows computer") {
			t.Fatalf("the cycle did not pick %q:\n%s", want, text)
		}
		if len(f.computerAnswers) != 0 {
			t.Fatal("cycling answered the proposal")
		}
	}
	drive(t, a, key("1"))
	if len(f.computerAnswers) != 1 {
		t.Fatalf("proposal answers = %d", len(f.computerAnswers))
	}
	answer := f.computerAnswers[0]
	if answer.Blanks[session.TaskComputerBlank] != "this computer" || answer.Blanks[session.TaskModelBlank] != "test/two" {
		t.Fatalf("the accepted choices were lost: %+v", answer)
	}
	if got := config.TaskRunsOnAt(a.profileDir); got != surfaceComputerB {
		t.Fatalf("a one-task choice changed the standing default: %q", got)
	}
}

func (f *surfaceTaskComputerAgent) StartTaskEffort(ctx context.Context, brief string, solo bool, effort string) (uint64, string, string, error) {
	f.effort = effort
	return f.StartTask(ctx, brief, solo)
}

// TestTaskComputerPlacementKeepsEveryExistingTaskForm covers SPEC-R4's
// compatibility requirement at the typed command boundary, including the
// workspace verbs that must be intercepted before any brief is started.
func TestTaskComputerPlacementKeepsEveryExistingTaskForm(t *testing.T) {
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "")
	for _, tc := range []struct {
		arg, brief, effort string
		solo, intercepted  bool
	}{
		{arg: "repair export", brief: "repair export"},
		{arg: "solo repair export", brief: "repair export", solo: true},
		{arg: "--best repair export", brief: "repair export", effort: "best"},
		{arg: "--cheap solo repair export", brief: "repair export", effort: "cheap", solo: true},
		{arg: "adaptive rate limiting", brief: "adaptive rate limiting"},
		{arg: "--unknown repair export", brief: "--unknown repair export"},
		{arg: "review", intercepted: true},
		{arg: "apply", intercepted: true},
	} {
		t.Run(tc.arg, func(t *testing.T) {
			a, base, _ := taskApp(t)
			f := &surfaceTaskComputerAgent{taskFake: base}
			a.agent, a.taskComputers = f, surfaceTaskComputers{}
			drive(t, a, taskMsg(a.slash("/task "+tc.arg)))
			if tc.intercepted {
				if f.starts != 0 || f.localStarts != 0 {
					t.Fatal("a workspace verb was sent as a brief")
				}
				return
			}
			if f.starts != 0 || f.localStarts != 1 || f.brief != tc.brief || f.solo != tc.solo || f.effort != tc.effort {
				t.Fatalf("an existing form changed: %+v", f)
			}
		})
	}
}

func (f *surfaceTaskComputerAgent) StartTask(_ context.Context, brief string, solo bool) (uint64, string, string, error) {
	f.localStarts++
	f.brief, f.solo = brief, solo
	return 7, "Repair export", f.startNote, f.startErr
}

func (f *surfaceTaskComputerAgent) StartTaskOn(_ context.Context, brief string, solo bool, computer string, effort ...string) (uint64, string, string, error) {
	f.starts++
	f.brief, f.solo, f.computer = brief, solo, computer
	if len(effort) > 0 {
		f.effort = effort[0]
	}
	return 7, "Repair export", f.startNote, f.startErr
}

// TestTaskOnNamesTheComputerTheTaskRunsOn maps to DESIGN contract 6 at the
// command boundary: the named computer reaches R3's existing placement door.
func TestTaskOnNamesTheComputerTheTaskRunsOn(t *testing.T) {
	for _, tc := range []struct {
		arg, computer, brief string
		solo                 bool
	}{
		{`--on work repair export`, "work", "repair export", false},
		{`--on "Abir's Mac" repair export`, "Abir's Mac", "repair export", false},
		{`--on here solo repair export`, "here", "repair export", true},
	} {
		t.Run(tc.arg, func(t *testing.T) {
			a, base, _ := taskApp(t)
			f := &surfaceTaskComputerAgent{taskFake: base}
			a.agent, a.taskComputers = f, surfaceTaskComputers{}
			drive(t, a, taskMsg(a.slash("/task "+tc.arg)))
			if f.starts != 1 || f.localStarts != 0 || f.computer != tc.computer || f.brief != tc.brief || f.solo != tc.solo {
				t.Fatalf("the command did not reach placement: %+v", f)
			}
		})
	}
}

// TestTaskOnWithAnUnknownNameListsTheComputers maps to DESIGN contract 6:
// the placement door's correctable error reaches the person unchanged.
func TestTaskOnWithAnUnknownNameListsTheComputers(t *testing.T) {
	const problem = `no paired computer is named "missing" — your computers are: Abir's Mac, work computer`
	a, base, _ := taskApp(t)
	f := &surfaceTaskComputerAgent{taskFake: base, startErr: errors.New(problem)}
	a.agent, a.taskComputers = f, surfaceTaskComputers{}
	drive(t, a, taskMsg(a.slash("/task --on missing repair export")))
	if f.starts != 1 || !strings.Contains(lastNote(t, a), problem) {
		t.Fatalf("the unknown computer's answer did not reach the person: %q", lastNote(t, a))
	}
}

// TestTheProposalCardNamesItsExplicitOrDefaultComputer covers DESIGN 1.1's
// placement disclosure before approval, independent of the later run state.
func TestTheProposalCardNamesItsExplicitOrDefaultComputer(t *testing.T) {
	for _, tc := range []struct{ computer, preference, shown string }{
		{"", "", ""},
		{"Abir's Mac", "", "Abir's Mac"},
		{"", surfaceComputerB, "work computer"},
		{"here", surfaceComputerB, "this computer"},
		{"Abir's Mac", surfaceComputerB, "Abir's Mac"},
	} {
		t.Run(tc.computer+tc.preference, func(t *testing.T) {
			a, _, _ := taskApp(t)
			a.taskComputers = surfaceTaskComputers{{Device: surfaceComputerB, Name: "work computer", CanTakeTasks: true}, {Device: surfaceComputerC, Name: "Abir's Mac", CanTakeTasks: true}}
			if err := config.WriteTaskRunsOn(a.profileDir, tc.preference); err != nil {
				t.Fatal(err)
			}
			drive(t, a, taskMsg(a.readTaskComputers()))
			ev := proposal(a, 7, 0)
			ev.Task.Computer = tc.computer
			a.proposeTask(ev)
			text := plain(strings.Join(a.taskCardRows(a.task, 120, false), "\n"))
			if tc.shown == "" {
				if strings.Contains(text, "runs on") || strings.Contains(text, "ctrl+d computer") {
					t.Fatalf("a default proposal disclosed computers:\n%s", text)
				}
			} else if !strings.Contains(text, "runs on "+tc.shown) {
				t.Fatalf("the proposal does not say where it will run:\n%s", text)
			}
		})
	}
}

// TestNothingAboutComputersIsDrawnWhenNothingIsPaired covers the absence law
// for controls and local work. R9 keeps recorded placement visible regardless.
func TestNothingAboutComputersIsDrawnWhenNothingIsPaired(t *testing.T) {
	a, base, _ := taskApp(t)
	f := &surfaceTaskComputerAgent{taskFake: base}
	a.agent = f
	a.proposeTask(proposal(a, 7, 0))
	if text := plain(strings.Join(a.taskCardRows(a.task, 120, false), "\n")); strings.Contains(text, "runs on") || strings.Contains(text, "computer") {
		t.Fatalf("an unpaired proposal draws placement:\n%s", text)
	}
	if cmd, took := a.taskKey(key("ctrl+d")); cmd != nil || took {
		t.Fatal("an unpaired proposal takes the computer key")
	}
	// A saved executor is a fact, and does not enable unavailable controls.
	entry := session.TaskIndexEntry{ID: "9", Title: "Earlier export", Status: string(session.TaskDone), Computer: "work computer", RanElsewhere: true}
	reading := readTasks(session.World{}, a.taskSheetMine(), session.LastDays(a.now(), 1), tasksSort{}, a.now(), a.now())
	line := tasksLine{kind: tasksLineTask, item: &tasksItem{entry: entry}}
	if text := plain(reading.paint([]tasksLine{line}, 0, 160, a.pal, false)); !strings.Contains(text, "ran on work computer") {
		t.Fatalf("a saved executor disappeared with the controls: %s", text)
	}
	a.task.verdict = "approved"
	drive(t, a, key("ctrl+,"))
	for i := range settingTabs {
		a.sheet.tab = i
		a.sheet.build()
		for _, item := range a.sheet.items {
			if item.row.Key == config.KeyTaskRunsOn {
				t.Fatal("an unpaired conversation offers Run tasks on")
			}
		}
	}
	a.sheet.query.setText("Run tasks on")
	a.sheet.build()
	for _, item := range a.sheet.items {
		if item.row.Key == config.KeyTaskRunsOn {
			t.Fatal("search reveals the unavailable computer setting")
		}
	}
	a.dropSettings()
	if strings.Contains(a.taskCommandUsage(), "--on") {
		t.Fatal("the old command advertises a computer flag")
	}
	drive(t, a, taskMsg(a.runTaskCommand("--on work repair export")))
	if f.starts != 0 || f.localStarts != 1 || f.brief != "--on work repair export" {
		t.Fatalf("the unavailable flag changed the old brief grammar: %+v", f)
	}
}

func (f *surfaceTaskComputerAgent) RunTaskHere(_ context.Context, id uint64) (string, error) {
	f.here = append(f.here, id)
	return "running task 7 Repair export here", nil
}

func computerRecoveryApp(t *testing.T, state session.TaskState, remote *session.TaskRemoteRecord, words string) (*app, *surfaceTaskComputerAgent, session.TaskIndexEntry) {
	t.Helper()
	a, base, _ := taskApp(t)
	f := &surfaceTaskComputerAgent{taskFake: base}
	a.agent = f
	a.taskComputers = surfaceTaskComputers{{Device: surfaceComputerB, Name: "work computer", CanTakeTasks: true}}
	entry := session.TaskIndexEntry{ID: "7", SessionID: a.taskSheetSelfID(), Title: "Repair export", Status: string(state), Computer: "work computer", Unreachable: remote.Unreachable}
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(7, entry.Title, state, session.TaskNotice{Computer: entry.Computer, Unreachable: remote.Unreachable, Remote: remote, Doing: words, Report: words})})
	a.taskSheet.detail, a.taskSheet.detailOn = entry, true
	return a, f, entry
}

// TestAComputerThatStoppedAnsweringOffersRunItHere maps to DESIGN contracts 9
// and 11: the missing computer is said without claiming it stopped, and one
// deliberate action reaches the session's local replacement operation.
func TestAComputerThatStoppedAnsweringOffersRunItHere(t *testing.T) {
	const words = "work computer stopped answering — it may still be working"
	for _, surface := range []string{"card", "page"} {
		t.Run(surface, func(t *testing.T) {
			remote := &session.TaskRemoteRecord{Unreachable: true}
			a, f, entry := computerRecoveryApp(t, session.TaskRunning, remote, words)
			rows, _, _, _ := a.taskCardFrame(120, 40)
			if text := plain(strings.Join(rows, "\n")); !strings.Contains(text, words) || strings.Count(text, "h run it here") != 1 {
				t.Fatalf("the card does not give one local choice:\n%s", text)
			}
			if surface == "card" {
				drive(t, a, taskMsg(a.taskCardKey("h")))
			} else {
				a.taskSheet.detailOn = false
				a.room = a.newRoom(7, entry.Title)
				if hint := a.roomHint(); !strings.Contains(hint, "h run it here") {
					t.Fatalf("the page lost the local choice: %q", hint)
				}
				cmd, took := a.roomKey(key("h"))
				if !took {
					t.Fatal("the page did not take its local-choice key")
				}
				drive(t, a, taskMsg(cmd))
			}
			if len(f.here) != 1 || f.here[0] != 7 {
				t.Fatalf("the local choice reached the wrong task: %v", f.here)
			}
		})
	}
}

// TestAComputerNotTakingTasksSaysToRunServeOnIt maps to DESIGN contract 8:
// the session's actual refusal is retained beside the local replacement verb.
func TestAComputerNotTakingTasksSaysToRunServeOnIt(t *testing.T) {
	const words = "work computer is not taking tasks right now — run codeaf serve --take-tasks on it"
	a, f, _ := computerRecoveryApp(t, session.TaskInterrupted, &session.TaskRemoteRecord{StartFailed: true}, words)
	rows, _, _, _ := a.taskCardFrame(120, 40)
	if text := plain(strings.Join(rows, "\n")); !strings.Contains(text, words) || strings.Count(text, "h run it here") != 1 {
		t.Fatalf("the start failure does not explain its two choices:\n%s", text)
	}
	drive(t, a, taskMsg(a.taskCardKey("h")))
	if len(f.here) != 1 || f.here[0] != 7 {
		t.Fatalf("the failed start did not reach RunTaskHere: %v", f.here)
	}
}

// TestAnInterruptedRemoteTaskOffersRunItHere covers DESIGN section 4's
// interrupted case without treating a local interrupted task as remote work.
func TestAnInterruptedRemoteTaskOffersRunItHere(t *testing.T) {
	a, _, entry := computerRecoveryApp(t, session.TaskInterrupted, &session.TaskRemoteRecord{}, "")
	rows, _, _, _ := a.taskCardFrame(120, 40)
	if text := plain(strings.Join(rows, "\n")); !strings.Contains(text, "interrupted on work computer") || !strings.Contains(text, "h run it here") {
		t.Fatalf("the interrupted remote run has no local choice:\n%s", text)
	}
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(7, entry.Title, session.TaskRunning, session.TaskNotice{})})
	rows, _, _, _ = a.taskCardFrame(120, 40)
	if text := plain(strings.Join(rows, "\n")); strings.Contains(text, "work computer") || strings.Contains(text, "run it here") {
		t.Fatalf("the local replacement kept the remote placement:\n%s", text)
	}
}

// TestTheRunTasksOnRowListsPairedComputersAndDefaultsToThisComputer maps to
// DESIGN contract 7: names are shown, device identities are saved, and an
// unavailable preference falls back to this computer.
func TestTheRunTasksOnRowListsPairedComputersAndDefaultsToThisComputer(t *testing.T) {
	a, _ := sheetApp(t)
	a.taskComputers = surfaceTaskComputers{
		{Device: surfaceComputerB, Name: "Abir's Mac", CanTakeTasks: true},
		{Device: surfaceComputerC, Name: "work computer", CanTakeTasks: true},
		{Device: surfaceComputerWindows, Name: "Windows computer"},
	}
	drive(t, a, key("ctrl+,"))
	cursorTo(t, a, config.KeyTaskRunsOn)
	if text := plain(frame(a)); !strings.Contains(text, "Run tasks on") || !strings.Contains(text, "this computer") || !strings.Contains(text, "where a task goes when you do not name a computer") {
		t.Fatalf("the row does not explain its default:\n%s", text)
	}
	drive(t, a, key("enter"))
	if a.sheet.choice == nil {
		t.Fatal("Run tasks on did not offer computers")
	}
	text := plain(frame(a))
	for _, name := range []string{"this computer", "Abir's Mac", "work computer"} {
		if !strings.Contains(text, name) {
			t.Fatalf("the list does not offer %q:\n%s", name, text)
		}
	}
	if strings.Contains(text, "Windows computer") || strings.Contains(text, surfaceComputerB) {
		t.Fatalf("the list offers an unavailable computer or exposes its id:\n%s", text)
	}
	drive(t, a, key("home"), key("down"), key("enter"))
	if got := config.TaskRunsOnAt(a.profileDir); got != surfaceComputerB {
		t.Fatalf("the choice saved %q instead of the device id", got)
	}
	if text := plain(frame(a)); !strings.Contains(text, "Abir's Mac") || strings.Contains(text, surfaceComputerB) {
		t.Fatalf("the saved row does not show its computer's name:\n%s", text)
	}
	chooseSheetValue(t, a, "")
	if got := config.TaskRunsOnAt(a.profileDir); got != "" {
		t.Fatalf("this computer did not restore the default: %q", got)
	}
	if err := config.WriteTaskRunsOn(a.profileDir, surfaceComputerRemoved); err != nil {
		t.Fatal(err)
	}
	a.sheet.build()
	if text := plain(frame(a)); !strings.Contains(text, "this computer") || strings.Contains(text, surfaceComputerRemoved) {
		t.Fatalf("a removed computer did not fall back to this computer:\n%s", text)
	}
}

// TestARemoteTaskSaysRunningOnThenRanOn maps to DESIGN contract 2: placement
// follows the task across the rail, roster, room and saved result.
func TestARemoteTaskSaysRunningOnThenRanOn(t *testing.T) {
	a, _, _ := taskApp(t)
	a.taskComputers = surfaceTaskComputers{{Device: surfaceComputerB, Name: "work computer", CanTakeTasks: true}}
	remote := &session.TaskRemoteRecord{Executor: session.TaskExecutor{Device: surfaceComputerB, Name: "work computer"}}
	entry := session.TaskIndexEntry{ID: "7", SessionID: a.taskSheetSelfID(), Title: "Repair export", Status: string(session.TaskRunning), Computer: "work computer"}
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(7, entry.Title, session.TaskRunning, session.TaskNotice{Computer: entry.Computer, Remote: remote, Doing: "running on work computer"})})
	node := a.tasks[7]
	for surface, text := range map[string]string{
		"rail":          plain(strings.Join(a.railUnder(node, 100), "\n")),
		"Tasks":         plain(tasksRow(tasksLine{item: &tasksItem{entry: entry, runs: true}}, 160, a.now(), tasksSort{}, a.pal, false)),
		"compact Tasks": plain(tasksCardTail(tasksItem{entry: entry, runs: true}, a.now())),
		"page":          plain(strings.Join(a.taskCardBody(entry, 120), "\n")),
		"room":          plain(a.roomStateWord(node)),
	} {
		if strings.Count(text, "running on work computer") != 1 {
			t.Errorf("%s does not say where the work runs:\n%s", surface, text)
		}
	}
	entry.Status, entry.RanElsewhere = string(session.TaskDone), true
	remote.Finished, remote.Settled = true, true
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(7, entry.Title, session.TaskDone, session.TaskNotice{Computer: entry.Computer, RanElsewhere: true, Remote: remote, Doing: "ran on work computer"})})
	for surface, text := range map[string]string{
		"saved card": plain(strings.Join(a.taskCardBody(entry, 120), "\n")),
		"room":       plain(a.roomStateWord(a.tasks[7])),
	} {
		if !strings.Contains(text, "ran on work computer") {
			t.Errorf("%s lost the computer after finishing:\n%s", surface, text)
		}
	}
	entry.Computer, entry.RanElsewhere = "", false
	entry.ID, entry.SessionID = "8", "local conversation"
	if text := plain(strings.Join(a.taskCardBody(entry, 120), "\n")); strings.Contains(text, "ran on") || strings.Contains(text, "this computer") {
		t.Fatalf("local work drew placement words:\n%s", text)
	}
}
