package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// taskPageModel is one worker's page open on a log long enough to scroll.
// Every thought is numbered so each block has an identity of its own, which is
// what the reader's place is restored against.
func taskPageModel(t *testing.T, thoughts int) (*Model, *fakeCommander) {
	t.Helper()
	model, commander := inspectedWorkerModel(t)
	var trace strings.Builder
	for index := range thoughts {
		fmt.Fprintf(&trace, "text: thought number %d\n", index)
	}
	model.nodeTraceText = trace.String()
	model.refreshNodeView(true)
	model.nodeTrace.GotoTop()
	return model, commander
}

// A steer draft that wraps to a second row makes the frame one line taller
// than the terminal. Without a relayout behind it the last row lands past the
// alt-screen bottom, the screen scrolls, and the header the clock re-stamps
// every tick is left standing above its own ghost.
func TestAWrappingSteerDraftRelaysOutTheFrame(t *testing.T) {
	model, _ := taskPageModel(t, 40)
	model.setSize(60, 24)
	if got := len(strings.Split(model.View(), "\n")); got != model.height {
		t.Fatalf("an empty steer line drew %d rows into a %d-row terminal", got, model.height)
	}
	typeIntoModel(model, strings.Repeat("steer this worker ", 8))
	if got := len(strings.Split(model.View(), "\n")); got != model.height {
		t.Fatalf("a wrapped steer draft drew %d rows into a %d-row terminal", got, model.height)
	}
}

// The same guard suppressed the completion palette, so a slash typed into the
// steer line offered nothing while the identical draft in the thread did.
func TestASlashSteerOffersTheSameCompletionsTheThreadDoes(t *testing.T) {
	model, _ := taskPageModel(t, 4)
	typeIntoModel(model, "/can")
	if model.palette != paletteCommands {
		t.Fatalf("a slash steer opened palette %v", model.palette)
	}
}

// The worker's log is a copy of somebody else's terminal session. None of its
// control sequences may reach a frame row: an escape run that survives makes
// the row measure one width and print another, which is the soft wrap the
// ghost header is made of.
func TestWorkerControlBytesNeverReachTheFrame(t *testing.T) {
	model, _ := taskPageModel(t, 0)
	model.nodeTraceText = "call sh {\"cmd\":\"make\"}\n" +
		"  → 40B: \x1b[31mcoloured\x1b[0m\x1b[2Jerased\rreturned\x1b]0;retitled\x07 and\ttabbed\n"
	model.refreshNodeView(true)

	feed := model.renderActivityFeed(88)
	for _, forbidden := range []string{"\x1b[31m", "\x1b[2J", "\x1b]0;", "\x07", "\r", "\t"} {
		if strings.Contains(feed, forbidden) {
			t.Fatalf("the worker's %q reached the feed:\n%q", forbidden, feed)
		}
	}
	stripped := ansi.Strip(feed)
	for _, word := range []string{"erased", "returned", "tabbed"} {
		if !strings.Contains(stripped, word) {
			t.Fatalf("sanitising took the worker's own words with it: %q", stripped)
		}
	}
	if strings.Contains(stripped, "retitled") {
		t.Fatalf("an OSC payload was printed as text: %q", stripped)
	}
}

// The chrome of this page is drawn in glyphs a terminal may print two cells
// wide while lipgloss counts one. A row padded to exactly the width then wraps;
// rows that can be miscounted keep one column of slack.
func TestTheTaskFrameKeepsAColumnOfSlackForAmbiguousGlyphs(t *testing.T) {
	model, _ := taskPageModel(t, 30)
	model.inspectedNode = store.Node{
		ID:     "worker",
		Brief:  "réndre · ‹ambiguous› — ✳ ⌕ ▤ ⋯ ─",
		Status: store.Running,
		Provenance: store.Provenance{
			WorkModel: "vendor/model-one", PlanModel: "vendor/model-two",
		},
	}
	for _, width := range dockWidths {
		model.setSize(width, 30)
		frame := model.renderNodePane()
		assertFitsWidth(t, frame, width, "the task page")
		for index, line := range strings.Split(frame, "\n") {
			if !hasAmbiguousWidth(line) {
				continue
			}
			if got := lipgloss.Width(line); got > width-1 {
				t.Fatalf("row %d mixes ambiguous glyphs with fill to %d of %d cells: %q",
					index, got, width, ansi.Strip(line))
			}
		}
	}
}

