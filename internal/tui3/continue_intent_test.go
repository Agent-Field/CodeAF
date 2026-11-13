package tui3

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/session"
)

type intentTaker struct {
	started  chan string
	proceed  chan struct{}
	released []string
	seen     []directory.LeaseExpectation
	files    map[string]string
	fail     bool
}

func (f *intentTaker) SafeToContinue(context.Context, string, string, bool) (bool, error) {
	return true, nil
}
func (f *intentTaker) Take(ctx context.Context, id string) (Taken, error) {
	return f.TakeSeen(ctx, id, nil)
}
func (f *intentTaker) TakeSeen(ctx context.Context, id string, seen *directory.LeaseExpectation) (Taken, error) {
	f.seen = append(f.seen, *seen)
	f.started <- id
	if id == "x" {
		<-f.proceed
		if f.fail {
			return Taken{}, errors.New("failed x")
		}
	}
	return Taken{Fence: 42, Transcript: f.files[id]}, nil
}
func (f *intentTaker) ReleaseUnused(ctx context.Context, id string, fence uint64) error {
	if ctx.Err() != nil {
		panic("cleanup inherited a cancelled context")
	}
	if _, ok := ctx.Deadline(); !ok || fence != 42 {
		panic("unbounded or incorrect cleanup")
	}
	f.released = append(f.released, id)
	return nil
}
func intentRig(t *testing.T) (*app, *intentTaker, []chatlist.Row) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	lab := newHomeLab(t)
	ws := lab.workspace("p")
	mine := lab.session("p", "local", "local", ws, time.Now())
	f := &intentTaker{started: make(chan string, 4), proceed: make(chan struct{}), files: map[string]string{}}
	for _, id := range []string{"x", "y"} {
		f.files[id] = lab.session("p", id, id, ws, time.Now())
	}
	a := lab.app(mine)
	a.width, a.height = 200, 70
	drain(t, a, a.openHome())
	rows := []chatlist.Row{
		{Cell: "x", Title: "automatic chat", Device: "tower", DeviceID: "dev-x", Fence: 7, Status: chatlist.Idle, Released: true, ReleasedAgo: time.Minute},
		{Cell: "y", Title: "chosen chat", Device: "laptop", DeviceID: "dev-y", Fence: 9, Status: chatlist.Off},
	}
	a.taker = f
	a.machineRead.rows = rows
	a.rebuildMachines()
	return a, f, rows
}
func beginAutomatic(t *testing.T, a *app) tea.Cmd {
	t.Helper()
	a.autoContinue = true
	check := a.continueAfterQuit(nil)
	if check == nil {
		t.Fatal("no startup safety check")
	}
	cmds := a.continuationReady(check().(continuationChecked))
	if cmds == nil {
		t.Fatal("no automatic take")
	}
	return cmds
}
func TestAPersonsPickIsTheChatThatOpens(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[failed], func(t *testing.T) {
			a, f, rows := intentRig(t)
			f.fail = failed
			result := make(chan []tea.Msg, 1)
			cmd := beginAutomatic(t, a)
			go func() { result <- collectBatchMsgs(cmd) }()
			if id := <-f.started; id != "x" {
				t.Fatal(id)
			}
			// Confirm the captured row, even if a later listing has a different ownership.
			standOn(t, a, rows[1].Cell)
			confirm(t, a)
			if a.home.msg != "continuing chosen chat next; automatic chat is still on its way here" {
				t.Fatalf("waiting choice: %q", a.home.msg)
			}
			select {
			case id := <-f.started:
				t.Fatalf("concurrent take %s", id)
			default:
			}
			a.machineRead.rows[1].Fence = 99
			// A committed acquisition may answer after its request was cancelled.
			// Cleanup must use its own bounded context rather than inherit this one.
			a.homeTakeCancel()
			close(f.proceed)
			for _, msg := range <-result {
				_, cmd := a.Update(msg)
				drain(t, a, cmd)
			}
			if a.file != f.files["y"] {
				t.Fatalf("opened %q, want y", a.file)
			}
			if !reflect.DeepEqual(f.seen, []directory.LeaseExpectation{{Device: "dev-x", Fence: 7}, {Device: "dev-y", Fence: 9}}) {
				t.Fatal(f.seen)
			}
			want := []string{"x"}
			if failed {
				want = nil
			}
			if !reflect.DeepEqual(f.released, want) {
				t.Fatalf("released %v", f.released)
			}
			if strings.Contains(homeText(a), "failed x") {
				t.Fatal("abandoned failure raised a card")
			}
		})
	}
}
func TestPickingTheArrivingChatJoinsItsMove(t *testing.T) {
	a, f, rows := intentRig(t)
	cmd := beginAutomatic(t, a)
	a.offerContinue(rows[0], chatlist.OfferFor(rows[0]))
	confirm(t, a)
	if strings.Contains(a.home.msg, continueAlreadyRunning) {
		t.Fatal(a.home.msg)
	}
	close(f.proceed)
	drain(t, a, cmd)
	if a.file != f.files["x"] || len(f.seen) != 1 || len(f.released) != 0 {
		t.Fatalf("join file=%s takes=%v releases=%v", a.file, f.seen, f.released)
	}
}

