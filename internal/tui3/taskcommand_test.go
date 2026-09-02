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

	"github.com/Agent-Field/aforge-v2/internal/config"
)

type taskCommandFake struct {
	Agent
	judgeCalls, singleCalls int
	yes                     bool
	parts                   []string
	why                     string
	brief                   string
	err                     error
}

func (f *taskCommandFake) StartTask(_ context.Context, brief string) (uint64, string, error) {
	f.singleCalls++
	f.brief = brief
	return 7, "named work", f.err
}
func (f *taskCommandFake) JudgeDecomposable(_ context.Context, brief string) (bool, []string, string) {
	f.judgeCalls++
	f.brief = brief
	return f.yes, f.parts, f.why
}

// THE ONE EXPLICIT FORM LEFT IS `solo`, and it skips the sizing call outright.
func TestTaskSoloSkipsSizing(t *testing.T) {
	f := &taskCommandFake{Agent: &fakeAgent{model: "m"}}
	a := newTestApp(f)
	_, _ = a.Update(taskMsg(a.slash("/task solo fix it")))
	if f.judgeCalls != 0 {
		t.Fatal("an explicit form called the judge")
	}
	if f.singleCalls != 1 || f.brief != "fix it" {
		t.Fatalf("single=%d brief=%q", f.singleCalls, f.brief)
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
	_, cmd := a.Update(taskMsg(a.slash("/task adaptive map the api")))
	if cmd == nil {
		t.Fatal("the retired word started nothing at all")
	}
	_, _ = a.Update(taskMsg(cmd))
	if f.judgeCalls != 1 {
		t.Fatalf("%d sizing calls, want the ordinary one", f.judgeCalls)
	}
	if f.singleCalls != 1 || f.brief != "adaptive map the api" {
		t.Fatalf("single=%d brief=%q", f.singleCalls, f.brief)
	}
	if !holdsNote(a, taskAdaptiveRetiredNote) {
		t.Fatalf("nothing said the word retired: %q", noteTexts(a))
	}
}

// AND THE WORD ON ITS OWN IS NOT WORK. `/task adaptive` with nothing under it is
// muscle memory, not a brief, so it buys no shaping call — it gets the line about
// the retirement and the usage line, which between them name the whole vocabulary.
func TestTheUsageLineNamesOnlyTheTwoForms(t *testing.T) {
	f := &taskCommandFake{Agent: &fakeAgent{model: "m"}}
	a := newTestApp(f)
	if cmd := a.slash("/task adaptive"); cmd != nil {
		if msg := cmd(); msg != nil {
			_, _ = a.Update(msg)
		}
	}
	if f.judgeCalls != 0 || f.singleCalls != 0 {
		t.Fatalf("the bare retired word spent something: judge=%d single=%d", f.judgeCalls, f.singleCalls)
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
	if f.singleCalls != 1 || f.brief != "fix the far parser" {
		t.Fatalf("far starts=%d brief=%q", f.singleCalls, f.brief)
	}
	if got := lastNote(t, a); got != "single task 7 started · named work" {
		t.Fatalf("started note = %q", got)
	}
}

// THE SIZING JUDGE'S YES STARTS THE WORK, and it starts it as ONE WORKER. There
// is no card in the way any more: the question the card asked — should this run
// wide — is answered later and from the material, by the worker that has opened
// it (internal/session's task_divide.go), and a yes here is what arms it to
// answer at all. So the only thing the surface owes the person is the one line
// saying their task may not stay one task.
func TestAWideBriefStartsOneWorkerAndSaysSoWithoutAsking(t *testing.T) {
	f := &taskCommandFake{Agent: &fakeAgent{model: "m"}, yes: true, parts: []string{"api scan", "ui scan"}, why: "independent"}
	a := newTestApp(f)
	_, cmd := a.Update(taskMsg(a.slash("/task inspect both")))
	if cmd == nil {
		t.Fatal("a yes started nothing at all")
	}
	_, _ = a.Update(taskMsg(cmd))
	if f.singleCalls != 1 || f.brief != "inspect both" {
		t.Fatalf("single=%d brief=%q", f.singleCalls, f.brief)
	}
	if !holdsNote(a, taskWideNote) {
		t.Fatalf("nothing on the surface said the work was wide: %q", noteTexts(a))
	}
	// AND THE LINE STAYS. It is a fact and not a wait, unlike the two notes
	// either side of it, so nothing takes it back when the task lands.
	if a.waiting() {
		t.Fatal("a wait outlived the command that raised it")
	}
}

// A NO SAYS NOTHING. The line is written on the one answer that makes it true,
// so narrow work reads exactly as it did before the division road existed.
func TestNarrowWorkStartsWithNoLineAboutWidth(t *testing.T) {
	f := &taskCommandFake{Agent: &fakeAgent{model: "m"}}
	a := newTestApp(f)
	_, cmd := a.Update(taskMsg(a.slash("/task write the release notes")))
	if cmd == nil {
		t.Fatal("a no started nothing at all")
	}
	_, _ = a.Update(taskMsg(cmd))
	if holdsNote(a, taskWideNote) {
		t.Fatalf("narrow work was announced as wide: %q", noteTexts(a))
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

func TestTaskNoStartsSingleAndErrorsBecomeNotes(t *testing.T) {
	f := &taskCommandFake{Agent: &fakeAgent{model: "m"}}
	a := newTestApp(f)
	msg := a.slash("/task linear work")()
	_, cmd := a.Update(msg)
	if cmd == nil {
		t.Fatal("a no did not return the single start command")
	}
	_, _ = a.Update(taskMsg(cmd))
	if f.singleCalls != 1 {
		t.Fatal("a no did not start single")
	}

	f.err = errors.New("unknown brief")
	started := taskStartedMsg{kind: "single", err: f.err}
	_, _ = a.Update(started)
	if got := lastNote(t, a); !strings.Contains(got, "unknown brief") {
		t.Fatalf("error note = %q", got)
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
// it reads as the default rather than as a row this build refuses.
func TestTaskStartDefaultsToOneWorkerThatCanSplit(t *testing.T) {
	f := &taskCommandFake{Agent: &fakeAgent{model: "m"}, yes: true, parts: []string{"api", "ui"}}
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
	_, cmd := a.Update(taskMsg(a.slash("/task inspect both")))
	if cmd == nil {
		t.Fatal("the default started nothing")
	}
	_, _ = a.Update(taskMsg(cmd))
	if f.singleCalls != 1 {
		t.Fatalf("the default started %d workers", f.singleCalls)
	}
}

// A PROFILE LEFT ON THE OLD ROW STARTS ONE WORKER LIKE EVERY OTHER ROW. Somebody
// who set `starting a task` to `adaptive` before this build is not sent down a
// road that no longer exists, and the word is not refused either: it reads as the
// default, which is the same thing every other answer to that row now means for
// wide work. The literal is deliberate — the setting's own constant may go, and
// what has to keep working is the string already sitting in people's profiles.
func TestAProfileLeftOnTheAdaptiveRowStartsOneWorker(t *testing.T) {
	for _, parallel := range []bool{true, false} {
		f := &taskCommandFake{Agent: &fakeAgent{model: "m"}, yes: parallel, parts: []string{"api", "ui"}, why: "independent"}
		a := taskStartApp(t, f, "adaptive")
		_, cmd := a.Update(taskMsg(a.slash("/task inspect both")))
		if cmd == nil {
			t.Fatal("nothing was started")
		}
		_, _ = a.Update(taskMsg(cmd))
		if f.judgeCalls != 1 {
			t.Fatalf("%d sizing calls, want one", f.judgeCalls)
		}
		if f.singleCalls != 1 {
			t.Fatalf("wide=%v started %d workers", parallel, f.singleCalls)
		}
		if holdsNote(a, taskWideNote) != parallel {
			t.Fatalf("wide=%v said %q", parallel, noteTexts(a))
		}
	}
}

// SET TO SINGLE, THE SIZING CALL IS NOT MADE AT ALL. This row is the person
// declining to have their brief read for width, and a reading nobody wants is a
// bill with nothing behind it — so nothing is spent, and nothing is said about
// width either.
func TestTaskStartSingleSkipsTheSizingCall(t *testing.T) {
	f := &taskCommandFake{Agent: &fakeAgent{model: "m"}, yes: true, parts: []string{"api", "ui"}}
	a := taskStartApp(t, f, config.TaskStartSingle)
	cmd := a.slash("/task inspect both")
	if cmd == nil {
		t.Fatal("nothing was started")
	}
	_, _ = a.Update(taskMsg(cmd))
	if f.judgeCalls != 0 {
		t.Fatal("single paid for a sizing call it had already answered")
	}
	if f.singleCalls != 1 {
		t.Fatalf("single=%d", f.singleCalls)
	}
	if holdsNote(a, taskWideNote) {
		t.Fatal("a road that never read the brief still claimed the work was wide")
	}
}

// AN EXPLICIT SOLO IS THE LAST WORD against a silent row: whatever `starting a
// task` says, `solo` skips the sizing call and starts one worker.
func TestSoloOverridesTheRow(t *testing.T) {
	for _, row := range []string{"adaptive", config.TaskStartSized, config.TaskStartSingle} {
		f := &taskCommandFake{Agent: &fakeAgent{model: "m"}, yes: true, parts: []string{"api"}}
		a := taskStartApp(t, f, row)
		_, _ = a.Update(taskMsg(a.slash("/task solo fix it")))
		if f.judgeCalls != 0 {
			t.Fatalf("solo under %q called the sizing judge", row)
		}
		if f.singleCalls != 1 || f.brief != "fix it" {
			t.Fatalf("solo under %q: single=%d brief=%q", row, f.singleCalls, f.brief)
		}
	}
}

// THE FORMING BLOCK IS REPLACED WHEN THE TASK LANDS. The same update that writes
// the settled row clears the scaffold, so there is no intermediate frame where
// finished work still claims to be shaping.
func TestTheShapingBlockCollapsesIntoTheSettledRow(t *testing.T) {
	f := &taskCommandFake{Agent: &fakeAgent{model: "m"}}
	a := taskStartApp(t, f, config.TaskStartSingle)
	cmd := a.slash("/task write the release notes")
	before := plainRowsText(a.preflightRows(60))
	if !strings.Contains(before, "▏ task") || !strings.Contains(before, `▏ "write the release notes"`) {
		t.Fatalf("the forming block lost the command:\n%s", before)
	}
	_, _ = a.Update(taskMsg(cmd))
	if got := lastNote(t, a); !strings.Contains(got, "task 7 started") {
		t.Fatalf("last note = %q", got)
	}
	frame := plainRowsText(a.preflightRows(60))
	if frame != "" {
		t.Fatalf("the scaffold survived in the settled frame: %q", frame)
	}
	if a.waiting() {
		t.Fatal("the wait's clock outlived the wait")
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
// taken off. It takes ROWS because the block is pressable now — the tail is a
// door, and each row of a block of several is one (formingblock.go).
func plainRowsText(rows []row) string {
	var out []string
	for _, r := range rows {
		out = append(out, plain(r.text))
	}
	return strings.Join(out, "\n")
}

// THE WAIT IS ALIVE WHILE IT IS RUNNING, and that is the whole defect: shaping a
// brief is up to twenty-five seconds of a model call, and the line saying so was
// a static dim note in the same lane as `⟲ 135.7k cached · saved $0.0069` — a
// finished fact, sitting under a screenful of other finished facts, while the
// surface stopped painting altogether. So it wears the braille spinner and the
// count-up every other genuinely in-flight row on this surface wears.
func TestTheShapingWaitCarriesASpinnerAndAClock(t *testing.T) {
	f := &taskCommandFake{Agent: &fakeAgent{model: "m"}}
	a := taskStartApp(t, f, config.TaskStartSingle)
	base := time.Now()
	a.clock = func() time.Time { return base }
	_ = a.slash("/task write the release notes")

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
	// AND THE FRAME KEEPS BEING ASKED FOR. No turn is running while a command
	// sizes and shapes a brief, so without this the spinner above would never
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

// EVERY ROW CARRIES THE ONE HAIRLINE, and the quoted brief never takes more
// than two rows even when the terminal is narrow.
func TestTheShapingBlockKeepsItsHairlineAtNarrowWidths(t *testing.T) {
	f := &taskCommandFake{Agent: &fakeAgent{model: "m"}}
	a := taskStartApp(t, f, config.TaskStartSingle)
	base := time.Now()
	a.clock = func() time.Time { return base }
	_ = a.slash("/task write the release notes")
	a.clock = func() time.Time { return base.Add(3 * time.Minute) }

	for _, width := range []int{24, 30, 40, 60} {
		body := a.preflightRows(width)
		if len(body) == 0 {
			t.Fatalf("width %d drew no wait at all", width)
		}
		if len(body) > 4 {
			t.Fatalf("width %d drew %d rows, want at most four", width, len(body))
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

func TestTaskPhasesAdvanceInsideOneBlockAndNeverEnterNotes(t *testing.T) {
	f := &taskCommandFake{Agent: &fakeAgent{model: "m"}}
	a := newTestApp(f)
	cmd := a.slash("/task fix the flaky auth test")
	if got := plainRowsText(a.preflightRows(60)); !strings.Contains(got, taskSizingNote) {
		t.Fatalf("sizing block = %q", got)
	}
	if holdsNote(a, taskSizingNote) || holdsNote(a, taskShapingNote) {
		t.Fatalf("a live phase entered the notes lane: %q", noteTexts(a))
	}
	_, _ = a.Update(taskMsg(cmd))
	if got := plainRowsText(a.preflightRows(60)); !strings.Contains(got, taskShapingNote) || strings.Contains(got, taskSizingNote) {
		t.Fatalf("shaping did not replace sizing in place: %q", got)
	}
}

func TestTaskErrorCollapsesTheBlockToTheErrorLine(t *testing.T) {
	f := &taskCommandFake{Agent: &fakeAgent{model: "m"}, err: errors.New("unknown brief")}
	a := taskStartApp(t, f, config.TaskStartSingle)
	_, _ = a.Update(taskMsg(a.slash("/task impossible work")))
	if a.waiting() || len(a.preflightRows(60)) != 0 {
		t.Fatal("the forming block outlived an error")
	}
	if got := lastNote(t, a); got != "could not start the task · unknown brief" {
		t.Fatalf("error line = %q", got)
	}
}

// AND A SPINNER NOBODY IS PAINTING IS A PHOTOGRAPH OF A SPINNER. This is the
// other half of the test above, and the half that was missing: [preflight.live]
// was on the paint clock's list of reasons to KEEP turning, and on nothing's
// list of reasons to START. Both doors onto the forming block open while the
// surface is still — nobody types `/task` mid-turn, and a yes on a proposal card
// is answered after the turn that raised it has ended — so `a.painting` was
// false, no frame was ever asked for, and the block sat with a motionless `⠙`
// and a count-up frozen at nothing for the whole shaping call. From in front of
// it: static and stuck, which is exactly what the block was built to end.
func TestTheFormingBlockArmsTheFrameClockFromAStillSurface(t *testing.T) {
	for _, road := range []struct {
		name  string
		raise func(*app)
	}{
		{"typed", func(a *app) { _ = a.slash("/task write the release notes") }},
		{"approved", func(a *app) {
			a.task = &taskCard{id: 41, title: "index the adapters", name: "adapter index"}
			a.entries = append(a.entries, entry{kind: entryTask, turn: a.turn, card: a.task})
			a.answerTask(true, "")
		}},
	} {
		t.Run(road.name, func(t *testing.T) {
			f := &taskCommandFake{Agent: &fakeAgent{model: "m"}}
			a := taskStartApp(t, f, config.TaskStartSingle)
			// Stood down first, because that is the state the defect lived in: a
			// wake that finds the clock already up answers nil for a good reason,
			// and what is under test is the wake that has real work to do.
			a.painting = false
			road.raise(a)
			if !a.waiting() {
				t.Fatal("the door raised no forming block")
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
		})
	}
}

// AND THE SPINNER ACTUALLY MOVES BETWEEN THOSE FRAMES, which is the fact the
// person reports on: two paints apart, the block is not the same picture.
func TestTheFormingBlockSpinnerAdvancesBetweenPaints(t *testing.T) {
	f := &taskCommandFake{Agent: &fakeAgent{model: "m"}}
	a := taskStartApp(t, f, config.TaskStartSingle)
	base := time.Now()
	a.clock = func() time.Time { return base }
	_ = a.slash("/task write the release notes")

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
// looked past. A forming block on screen ARMS that clock ([app.Update]), so what
// these doors return is a batch — the door's work, and one frame — and a test
// asking what the door did is not asking about the frame.
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

// THE OTHER DOOR WEARS THE SAME BLOCK. A proposal the person approves stands in
// the identical shaping pause the typed command does, so the yes raises the one
// forming block — the card's own name on the identity line, unquoted, because
// nobody typed it — and the first update for that task's id collapses it.
func TestAnApprovedProposalRaisesTheFormingBlockUntilItsTaskExists(t *testing.T) {
	a := newTestApp(&taskCommandFake{Agent: &fakeAgent{model: "m"}})
	a.task = &taskCard{id: 41, title: "index the adapters", name: "adapter index"}
	a.entries = append(a.entries, entry{kind: entryTask, turn: a.turn, card: a.task})
	a.answerTask(true, "")
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
	a.paste("alpha\nbeta\ngamma")
	literal := a.input.String()
	a.answerTask(false, "")
	if a.waiting() {
		t.Fatal("a declined proposal left a forming block on screen")
	}
	if len(a.pastes) != 0 {
		t.Fatalf("declining left paste metadata %+v", a.pastes)
	}
	a.input.setText(literal)
	if got := a.pastesUnfolded(literal); got != literal {
		t.Fatalf("a later literal token unfolded as %q", got)
	}
}
