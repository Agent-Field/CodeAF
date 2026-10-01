package tui3

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

type taskCommandFake struct {
	Agent
	singleCalls int
	brief       string
	// solo is what the last StartTask was told about the work's width: true
	// where the person said the work is one worker's.
	solo bool
	// note is the engine's where-the-work-stands line, handed back as is.
	note string
	err  error
}

func TestTaskStartLateAnswerDoesNotSayTheTaskFailedToStart(t *testing.T) {
	note := taskStartFailureNote(session.ErrSendUnanswered)
	if note != taskStartLateNote {
		t.Fatalf("late start note = %q, want %q", note, taskStartLateNote)
	}
	if strings.Contains(note, "could not start") {
		t.Fatalf("late start was reported as a start failure: %q", note)
	}
	f := &taskCommandFake{Agent: &fakeAgent{model: "m"}, err: session.ErrSendUnanswered}
	a := newTestApp(f)
	_, _ = a.Update(a.runTaskCommand("accepted work")())
	if got := lastNote(t, a); got != taskStartLateNote {
		t.Fatalf("task command displayed %q, want %q", got, taskStartLateNote)
	}
	if got := taskStartFailureNote(errors.New("the planner did not answer in time")); got != "could not start the task · the planner did not answer in time" {
		t.Fatalf("a definite refusal was changed to uncertainty: %q", got)
	}
}

func (f *taskCommandFake) StartTask(_ context.Context, brief string, solo bool) (uint64, string, string, error) {
	f.singleCalls++
	f.brief, f.solo = brief, solo
	return 7, "named work", f.note, f.err
}

// THE TYPED COMMAND WAITS ON NOTHING (#936). It used to raise a forming block
// that read `sizing it up…` and then `shaping the brief…` for as long as two
// model calls took in series; now the door admits the work at once and the
// brief and the width are read beside the worker, inside the engine. So the
// command's own answer IS the started message — no sizing call first, no batch
// with a pump beside it — and nothing on the tail claims a pause.
func TestTaskStartsAtOnceWithNoFormingBlock(t *testing.T) {
	f := &taskCommandFake{Agent: &fakeAgent{model: "m"}}
	a := newTestApp(f)
	cmd := a.runTaskCommand("write the release notes")
	if cmd == nil {
		t.Fatal("the command started nothing at all")
	}
	if a.waiting() {
		t.Fatalf("the command raised a forming block:\n%s", plainRowsText(a.preflightRows(60)))
	}
	frame := plainRowsText(a.preflightRows(60))
	if strings.Contains(frame, taskShapingNote) || strings.Contains(frame, "sizing it up") {
		t.Fatalf("the tail names a wait that no longer exists:\n%s", frame)
	}
	started, ok := cmd().(taskStartedMsg)
	if !ok {
		t.Fatal("the command's answer is not the started task")
	}
	if f.singleCalls != 1 || f.brief != "write the release notes" || f.solo {
		t.Fatalf("single=%d brief=%q solo=%v", f.singleCalls, f.brief, f.solo)
	}
	if started.id != "7" || started.title != "named work" || started.brief != "write the release notes" {
		t.Fatalf("started = %+v", started)
	}
	_, _ = a.Update(started)
	if got := lastNote(t, a); got != "single task 7 started · named work" {
		t.Fatalf("started note = %q", got)
	}
	if a.waiting() {
		t.Fatal("a wait appeared after the task landed")
	}
}

// THE ONE EXPLICIT FORM LEFT IS `solo`, and it tells the engine the work is one
// worker's, so nothing reads it for width.
func TestTaskSoloTellsTheDoorItIsOneWorkers(t *testing.T) {
	f := &taskCommandFake{Agent: &fakeAgent{model: "m"}}
	a := newTestApp(f)
	_, _ = a.Update(taskMsg(a.slash("/task solo fix it")))
	if f.singleCalls != 1 || f.brief != "fix it" || !f.solo {
		t.Fatalf("single=%d brief=%q solo=%v", f.singleCalls, f.brief, f.solo)
	}
}

