package tui3

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// A real agentic exchange shape: an inspected queue, narration at that
// phase's endpoint, then cancellation reasoning and a still-running test.
// Turn 1 deliberately collides with the first settled phase's key.
func roomCompactApp(t *testing.T) *app {
	t.Helper()
	a, _, _ := roomApp(t)
	a.width, a.height = 110, 60
	a.workMode = config.WorkFold
	a.openRoom(7, "Fix cancellation of pending requests")
	base := time.Unix(100, 0)
	a.clock = func() time.Time { return base.Add(9 * time.Second) }
	a.room.entries = []entry{
		{kind: entryUser, turn: 1, text: "Fix cancellation without losing pending requests.", settled: true},
		{kind: entryAssistant, turn: 1, text: "Inspecting the queue.", settled: true},
		{kind: entryTool, turn: 1, tool: "read", callID: "inspect", status: toolOK,
			detail: toolDetail{Args: `{"path":"pending.go"}`}, began: base, ended: base.Add(time.Second)},
		{kind: entryAssistant, turn: 1, text: "Repairing cancellation.\nClose pending receivers before joining workers.", settled: true},
		{kind: entryThinking, turn: 1, text: "The shutdown waiter needs the queue lock released first.", settled: true,
			began: base.Add(2 * time.Second), ended: base.Add(3 * time.Second)},
		{kind: entryTool, turn: 1, tool: "bash", callID: "verify", status: toolRunning,
			detail: toolDetail{Args: `{"command":"go test ./internal/queue -run TestCancellation -count=50"}`}, began: base.Add(4 * time.Second)},
	}
	a.room.turn, a.room.live = 1, -1
	a.room.workOpen, a.room.capOpen = map[int]bool{}, map[int]bool{}
	a.room.unfolded = map[int]bool{}
	a.room.readingRestored, a.room.dirty = true, true
	a.room.offset, a.room.stick = 0, false
	a.input.reset()
	a.touch()
	return a
}

// Detailed-tool tests enter the live outline through the same key a reader
// uses. The key assertion protects the settled phase's independent disclosure.
func openRoomCompactWork(t *testing.T, a *app) {
	t.Helper()
	if a.room == nil {
		t.Fatal("no room to expand")
	}
	if key, ok := a.liveWorkOf(a.room.deck()); !ok || key != 0 {
		t.Fatalf("no live room disclosure: key=%d present=%v\n%s", key, ok, roomText(a))
	}
	drive(t, a, key("ctrl+e"))
	if !a.room.workOpen[0] {
		t.Fatal("Ctrl+E did not open the live room outline")
	}
}

func roomCompactClick(t *testing.T, a *app, hit hitKind, key int) {
	t.Helper()
	a.room.offset, a.room.stick = 0, false
	for y := a.bodyTop(); y < a.bodyTop()+a.viewHeight(); y++ {
		r, ok := a.rowAt(y)
		if !ok || r.hit != hit || r.turn != key {
			continue
		}
		drive(t, a, tea.MouseClickMsg{X: 8, Y: y, Button: tea.MouseLeft}, tea.MouseReleaseMsg{X: 8, Y: y, Button: tea.MouseLeft})
		return
	}
	t.Fatalf("no visible hit %v key %d:\n%s", hit, key, roomText(a))
}

func roomCompactWantOnce(t *testing.T, page, words string) {
	t.Helper()
	if n := strings.Count(page, words); n != 1 {
		t.Fatalf("want %q exactly once, got %d:\n%s", words, n, page)
	}
}

