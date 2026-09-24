package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

func TestHomeFooterNamesDoubleSpaceOnlyWhereItWorks(t *testing.T) {
	a, _ := homeTabsFixture(t)
	a.closeHome()
	if got := a.idleHint(); !strings.Contains(got, homeDoorWord) {
		t.Fatalf("conversation footer: %q", got)
	}
	a.input.setText("draft")
	if got := a.idleHint(); strings.Contains(got, homeDoorWord) {
		t.Fatalf("draft advertises the empty-box gesture: %q", got)
	}
	b := crumbApp(t)
	if got := b.idleHint(); strings.Contains(got, homeDoorWord) {
		t.Fatalf("task room advertises a conversation-only gesture: %q", got)
	}
}

func TestAskStartsAndAdvancesItsSpinnerBeforeAnyReply(t *testing.T) {
	lab := newErrandLab(t)
	a := lab.app("")
	a.openHome()
	a.painting = false
	if cmd := a.askHere("explain unicode identifiers"); cmd == nil || !a.painting {
		t.Fatal("ask did not arm the frame clock")
	}
	ex := a.exchanges[0]
	a.linear, a.pal.ascii = false, false
	first := plain(a.exchangeStateLine(ex, 50, a.pal))
	for i := 0; i < spinnerStep; i++ {
		a.paint()
	}
	next := plain(a.exchangeStateLine(ex, 50, a.pal))
	if first == next || !strings.Contains(next, "thinking") {
		t.Fatalf("frozen spinner: %q -> %q", first, next)
	}
	ex.working = false
	a.paint()
	if a.exchangeAnimating() {
		t.Fatal("settled ask still animates")
	}
	ex.box.setText("and the parser?")
	a.painting = false
	a.exchangeEnter(ex)
	if !a.painting || !ex.working {
		t.Fatal("follow-up did not restart the clock")
	}
}

func TestHomeConversationBulletsFollowAnswerAndUnreadState(t *testing.T) {
	a, files := homeTabsFixture(t)
	open, _ := homeConversationLines(a)
	cell := open[0].cell
	idle := plain(a.homeConversationBullet(cell, a.pal))
	if idle != a.pal.glyph(tokens.GProseBullet) {
		t.Fatalf("idle bullet: %q", idle)
	}
	a.state = stateWorking
	a.linear, a.pal.ascii = false, false
	a.home.spin = homeNoLine
	first := plain(a.homeConversationBullet(cell, a.pal))
	a.paints += spinnerStep
	if next := plain(a.homeConversationBullet(cell, a.pal)); next == first || !a.homeAnimating() {
		t.Fatal("answering bullet did not animate")
	}
	a.applyEvent(session.Event{Kind: session.EventTurnDone}, false)
	if got := plain(a.homeConversationBullet(cell, a.pal)); got != a.pal.glyph(tokens.GStepDone) {
		t.Fatalf("unread bullet: %q", got)
	}
	a.closeHome()
	frame(a)
	if a.unreadChats[a.frontTabKey()] {
		t.Fatal("reading the conversation did not clear unread")
	}
	// The keeper's completion counter survives notification delivery until the chat opens.
	key := a.convKey(files[1])
	watch := &behindWatch{}
	watch.finished.Store(1)
	if a.behind == nil {
		a.behind = make(map[string]*kept)
	}
	a.behind[key] = &kept{watch: watch}
	other := &homeCell{chatKey: key}
	watch.turning.Store(true)
	if working, _ := a.homeChatState(other); !working {
		t.Fatal("background answer has no working indicator")
	}
	watch.turning.Store(false)
	if _, unread := a.homeChatState(other); !unread {
		t.Fatal("background reply lost unread state")
	}
	watch.took()
	if _, unread := a.homeChatState(other); !unread {
		t.Fatal("notification consumed unread state")
	}
	other.closed = true
	if got := plain(a.homeConversationBullet(other, a.pal)); got != idle {
		t.Fatal("closed conversation is not dim/idle")
	}
}

