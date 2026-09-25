package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE ONE HEAD ────────────────────────────────────────────────────────────
//
// A conversation and every place draw the same four rows at the top of the
// frame — the pulse, the strip or the bar, the rule, a blank (head.go) — so a
// person walking from home into a chat and back sees the head stay still and
// only its middle row change.

// headSizes are the three frames the one head is pinned at: the classic
// terminal, a tall one, and a wide one.
var headSizes = []struct{ w, h int }{{80, 24}, {80, 40}, {120, 45}}

// headLab is a window standing in a conversation on a machine with something
// to say: two things waiting on a person, one moving, fourteen cents spent of
// a twenty-dollar day, on a Thursday morning.
func headLab(t *testing.T) *app {
	t.Helper()
	a := placeApp(t)
	now := time.Date(2026, 9, 10, 9, 49, 0, 0, time.Local)
	a.clock = func() time.Time { return now }
	a.title = "Understanding Hash Tables"
	a.showPage(pageNone)
	seedHeadFacts(a)
	return a
}

// seedHeadFacts puts the lab's facts into the memo the pulse draws. It is
// called after every door, because a door takes the day's money off the real
// ledger ([app.showPage]) and the subject here is the head, not the reading.
func seedHeadFacts(a *app) {
	a.machine = machineFacts{wants: 2, hands: 1, spent: 0.14, ceiling: 20}
}

// headOf is the first [placeHeadRows] rows of the frame, as a reader sees them.
func headOf(t *testing.T, a *app) []string {
	t.Helper()
	rows := strings.Split(frame(a), "\n")
	if len(rows) < placeHeadRows {
		t.Fatalf("a %d-row frame has no room for the head", len(rows))
	}
	head := make([]string, placeHeadRows)
	for i := range head {
		head[i] = strings.TrimRight(plain(rows[i]), " ")
	}
	return head
}

// headPulseWhole is the pulse the lab should draw inside a chat, clause for
// clause: the counts AND the budget.
func headPulseWhole() string {
	return "2 want you · 1 moving · $0.14 / " + railFigure(20) + " · thu 9:49am"
}

// THE CONVERSATION WEARS THE PLACES' HEAD, AT EVERY SIZE THE STRIP IS DRAWN.
// The pulse on row 0, the strip on the bar's row, the rule, a blank — and the
// body starts under exactly those four rows.
func TestTheConversationWearsThePlacesHead(t *testing.T) {
	a := headLab(t)
	for _, size := range headSizes {
		a.width, a.height = size.w, size.h
		a.touch()
		head := headOf(t, a)
		if !strings.HasPrefix(head[0], " "+plain(a.pal.wordmark(a.width))) || !strings.HasSuffix(head[0], headPulseWhole()) {
			t.Fatalf("at %dx%d the chat's first row is not the pulse with its counts and budget:\n%q", size.w, size.h, head[0])
		}
		if !strings.Contains(head[placeTabRow], " home ") || !strings.Contains(head[placeTabRow], a.chatDisplayName()) {
			t.Fatalf("at %dx%d the strip is not under the pulse:\n%q", size.w, size.h, head[placeTabRow])
		}
		if head[2] != strings.Repeat("─", size.w) || head[3] != "" {
			t.Fatalf("at %dx%d the head does not close with a rule and a blank:\n%q\n%q", size.w, size.h, head[2], head[3])
		}
		if a.headHeight() != placeHeadRows || a.bodyTop() != placeHeadRows+a.stripHeight() {
			t.Fatalf("at %dx%d the head draws %d rows and is charged %d, body at %d",
				size.w, size.h, placeHeadRows, a.headHeight(), a.bodyTop())
		}
		if size.w == 80 && size.h == 24 || size.w == 120 {
			t.Logf("the chat head at %dx%d:\n%s", size.w, size.h, strings.Join(head, "\n"))
		}
	}
}

// headFrameSizes are the three frames a person walks between chats, rooms and
// places at: the classic terminal, and a tall one at two widths — the wider two
// being where a task room lays itself out beside its roster
// (roompanel.go's [app.roomOrganized]).
var headFrameSizes = []struct{ w, h int }{{80, 24}, {120, 45}, {180, 45}}