func TestRoomCompactLiveKeyDoesNotOpenPhaseWithSameTurn(t *testing.T) {
	a := roomCompactApp(t)
	parentWork := map[int]bool{9: true}
	parentUnfolded := map[int]bool{9: true}
	a.workOpen, a.unfolded = parentWork, parentUnfolded
	page := roomText(a)
	roomCompactWantOnce(t, page, "Repairing cancellation")
	for _, hidden := range []string{"pending.go", "TestCancellation", "shutdown waiter", "Close pending receivers"} {
		if strings.Contains(page, hidden) {
			t.Fatalf("compact room leaked %q:\n%s", hidden, page)
		}
	}
	roomCompactClick(t, a, hitWorkFold, 0)
	if !a.room.workOpen[0] || a.room.workOpen[1] {
		t.Fatalf("live click changed settled phase: %v", a.room.workOpen)
	}
	page = roomText(a)
	roomCompactWantOnce(t, page, "Repairing cancellation")
	roomCompactWantOnce(t, page, "Close pending receivers")
	if !strings.Contains(page, "go test ./internal/queue") || strings.Contains(page, "pending.go") {
		t.Fatalf("live opening lost its call or opened historical call:\n%s", page)
	}
	drive(t, a, key("ctrl+e"))
	if a.room.workOpen[0] || a.room.workOpen[1] {
		t.Fatalf("Ctrl+E did not close only live work: %v", a.room.workOpen)
	}
	roomCompactClick(t, a, hitWorkFold, 1)
	if !a.room.workOpen[1] || a.room.workOpen[0] {
		t.Fatalf("phase click changed live work: %v", a.room.workOpen)
	}
	roomCompactWantOnce(t, roomText(a), "Inspecting the queue")
	drive(t, a, key("ctrl+e"))
	if !a.room.workOpen[0] || !a.room.workOpen[1] {
		t.Fatalf("Ctrl+E did not prefer live frontier: %v", a.room.workOpen)
	}
	if !reflect.DeepEqual(a.workOpen, map[int]bool{9: true}) || !reflect.DeepEqual(a.unfolded, map[int]bool{9: true}) {
		t.Fatal("room disclosure changed the parent conversation")
	}
}

func TestRoomCompactCtrlOUsesRealTurnAndCtrlEResetsOverride(t *testing.T) {
	a := roomCompactApp(t)
	drive(t, a, key("ctrl+o"))
	if !a.room.unfolded[1] || a.room.unfolded[0] {
		t.Fatalf("Ctrl+O used disclosure key instead of turn: %v", a.room.unfolded)
	}
	if page := roomText(a); !strings.Contains(page, "go test ./internal/queue") {
		t.Fatalf("Ctrl+O lost live call:\n%s", page)
	}
	drive(t, a, key("ctrl+e"))
	if a.room.unfolded[1] || a.room.workOpen[0] {
		t.Fatalf("Ctrl+E left raw override active: unfolded=%v open=%v", a.room.unfolded, a.room.workOpen)
	}
	if page := roomText(a); strings.Contains(page, "go test ./internal/queue") {
		t.Fatalf("Ctrl+E did not compact live call:\n%s", page)
	}
}

func TestRoomCompactScrollOpensPastThenLiveWithoutTogglingPast(t *testing.T) {
	a := roomCompactApp(t)
	a.roomScroll(-1)
	if !a.room.workOpen[1] || a.room.workOpen[0] {
		t.Fatalf("first upward scroll missed oldest phase: %v", a.room.workOpen)
	}
	// Once the reader reaches a frontier with no older fold above it, the
	// same actual upward-scroll gesture must open the live disclosure.
	b := roomCompactApp(t)
	b.room.entries = append(b.room.entries[:1:1], b.room.entries[3:]...)
	b.room.dirty = true
	b.roomScroll(-1)
	if !b.room.workOpen[0] || b.room.workOpen[1] {
		t.Fatalf("upward scroll did not open only live work: %v\n%s", b.room.workOpen, roomText(b))
	}
	if !strings.Contains(roomText(b), "go test ./internal/queue") {
		t.Fatal("scroll-open live work lost its call")
	}
	for _, r := range b.roomRows(b.bodyWidth()) {
		if r.hit == hitWorkFold && r.turn == 0 {
			if b.roomFoldDoor(r) != nil {
				t.Fatal("already-open live row still claims a scroll-open door")
			}
			return
		}
	}
	t.Fatal("expanded live work has no disclosure row")
}

