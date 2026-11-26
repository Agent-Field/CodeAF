package tui3

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	machine "github.com/Agent-Field/codeaf/internal/preflight"
)

// V8 (contract 4, contract 5, contract 6): pressing alt+m in a live view opens
// Home and raises the Move card for the live view's row even when a managed
// team is active; answering 1 shows "Moving here…" on Home and on the live
// view's composer-position bar with highest precedence; on completion the live
// agent is closed, the live seam is gone, and the moved chat runs here with the
// Moved from banner.
func TestMoveFromALiveViewShowsMovingHereThenTheTranscriptThenRunsHere(t *testing.T) {
	lab := newHomeLab(t)
	workspace := lab.workspace("project")
	initial := lab.session("project", "0000000000000001", "Mine", workspace, time.Now())
	a := lab.app(initial)
	t.Cleanup(a.doorLine.close)
	a.width, a.height = 200, 70
	a.dirFeed = &fakeFeed{state: dirwatch.State{Up: true, Online: []string{"dev_tower"}}}
	a.devRow.devices = rosterOf

	cellID := "0000000000000042"
	remoteRow := chatlist.Row{
		Cell:     cellID,
		Title:    "Port the parser",
		Device:   "tower",
		DeviceID: "dev_tower",
		Status:   chatlist.Running,
	}
	src := &continuationListing{row: remoteRow}
	a.machines = src

	liveAgent := &fakeAgent{model: "m"}
	hostSessionFile := filepath.Join(t.TempDir(), cellID, "session.jsonl")
	a.takeUp(Conversation{
		Agent:       liveAgent,
		SessionFile: hostSessionFile,
		Live: &LiveSeam{
			Host:   "tower",
			Device: "dev_tower",
			Cell:   cellID,
			Row:    remoteRow,
			// Even if the seam reports another state, Moving here… takes highest precedence.
			Read: func() LiveRead { return LiveRead{Kind: LiveWentOffline} },
		},
		Link: &LinkSeam{},
	}, true)

	// Wire a managed team overlay to prove alt+m in a live view is handled before trafficKeyPress.
	a.wall.loaded = true
	a.wall.activeID = "team-1"
	a.wall.teams = []team{{
		ID:      "team-1",
		Name:    "harbor",
		Manager: "other-key",
		Members: []teamMember{{Key: a.frontTabKey(), Handle: "worker"}},
	}}

	entered, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	var localTranscript string
	a.taker = &fakeTaker{
		taken: Taken{
			Device:  "This Mac",
			Elapsed: 1200 * time.Millisecond,
			Resume:  machine.Resume{From: "tower", Now: workspace},
		},
		on: func(cell string) {
			localTranscript = lab.session("project", cell, "Port the parser", workspace, time.Now())
			src.change(func(row *chatlist.Row) { row.Mine, row.Status = true, chatlist.Here })
			close(entered)
			<-release
		},
	}

	// Pressing alt+m in the live view opens Home and raises Move this chat here? with cursor on move here.
	drive(t, a, key("alt+m"))
	if !a.at(pageHome) {
		t.Fatal("alt+m in a live view did not open Home")
	}
	ask, ok := a.homeAsking()
	if !ok || ask.question.Head != moveAsk || ask.question.Subject.Name != "Port the parser" || ask.pick != continueYesAt {
		t.Fatalf("alt+m did not raise Move card at continueYesAt: ok=%v pick=%d question=%+v", ok, ask.pick, ask.question)
	}
	if a.liveMoveFrom == "" {
		t.Fatal("alt+m did not mark a.liveMoveFrom")
	}

	// Answering 1 starts the Move and shows "Moving here…" on Home and on the live bar.
	_, moveCmd := a.Update(key("enter"))
	if a.home.msg != chatlist.LiveMoving {
		t.Fatalf("Home progress line during live move = %q, want %q", a.home.msg, chatlist.LiveMoving)
	}
	if !strings.Contains(homeText(a), chatlist.LiveMoving) {
		t.Fatalf("Home screen does not show %q:\n%s", chatlist.LiveMoving, homeText(a))
	}
	bar, barOn := a.liveBar()
	if !barOn || bar != chatlist.LiveMoving {
		t.Fatalf("liveBar() during move = (%q, %v), want (%q, true)", bar, barOn, chatlist.LiveMoving)
	}

	// Pressing esc on Home returns to the live chat view where the bar shows "Moving here…".
	_, _ = a.Update(key("esc"))
	if a.at(pageHome) {
		t.Fatal("esc on Home during a live move did not return to the live view")
	}
	if screen := plain(frame(a)); !strings.Contains(screen, chatlist.LiveMoving) {
		t.Fatalf("live chat frame while moving did not draw %q:\n%s", chatlist.LiveMoving, screen)
	}

	// Let the Move finish and drain the completion commands.
	close(release)
	drain(t, a, moveCmd)

	if a.at(pageHome) {
		t.Fatalf("completed move left the person on Home:\n%s", homeText(a))
	}
	if liveAgent.closes != 1 {
		t.Fatalf("retired live agent Close count = %d, want 1", liveAgent.closes)
	}
	if a.liveFront() != nil {
		t.Fatalf("moved chat kept a live seam: %+v", a.liveFront())
	}
	if a.liveMoveFrom != "" {
		t.Fatalf("a.liveMoveFrom was not cleared after move success: %q", a.liveMoveFrom)
	}
	if a.file != localTranscript {
		t.Fatalf("front conversation file = %q, want %q", a.file, localTranscript)
	}
	wantMoved := chatlist.Moved("tower", 1200*time.Millisecond, false)
	if got := plain(lastNote(t, a)); got != wantMoved {
		t.Fatalf("moved banner note = %q, want %q", got, wantMoved)
	}
}