func TestAnExplicitMoveKeepsTheBusyAnswer(t *testing.T) {
	a, f, rows := intentRig(t)
	cmd := a.takeRemote(rows[0])
	standOn(t, a, rows[1].Cell)
	confirm(t, a)
	q, ok := a.homeAsking()
	if !ok || q.question.Reason != continueAlreadyRunning || a.home.msg != continueAlreadyRunning {
		t.Fatalf("explicit move lost its busy card: %s", homeText(a))
	}
	close(f.proceed)
	drain(t, a, cmd)
	if a.file != f.files["x"] || len(f.seen) != 1 || len(f.released) != 0 {
		t.Fatalf("explicit move changed: %s %+v %v", a.file, f.seen, f.released)
	}
}

func TestABareStartWithoutInputStillContinuesTheLatestRelease(t *testing.T) {
	a, f, _ := intentRig(t)
	close(f.proceed)
	drain(t, a, beginAutomatic(t, a))
	if a.file != f.files["x"] || len(f.seen) != 1 {
		t.Fatal("unattended startup changed")
	}
}
func TestALateAutomaticMoveDoesNotOpenOverAChosenChat(t *testing.T) {
	a, f, _ := intentRig(t)
	cmd := beginAutomatic(t, a)
	line := homeLine{kind: homeSession, row: session.SessionRow{ID: "local", Transcript: a.file}}
	drain(t, a, a.homeOpenLine(line))
	chosen := a.file
	close(f.proceed)
	drain(t, a, cmd)
	if a.file != chosen || !reflect.DeepEqual(f.released, []string{"x"}) {
		t.Fatalf("late move file=%s released=%v", a.file, f.released)
	}
}
func TestEscapeDropsAWaitingPick(t *testing.T) {
	a, f, rows := intentRig(t)
	cmd := beginAutomatic(t, a)
	a.offerContinue(rows[1], chatlist.OfferFor(rows[1]))
	confirm(t, a)
	drive(t, a, key("esc"))
	chosen := a.file
	close(f.proceed)
	drain(t, a, cmd)
	if len(f.seen) != 1 || a.file != chosen || !reflect.DeepEqual(f.released, []string{"x"}) {
		t.Fatalf("cancel file=%s takes=%v released=%v", a.file, f.seen, f.released)
	}
}

func TestAResumeChordKeepsItsChoiceBehindAnAutomaticMove(t *testing.T) {
	a, f, rows := intentRig(t)
	cmd := beginAutomatic(t, a)
	a.leftOff.row = &rows[1]
	drive(t, a, key(resumeKey))
	if a.home.msg != "continuing chosen chat next; automatic chat is still on its way here" {
		t.Fatal(a.home.msg)
	}
	close(f.proceed)
	drain(t, a, cmd)
	if a.file != f.files["y"] || len(f.seen) != 2 || f.seen[1].Fence != 9 {
		t.Fatalf("chord choice lost: file=%s consent=%+v", a.file, f.seen)
	}
}

