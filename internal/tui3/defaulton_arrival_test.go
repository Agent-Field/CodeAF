package tui3

import (
	"errors"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"strings"
	"testing"
	"time"
)

// TestD6EarlyBringWaitsForTheCatalog proves D6 when presence arrives before
// the published chat. The chord must retain the request and offer confirmation
// once the listing arrives, without silently taking anything.
func TestD6EarlyBringWaitsForTheCatalog(t *testing.T) {
	a, _ := devicesRig("dev_spark")
	a.machines = chatlist.Static{}
	a.machineRead.rows = nil
	a.bringWork()
	if !a.devRow.bringPending {
		t.Fatal("early chord was lost")
	}
	if _, ok := a.homeAsking(); ok {
		t.Fatal("offered a chat before it arrived")
	}
	a.tookMachines(homeMachinesMsg{rows: []chatlist.Row{{Cell: "d6", Title: "arrived", Device: "spark", DeviceID: "dev_spark", Status: chatlist.Running}}})
	ask, ok := a.homeAsking()
	if !ok || !strings.Contains(ask.question.Head, "Continue this chat here?") {
		t.Fatalf("arrival never offered confirmation: %+v", ask)
	}
	if a.devRow.bringPending {
		t.Fatal("pending chord not consumed")
	}
}

// TestD6PresenceFinishesAnEarlyChord proves D6 when the chat list beats the
// first presence frame. Either ordering must finish the same pending request.
func TestD6PresenceFinishesAnEarlyChord(t *testing.T) {
	a, feed := devicesRig()
	a.machines = chatlist.Static{}
	feed.state.Up = false
	a.bringWork()
	if !a.devRow.bringPending {
		t.Fatal("early request not kept")
	}
	feed.state.Up = true
	feed.state.Online = []string{"dev_spark"}
	a.tookWatch(dirWatchMsg{from: feed})
	if _, ok := a.homeAsking(); !ok {
		t.Fatal("presence arrived but request was lost")
	}
}

// TestD6FailedCatalogReadRetriesWhileWatching proves D6: a healthy change
// socket cannot turn a failed initial listing into a five-minute stale home.
func TestD6FailedCatalogReadRetriesWhileWatching(t *testing.T) {
	a, _ := devicesRig("dev_spark")
	now := time.Unix(100, 0)
	a.clock = func() time.Time { return now }
	a.fileMachines(homeMachinesMsg{err: errors.New("temporary read failure")})
	if a.machinePoll.due.Sub(now) > machinesFast {
		t.Fatalf("failed listing backed off for %s", a.machinePoll.due.Sub(now))
	}
}