// A TASK ROOM SPENDS THE PLACES' HEAD ABOVE ITS BODY, as the conversation it
// opened from does and as every place does: the pulse, the strip, the rule and
// a blank, and the room's own trail on the first row under them — where a
// place's heading is. It used to lay the trail where the rule stands and its
// facts where the blank does, so walking into a task moved the rule down a row,
// and two in the roomy layout (PLACES-AUDIT.md, lane K).
func TestATaskRoomSpendsThePlacesHeadAboveItsBody(t *testing.T) {
	room, chat := crumbApp(t), headLab(t)
	for _, size := range headFrameSizes {
		for _, f := range []struct {
			where string
			a     *app
			to    page
		}{{"the task room", room, pageNone}, {"the conversation", chat, pageNone}, {"the tasks place", chat, pageTasks}} {
			f.a.width, f.a.height = size.w, size.h
			if f.to != pageNone {
				walkTo(t, f.a, f.to)
			}
			f.a.touch()
			head := headOf(t, f.a)
			if !strings.HasPrefix(head[0], " "+plain(f.a.pal.wordmark(f.a.width))) || head[2] != strings.Repeat("─", size.w) || head[3] != "" {
				t.Fatalf("at %dx%d %s's head is not the pulse, a row, the rule and a blank:\n%s",
					size.w, size.h, f.where, strings.Join(head, "\n"))
			}
			if f.to != pageNone {
				f.a.showPage(pageNone)
			}
		}
		// The room's heading is the first row under the head, and the rows the
		// frame drew there are the rows the geometry charged for.
		rows := strings.Split(plain(frame(room)), "\n")
		if room.roomHeadRow() != placeHeadRows || !strings.Contains(rows[placeHeadRows], "Write the tree") {
			t.Fatalf("at %dx%d the room's trail is on row %d, not under the %d-row head:\n%q",
				size.w, size.h, room.roomHeadRow(), placeHeadRows, rows[placeHeadRows])
		}
		if room.bodyTop() != room.headHeight()+room.stripHeight() || room.headHeight() < placeHeadRows+room.roomHeadCount() {
			t.Fatalf("at %dx%d the room's head is charged %d rows, body at %d",
				size.w, size.h, room.headHeight(), room.bodyTop())
		}
	}
}

// ── THE ONE FOOT ────────────────────────────────────────────────────────────

// footEdges is where a frame's foot stands: the rule, the composer's row, and
// the last row — the status line in a chat, the hint on a place.
type footEdges struct{ rule, box, status int }

// footOf reads those rows off a drawn frame, from the bottom up: the composer
// is the lowest row that opens on the prompt, and the rule is the row over it.
func footOf(rows []string) footEdges {
	got := footEdges{rule: -1, box: -1, status: -1}
	for i := len(rows) - 1; i >= 0 && got.box < 0; i-- {
		if strings.HasPrefix(rows[i]+" ", inputPad+prompt) {
			got.box = i
		}
	}
	if got.box > 0 && strings.HasPrefix(rows[got.box-1], "─") {
		got.rule = got.box - 1
	}
	if last := len(rows) - 1; last >= 0 && strings.TrimSpace(rows[last]) != "" {
		got.status = last
	}
	return got
}

// THE CONVERSATION'S FOOT IS A PLACE'S FOOT: a blank, the rule, the box, and
// the status line where a place draws its hint. The chat used to keep a second
// blank between the box and the status line, so `esc` from home into a chat
// moved the rule and the box up a row and walking back moved them down again
// (PLACES-AUDIT.md, lane K). Walked chat → home → chat at three sizes, and the
// rule, the box and the last row never move. Tasks is no longer on the walk:
// a place that is not home draws no box (pages.go's [place.box]), so its foot
// is its own three rows and [TestEveryPlaceSpendsTheSameHeadAndFoot] holds it
// to those.
func TestWalkingBetweenAChatAndThePlacesMovesNothingAtTheFoot(t *testing.T) {
	a := headLab(t)
	for _, size := range headFrameSizes {
		a.width, a.height = size.w, size.h
		// The box row is the row the PROMPT is on, which is the block's first
		// ([footOf] reads it back that way), so it stands as far above the last
		// row as the floor this height can afford is deep — asked of the frame's
		// own door rather than written out again here.
		floor := boxFloor(size.h)
		want := footEdges{rule: size.h - placeFootRowsAt(size.h) + 1,
			box: size.h - 1 - floor, status: size.h - 1}
		for _, to := range []page{pageNone, pageHome, pageNone} {
			if to == pageNone {
				a.showPage(pageNone)
			} else {
				walkTo(t, a, to)
			}
			a.touch()
			rows := strings.Split(plain(frame(a)), "\n")
			if got := footOf(rows); got != want || strings.TrimSpace(rows[want.rule-1]) != "" {
				t.Fatalf("at %dx%d %s puts its foot at %+v, and every frame puts it at %+v under a blank:\n%s",
					size.w, size.h, pageName(to), got, want, strings.Join(rows[len(rows)-placeFootRowsAt(size.h)-1:], "\n"))
			}
		}
	}
}

// pageName is what a failure calls the frame it was standing on.
func pageName(id page) string {
	if id == pageNone {
		return "the conversation"
	}
	return "the " + id.word() + " place"
}