// THE OLD SECOND WORD OPENS NOTHING OF ITS OWN. `/task adaptive <brief>` is a
// brief that happens to start with the word "adaptive": it takes the one road
// every other brief takes, the words are handed over exactly as they were typed,
// and one line says the word no longer means anything so that a person who meant
// the old shape is not left thinking they got it.
func TestTheRetiredAdaptiveWordIsJustAWordAndSaysSo(t *testing.T) {
	f := &taskCommandFake{Agent: &fakeAgent{model: "m"}}
	a := newTestApp(f)
	_, _ = a.Update(taskMsg(a.slash("/task adaptive map the api")))
	if f.singleCalls != 1 || f.brief != "adaptive map the api" || f.solo {
		t.Fatalf("single=%d brief=%q solo=%v", f.singleCalls, f.brief, f.solo)
	}
	if !holdsNote(a, taskAdaptiveRetiredNote) {
		t.Fatalf("nothing said the word retired: %q", noteTexts(a))
	}
}

// AND THE WORD ON ITS OWN IS NOT WORK. `/task adaptive` with nothing under it is
// muscle memory, not a brief, so it starts nothing — it gets the line about the
// retirement and the usage line, which between them name the whole vocabulary.
func TestTheUsageLineNamesOnlyTheTwoForms(t *testing.T) {
	f := &taskCommandFake{Agent: &fakeAgent{model: "m"}}
	a := newTestApp(f)
	if cmd := a.slash("/task adaptive"); cmd != nil {
		if msg := cmd(); msg != nil {
			_, _ = a.Update(msg)
		}
	}
	if f.singleCalls != 0 {
		t.Fatalf("the bare retired word started %d tasks", f.singleCalls)
	}
	usage := ""
	for _, text := range noteTexts(a) {
		if strings.HasPrefix(text, "usage:") {
			usage = text
		}
	}
	if usage == "" {
		t.Fatalf("no usage line at all: %q", noteTexts(a))
	}
	if !strings.Contains(usage, "/task <brief>") || !strings.Contains(usage, "/task solo <brief>") {
		t.Fatalf("the usage line does not name the two forms: %q", usage)
	}
	if strings.Contains(usage, "adaptive") {
		t.Fatalf("the usage line still offers a form that is gone: %q", usage)
	}
	// AND `/task solo` WITH NOTHING UNDER IT IS THE SAME EMPTY HAND, rather than a
	// task briefed "solo".
	b := newTestApp(&taskCommandFake{Agent: &fakeAgent{model: "m"}})
	if cmd := b.slash("/task solo"); cmd != nil {
		if msg := cmd(); msg != nil {
			_, _ = b.Update(msg)
		}
	}
	if got := lastNote(t, b); !strings.HasPrefix(got, "usage:") {
		t.Fatalf("a bare /task solo started something: %q", got)
	}
}

func TestHostedTaskUsesTheAgentDoor(t *testing.T) {
	f := &taskCommandFake{Agent: &fakeAgent{model: "m"}}
	a := newTestApp(f)
	a.host = "spark"
	cmd := a.runTaskCommand("solo fix the far parser")
	if cmd == nil {
		t.Fatal("the hosted command opened no task door")
	}
	_, _ = a.Update(taskMsg(cmd))
	if f.singleCalls != 1 || f.brief != "fix the far parser" || !f.solo {
		t.Fatalf("far starts=%d brief=%q solo=%v", f.singleCalls, f.brief, f.solo)
	}
	if got := lastNote(t, a); got != "single task 7 started · named work" {
		t.Fatalf("started note = %q", got)
	}
}

// holdsNote reports whether this exact note is standing in the transcript.
func holdsNote(a *app, text string) bool {
	for _, e := range a.entries {
		if e.kind == entryNote && e.text == text {
			return true
		}
	}
	return false
}

// noteTexts is what a failure above prints: every note the surface holds.
func noteTexts(a *app) []string {
	var out []string
	for _, e := range a.entries {
		if e.kind == entryNote {
			out = append(out, e.text)
		}
	}
	return out
}