func TestHomeConversationKeepsWorkingUntilItsLastTaskSettles(t *testing.T) {
	for _, background := range []bool{false, true} {
		name := "foreground"
		if background {
			name = "background"
		}
		t.Run(name, func(t *testing.T) {
			a, files := homeTabsFixture(t)
			open, _ := homeConversationLines(a)
			cell := open[0].cell
			watch := &behindWatch{}
			if background {
				key := a.convKey(files[1])
				if a.behind == nil {
					a.behind = make(map[string]*kept)
				}
				a.behind[key] = &kept{watch: watch}
				for _, line := range open {
					if line.cell.chatKey == key {
						cell = line.cell
					}
				}
			}
			update := func(id uint64, state session.TaskState) {
				event := signalSettled(id, state)
				if background {
					watch.noteTask(event.Task)
				} else {
					a.taskUpdate(event)
				}
			}
			a.state = stateIdle
			a.linear = true
			update(21, session.TaskRunning)
			update(22, session.TaskRunning)
			if got := plain(a.homeConversationBullet(cell, a.pal)); got != a.pal.glyph(tokens.GWorking) {
				t.Fatalf("Home shows %q while tasks run after the answer ended", got)
			}
			a.linear = false
			a.home.spin = homeNoLine
			first := plain(a.homeConversationBullet(cell, a.pal))
			a.paints += spinnerStep
			if next := plain(a.homeConversationBullet(cell, a.pal)); next == first || !a.homeAnimating() {
				t.Fatal("task-only conversation did not animate on Home")
			}
			a.linear = true
			cell.mark = cellMarkNeeds
			if got := plain(a.homeConversationBullet(cell, a.pal)); got != a.pal.glyph(tokens.GNeedsHuman) {
				t.Fatalf("running tasks hid an unanswered question: %q", got)
			}
			cell.mark = cellMarkNone
			update(21, session.TaskDone)
			if got := plain(a.homeConversationBullet(cell, a.pal)); got != a.pal.glyph(tokens.GWorking) {
				t.Fatalf("one finished task hid the other running task: %q", got)
			}
			update(22, session.TaskDone)
			// A stale disk count must not overrule this window's completed tasks.
			cell.row = &switcherRow{session: session.SessionRow{Tasks: session.TaskRollup{Running: 2}}}
			if got := plain(a.homeConversationBullet(cell, a.pal)); got != a.pal.glyph(tokens.GProseBullet) {
				t.Fatalf("Home still shows work after the last task settled: %q", got)
			}
		})
	}
}

func TestHomeConversationReadsAnotherWindowsRunningTasks(t *testing.T) {
	a, _ := homeTabsFixture(t)
	a.linear = true
	cell := &homeCell{row: &switcherRow{session: session.SessionRow{
		Live: true, Tasks: session.TaskRollup{Running: 1},
	}}}
	if got := plain(a.homeConversationBullet(cell, a.pal)); got != a.pal.glyph(tokens.GWorking) {
		t.Fatalf("another window's running task lost its working mark: %q", got)
	}
	cell.closed = true
	if got := plain(a.homeConversationBullet(cell, a.pal)); got != a.pal.glyph(tokens.GProseBullet) {
		t.Fatalf("a closed conversation kept a working mark: %q", got)
	}
}

// A task's decision is drawn on the task's own row, so the conversation that
// owns it keeps its working mark while other work of its own is still going.
func TestHomeConversationKeepsWorkingWhileOneOfItsTasksWaitsForADecision(t *testing.T) {
	t.Run("background", func(t *testing.T) {
		a, files := homeTabsFixture(t)
		a.linear = true
		key := a.convKey(files[1])
		watch := &behindWatch{}
		if a.behind == nil {
			a.behind = make(map[string]*kept)
		}
		a.behind[key] = &kept{watch: watch}
		cell := &homeCell{chatKey: key}
		watch.noteTask(&session.TaskNotice{ID: 41, State: session.TaskRunning})
		watch.waits.Store(true)
		if got := plain(a.homeConversationBullet(cell, a.pal)); got != a.pal.glyph(tokens.GWorking) {
			t.Fatalf("a decision on one task hid the conversation's running task: %q", got)
		}
		watch.noteTask(&session.TaskNotice{ID: 41, State: session.TaskDone})
		watch.turning.Store(true)
		if got := plain(a.homeConversationBullet(cell, a.pal)); got != a.pal.glyph(tokens.GWorking) {
			t.Fatalf("a decision on one task hid the conversation's answer: %q", got)
		}
		watch.turning.Store(false)
		if got := plain(a.homeConversationBullet(cell, a.pal)); got != a.pal.glyph(tokens.GProseBullet) {
			t.Fatalf("a waiting conversation with no work left still shows work: %q", got)
		}
		cell.mark = cellMarkNeeds
		if got := plain(a.homeConversationBullet(cell, a.pal)); got != a.pal.glyph(tokens.GNeedsHuman) {
			t.Fatalf("the conversation's own question lost its mark: %q", got)
		}
	})
	t.Run("foreground", func(t *testing.T) {
		a, _ := homeTabsFixture(t)
		open, _ := homeConversationLines(a)
		cell := open[0].cell
		a.linear = true
		a.state = stateIdle
		a.taskUpdate(signalSettled(42, session.TaskRunning))
		a.questions = append(a.questions, questionShown{question: session.Question{Kind: session.QuestionConsent}})
		if got := plain(a.homeConversationBullet(cell, a.pal)); got != a.pal.glyph(tokens.GWorking) {
			t.Fatalf("an open question hid the conversation's running task: %q", got)
		}
		cell.mark = cellMarkNeeds
		if got := plain(a.homeConversationBullet(cell, a.pal)); got != a.pal.glyph(tokens.GNeedsHuman) {
			t.Fatalf("the conversation's own question lost its mark: %q", got)
		}
	})
}