// The trace is a window on the last sixty-four kilobytes of a file the worker
// keeps appending to. When its head falls off, every line number in the
// document changes — and a reader restored to a line number is a reader thrown
// somewhere else on every poll. They are put back on the block they were
// reading, with whatever they had opened still open.
func TestASlidingTraceWindowKeepsTheReaderAndTheirExpansions(t *testing.T) {
	model, _ := taskPageModel(t, 0)
	lines := make([]string, 0, 41)
	for index := range 40 {
		if index == 20 {
			lines = append(lines, "text: "+strings.Repeat("a long deliberate thought⏎", 9))
			continue
		}
		lines = append(lines, fmt.Sprintf("text: thought number %d", index))
	}
	model.nodeTraceText = strings.Join(lines, "\n") + "\n"
	model.refreshNodeView(true)

	// Open the long thought and park the reader on its first line.
	block := -1
	for index := range model.feedBlocks {
		if model.feedBlocks[index].expandable() {
			block = index
			break
		}
	}
	if block < 0 {
		t.Fatal("no expandable block in the feed")
	}
	key := model.feedKeys[block]
	model.feedExpanded[key] = true
	for _, row := range model.feedRows {
		if row.block == block {
			model.nodeTrace.SetYOffset(row.line)
			break
		}
	}
	model.refreshNodeView(false)
	anchor := model.feedAnchorAt()
	if anchor.key != key {
		t.Fatalf("the reader did not land on the opened block: %q", anchor.key)
	}

	// The window slides: the first ten lines fall off its head.
	model.nodeTraceText = strings.Join(lines[10:], "\n") + "\n"
	model.refreshNodeView(false)
	if got := model.feedAnchorAt(); got.key != key || got.within != anchor.within {
		t.Fatalf("a slid window moved the reader from %q+%d to %q+%d",
			key, anchor.within, got.key, got.within)
	}
	if !model.feedExpanded[key] {
		t.Fatal("the slid window closed a block the reader had opened")
	}
	if !strings.Contains(model.renderActivityFeed(model.nodeTrace.Width), "▾") {
		t.Fatal("the opened block re-collapsed when the window slid")
	}
}

// The steer the person just sent comes back from the journal as an echo. That
// is the machine catching up, not an act of theirs — it may not teleport a
// reader who has scrolled up to read what the worker did.
func TestALandingSteerDoesNotYankAScrolledReader(t *testing.T) {
	model, _ := taskPageModel(t, 60)
	model.nodeTrace.SetYOffset(6)
	offset := model.nodeTrace.YOffset

	model.landOptimisticNodeMessage("worker", store.Message{
		Seq: 7, Role: store.RoleUser, Body: "try the other branch",
	})
	if model.nodeTrace.YOffset != offset {
		t.Fatalf("a landing steer moved the reader from %d to %d", offset, model.nodeTrace.YOffset)
	}

	model.nodeTrace.GotoBottom()
	model.landOptimisticNodeMessage("worker", store.Message{
		Seq: 8, Role: store.RoleUser, Body: "and check the tests",
	})
	if !model.nodeTrace.AtBottom() {
		t.Fatal("a reader at the live end stopped following it")
	}
}

// A document is laid out for the width it was rendered at. Resizing only the
// viewport left a settled worker wrapped at the old width for as long as it
// stayed open.
func TestResizingTheWindowRerendersTheDocument(t *testing.T) {
	model, _ := taskPageModel(t, 0)
	var trace strings.Builder
	for index := range 10 {
		fmt.Fprintf(&trace, "text: thought %d, long enough that a narrower pane has to wrap it\n", index)
	}
	model.nodeTraceText = trace.String()
	model.setSize(120, 30)
	model.refreshNodeView(true)
	wide := model.nodeTrace.TotalLineCount()

	model.setSize(56, 30)
	narrow := model.nodeTrace.TotalLineCount()
	if narrow <= wide {
		t.Fatalf("the document kept its old wrap: %d lines at 120, %d at 56", wide, narrow)
	}
	if model.nodeTrace.YOffset > max(0, narrow-model.nodeTrace.Height) {
		t.Fatalf("offset %d points past a %d-line document", model.nodeTrace.YOffset, narrow)
	}
}

// With cell motion reporting on, the terminal names every cell the pointer
// crosses. A hand that brushed the trackpad has decided nothing; only a wheel
// or a click is a decision the reading pin yields to.
func TestPointerMotionLeavesTheReadingPinAlone(t *testing.T) {
	model, _ := taskPageModel(t, 60)
	model.inputFocused = false
	model.input.Blur()
	model.nodePinTop = true

	_, _ = model.Update(tea.MouseMsg{
		X: 10, Y: 6, Button: tea.MouseButtonNone, Action: tea.MouseActionMotion,
	})
	if !model.nodePinTop {
		t.Fatal("pointer motion released the reading pin")
	}
	_, _ = model.Update(tea.MouseMsg{
		X: 10, Y: 6, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress,
	})
	if model.nodePinTop {
		t.Fatal("the wheel did not release the reading pin")
	}
}

// One page, one scroll: the wheel means the same thing over the steer frame and
// the footer as it does over the feed.
func TestTheWheelScrollsTheDocumentFromAnywhereOnThePage(t *testing.T) {
	model, _ := taskPageModel(t, 80)
	_ = model.View()
	for _, y := range []int{model.nodeBounds.y + 1, model.inputBounds.y, model.height - 1} {
		model.nodeTrace.GotoTop()
		_, _ = model.Update(tea.MouseMsg{
			X: 3, Y: y, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress,
		})
		if model.nodeTrace.YOffset != 3 {
			t.Fatalf("the wheel at row %d scrolled the document to %d", y, model.nodeTrace.YOffset)
		}
	}
}