func TestTaskStartsSingleAndErrorsBecomeNotes(t *testing.T) {
	f := &taskCommandFake{Agent: &fakeAgent{model: "m"}}
	a := newTestApp(f)
	cmd := a.slash("/task linear work")
	if cmd == nil {
		t.Fatal("the command returned no start")
	}
	_, _ = a.Update(taskMsg(cmd))
	if f.singleCalls != 1 {
		t.Fatal("the command did not start the work")
	}

	f.err = errors.New("unknown brief")
	started := taskStartedMsg{kind: "single", err: f.err}
	_, _ = a.Update(started)
	if got := lastNote(t, a); !strings.Contains(got, "unknown brief") {
		t.Fatalf("error note = %q", got)
	}
}

// WHERE THE WORK STANDS GETS ONE DIM LINE BESIDE THE STARTED ROW, and only when
// the engine had something to say about it: the note is the ground ladder's
// redirect, carried back from the door as it is. An ordinary start carries
// nothing: empty is the absence law.
func TestAStartedTaskCarriesTheEnginesNoteOnlyWhenThereIsOne(t *testing.T) {
	const where = "in place was asked for, and /src/app is a repository — the work goes on a branch cut from it instead"
	f := &taskCommandFake{Agent: &fakeAgent{model: "m"}, note: where}
	a := newTestApp(f)
	_, _ = a.Update(taskMsg(a.slash("/task port the parser")))
	if got := lastNote(t, a); got != where {
		t.Fatalf("last note = %q, want the engine's line carried under the started row", got)
	}
	if !holdsNote(a, "single task 7 started · named work") {
		t.Fatalf("the started row went missing: %q", noteTexts(a))
	}

	// AND NOT ON AN ORDINARY START.
	b := newTestApp(&taskCommandFake{Agent: &fakeAgent{model: "m"}})
	_, _ = b.Update(taskStartedMsg{kind: "single", id: "7", title: "port the parser"})
	if got := lastNote(t, b); got != "single task 7 started · port the parser" {
		t.Fatalf("an ordinary start said %q", got)
	}
}

// taskStartApp is a surface whose `starting a task` row is already answered. The
// row is read at the moment the command is typed ([config.TaskStartAt]), so a
// profile on disk is the only way to state it and the only way a test can.
func taskStartApp(t *testing.T, f *taskCommandFake, mode string) *app {
	t.Helper()
	a := newTestApp(f)
	a.profileDir = taskStartProfile(t, mode)
	return a
}

// taskStartProfile is that profile on its own, for the one assertion that is
// about the row and not about the command it steers.
func taskStartProfile(t *testing.T, mode string) string {
	t.Helper()
	dir := t.TempDir()
	body := []byte(`{"` + config.KeyTaskStart + `":"` + mode + `"}`)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), body, 0o600); err != nil {
		t.Fatalf("writing the profile: %v", err)
	}
	return dir
}

// SHIPPED, /task STARTS ONE WORKER THAT CAN SPLIT ITSELF. That is the default
// every other test on this page is written against, and a profile with nothing
// in it is a person who has never opened the settings panel. The word `ask` the
// row used to carry is gone with the card it named, and a profile still holding
// it reads as the default rather than as a row this build refuses. The default
// is not solo: the engine is free to read the work for width beside the worker.
func TestTaskStartDefaultsToOneWorkerThatCanSplit(t *testing.T) {
	f := &taskCommandFake{Agent: &fakeAgent{model: "m"}}
	a := taskStartApp(t, f, config.TaskStartSized)
	if got := config.TaskStartAt(a.profileDir); got != config.TaskStartSized {
		t.Fatalf("the row reads %q", got)
	}
	if got := config.TaskStartAt(t.TempDir()); got != config.TaskStartSized {
		t.Fatalf("an unanswered profile reads %q", got)
	}
	if got := config.TaskStartAt(taskStartProfile(t, "ask")); got != config.TaskStartSized {
		t.Fatalf("a profile left on the retired word reads %q", got)
	}
	_, _ = a.Update(taskMsg(a.slash("/task inspect both")))
	if f.singleCalls != 1 || f.solo {
		t.Fatalf("the default started %d workers, solo=%v", f.singleCalls, f.solo)
	}
}