// V9 (contract 7): when a Move started from a live view fails (including a lost
// race), Home closes, the live view is in front again with its agent open, its
// composer words untouched, its bar back to what it was, and the failure
// sentence said once as a note.
func TestAFailedMoveLeavesTheChatThereAndReturnsToTheLiveView(t *testing.T) {
	cases := []struct {
		name     string
		takeErr  error
		wantNote string
	}{
		{
			name:     "holder error",
			takeErr:  errors.New("disk full"),
			wantNote: moveFails + " because the chat could not be put in this folder; your words are still in the box. Try move here again.",
		},
		{
			name:     "lost race",
			takeErr:  fmt.Errorf("take: %w", directory.ErrLeaseHeld),
			wantNote: chatlist.LostRace,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, taker := liveHomeRig(t, "dev_tower")
			a.closeHome()
			taker.err = tc.takeErr

			cellID := "c1"
			remoteRow := a.machineRead.rows[0]
			liveAgent := &fakeAgent{model: "m"}
			hostSessionFile := filepath.Join(t.TempDir(), cellID, "session.jsonl")
			a.takeUp(Conversation{
				Agent:       liveAgent,
				SessionFile: hostSessionFile,
				Live: &LiveSeam{
					Host:   "tower",
					Device: "dev_tower",
					Cell:   cellID,
					Row:    remoteRow,
					Read:   func() LiveRead { return LiveRead{Kind: LiveOn} },
				},
				Link: &LinkSeam{},
			}, true)
			a.input.setText("keep these live words")

			drive(t, a, key("alt+m"))
			if !a.at(pageHome) {
				t.Fatal("alt+m did not open Home")
			}
			confirm(t, a)

			if a.at(pageHome) {
				t.Fatal("failed move from a live view left Home open")
			}
			if _, asking := a.homeAsking(); asking {
				t.Fatal("failed move from a live view left a retry card on Home")
			}
			if a.liveFront() == nil || a.liveFront().Host != "tower" {
				t.Fatalf("live front was lost after failed move: %+v", a.liveFront())
			}
			if liveAgent.closes != 0 {
				t.Fatalf("live agent was closed on a failed move: closes=%d", liveAgent.closes)
			}
			if a.liveMoveFrom != "" {
				t.Fatalf("a.liveMoveFrom = %q, want empty", a.liveMoveFrom)
			}
			if bar, on := a.liveBar(); on || bar != "" {
				t.Fatalf("liveBar() after failed move = (%q, %v), want (\"\", false)", bar, on)
			}
			if got := a.input.String(); got != "keep these live words" {
				t.Fatalf("composer words = %q, want %q", got, "keep these live words")
			}

			notes := 0
			for _, e := range a.entries {
				if e.kind == entryNote && e.text == tc.wantNote {
					notes++
				}
			}
			if notes != 1 {
				t.Fatalf("note %q appeared %d times in entries %+v, want 1", tc.wantNote, notes, a.entries)
			}
		})
	}
}

// V14 (contract 7 and contract 9): a Move not started from a live view still
// stays on Home with the retry card when it fails.
func TestAMoveNotStartedFromALiveViewFailsAsItDidBefore(t *testing.T) {
	taker := &fakeTaker{err: errors.New("boom")}
	a := continueHome(t, taker, continueRows, 200)
	standOn(t, a, "c-off")
	confirm(t, a)

	if !a.at(pageHome) {
		t.Fatal("failed move from Home closed Home")
	}
	ask, ok := a.homeAsking()
	if !ok || ask.question.Options[0].Label != moveRetry || ask.question.Options[1].Label != moveStay {
		t.Fatalf("failed move from Home did not keep the retry card: ok=%v question=%+v", ok, ask.question)
	}
}

