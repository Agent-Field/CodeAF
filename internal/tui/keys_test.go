package tui

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

func inspectedWorkerModel(t *testing.T) (*Model, *fakeCommander) {
	t.Helper()
	backend := &fakeBackend{snapshot: store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID},
		{ID: "worker", Parent: store.RootID, Brief: "Inspect this worker", Status: store.Running},
	}}}
	commander := newFakeCommander()
	model := NewWithCommander(backend, "test-session", commander)
	model.snapshot = backend.snapshot
	model.refreshGraph()
	model.toggleGraph()
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.nodeViewID != "worker" || !model.inputFocused {
		t.Fatalf("node view did not open focused: id=%q focused=%v", model.nodeViewID, model.inputFocused)
	}
	return model, commander
}

// The first character of "check the tests" used to cancel the job the steer
// line was written for. The whole sentence must survive, and nothing may be
// requested of the commander while it is being written.
func TestSteeringSentenceNeverCancelsTheWorkerItSteers(t *testing.T) {
	model, commander := inspectedWorkerModel(t)

	typeIntoModel(model, "check the tests, verify j/k, [see] v")
	if got := model.input.Value(); got != "check the tests, verify j/k, [see] v" {
		t.Fatalf("steer draft lost characters to the action keys: %q", got)
	}
	if len(commander.cancelled) != 0 {
		t.Fatalf("typing into the steer line cancelled %v", commander.cancelled)
	}
}

// The structural rule, over every bare key that operates the surface: while a
// text field has focus each one is a character and none of them acts.
func TestActionKeysNeverFireWhileAFieldHasFocus(t *testing.T) {
	for key := range actionRunes {
		t.Run("node view "+key, func(t *testing.T) {
			model, commander := inspectedWorkerModel(t)
			before := model.receiptsExpanded
			_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
			if got := model.input.Value(); got != key {
				t.Fatalf("%q did not reach the steer line: draft %q", key, got)
			}
			if len(commander.cancelled) != 0 {
				t.Fatalf("%q cancelled %v from inside the steer line", key, commander.cancelled)
			}
			if model.receiptsExpanded != before {
				t.Fatalf("%q toggled receipts from inside the steer line", key)
			}
			if model.palette != paletteNone {
				t.Fatalf("%q opened palette %v from inside the steer line", key, model.palette)
			}
		})
		t.Run("composer "+key, func(t *testing.T) {
			model := NewWithCommander(&fakeBackend{}, "test-session", newFakeCommander())
			before := model.receiptsExpanded
			_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
			if got := model.input.Value(); got != key {
				t.Fatalf("%q did not reach the composer: draft %q", key, got)
			}
			if model.receiptsExpanded != before {
				t.Fatalf("%q toggled receipts while composing", key)
			}
			if model.palette != paletteNone {
				t.Fatalf("%q opened palette %v while composing", key, model.palette)
			}
		})
	}
}

// A drill-in filter is a text field too: its letters belong to the query and
// the same keys act again the moment the filter is not the thing listening.
func TestSelfDrillFilterKeepsTheActionLetters(t *testing.T) {
	model := NewWithCommander(&fakeBackend{}, "test-session", newFakeCommander())
	_ = model.selectPlace(placeSelf)
	model.focus = focusSelf
	model.inputFocused = false
	model.input.Blur()
	model.selfRoute = selfRouteBeliefs
	if !model.textEntryFocused() {
		t.Fatal("an open drill-in filter must count as a focused field")
	}
	before := model.receiptsExpanded
	typeIntoModel(model, "cvjk")
	if model.selfQuery != "cvjk" {
		t.Fatalf("filter query is %q, want cvjk", model.selfQuery)
	}
	if model.receiptsExpanded != before {
		t.Fatal("v toggled receipts from inside a drill-in filter")
	}
}

// Cancel stays reachable without a chord: tab hands the keyboard from the
// steer line to the feed, and the footer says so before c is pressed.
func TestTabLeavesTheSteerLineAndCancelActsThere(t *testing.T) {
	model, commander := inspectedWorkerModel(t)

	if hint := model.contextHelpLine(); !strings.Contains(hint, "type to steer") ||
		strings.Contains(hint, "c stop") {
		t.Fatalf("focused footer advertises a key that cannot act: %q", hint)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	if model.inputFocused {
		t.Fatal("tab did not leave the steer line")
	}
	if hint := model.contextHelpLine(); !strings.Contains(hint, "c stop") {
		t.Fatalf("unfocused footer does not advertise cancel: %q", hint)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if len(commander.cancelled) != 1 || commander.cancelled[0] != "worker" {
		t.Fatalf("cancel outside the steer line requested %v", commander.cancelled)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	if !model.inputFocused {
		t.Fatal("tab did not return the keyboard to the steer line")
	}
}

// end is the caret's while there is text for it to travel through, and the
// surface's when there is not.
func TestEndBelongsToTheCaretOnlyWhileADraftExists(t *testing.T) {
	model, _ := inspectedWorkerModel(t)
	model.nodeTraceText = strings.Repeat("line\n", 200)
	model.refreshNodeView(true)
	model.nodeTrace.SetYOffset(0)

	typeIntoModel(model, "hold on")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if model.nodeTrace.AtBottom() {
		t.Fatal("end jumped the feed while a steer draft was being written")
	}
	model.input.SetValue("")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if !model.nodeTrace.AtBottom() {
		t.Fatal("end did not jump the feed with an empty steer line")
	}
}

// Help says esc discards voice. It was false in the one surface where a
// dictated line is most likely — the node view closed instead.
func TestEscapeDiscardsVoiceBeforeClosingTheNodeView(t *testing.T) {
	model, _ := inspectedWorkerModel(t)
	model.voiceState = voiceRecording

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.voiceState != voiceIdle {
		t.Fatalf("esc did not discard voice: state=%v", model.voiceState)
	}
	if model.nodeViewID != "worker" {
		t.Fatal("esc closed the node view while voice was recording")
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.nodeViewID != "" {
		t.Fatal("a second esc did not close the node view")
	}
}

// The header draws the three places as clickable targets and the keyboard
// could not reach any of them; the option chords that can are the ones a
// default terminal swallows.
func TestHeaderFocusReachesThePlacesItDraws(t *testing.T) {
	model := NewWithCommander(&fakeBackend{}, "places", newFakeCommander())
	model.setSize(120, 30)
	_ = model.View()
	if !model.headerPlacesShown {
		t.Fatal("a 120-column header did not draw the places")
	}
	model.focus = focusHeader
	model.inputFocused = false
	model.input.Blur()
	model.headerFocusIndex = 0
	if got := model.headerDoorCount(); got != headerDoors+3 {
		t.Fatalf("header doors = %d, want the four actions plus three places", got)
	}
	for step := 0; step < headerDoors+2; step++ {
		_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	}
	if model.headerFocusIndex != headerDoors+2 {
		t.Fatalf("right walked to index %d", model.headerFocusIndex)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.activePlace() != placeSelf {
		t.Fatalf("enter on the self label opened %v", model.activePlace())
	}
	// A narrow frame folds the places away, and the cycle folds with them.
	narrow := NewWithCommander(&fakeBackend{}, "places", newFakeCommander())
	narrow.setSize(46, 24)
	_ = narrow.View()
	if narrow.headerPlacesShown != (narrow.headerDoorCount() > headerDoors) {
		t.Fatal("the header cycle disagrees with what the header drew")
	}
}
