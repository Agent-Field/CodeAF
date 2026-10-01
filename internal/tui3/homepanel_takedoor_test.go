package tui3

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

const heldCell = "0000000000000001"

// flipping is a Source whose one chat can be moved between machines.
type flipping struct {
	mu     sync.Mutex
	status chatlist.Status
}

func (f *flipping) set(s chatlist.Status) { f.mu.Lock(); f.status = s; f.mu.Unlock() }

func (f *flipping) Rows(context.Context) ([]chatlist.Row, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return []chatlist.Row{{Cell: heldCell, Title: "Mine", Device: "studio", Status: f.status, DurableAgo: time.Minute}}, nil
}

// heldLab opens home with a chat this machine lists and the directory says
// another machine holds.
func heldLab(t *testing.T, src chatlist.Source, taker Taker) (*app, *fakeAgent) {
	t.Helper()
	lab := newHomeLab(t)
	workspace := lab.workspace("project")
	mine := lab.session("project", heldCell, "Mine", workspace, time.Now())
	a := lab.app(mine)
	agent := a.agent.(*fakeAgent)
	a.width, a.height = 200, 70
	drain(t, a, a.openHome())
	a.machines, a.taker = src, taker
	drain(t, a, a.askMachines())
	return a, agent
}

func rowsOf(a *app, cell string) (local, remote int) {
	for _, line := range a.home.lines {
		switch {
		case line.kind == homeMachineRow && line.remote != nil && line.remote.Cell == cell:
			remote++
		case line.kind == homeSession && line.row.ID == cell:
			local++
		}
	}
	return
}

func TestListedHereAndHeldElsewhereDrawsTheDoor(t *testing.T) {
	src := &flipping{status: chatlist.Running}
	a, _ := heldLab(t, src, &fakeTaker{})
	if local, remote := rowsOf(a, heldCell); local != 1 || remote != 1 {
		t.Fatalf("local=%d remote=%d rows, want one of each:\n%s", local, remote, homeText(a))
	}
	if !strings.Contains(homeText(a), "running on studio") {
		t.Fatalf("the held chat does not say where it runs:\n%s", homeText(a))
	}
}

func TestListedHereAndReleasedDrawsNoSecondRow(t *testing.T) {
	for _, s := range []chatlist.Status{chatlist.Here, chatlist.Idle, chatlist.Off} {
		a, _ := heldLab(t, &flipping{status: s}, &fakeTaker{})
		if _, remote := rowsOf(a, heldCell); remote != 0 {
			t.Errorf("status %s: a chat listed here is drawn again as another machine's", s)
		}
	}
}

func TestEnterOnTheDoorTakesTheListedChat(t *testing.T) {
	taker := &fakeTaker{}
	a, _ := heldLab(t, &flipping{status: chatlist.Running}, taker)
	standOn(t, a, heldCell)
	if !strings.Contains(homeText(a), chatlist.OfferContinue) {
		t.Fatalf("the card does not say %q:\n%s", chatlist.OfferContinue, homeText(a))
	}
	confirm(t, a)
	if fmt.Sprint(taker.cells) != "["+heldCell+"]" {
		t.Fatalf("Take was asked for %v", taker.cells)
	}
}

// Warm take, then take back: the door goes away while this machine holds the
// chat and comes back when the other machine takes it again.
func TestWarmTakeAndTakeBack(t *testing.T) {
	src := &flipping{status: chatlist.Running}
	taker := &fakeTaker{}
	a, _ := heldLab(t, src, taker)
	standOn(t, a, heldCell)
	confirm(t, a)
	src.set(chatlist.Here)
	a.machinesAsking = false
	drain(t, a, a.openHome())
	a.machinesAsking = false
	drain(t, a, a.askMachines())
	if _, remote := rowsOf(a, heldCell); remote != 0 {
		t.Fatalf("the door stayed: asking=%v page=%v rows=%v\n%s", a.machinesAsking, a.at(pageHome), a.machineRead.rows, homeText(a))
	}
	src.set(chatlist.Running)
	a.machinesAsking = false
	drain(t, a, a.askMachines())
	standOn(t, a, heldCell)
	confirm(t, a)
	if len(taker.cells) != 2 {
		t.Fatalf("take back did not run the take again: %v", taker.cells)
	}
}

func TestTypingIntoAStaleWindowShowsTheDoorAndSendsNothing(t *testing.T) {
	taker := &fakeTaker{}
	a, agent := heldLab(t, &flipping{status: chatlist.Running}, taker)
	a.closeHome()
	typeText(t, a, "hello")
	drive(t, a, key("enter"))
	if len(agent.sent) != 0 {
		t.Fatalf("a stale window sent %v", agent.sent)
	}
	if !a.at(pageHome) || !strings.Contains(homeText(a), chatlist.OfferContinue) {
		t.Fatalf("no door was shown:\n%s", homeText(a))
	}
	if got := a.input.String(); got != "hello" {
		t.Errorf("the words were lost: %q", got)
	}
	confirm(t, a)
	if fmt.Sprint(taker.cells) != "["+heldCell+"]" {
		t.Fatalf("Take was asked for %v", taker.cells)
	}
}

func TestStaleWindowGuardIsQuietWithoutAReading(t *testing.T) {
	a, agent := heldLab(t, nil, &fakeTaker{})
	a.closeHome()
	typeText(t, a, "hello")
	drive(t, a, key("enter"))
	if len(agent.sent) != 1 {
		t.Fatalf("a window with no reading did not send: %v", agent.sent)
	}
}
