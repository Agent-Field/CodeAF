package tui3

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
)

type idleOpenTaker struct {
	fakeTaker
	opens int
	seen  *directory.LeaseExpectation
}

func (t *idleOpenTaker) OpenAvailable(_ context.Context, _ string, seen *directory.LeaseExpectation) (Taken, error) {
	t.opens++
	t.seen = seen
	return Taken{}, directory.ErrLeaseHeld
}

func TestIdleOpen_C6_C7_C10_TerminalEnterUsesTheFreeDoorOnly(t *testing.T) {
	for _, automatic := range []bool{true, false} {
		t.Run(map[bool]string{true: "paired free", false: "live or single"}[automatic], func(t *testing.T) {
			taker := &idleOpenTaker{}
			a, _ := heldLab(t, &flipping{status: chatlist.Running}, taker)
			row := chatlist.Row{Cell: heldCell, Title: "saved chat", Device: "tower", DeviceID: "A", Fence: 7, Head: "saved", Status: chatlist.Running, AutoOpen: automatic}
			if automatic {
				row.Status = chatlist.Idle
			}
			command := a.homeMachineEnter(homeLine{remote: &row})
			if automatic {
				if strings.Contains(homeText(a), continueAsk) {
					t.Fatal("C6 free open raised Continue here")
				}
				if command == nil {
					t.Fatal("C6 free open did not start")
				}
				// Observe the open request before handling its raced-holder
				// response, which may legitimately offer an explicit continuation.
				_ = command()
				if taker.opens != 1 || len(taker.cells) != 0 || taker.seen == nil || taker.seen.Fence != 7 || taker.seen.Device != "A" {
					t.Fatalf("C6 wrong open door: %+v", taker)
				}
			} else if taker.opens != 0 || len(taker.cells) != 0 || !strings.Contains(homeText(a), continueAsk) {
				t.Fatal("C7/C10 held chat no longer waits for the person's choice")
			}
		})
	}
}
