package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
)

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