// V8 (contract 8): when recordMachines learns that another paired computer took
// this window's open local chat, the window opens a live view of the new holder
// in place of the stale local conversation, closes the old local agent, keeps
// the typed words in the composer box, and prints no Superseded note; when
// OpenLive fails or is nil, the Superseded note is printed as before.
func TestTheOldHoldersWindowBecomesALiveViewOfTheNewHolder(t *testing.T) {
	cellID := "0000000000000042"
	remoteRow := chatlist.Row{
		Cell:     cellID,
		Title:    "Parser work",
		Device:   "tower",
		DeviceID: "dev_tower",
		Status:   chatlist.Running,
	}

	t.Run("follows to a live view of the new holder", func(t *testing.T) {
		oldAgent := &fakeAgent{model: "m"}
		a := newTestApp(oldAgent)
		t.Cleanup(a.doorLine.close)
		a.dismissWelcome()
		a.devRow.devices = rosterOf
		localFile := filepath.Join(t.TempDir(), cellID, "session.jsonl")
		a.takeUp(Conversation{
			Agent:       oldAgent,
			SessionFile: localFile,
		}, true)
		a.input.setText("words already in the box")

		calls, liveAgents := wireFakeOpenLive(t, a, nil)
		drain(t, a, a.recordMachines(homeMachinesMsg{rows: []chatlist.Row{remoteRow}}))

		if len(*calls) != 1 || (*calls)[0].Cell != cellID {
			t.Fatalf("OpenLive calls = %+v, want 1 call for %s", *calls, cellID)
		}
		if oldAgent.closes != 1 {
			t.Fatalf("old local agent closes = %d, want 1", oldAgent.closes)
		}
		if a.liveFront() == nil || a.liveFront().Host != "tower" || a.agent != (*liveAgents)[0] {
			t.Fatalf("front conversation did not become the live view: live=%+v", a.liveFront())
		}
		if got := a.input.String(); got != "words already in the box" {
			t.Fatalf("composer words after following holder = %q, want %q", got, "words already in the box")
		}
		for _, e := range a.entries {
			if e.kind == entryNote && strings.Contains(e.text, chatlist.Superseded("tower")) {
				t.Fatalf("recordMachines printed a Superseded note after opening live: %q", e.text)
			}
		}

		// A second listing for the same holder does not call OpenLive again or loop.
		drain(t, a, a.recordMachines(homeMachinesMsg{rows: []chatlist.Row{remoteRow}}))
		if len(*calls) != 1 {
			t.Fatalf("repeated recordMachines called OpenLive %d times, want 1", len(*calls))
		}
	})

	t.Run("falls back to Superseded when OpenLive fails", func(t *testing.T) {
		oldAgent := &fakeAgent{model: "m"}
		a := newTestApp(oldAgent)
		t.Cleanup(a.doorLine.close)
		a.dismissWelcome()
		a.devRow.devices = rosterOf
		localFile := filepath.Join(t.TempDir(), cellID, "session.jsonl")
		a.takeUp(Conversation{
			Agent:       oldAgent,
			SessionFile: localFile,
		}, true)
		a.input.setText("words already in the box")

		calls, _ := wireFakeOpenLive(t, a, errors.New("live door refused"))
		drain(t, a, a.recordMachines(homeMachinesMsg{rows: []chatlist.Row{remoteRow}}))

		if len(*calls) != 1 {
			t.Fatalf("OpenLive calls = %d, want 1", len(*calls))
		}
		if a.liveFront() != nil {
			t.Fatalf("liveFront() = %+v after failed OpenLive, want nil", a.liveFront())
		}
		if got := a.input.String(); got != "words already in the box" {
			t.Fatalf("composer words = %q, want %q", got, "words already in the box")
		}
		found := false
		for _, e := range a.entries {
			if e.kind == entryNote && strings.Contains(e.text, chatlist.Superseded("tower")) {
				found = true
			}
		}
		if !found {
			t.Fatalf("failed OpenLive did not fall back to the Superseded note: %+v", a.entries)
		}

		// A second listing for the same transition does not retry OpenLive in a loop.
		drain(t, a, a.recordMachines(homeMachinesMsg{rows: []chatlist.Row{remoteRow}}))
		if len(*calls) != 1 {
			t.Fatalf("repeated recordMachines after failure called OpenLive %d times, want 1", len(*calls))
		}
	})

	t.Run("absent path with OpenLive nil prints Superseded as before", func(t *testing.T) {
		oldAgent := &fakeAgent{model: "m"}
		a := newTestApp(oldAgent)
		t.Cleanup(a.doorLine.close)
		a.dismissWelcome()
		a.devRow.devices = rosterOf
		localFile := filepath.Join(t.TempDir(), cellID, "session.jsonl")
		a.takeUp(Conversation{
			Agent:       oldAgent,
			SessionFile: localFile,
		}, true)
		a.input.setText("words already in the box")

		drain(t, a, a.recordMachines(homeMachinesMsg{rows: []chatlist.Row{remoteRow}}))

		if a.liveFront() != nil {
			t.Fatalf("liveFront() = %+v with openLive == nil, want nil", a.liveFront())
		}
		if got := a.input.String(); got != "words already in the box" {
			t.Fatalf("composer words = %q, want %q", got, "words already in the box")
		}
		found := false
		for _, e := range a.entries {
			if e.kind == entryNote && strings.Contains(e.text, chatlist.Superseded("tower")) {
				found = true
			}
		}
		if !found {
			t.Fatalf("openLive == nil did not print the Superseded note: %+v", a.entries)
		}
	})
}
