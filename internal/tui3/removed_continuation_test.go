package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
)

// TestRemovedComputerStillSendsOnItsOwnChats proves that removal ends syncing,
// not local work: a chat held here, or never shared, still sends, while a chat
// another computer took keeps the words and says why.
func TestRemovedComputerStillSendsOnItsOwnChats(t *testing.T) {
	for _, tc := range []struct {
		name string
		row  chatlist.Row
		mine bool
		want bool
	}{
		{name: "held here", row: leftOffRow(heldCell, "tower", chatlist.Running, time.Minute), mine: true, want: true},
		{name: "never shared", row: leftOffRow("0000000000000009", "tower", chatlist.Running, time.Minute), want: true},
		{name: "taken elsewhere", row: leftOffRow(heldCell, "tower", chatlist.Running, time.Minute), want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := &continuationListing{row: tc.row}
			if tc.mine {
				src.row.Mine, src.row.Status, src.row.Fence = true, chatlist.Here, 3
			}
			a, agent := heldLab(t, src, &fakeTaker{})
			a.closeHome()
			src.err = directory.ErrRevoked
			typeText(t, a, "keep working here")
			drive(t, a, key("enter"))
			if !a.machineRead.computerRemoved {
				t.Fatal("the fresh reading did not mark this computer removed")
			}
			if tc.want {
				if len(agent.sent) != 1 {
					t.Fatalf("removed computer refused its own chat: sent=%v draft=%q", agent.sent, a.input.String())
				}
				return
			}
			if len(agent.sent) != 0 || a.input.String() != "keep working here" {
				t.Fatalf("removed computer sent on a chat it does not hold: sent=%v draft=%q", agent.sent, a.input.String())
			}
			if notes := strings.Join(noteTexts(a), "\n"); !strings.Contains(notes, "this device was removed") {
				t.Errorf("refusal did not say why: %q", notes)
			}
		})
	}
}

func TestRemovedComputerOffersNoContinuation(t *testing.T) {
	for _, notice := range []bool{false, true} {
		a := continueHome(t, &fakeTaker{}, chatlist.Static{leftOffRow("away", "other", chatlist.Off, time.Minute)}, 200)
		a.considerResume()
		if a.leftOff.row == nil {
			t.Fatal("fixture has no continuation offer")
		}
		if notice {
			a.useNotices(NewNotices())
			a.outsideDesk.SayHomeOnce(chatlist.Removed)
			a.saidNotices()
		} else {
			drain(t, a, a.recordMachines(homeMachinesMsg{err: directory.ErrRevoked}))
		}
		text := homeText(a)
		if strings.Contains(text, "latest chat here?") || strings.Contains(text, "continue here") {
			t.Errorf("removed computer offered continuing: %s", text)
		}
		if !strings.Contains(text, "this device was removed") {
			t.Errorf("removal sentence missing: %s", text)
		}
		if _, claimed := a.resumeKeyHandler(resumeKey); claimed {
			t.Error("removed computer claimed the continue chord")
		}
		standOn(t, a, "away")
		if _, card := a.homeAsking(); card {
			t.Error("removed computer raised a continuation card")
		}
		if taker := a.taker.(*fakeTaker); len(taker.cells) != 0 {
			t.Error("removed computer tried to take a chat")
		}
	}
}