// The arrows used to move the document three lines, one line, or none at all
// depending on which half held the keyboard and whether a draft was being
// written. A single-line steer field has no caret for them to move.
func TestTheArrowsScrollTheDocumentByThreeWhereverTheKeyboardIs(t *testing.T) {
	model, _ := taskPageModel(t, 80)
	for _, draft := range []string{"", "a steer being written"} {
		for _, focused := range []bool{true, false} {
			model.input.SetValue(draft)
			model.inputFocused = focused
			if focused {
				_ = model.input.Focus()
			} else {
				model.input.Blur()
			}
			model.nodeTrace.GotoTop()
			_, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
			if model.nodeTrace.YOffset != 3 {
				t.Fatalf("down with draft=%q focused=%v scrolled to %d",
					draft, focused, model.nodeTrace.YOffset)
			}
			_, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
			if model.nodeTrace.YOffset != 0 {
				t.Fatalf("up with draft=%q focused=%v scrolled to %d",
					draft, focused, model.nodeTrace.YOffset)
			}
		}
	}

	// home and end are the ends of the same walk, and both belong to the caret
	// while there is a draft for it to travel through.
	model.input.SetValue("")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if !model.nodeTrace.AtBottom() {
		t.Fatal("end did not reach the live end of the document")
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyHome})
	if !model.nodeTrace.AtTop() {
		t.Fatal("home did not reach the first line of the document")
	}
	model.inputFocused = true
	_ = model.input.Focus()
	model.input.SetValue("hold on")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if model.nodeTrace.AtBottom() {
		t.Fatal("end jumped the document while a steer draft was being written")
	}
}

// A recall walk belongs to the composer that started it. Left standing, it
// captured the thread's arrows for the rest of the session after a visit to a
// task, because the steer line never types the walk to an end.
func TestVisitingATaskEndsTheComposersRecallWalk(t *testing.T) {
	model, _ := inspectedWorkerModel(t)
	model.closeNodeView()
	model.inputHistory = []string{"first thing", "second thing"}
	if !model.recallInput(true) || model.inputRecall == 0 {
		t.Fatal("the recall walk did not start")
	}

	_ = model.openNodeByID("worker")
	if model.inputRecall != 0 {
		t.Fatalf("the recall walk followed the reader into the task page: step %d", model.inputRecall)
	}
	model.inputRecall = 2
	model.closeNodeView()
	if model.inputRecall != 0 {
		t.Fatalf("leaving the task page left a recall walk at step %d", model.inputRecall)
	}
}

// The chips belong to the line they were attached to, and that line stays in
// the thread: they used to linger over the steer field and be dropped on the
// way back out.
func TestAttachmentsWaitInTheThreadWhileATaskIsOpen(t *testing.T) {
	model, _ := inspectedWorkerModel(t)
	model.closeNodeView()
	model.attachments = []string{"/tmp/shot.png"}

	_ = model.openNodeByID("worker")
	if len(model.attachments) != 0 {
		t.Fatalf("the thread's attachments followed the reader in: %v", model.attachments)
	}
	model.attachments = append(model.attachments, "/tmp/steer.png")
	model.closeNodeView()
	if len(model.attachments) != 1 || model.attachments[0] != "/tmp/shot.png" {
		t.Fatalf("the thread's attachments did not come back: %v", model.attachments)
	}
}

// A steer is a sent turn like any other, so the arrows reach it.
func TestASteerJoinsTheComposersRing(t *testing.T) {
	model, _ := inspectedWorkerModel(t)
	model.input.SetValue("try the other branch")
	_ = model.submitSteer()
	if len(model.inputHistory) == 0 ||
		model.inputHistory[len(model.inputHistory)-1] != "try the other branch" {
		t.Fatalf("the steer never reached the ring: %v", model.inputHistory)
	}
}

// A poll that moved the journal somewhere else is not news for this document.
func TestAPollThatChangedNothingLeavesTheDocumentStanding(t *testing.T) {
	model, _ := taskPageModel(t, 40)
	node := model.inspectedNode
	model.nodeTrace.SetContent("sentinel")

	model.applyPoll(pollResultMsg{nodeID: "worker", node: node, nodeFound: true})
	if !strings.Contains(model.nodeTrace.View(), "sentinel") {
		t.Fatal("an unchanged poll rebuilt the document")
	}

	settled := node
	settled.Status, settled.Summary = store.Done, "finished the thing"
	model.applyPoll(pollResultMsg{nodeID: "worker", node: settled, nodeFound: true})
	if strings.Contains(model.nodeTrace.View(), "sentinel") {
		t.Fatal("a poll that settled the worker left the document as it was")
	}
}