// A PROFILE LEFT ON THE OLD ROW STARTS ONE WORKER LIKE EVERY OTHER ROW. Somebody
// who set `starting a task` to `adaptive` before this build is not sent down a
// road that no longer exists, and the word is not refused either: it reads as the
// default. The literal is deliberate — the setting's own constant may go, and
// what has to keep working is the string already sitting in people's profiles.
func TestAProfileLeftOnTheAdaptiveRowStartsOneWorker(t *testing.T) {
	f := &taskCommandFake{Agent: &fakeAgent{model: "m"}}
	a := taskStartApp(t, f, "adaptive")
	_, _ = a.Update(taskMsg(a.slash("/task inspect both")))
	if f.singleCalls != 1 || f.solo {
		t.Fatalf("the old row started %d workers, solo=%v", f.singleCalls, f.solo)
	}
}

// SET TO SINGLE, THE WORK IS ONE WORKER'S. This row is the person declining to
// have their brief read for width, said once in advance for every brief, so the
// door is told solo exactly as `/task solo` tells it.
func TestTaskStartSinglePassesSolo(t *testing.T) {
	f := &taskCommandFake{Agent: &fakeAgent{model: "m"}}
	a := taskStartApp(t, f, config.TaskStartSingle)
	_, _ = a.Update(taskMsg(a.slash("/task inspect both")))
	if f.singleCalls != 1 || !f.solo {
		t.Fatalf("single=%d solo=%v", f.singleCalls, f.solo)
	}
	if a.waiting() {
		t.Fatal("the single road raised a forming block")
	}
}

// AN EXPLICIT SOLO IS THE LAST WORD against a silent row: whatever `starting a
// task` says, `solo` starts one worker and tells the door so.
func TestSoloOverridesTheRow(t *testing.T) {
	for _, row := range []string{"adaptive", config.TaskStartSized, config.TaskStartSingle} {
		f := &taskCommandFake{Agent: &fakeAgent{model: "m"}}
		a := taskStartApp(t, f, row)
		_, _ = a.Update(taskMsg(a.slash("/task solo fix it")))
		if f.singleCalls != 1 || f.brief != "fix it" || !f.solo {
			t.Fatalf("solo under %q: single=%d brief=%q solo=%v", row, f.singleCalls, f.brief, f.solo)
		}
	}
}

// paintedRowsText is the block with its ink left on, for the tests that ask
// what colour or which glyph a row is wearing.
func paintedRowsText(rows []row) string {
	var out []string
	for _, r := range rows {
		out = append(out, r.text)
	}
	return strings.Join(out, "\n")
}

// plainRowsText is the forming block as a person would read it, with the ink
// taken off.
func plainRowsText(rows []row) string {
	var out []string
	for _, r := range rows {
		out = append(out, plain(r.text))
	}
	return strings.Join(out, "\n")
}

// approveProposal puts a proposal card for id on the surface and answers it
// yes, which is the one road that raises the forming block (task.go's
// [app.taskAnswered]).
func approveProposal(a *app, id uint64, name string) {
	a.task = &taskCard{id: id, title: "index the adapters", name: name}
	a.entries = append(a.entries, entry{kind: entryTask, turn: a.turn, card: a.task})
	a.taskAnswered(id, session.Answer{Key: "1", Picked: []string{"1"}})
}

// THE WAIT IS ALIVE WHILE IT IS RUNNING. The line saying a task was on its way
// was once a static dim note in the same lane as `⟲ 135.7k cached · saved
// $0.0069` — a finished fact, sitting under a screenful of other finished facts,
// while the surface stopped painting altogether. So the block wears the braille
// spinner and the count-up every other genuinely in-flight row on this surface
// wears, and its phase never enters the notes lane.
func TestTheFormingWaitCarriesASpinnerAndAClock(t *testing.T) {
	a := newTestApp(&taskCommandFake{Agent: &fakeAgent{model: "m"}})
	base := time.Now()
	a.clock = func() time.Time { return base }
	approveProposal(a, 41, "adapter index")

	// SIX SECONDS IN, the tail is rebuilt with the shared animation grid.
	a.clock = func() time.Time { return base.Add(6 * time.Second) }
	line := plainRowsText(a.preflightRows(60))
	if !strings.Contains(line, "· 6s") {
		t.Fatalf("the wait has no clock: %q", line)
	}
	painted := paintedRowsText(a.preflightRows(60))
	if !strings.ContainsAny(painted, "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏") {
		t.Fatalf("the wait has no spinner: %q", plain(painted))
	}
	if holdsNote(a, taskShapingNote) {
		t.Fatalf("a live phase entered the notes lane: %q", noteTexts(a))
	}
	// AND THE FRAME KEEPS BEING ASKED FOR. No turn is running between a yes and
	// the task's first update, so without this the spinner above would never
	// turn and the clock would never climb.
	if !a.waiting() {
		t.Fatal("the surface stopped painting while the wait was up")
	}
	// UNDER A SECOND IT SAYS NOTHING ABOUT ITS LENGTH — the emptiness law, in the
	// spelling every other live clock on this surface uses.
	a.clock = func() time.Time { return base }
	a.touch()
	if got := plainRowsText(a.preflightRows(60)); strings.Contains(got, "0s") {
		t.Fatalf("a wait that has just started is timing itself: %q", got)
	}
}