// AND THE PULSE IS ONE LINE, NOT TWO THAT AGREE. The tasks place and the
// conversation draw it from one function over one memo, so over the same
// machine they draw the same characters.
func TestThePulseOverAChatIsThePulseOverAPlace(t *testing.T) {
	a := headLab(t)
	a.width, a.height = 120, 45
	a.touch()
	chat := headOf(t, a)[0]
	walkTo(t, a, pageTasks)
	seedHeadFacts(a)
	place := headOf(t, a)
	if place[0] != chat {
		t.Fatalf("the pulse over the tasks place is not the pulse over the chat:\n%q\n%q", place[0], chat)
	}
	if !strings.Contains(place[placeTabRow], "home") || !strings.Contains(place[placeTabRow], "sessions") {
		t.Fatalf("the bar is not on the strip's row: %q", place[placeTabRow])
	}
}

// HOME'S PULSE LEAVES ITS COUNTS TO THE PANELS. The budget and the clock stay;
// `2 want you · 1 moving` is what home's own panels are, and above them it
// would be the same news twice (DESIGN.md's law 11).
func TestHomesPulseIsTheBudgetAndTheClock(t *testing.T) {
	a := headLab(t)
	a.width, a.height = 120, 45
	walkTo(t, a, pageHome)
	seedHeadFacts(a)
	pulse := headOf(t, a)[0]
	if !strings.HasSuffix(pulse, "$0.14 / "+railFigure(20)+" · thu 9:49am") || strings.Contains(pulse, pulseWantWord) || strings.Contains(pulse, pulseMovingWord) {
		t.Fatalf("home's pulse should be the budget and the clock alone:\n%q", pulse)
	}
}

// THE STRIP AND THE BAR ANSWER THE POINTER ON THE SAME ROW, and the pulse above
// them answers nothing. A press resolved against the old head — the strip on
// row zero, or on row one only on tall terminals — would open a chat for a
// click on the pulse.
func TestTheStripAndTheBarAnswerOnTheSameRow(t *testing.T) {
	a := headLab(t)
	for _, size := range headSizes {
		a.width, a.height = size.w, size.h
		a.touch()
		frame(a)
		home := headerHomeTarget(t, a)
		if _, ok := a.tabAt(home.span.from, placeTabRow); !ok {
			t.Fatalf("at %dx%d the strip does not answer on row %d", size.w, size.h, placeTabRow)
		}
		if cmd, took := a.tabPress(home.span.from, 0); !took || cmd != nil || a.at(pageHome) {
			t.Fatalf("at %dx%d a press on the pulse leaked into the strip", size.w, size.h)
		}
	}
	walkTo(t, a, pageTasks)
	frame(a)
	if a.tabRow != placeTabRow {
		t.Fatalf("the bar was drawn on row %d, the strip on row %d", a.tabRow, placeTabRow)
	}
	for _, span := range a.tabs {
		if span.id != pageHome {
			continue
		}
		cmd, took := a.placeTabPress(span.from, placeTabRow)
		runCmd(cmd)
		if !took || !a.at(pageHome) {
			t.Fatal("a press on the bar's `home` did not open home")
		}
		return
	}
	t.Fatal("the bar drew no `home`")
}

// INSIDE A CHAT THE COUNTS COME OFF THE PULSE'S OWN BEAT. No home is open to
// read the world, so the beat walks it off the loop and counts what comes back
// (pulsebeat.go) — and while home IS open, a walk that lands late is dropped
// rather than laid over home's fresher reading.
func TestTheChatPulseCountsWhatItsOwnBeatWalked(t *testing.T) {
	a := headLab(t)
	a.machine = machineFacts{}
	world := session.World{Projects: []session.Project{{Sessions: []session.SessionRow{
		{Live: true, Presence: session.SessionPresence{State: session.PresenceWaiting}},
		{Live: true, Presence: session.SessionPresence{State: session.PresenceWorking}},
		{Tasks: session.TaskRollup{Running: 2}},
	}}}}
	if a.pulseBeat() == nil {
		t.Fatal("the beat asked for nothing inside a chat")
	}
	a.pulseCounted(pulseWorldMsg{world: world, known: true})
	if pulse := headOf(t, a)[0]; !strings.Contains(pulse, "1 want you · 3 moving") {
		t.Fatalf("the chat's pulse did not count the walked world:\n%q", pulse)
	}
	walkTo(t, a, pageHome)
	a.machine = machineFacts{}
	a.pulseCounted(pulseWorldMsg{world: world, known: true})
	if a.machine.wants != 0 || a.machine.hands != 0 {
		t.Fatalf("a walk landing on home overwrote home's own reading: %+v", a.machine)
	}
}
