package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The room seam's acceptance tests: the line over a task's box is the box
// seam with the node's facts on it, and the node's gate is a reading
// (roomseam.go).

// roomEffortFake is a [roomFake] with the task's rung doors, so the seam has a
// rung to draw and `alt+e` has one to move.
type roomEffortFake struct {
	*roomFake
	rungs map[uint64]string
}

func (f *roomEffortFake) TaskEffort(id uint64) string { return f.rungs[id] }

func (f *roomEffortFake) SetTaskEffort(id uint64, rung string) error {
	f.rungs[id] = rung
	return nil
}

// THE ROOM'S SEAM CARRIES THE NODE'S MODEL, RUNG AND GATE after the way out,
// in the conversation's own spelling, and the status row names the task alone.
func TestTheRoomsSeamCarriesTheNodesModelRungAndGate(t *testing.T) {
	a, fake := roomModelApp(t, "z-ai/glm-5.2")
	a.agent = &roomEffortFake{roomFake: fake, rungs: map[uint64]string{9: effort.High.String()}}
	a.width = 160
	a.touch()
	seam := plain(a.legend(a.width))
	for _, want := range []string{roomLegendWord, roomModelLead + "glm-5.2", a.effortChip(effort.High.String()), glyphPermTool + " " + approvalTaskWord} {
		if !strings.Contains(seam, want) {
			t.Fatalf("the room's seam is missing %q:\n%q", want, seam)
		}
	}
	// AND THE CELLS ARE THE NODE'S DOORS, EXCEPT THE GATE. The frame records
	// the spans.
	_ = frame(a)
	if !a.seamModelSpan.pressable() || !a.seamEffortSpan.pressable() {
		t.Fatalf("a running node's seam recorded no doors: model %+v, rung %+v", a.seamModelSpan, a.seamEffortSpan)
	}
	if a.seamApprovalSpan.pressable() {
		t.Fatalf("the node's gate is a reading and was recorded as a door: %+v", a.seamApprovalSpan)
	}
	if line := plain(strings.Join(a.statusRow(a.width), "\n")); strings.Contains(line, "glm-5.2") || strings.Contains(line, approvalYoloWord) {
		t.Fatalf("the keys row repeats what the seam says:\n%q", line)
	}
}

// `alt+e` INSIDE A ROOM WALKS THE NODE'S RUNG, and the cell says the new
// word on the same keystroke; the conversation's own rung is untouched.
func TestAltEInARoomWalksTheNodesRungAndTheSeamSaysSo(t *testing.T) {
	a, fake := roomModelApp(t, "z-ai/glm-5.2")
	doors := &roomEffortFake{roomFake: fake, rungs: map[uint64]string{9: effort.High.String()}}
	a.agent = doors
	a.width = 160
	drive(t, a, key("alt+e"))
	next := effortNextClearing(effort.High).String()
	if got := doors.rungs[9]; got != next {
		t.Fatalf("alt+e in the room set the node's rung to %q, want %s", got, next)
	}
	if seam := plain(a.legend(a.width)); !strings.Contains(seam, a.effortChip(next)) {
		t.Fatalf("the seam does not say the node's new rung:\n%q", seam)
	}
	if !a.roomEffortFlashing() {
		t.Fatal("the cell on the room's seam is not wearing its change")
	}
}

// `alt+a` INSIDE A ROOM MOVES NOTHING AND SAYS WHY: a node runs on its own,
// and the conversation's gate is not on this page.
func TestAltYInARoomSaysATaskRunsOnItsOwn(t *testing.T) {
	a, _ := roomModelApp(t, "z-ai/glm-5.2")
	a.width = 160
	before := a.approval
	drive(t, a, key("alt+a"))
	if a.approval != before {
		t.Fatalf("alt+a in a room moved the conversation's gate to %q", a.approval)
	}
	found := false
	for _, e := range a.entries {
		if e.kind == entryNote && strings.Contains(e.text, approvalTaskWord) {
			found = true
		}
	}
	if !found {
		t.Fatal("alt+a in a room did not say that a task runs on its own")
	}
}

// THE BADGE IS OFF THE STATUS ROW INSIDE A ROOM, whatever the conversation's
// gate is doing: the seam carries the node's cell, and the conversation's
// gate is one esc away on its own seam.
func TestTheStatusRowsBadgeIsOffInsideARoom(t *testing.T) {
	a, _ := roomModelApp(t, "z-ai/glm-5.2")
	a.width = 160
	a.approval = "allow"
	if !a.seamCarriesChip() {
		t.Fatal("the room's seam does not count as carrying a gate cell")
	}
	if got := a.approvalSegment(); got != "" {
		t.Fatalf("the status row says %q inside a room", got)
	}
	// AND OUT OF THE ROOM THE OLD RULE HOLDS: this session has no dial, so its
	// seam has no cell and the open gate is back on the row as the badge.
	a.closeRoom()
	if a.seamCarriesChip() {
		t.Fatal("a session with no dial claims a cell on its seam")
	}
	if got := a.approvalSegment(); got != approvalYoloWord {
		t.Fatalf("out of the room the open gate reads %q on the row, want the badge", got)
	}
}

// A NODE THAT CAN NO LONGER BE MOVED KEEPS ITS CELLS AS FACTS: the rung is
// drawn and is not a door, and the gate still reads `on its own`.
func TestASettledNodesRungIsAFactOnTheSeam(t *testing.T) {
	a, fake := roomModelApp(t, "z-ai/glm-5.2")
	a.agent = &roomEffortFake{roomFake: fake, rungs: map[uint64]string{9: effort.Max.String()}}
	a.width = 160
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(9, "Ship the parser fix",
		session.TaskDone, session.TaskNotice{Model: "z-ai/glm-5.2"})})
	a.tasks[9].run = "adaptive"
	seam := plain(a.legend(a.width))
	if !strings.Contains(seam, a.effortChip(effort.Max.String())) || !strings.Contains(seam, approvalTaskWord) {
		t.Fatalf("a settled node's seam dropped a fact:\n%q", seam)
	}
	_ = frame(a)
	if a.seamEffortSpan.pressable() || a.seamModelSpan.pressable() {
		t.Fatalf("a node past being moved still has doors: model %+v, rung %+v", a.seamModelSpan, a.seamEffortSpan)
	}
}