// EVERY ROW CARRIES THE ONE HAIRLINE, and the block is never taller than its
// three rows even when the terminal is narrow.
func TestTheFormingBlockKeepsItsHairlineAtNarrowWidths(t *testing.T) {
	a := newTestApp(&taskCommandFake{Agent: &fakeAgent{model: "m"}})
	base := time.Now()
	a.clock = func() time.Time { return base }
	approveProposal(a, 41, "an adapter index with a long name that has to be cut")
	a.clock = func() time.Time { return base.Add(3 * time.Minute) }

	for _, width := range []int{24, 30, 40, 60} {
		body := a.preflightRows(width)
		if len(body) != 3 {
			t.Fatalf("width %d drew %d rows, want three", width, len(body))
		}
		for i, drawn := range body {
			line := plain(drawn.text)
			if got := ansi.StringWidth(line); got > width {
				t.Fatalf("width %d row %d is %d cells wide: %q", width, i, got, line)
			}
			if !strings.HasPrefix(line, "▏ ") {
				t.Fatalf("width %d row %d lost the hairline: %q", width, i, line)
			}
		}
	}
}

// AN ERROR IS ONE LINE AND NOTHING ELSE. The door refused the brief, and since
// the typed road raises no block there is no scaffold to collapse — only the
// refusal, said in the note lane where finished facts go.
func TestTaskErrorBecomesTheErrorLine(t *testing.T) {
	f := &taskCommandFake{Agent: &fakeAgent{model: "m"}, err: errors.New("unknown brief")}
	a := newTestApp(f)
	_, _ = a.Update(taskMsg(a.slash("/task impossible work")))
	if a.waiting() || len(a.preflightRows(60)) != 0 {
		t.Fatal("a forming block stood beside an error")
	}
	if got := lastNote(t, a); got != "could not start the task · unknown brief" {
		t.Fatalf("error line = %q", got)
	}
}

// AND A SPINNER NOBODY IS PAINTING IS A PHOTOGRAPH OF A SPINNER. This is the
// other half of the test above, and the half that was missing: the forming
// block was on the paint clock's list of reasons to KEEP turning, and on
// nothing's list of reasons to START. Its door opens while the surface is still
// — a yes on a proposal card is answered after the turn that raised it has
// ended — so `a.painting` was false, no frame was ever asked for, and the block
// sat with a motionless `⠙` and a count-up frozen at nothing for the whole
// pause. From in front of it: static and stuck, which is exactly what the block
// was built to end.
func TestTheFormingBlockArmsTheFrameClockFromAStillSurface(t *testing.T) {
	a := newTestApp(&taskCommandFake{Agent: &fakeAgent{model: "m"}})
	// Stood down first, because that is the state the defect lived in: a wake
	// that finds the clock already up answers nil for a good reason, and what is
	// under test is the wake that has real work to do.
	a.painting = false
	approveProposal(a, 41, "adapter index")
	if !a.waiting() {
		t.Fatal("the yes raised no forming block")
	}
	_, cmd := a.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
	framed := false
	for _, msg := range runCmd(cmd) {
		if _, ok := msg.(frameMsg); ok {
			framed = true
		}
	}
	if !framed {
		t.Fatal("the forming block is up with no frame on the way")
	}
	if !a.painting {
		t.Fatal("the clock was never claimed")
	}
}

