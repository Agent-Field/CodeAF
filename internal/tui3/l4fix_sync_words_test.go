package tui3

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncwords"
	"strings"
	"testing"
	"time"
)

// L4-G1: Chat removal points to the chat's working pairing door; Home uses its account line.
func TestL4FixRemovedWordsByPlace(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.outsideDesk.Say(chatlist.Removed)
	a.saidNotices()
	notes := strings.Join(noteTexts(a), "\n")
	if !strings.Contains(notes, "/pair brings it back") || strings.Contains(notes, "run `codeaf pair`") {
		t.Fatal(notes)
	}
	if a.homeAccountNotice != "removed by another of your devices — pair again to sync" {
		t.Fatal(a.homeAccountNotice)
	}
}

// L4-G2: Home, continuation and timeout identify the service; chat outage producers share one notice.
func TestL4FixSyncOutageWords(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{directory.ErrUnreachable, "the sync service is not answering"},
		{context.DeadlineExceeded, "the sync service did not answer in time"},
	} {
		got := (machineReading{err: tc.err}).unavailableWord()
		if got != tc.want {
			t.Fatalf("got %q want %q", got, tc.want)
		}
	}
	a := newTestApp(&fakeAgent{model: "m"})
	a.syncService = "http://127.0.0.1:8901"
	a.note(chatlist.SyncPaused)
	a.apply(session.Event{Kind: session.EventNotice, Text: syncwords.Outage(a.syncService)})
	notes := strings.Join(noteTexts(a), "\n")
	if strings.Count(notes, "this chat keeps working here") != 1 || strings.Contains(notes, "other machines") {
		t.Fatal(notes)
	}
	if !strings.Contains(continueUnreachable, "because the sync service is not answering; your words are still in the box") {
		t.Fatal(continueUnreachable)
	}
}

// L4-G5: Losing service freshness explains a withdrawn resume card in the same Home band.
func TestL4FixResumeCardExplainsOutage(t *testing.T) {
	a, feed := devicesRig()
	a.syncService = "http://127.0.0.1:8901"
	a.taker = &fakeTaker{}
	a.tookMachines(homeMachinesMsg{rows: []chatlist.Row{leftOffRow("a", "desk-b", chatlist.Running, time.Minute)}})
	if leftOffFrame(a) == "" {
		t.Fatal("no initial card")
	}
	feed.state = dirwatch.State{Up: false}
	a.tookWatch(dirWatchMsg{from: feed})
	got := leftOffFrame(a)
	if !strings.Contains(got, "nothing is answering at http://127.0.0.1:8901") {
		t.Fatalf("card vanished without reason: %q", got)
	}
	// Fresh evidence restores the offer, rather than preserving an obsolete failure.
	feed.state = dirwatch.State{Up: true}
	a.tookWatch(dirWatchMsg{from: feed})
	if got := leftOffFrame(a); !strings.Contains(got, "Continue desk-b's latest chat here?") {
		t.Fatal(got)
	}
}