func TestALateAutomaticDoorDoesNotReplaceAPick(t *testing.T) {
	for _, choice := range []string{"local", "remote", "new"} {
		t.Run(choice, func(t *testing.T) {
			a, f, rows := intentRig(t)
			opened, proceed := make(chan struct{}), make(chan struct{})
			original := a.open
			a.open = func(where, file string) (Conversation, error) {
				if file == f.files["x"] {
					close(opened)
					<-proceed
				}
				return original(where, file)
			}
			close(f.proceed)
			answers := collectBatchMsgs(beginAutomatic(t, a))
			var door tea.Cmd
			for _, msg := range answers {
				if got, ok := msg.(homeTakenMsg); ok {
					door = a.tookTakeover(got)
				}
			}
			<-opened
			var newDoor tea.Cmd
			switch choice {
			case "local":
				drain(t, a, a.homeOpenLine(homeLine{kind: homeSession, row: session.SessionRow{ID: "local", Transcript: a.file}}))
			case "remote":
				standOn(t, a, rows[1].Cell)
				confirm(t, a)
			case "new":
				newDoor = a.homeStart("")
				drive(t, a, key("z"))
			}
			chosen := a.file
			close(proceed)
			drain(t, a, door)
			if newDoor != nil {
				drain(t, a, newDoor)
				if a.file == chosen {
					t.Fatal("new chat was not opened")
				}
				chosen = a.file
				if a.input.String() != "z" {
					t.Fatal("new chat follow-up was lost behind the automatic arrival")
				}
			}
			if choice == "remote" {
				chosen = f.files["y"]
			}
			if a.file != chosen || !reflect.DeepEqual(f.released, []string{"x"}) {
				t.Fatalf("late door: choice=%s file=%s releases=%v", choice, a.file, f.released)
			}
		})
	}
}

func TestMovingTheCursorDoesNotWithdrawAnAutomaticArrival(t *testing.T) {
	a, f, _ := intentRig(t)
	cmd := beginAutomatic(t, a)
	drive(t, a, key("down"))
	drain(t, a, a.openHome())
	close(f.proceed)
	drain(t, a, cmd)
	if a.file != f.files["x"] || len(f.released) != 0 {
		t.Fatalf("navigation withdrew arrival: %s %v", a.file, f.released)
	}
}

func TestALaterLocalChoiceDropsAQueuedContinue(t *testing.T) {
	a, f, rows := intentRig(t)
	chosen := a.file
	cmd := beginAutomatic(t, a)
	result := make(chan []tea.Msg, 1)
	go func() { result <- collectBatchMsgs(cmd) }()
	<-f.started
	standOn(t, a, rows[1].Cell)
	confirm(t, a)
	drain(t, a, a.homeOpenLine(homeLine{kind: homeSession, row: session.SessionRow{ID: "local", Transcript: chosen}}))
	close(f.proceed)
	for _, msg := range <-result {
		drive(t, a, msg)
	}
	if a.file != chosen || len(f.seen) != 1 || !reflect.DeepEqual(f.released, []string{"x"}) {
		t.Fatalf("latest choice lost: %s %+v %v", a.file, f.seen, f.released)
	}
}

func TestInputBeforeTheStartupSafetyAnswerCancelsTheMove(t *testing.T) {
	a, f, _ := intentRig(t)
	file := a.file
	a.autoContinue = true
	check := a.continueAfterQuit(nil)
	drive(t, a, key("down"))
	if cmd := a.continuationReady(check().(continuationChecked)); cmd != nil {
		t.Fatal("input did not cancel the safety answer")
	}
	if len(f.seen) != 0 || a.file != file {
		t.Fatal("cancelled startup moved a chat")
	}
}

func TestCancellingAJoinedAutomaticTakeStillLetsItGo(t *testing.T) {
	a, f, rows := intentRig(t)
	file := a.file
	cmd := beginAutomatic(t, a)
	a.offerContinue(rows[0], chatlist.OfferFor(rows[0]))
	confirm(t, a)
	drive(t, a, key("esc"))
	close(f.proceed)
	drain(t, a, cmd)
	if a.file != file || len(f.seen) != 1 || !reflect.DeepEqual(f.released, []string{"x"}) {
		t.Fatalf("cancelled join stayed held: %s %+v %v", a.file, f.seen, f.released)
	}
}