// A conversation that has never been named has no tab, so Home draws it from the
// saved rows; a task that is the first thing it does still marks it working.
func TestHomeUnnamedConversationWorksWhileItsFirstTaskRuns(t *testing.T) {
	lab := newHomeLab(t)
	workspace := lab.workspace("project")
	file := lab.session("project", "0000000000000001", "", workspace, time.Now())
	a := lab.app(file)
	a.workspace = workspace
	a.width, a.height = 180, 45
	a.linear = true
	a.state = stateIdle
	if tabs := a.tabList(); len(tabs) != 0 {
		t.Fatalf("an unnamed conversation drew a tab: %+v", tabs)
	}
	a.taskUpdate(signalSettled(61, session.TaskRunning))
	a.openHome()
	var cell *homeCell
	for _, line := range a.home.lines {
		if line.kind == homeSession && line.cell != nil && line.cell.row != nil && line.cell.row.here {
			cell = line.cell
		}
	}
	if cell == nil {
		t.Fatal("Home drew no row for the conversation in front")
	}
	if got := plain(a.homeConversationBullet(cell, a.pal)); got != a.pal.glyph(tokens.GWorking) {
		t.Fatalf("Home shows %q while the unnamed conversation's task runs", got)
	}
	a.taskUpdate(signalSettled(61, session.TaskDone))
	if got := plain(a.homeConversationBullet(cell, a.pal)); got != a.pal.glyph(tokens.GProseBullet) {
		t.Fatalf("Home still shows work after the unnamed conversation's task settled: %q", got)
	}
}

// Another window's conversation keeps its working mark while one of its tasks
// runs, even when a different task's decision is waiting on the task's own row.
func TestHomeAnotherWindowsDecisionDoesNotHideItsRunningTask(t *testing.T) {
	a, _ := homeTabsFixture(t)
	a.linear = true
	cell := &homeCell{row: &switcherRow{session: session.SessionRow{Live: true,
		Tasks:    session.TaskRollup{Running: 1},
		Presence: session.SessionPresence{State: session.PresenceWaiting, Reason: "your call on the parser"}}}}
	if got := plain(a.homeConversationBullet(cell, a.pal)); got != a.pal.glyph(tokens.GWorking) {
		t.Fatalf("a waiting decision hid another window's running task: %q", got)
	}
}

// Home's frame clock runs only for a spinner a person can see: a working row that
// wears a question draws a still `?`, so it never takes the spinner.
func TestHomeSpinnerSkipsARowWearingAQuestion(t *testing.T) {
	a, files := homeTabsFixture(t)
	open, _ := homeConversationLines(a)
	front := open[0].cell
	a.linear, a.pal.ascii = false, false
	a.home.spin = homeNoLine
	a.state = stateIdle
	a.taskUpdate(signalSettled(71, session.TaskRunning))
	front.mark = cellMarkNeeds
	if a.homeAnimating() {
		t.Fatal("Home animates although its only working row draws a still question")
	}
	key := a.convKey(files[1])
	watch := &behindWatch{}
	watch.noteTask(&session.TaskNotice{ID: 72, State: session.TaskRunning})
	if a.behind == nil {
		a.behind = make(map[string]*kept)
	}
	a.behind[key] = &kept{watch: watch}
	var held *homeCell
	for _, line := range open {
		if line.cell.chatKey == key {
			held = line.cell
		}
	}
	if held == nil {
		t.Fatal("the held conversation has no Home row")
	}
	first := plain(a.homeConversationBullet(held, a.pal))
	a.paints += spinnerStep
	if next := plain(a.homeConversationBullet(held, a.pal)); next == first || !a.homeAnimating() {
		t.Fatal("the spinner did not move to the working row a person can see")
	}
	if got := plain(a.homeConversationBullet(front, a.pal)); got != a.pal.glyph(tokens.GNeedsHuman) {
		t.Fatalf("the question lost its mark to the spinner: %q", got)
	}
}