func TestRoomCompactCaptionMovesFromFrontierToSettledPhaseOnce(t *testing.T) {
	for _, pastOpen := range []bool{false, true} {
		t.Run(map[bool]string{false: "past closed", true: "past open"}[pastOpen], func(t *testing.T) {
			a := roomCompactApp(t)
			a.room.workOpen[1] = pastOpen
			roomCompactWantOnce(t, roomText(a), "Repairing cancellation")
			drive(t, a, roomEventMsg{gen: a.room.gen, ev: session.Event{Kind: session.EventToolEnd, CallID: "verify", Tool: "bash", Hint: "all cancellation tests passed"}})
			drive(t, a, roomEventMsg{gen: a.room.gen, ev: session.Event{Kind: session.EventTextDelta, Text: "Cancellation now resolves every waiting request."}})
			drive(t, a, roomEventMsg{gen: a.room.gen, ev: session.Event{Kind: session.EventToolBegin, CallID: "race", Tool: "bash", Hint: "bash go test -race", Args: `{"command":"go test -race ./internal/queue"}`}})
			page := roomText(a)
			if strings.Contains(page, "Repairing cancellation") {
				t.Fatalf("settled phase's caption remained in frontier:\n%s", page)
			}
			roomCompactWantOnce(t, page, "Cancellation now resolves every waiting request")
			a.room.workOpen[2] = true
			a.room.dirty = true
			page = roomText(a)
			roomCompactWantOnce(t, page, "Repairing cancellation")
			roomCompactWantOnce(t, page, "Cancellation now resolves every waiting request")
			// The complete multi-line narration is one caption disclosure away.
			for _, c := range deriveCaptions(a.room.entries, a.room.turn) {
				if strings.Contains(c.text, "Repairing cancellation") {
					a.setCapOpen(a.room.deck(), c.start, true)
				}
			}
			roomCompactWantOnce(t, roomText(a), "Close pending receivers")
		})
	}
}

func TestRoomCompactProtectsFailureCorrectionAndFinalAnswer(t *testing.T) {
	a := roomCompactApp(t)
	drive(t, a, roomEventMsg{gen: a.room.gen, ev: session.Event{Kind: session.EventToolFailed, CallID: "verify", Tool: "bash", Hint: "cancellation test failed", Err: errors.New("pending request was stranded")}})
	page := roomText(a)
	if !strings.Contains(page, "cancellation test failed") && !strings.Contains(page, "pending request was stranded") {
		t.Fatalf("compact work hid real tool failure:\n%s", page)
	}
	a.room.entries = append(a.room.entries,
		entry{kind: entryUser, turn: 1, text: "Keep the public API unchanged.", settled: true},
		entry{kind: entryAssistant, turn: 1, text: "Which cancellation guarantee should callers receive?", settled: true},
	)
	a.room.dirty = true
	page = roomText(a)
	roomCompactWantOnce(t, page, "Keep the public API unchanged")
	roomCompactWantOnce(t, page, "Which cancellation guarantee should callers receive?")
	a.room.entries = append(a.room.entries, entry{kind: entryAssistant, turn: 1, text: "Pending calls now terminate reliably; the public API is unchanged.", settled: true})
	a.room.setDone(true)
	page = roomText(a)
	roomCompactWantOnce(t, page, "Pending calls now terminate reliably")
	if a.room.workOpen[0] {
		t.Fatal("completion retained live disclosure state")
	}
}

func TestRoomCompactReasoningBeforeFirstToolHasWorkingDisclosure(t *testing.T) {
	a := roomCompactApp(t)
	a.room.entries = []entry{
		a.room.entries[0],
		{kind: entryThinking, turn: 1, text: "The queue lock must be released before cancellation joins the worker.", began: time.Unix(104, 0)},
	}
	a.room.dirty = true
	page := roomText(a)
	if strings.Contains(page, "queue lock must") {
		t.Fatalf("initial reasoning escaped compact disclosure:\n%s", page)
	}
	if !strings.Contains(page, "ctrl+e") || (!strings.Contains(strings.ToLower(page), "thinking") && !strings.Contains(strings.ToLower(page), "working")) {
		t.Fatalf("reasoning has no honest activity label:\n%s", page)
	}
	roomCompactClick(t, a, hitWorkFold, 0)
	if page := roomText(a); !strings.Contains(page, "queue lock must") {
		t.Fatalf("thinking disclosure lost actual reasoning:\n%s", page)
	}
	drive(t, a, key("ctrl+e"))
	if page := roomText(a); strings.Contains(page, "queue lock must") {
		t.Fatalf("Ctrl+E did not close live reasoning:\n%s", page)
	}
}