func TestPickingTheArrivingChatAfterCancellingAWaitingPickJoins(t *testing.T) {
	a, f, rows := intentRig(t)
	cmd := beginAutomatic(t, a)
	standOn(t, a, rows[1].Cell)
	confirm(t, a)
	drive(t, a, key("esc"))
	a.offerContinue(rows[0], chatlist.OfferFor(rows[0]))
	confirm(t, a)
	close(f.proceed)
	drain(t, a, cmd)
	if a.file != f.files["x"] || len(f.seen) != 1 || len(f.released) != 0 {
		t.Fatalf("join did not adopt its arrival: %s %+v %v", a.file, f.seen, f.released)
	}
}

type checkedIntentTaker struct {
	*intentTaker
	checking chan struct{}
	checked  chan struct{}
	checkErr error
}

func (f *checkedIntentTaker) SafeToContinue(_ context.Context, id, _ string, _ bool) (bool, error) {
	if id == "y" {
		close(f.checking)
		<-f.checked
		return true, f.checkErr
	}
	return true, nil
}

func TestALateSafetyAnswerCannotUndoANewerPick(t *testing.T) {
	for _, action := range []string{"local", "escape"} {
		t.Run(action, func(t *testing.T) {
			a, f, rows := intentRig(t)
			file := a.file
			checked := &checkedIntentTaker{intentTaker: f, checking: make(chan struct{}), checked: make(chan struct{})}
			a.taker = checked
			arriving := make(chan []tea.Msg, 1)
			cmd := beginAutomatic(t, a)
			go func() { arriving <- collectBatchMsgs(cmd) }()
			<-f.started
			a.leftOff.row = &rows[1]
			check := stepKeyNoDrain(t, a, resumeKey)
			answer := make(chan []tea.Msg, 1)
			go func() { answer <- collectBatchMsgs(check) }()
			<-checked.checking
			close(f.proceed)
			for _, msg := range <-arriving {
				drive(t, a, msg)
			}
			if a.file != file || !reflect.DeepEqual(f.released, []string{"x"}) {
				t.Fatal("automatic arrival opened over a checking choice")
			}
			if action == "local" {
				drain(t, a, a.homeOpenLine(homeLine{kind: homeSession, row: session.SessionRow{ID: "local", Transcript: file}}))
			} else {
				drive(t, a, key("esc"))
			}
			close(checked.checked)
			for _, msg := range <-answer {
				drive(t, a, msg)
			}
			if a.file != file || len(f.seen) != 1 {
				t.Fatal("late safety answer opened an abandoned choice")
			}
		})
	}
}

func TestAFailedSafetyCheckDoesNotCancelAnAutomaticAcquisition(t *testing.T) {
	a, f, rows := intentRig(t)
	file := a.file
	checked := &checkedIntentTaker{intentTaker: f, checking: make(chan struct{}), checked: make(chan struct{}), checkErr: errors.New("safety read failed")}
	a.taker = checked
	arriving := make(chan []tea.Msg, 1)
	cmd := beginAutomatic(t, a)
	go func() { arriving <- collectBatchMsgs(cmd) }()
	<-f.started
	a.leftOff.row = &rows[1]
	check := stepKeyNoDrain(t, a, resumeKey)
	answer := make(chan []tea.Msg, 1)
	go func() { answer <- collectBatchMsgs(check) }()
	<-checked.checking
	close(checked.checked)
	for _, msg := range <-answer {
		drive(t, a, msg)
	}
	if a.homeTaking != "x" {
		t.Fatal("a safety error discarded the acquisition still running")
	}
	if _, ok := a.homeAsking(); !ok {
		t.Fatal("failed safety check has no retry card")
	}
	close(f.proceed)
	for _, msg := range <-arriving {
		drive(t, a, msg)
	}
	if a.file != file || !reflect.DeepEqual(f.released, []string{"x"}) || len(f.seen) != 1 {
		t.Fatal("failed safety check let the automatic arrival open")
	}
}