// AND THE SPINNER ACTUALLY MOVES BETWEEN THOSE FRAMES, which is the fact the
// person reports on: two paints apart, the block is not the same picture.
func TestTheFormingBlockSpinnerAdvancesBetweenPaints(t *testing.T) {
	a := newTestApp(&taskCommandFake{Agent: &fakeAgent{model: "m"}})
	base := time.Now()
	a.clock = func() time.Time { return base }
	approveProposal(a, 41, "adapter index")

	a.paints = 0
	first := plainRowsText(a.preflightRows(60))
	a.paints = spinnerStep
	a.clock = func() time.Time { return base.Add(2 * time.Second) }
	second := plainRowsText(a.preflightRows(60))
	if first == second {
		t.Fatalf("the forming block is the same picture two frames apart: %q", first)
	}
	// The linear tier keeps its still mark on purpose — a claim repeated thirty
	// times a second is heard thirty times a second by a surface being read
	// aloud — so only the count-up moves there.
	a.linear = true
	a.paints = 0
	still := plainRowsText(a.preflightRows(60))
	a.paints = spinnerStep
	if got := plainRowsText(a.preflightRows(60)); got != still {
		t.Fatalf("the linear tier animated its mark: %q then %q", still, got)
	}
}

// taskMsg is what a task command handed back, with the paint clock's own tick
// looked past. Anything else on screen that is live ARMS that clock
// ([app.Update]), so what a door returns may be a batch — the door's work, and
// one frame — and a test asking what the door did is not asking about the
// frame.
func taskMsg(cmd tea.Cmd) tea.Msg {
	for _, msg := range runCmd(cmd) {
		if _, tick := msg.(frameMsg); !tick {
			return msg
		}
	}
	return nil
}

func TestNoTaskCommandDrawsNoFormingBlock(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	if got := a.preflightRows(60); len(got) != 0 {
		t.Fatalf("idle surface drew a forming block: %q", plainRowsText(got))
	}
}

// THE ONE DOOR THAT WEARS THE BLOCK. A proposal the person approves has a pause
// before its task exists, so the yes raises the forming block — the card's own
// name on the identity line, unquoted, because nobody typed it — and the first
// update for that task's id collapses it.
func TestAnApprovedProposalRaisesTheFormingBlockUntilItsTaskExists(t *testing.T) {
	a := newTestApp(&taskCommandFake{Agent: &fakeAgent{model: "m"}})
	approveProposal(a, 41, "adapter index")
	frame := plainRowsText(a.preflightRows(60))
	if !strings.Contains(frame, "▏ task") || !strings.Contains(frame, "▏ adapter index") {
		t.Fatalf("the approved proposal raised no forming block:\n%s", frame)
	}
	if strings.Contains(frame, `"adapter index"`) {
		t.Fatalf("the card's name wears quotes nobody typed:\n%s", frame)
	}
	if !strings.Contains(frame, taskShapingNote) {
		t.Fatalf("the block does not say what it is waiting on:\n%s", frame)
	}
	// An update for a DIFFERENT task settles nothing; the first breath of this
	// one collapses the scaffold in the same frame its row lands.
	a.settleProposalWait(7)
	if !a.waiting() {
		t.Fatal("another task's update stole the block")
	}
	a.settleProposalWait(41)
	if a.waiting() {
		t.Fatal("the block outlived its task's first update")
	}
	if got := plainRowsText(a.preflightRows(60)); got != "" {
		t.Fatalf("the scaffold survived the collapse: %q", got)
	}
}

// A no is not a pause: nothing is coming, so nothing forms.
func TestADeclinedProposalRaisesNoFormingBlock(t *testing.T) {
	a := newTestApp(&taskCommandFake{Agent: &fakeAgent{model: "m"}})
	a.task = &taskCard{id: 42, title: "index the adapters", name: "adapter index"}
	a.entries = append(a.entries, entry{kind: entryTask, turn: a.turn, card: a.task})
	a.taskAnswered(a.task.id, session.Answer{Key: "2", Picked: []string{"2"}})
	if a.waiting() {
		t.Fatal("a declined proposal left a forming block on screen")
	}
}
