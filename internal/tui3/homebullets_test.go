package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

func TestEscapeFooterNamesHomeAndMain(t *testing.T) {
	a, _ := homeTabsFixture(t)
	a.closeHome()
	if got := a.idleHint(); !strings.Contains(got, "esc home") {
		t.Fatalf("conversation footer: %q", got)
	}
	b := crumbApp(t)
	if got := b.idleHint(); !strings.Contains(got, "esc main") || strings.Contains(got, "esc home") {
		t.Fatalf("task footer: %q", got)
	}
	b.hintRow(b.width)
	if b.homeDoor.to <= b.homeDoor.from {
		t.Fatal("task Escape label has no mouse target")
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
