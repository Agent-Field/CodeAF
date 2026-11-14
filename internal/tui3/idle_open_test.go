package tui3

import (
	"context"
	"strings"
	"testing"
	"time"

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
	t.Setenv("CODEAF_IDLE_HANDOFF", "on")
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

func TestIdleOpen_DefaultOff_ReleasedRowRequiresContinueHereAndLocalRowOpensLocally(t *testing.T) {
	taker := &idleOpenTaker{}
	a, _ := heldLab(t, &flipping{status: chatlist.Idle}, taker)
	row := chatlist.Row{Cell: heldCell, Title: "saved chat", Device: "tower", DeviceID: "A", Fence: 7, Head: "saved", Status: chatlist.Idle, Released: true, AutoOpen: true}
	command := a.homeMachineEnter(homeLine{remote: &row})
	if command != nil || taker.opens != 0 || len(taker.cells) != 0 || !strings.Contains(homeText(a), continueAsk) {
		t.Fatalf("default-off released remote row must require Continue here: opens=%d cells=%v home=%s", taker.opens, taker.cells, homeText(a))
	}

	source := &r5bHomeSource{read: func(context.Context) ([]chatlist.Row, uint64, error) {
		t.Fatal("default-off local open must not probe the relay")
		return nil, 0, nil
	}}
	lab := newHomeLab(t)
	file := lab.session("project", heldCell, "Mine", lab.workspace("project"), time.Now())
	localApp := lab.app("")
	keepSurfaceWorkersInTest(t, localApp)
	localApp.width, localApp.height = 200, 70
	localApp.teamsPaired = func() bool { return true }
	localApp.machines, localApp.taker = source, taker
	drain(t, localApp, localApp.openHome())
	r5bSelectChat(t, localApp, file)
	drive(t, localApp, key("enter"))
	if source.reads != 0 || taker.opens != 0 || localApp.at(pageHome) || localApp.file != file {
		t.Fatalf("default-off local open changed dev behaviour: reads=%d opens=%d home=%v file=%q", source.reads, taker.opens, localApp.at(pageHome), localApp.file)
	}
}
