package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	machine "github.com/Agent-Field/codeaf/internal/preflight"
)

// Contract G2.1 describes a stale self-hold without naming this computer.
func TestContinueCardDescribesTheChatLastHeldHere(t *testing.T) {
	for _, mine := range []bool{false, true} {
		row := leftOffRow(heldCell, "this computer's name", chatlist.Off, 2*time.Hour)
		row.Mine, row.DeviceID, row.Pending = mine, "self", 1
		a, _ := heldLab(t, &continuationListing{row: row}, &fakeTaker{})
		a.deviceID = "self"
		a.closeHome()
		typeText(t, a, "continue my chat")
		drive(t, a, key("enter"))
		ask, ok := a.homeAsking()
		if !ok {
			t.Fatal("stale self-hold did not offer continuation")
		}
		reason := ask.question.Reason
		if strings.Contains(reason, row.Device) || !strings.Contains(reason, "last held here") || !strings.Contains(reason, "last saved turn 2h ago") {
			t.Errorf("self-hold card: %q", reason)
		}
	}
}

// Contract G2.2 never describes a local continuation as a move from this computer.
func TestSelfHoldLandingDoesNotSayMovedFromThisComputer(t *testing.T) {
	row := leftOffRow(heldCell, "this computer's name", chatlist.Off, time.Hour)
	row.Mine, row.DeviceID = true, "self"
	src := &continuationListing{row: row}
	taker := &fakeTaker{}
	a, _ := heldLab(t, src, taker)
	a.deviceID = "self"
	taker.taken = Taken{Transcript: a.file, Elapsed: time.Second, Resume: machine.Resume{From: row.Device}}
	taker.on = func(string) { src.change(func(r *chatlist.Row) { r.Status = chatlist.Here }) }
	a.closeHome()
	typeText(t, a, "continue my chat")
	drive(t, a, key("enter"))
	confirm(t, a)
	for _, note := range noteTexts(a) {
		if strings.Contains(note, "Moved from "+row.Device) {
			t.Errorf("self-hold landing: %q", note)
		}
	}
}
