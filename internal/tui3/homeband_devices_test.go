package tui3

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The devices row's tests: the roster, the rows and the feed are set by hand,
// so each test states who is online and reads the row back.

var rosterOf = []chatlist.Device{
	{ID: "dev_me", Name: "This Mac", Self: true},
	{ID: "dev_spark", Name: "spark"},
	{ID: "dev_dumb", Name: "dumb"},
}

func devicesRig(online ...string) (*app, *fakeFeed) {
	a := newTestApp(&fakeAgent{model: "m"})
	feed := &fakeFeed{state: dirwatch.State{Up: true, Online: online}}
	a.dirFeed, a.devRow.devices, a.taker = feed, rosterOf, &fakeTaker{}
	a.machineRead = machineReading{rows: []chatlist.Row{
		{Cell: "c1", Title: "Port", Device: "spark", DeviceID: "dev_spark", Status: chatlist.Running},
		{Cell: "c2", Title: "Docs", Device: "dumb", DeviceID: "dev_dumb", Status: chatlist.Off},
	}}
	return a, feed
}

func devicesFrame(a *app, width int) string {
	ctx := ambientBandContext(a, session.SessionRow{}, a.now(), width)
	return plain(strings.Join(drawDevicesBand(a, ctx), "\n"))
}

func TestDevicesRowDrawsPresenceFromTheFeed(t *testing.T) {
	a, feed := devicesRig("dev_spark")
	got := devicesFrame(a, 80)
	if !strings.Contains(got, "● This Mac  ● spark  ○ dumb (offline)") {
		t.Fatalf("row wrong:\n%s", got)
	}
	feed.state.Online = []string{"dev_dumb", "dev_spark"}
	if got := devicesFrame(a, 80); !strings.Contains(got, "● dumb") || strings.Contains(got, "(offline)") {
		t.Fatalf("presence change not drawn:\n%s", got)
	}
}

func TestDevicesRowIsAbsentWhenItCannotSpeakTruly(t *testing.T) {
	a, feed := devicesRig("dev_spark")
	feed.state.Up = false
	if got := devicesFrame(a, 80); got != "" {
		t.Fatalf("drew with the feed down:\n%s", got)
	}
	a, _ = devicesRig()
	a.devRow.devices = rosterOf[:1]
	if got := devicesFrame(a, 80); got != "" {
		t.Fatalf("drew a row for one device:\n%s", got)
	}
	a.dirFeed = nil
	a.devRow.devices = rosterOf
	if devicesFrame(a, 80) != "" {
		t.Fatal("drew with no feed")
	}
}

func TestDevicesRowObeysWidthAndVocabulary(t *testing.T) {
	a, _ := devicesRig("dev_spark")
	for _, width := range []int{12, 24, 40, 80} {
		for _, line := range strings.Split(devicesFrame(a, width), "\n") {
			if ansi.StringWidth(line) > width {
				t.Errorf("width %d: %q is %d wide", width, line, ansi.StringWidth(line))
			}
		}
	}
	low := strings.ToLower(devicesFrame(a, 120))
	for _, banned := range []string{"node", "relay", "take", "lease", "manifest"} {
		if strings.Contains(low, banned) {
			t.Errorf("row says %q", banned)
		}
	}
}

func TestOnlineDeviceOffersMoveHereForARunningChat(t *testing.T) {
	a, _ := devicesRig("dev_spark")
	if cmd := a.bringWork(); cmd != nil {
		t.Fatal("offering the card started work")
	}
	ask, ok := a.homeAsking()
	if !ok || ask.question.Kind != homeContinueKind || ask.question.Subject.Name != "Port" {
		t.Fatalf("no takeover card for spark's chat: %+v", ask.question)
	}
	if ask.pick != continueYesAt {
		t.Fatalf("Move here left the cursor on %d, not on `continue here`", ask.pick)
	}
	if got := chatlist.VerbFor(a.machineRead.rows[0]); got != "Move here" {
		t.Fatalf("a running chat offers %q", got)
	}
	if got := chatlist.VerbFor(a.machineRead.rows[1]); got != "Continue here" {
		t.Fatalf("a chat that went off offers %q", got)
	}
}

func TestSeveralOnlineDevicesAskWhichAndThenOfferTheCard(t *testing.T) {
	a, _ := devicesRig("dev_spark", "dev_dumb")
	a.bringWork()
	ask, ok := a.homeAsking()
	if !ok {
		t.Fatal("no picker")
	}
	labels := []string{}
	for _, o := range ask.question.Options {
		labels = append(labels, o.Label)
	}
	if want := "Move here from spark|Continue here from dumb|leave it there"; strings.Join(labels, "|") != want {
		t.Fatalf("picker options %q", labels)
	}
	ask.local(session.Answer{Key: "2", Picked: []string{"2"}})
	if ask, ok = a.homeAsking(); !ok || ask.question.Subject.Name != "Docs" {
		t.Fatal("the pick did not raise the takeover card for dumb's chat")
	}
}

func TestOfflineOrEmptyDevicesOfferNothing(t *testing.T) {
	a, _ := devicesRig()
	if a.bringWork() != nil {
		t.Fatal("work started")
	}
	if _, ok := a.homeAsking(); ok {
		t.Fatal("a card was raised for devices that are all offline")
	}
}

func TestRosterAskFilesDevicesAndFailureKeepsThem(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.tookRoster(rosterMsg{devices: rosterOf})
	a.tookRoster(rosterMsg{err: errors.New("down")})
	if len(a.devRow.devices) != 3 {
		t.Fatal("a failed ask dropped the roster")
	}
}
