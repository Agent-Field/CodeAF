package tui3

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/dirwatch"
)

// The approve screen and the device list (approve.go, devices.go). The door is
// a fake that records what it was asked, so each test reads the screen and then
// the door.

type fakeApprovals struct {
	mu      sync.Mutex
	pending PendingDevice
	err     error
	devices []DeviceRow
	calls   []string
}

func (f *fakeApprovals) log(s string) { f.mu.Lock(); f.calls = append(f.calls, s); f.mu.Unlock() }
func (f *fakeApprovals) did() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.calls, ",")
}

func (f *fakeApprovals) Pending(_ context.Context, typed string) (PendingDevice, error) {
	f.log("pending:" + typed)
	return f.pending, f.err
}
func (f *fakeApprovals) Approve(_ context.Context, p PendingDevice) error {
	f.log("approve:" + p.Code)
	return f.err
}
func (f *fakeApprovals) Deny(_ context.Context, p PendingDevice) error {
	f.log("deny:" + p.Code)
	return f.err
}
func (f *fakeApprovals) Devices(context.Context) ([]DeviceRow, error) {
	return append([]DeviceRow(nil), f.devices...), f.err
}
func (f *fakeApprovals) Revoke(_ context.Context, id string) error {
	f.log("revoke:" + id)
	return f.err
}
func (f *fakeApprovals) DeviceName(sealed string) string { return "n:" + sealed }

func spark(now time.Time) PendingDevice {
	return PendingDevice{Code: "k7m2q9xd", Device: "dev_B", Name: "spark", Platform: "linux", Check: "4821",
		RequestedAt: now.Add(-2 * time.Minute), ExpiresAt: now.Add(8 * time.Minute)}
}

const testLink = "https://codeaf.link/p/k7m2q9xd#Qm9v"

func approveApp(t *testing.T, door Approvals) (*app, *pairRig) {
	a := newTestApp(&fakeAgent{model: "test/model"})
	a.approvals = door
	a.width, a.height = 80, 30
	t.Cleanup(a.pair.close)
	return a, newPairRig(t, a)
}

func TestLinkShapeRoutesAndSixDigitsDoNot(t *testing.T) {
	for typed, want := range map[string]bool{
		testLink: true, "k7m2q9xd": true, "k7m2q9xd.Qm9v": true, "codeaf.link/p/k7m2q9xd#Qm9v": true,
		"42-715-302": false, "715 302": false, "": false, "hello": false,
	} {
		if got := isLinkShape(typed); got != want {
			t.Errorf("isLinkShape(%q) = %t, want %t", typed, got, want)
		}
	}
}

func TestApproveCardShowsWhoIsAsking(t *testing.T) {
	door := &fakeApprovals{pending: spark(time.Now())}
	a, r := approveApp(t, door)
	r.slash("/pair " + testLink)
	r.until("the card", func() bool { c, ok := a.pair.card.(*approveCard); return ok && c.req != nil })
	got := plain(frame(a))
	for _, want := range []string{"spark", "Linux", "$", "Check number 4821", "asked 2m ago", "min left"} {
		if !strings.Contains(got, want) {
			t.Fatalf("card lacks %q:\n%s", want, got)
		}
	}
	if hint := a.hintWord(); hint != approveKeys {
		t.Fatalf("hint = %q", hint)
	}
	for _, banned := range []string{"node", "relay", "lease", "manifest"} {
		if strings.Contains(strings.ToLower(got), banned) {
			t.Fatalf("vocabulary law: %q on the card:\n%s", banned, got)
		}
	}
}

func TestApproveNeedsAKeyEnterAndStrayKeysDoNothing(t *testing.T) {
	door := &fakeApprovals{pending: spark(time.Now())}
	a, r := approveApp(t, door)
	r.slash("/pair " + testLink)
	r.until("the card", func() bool { c, ok := a.pair.card.(*approveCard); return ok && c.req != nil })
	r.press("enter", "y", "x", "tab")
	if strings.Contains(door.did(), "approve") || strings.Contains(door.did(), "deny") {
		t.Fatalf("a stray key decided: %s", door.did())
	}
}

func TestApproveToastsAndClosesAndTheJoinedEventIsNotToldTwice(t *testing.T) {
	door := &fakeApprovals{pending: spark(time.Now())}
	a, r := approveApp(t, door)
	r.slash("/pair " + testLink)
	r.until("the card", func() bool { c, ok := a.pair.card.(*approveCard); return ok && c.req != nil })
	r.press("a")
	r.until("the card closed", func() bool { return !a.pair.open })
	if !strings.Contains(door.did(), "approve:k7m2q9xd") {
		t.Fatalf("door: %s", door.did())
	}
	if notes := updateNotes(a); !strings.Contains(notes, "spark joined your fleet - your chats are now everywhere.") {
		t.Fatalf("no toast:\n%s", notes)
	}
	if !a.approve.told("dev_B") {
		t.Fatal("the approved device was not remembered, so its joined event would say it again")
	}
}

func TestDenyTurnsAwayAndEscLeavesItUndecided(t *testing.T) {
	door := &fakeApprovals{pending: spark(time.Now())}
	a, r := approveApp(t, door)
	r.slash("/pair " + testLink)
	r.until("the card", func() bool { c, ok := a.pair.card.(*approveCard); return ok && c.req != nil })
	r.press("esc")
	if a.pair.open || strings.Contains(door.did(), "deny") {
		t.Fatalf("esc must close without deciding: open=%t door=%s", a.pair.open, door.did())
	}
	r.slash("/pair " + testLink)
	r.until("the card again", func() bool { c, ok := a.pair.card.(*approveCard); return ok && c.req != nil })
	r.press("d")
	r.until("closed", func() bool { return !a.pair.open })
	if !strings.Contains(door.did(), "deny:k7m2q9xd") || strings.Contains(updateNotes(a), "joined your fleet") {
		t.Fatalf("door %s, notes %s", door.did(), updateNotes(a))
	}
}

func TestAGoneRequestSaysSoAndKeepsNoKeys(t *testing.T) {
	door := &fakeApprovals{err: errors.New("gone")}
	a, r := approveApp(t, door)
	r.slash("/pair " + testLink)
	r.until("the sentence", func() bool { return strings.Contains(plain(frame(a)), approveGone) })
	r.press("a")
	if strings.Contains(door.did(), "approve:") {
		t.Fatal("a card with no request approved")
	}
}

func TestNoDoorSaysOneSentence(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "test/model"})
	for _, line := range []string{"/pair " + testLink, "/devices"} {
		if cmd := a.slash(line); cmd != nil || a.pair.open {
			t.Fatalf("%s opened something with no door", line)
		}
	}
}

func devicesFake() *fakeApprovals {
	return &fakeApprovals{devices: []DeviceRow{
		{ID: "dev_A", Name: "this mac", Platform: "darwin", Self: true},
		{ID: "dev_B", Name: "spark", Platform: "linux"},
		{ID: "dev_C", Name: "dumb", Platform: "darwin", LastSeen: time.Now().Add(-3 * time.Hour)},
	}}
}

func TestDeviceListShowsPresenceAndRevokesWithOneKey(t *testing.T) {
	door := devicesFake()
	a, r := approveApp(t, door)
	r.slash("/devices")
	r.until("the list", func() bool { c, ok := a.pair.card.(*deviceCard); return ok && c.loaded })
	got := plain(frame(a))
	for _, want := range []string{"● this mac", "this device", "○ spark", "○ dumb", "seen 3h ago"} {
		if !strings.Contains(got, want) {
			t.Fatalf("list lacks %q:\n%s", want, got)
		}
	}
	r.press("r")
	if door.did() != "" {
		t.Fatalf("r on this device revoked it: %s", door.did())
	}
	r.press("down", "r")
	r.until("revoked", func() bool { return strings.Contains(plain(frame(a)), "spark was revoked") })
	if door.did() != "revoke:dev_B" {
		t.Fatalf("door: %s", door.did())
	}
	r.press("r")
	if door.did() != "revoke:dev_B" {
		t.Fatalf("a revoked row was revoked again: %s", door.did())
	}
}

func TestOnlineDotsComeFromTheFeed(t *testing.T) {
	a, _ := approveApp(t, devicesFake())
	a.dirFeed = stillFeed{online: []string{"dev_B"}}
	if got := a.onlineNow(); !got["dev_B"] || got["dev_C"] {
		t.Fatalf("online = %v", got)
	}
}

func TestJoinedEventFromAnotherDeviceIsToldOnce(t *testing.T) {
	a, _ := approveApp(t, &fakeApprovals{})
	a.dirFeed = stillFeed{joined: []dirwatch.Joined{{Seq: 1, Device: "dev_D", Name: "sealed"}}}
	a.announceJoined()
	a.announceJoined()
	if n := strings.Count(updateNotes(a), "n:sealed joined your fleet - your chats are now everywhere."); n != 1 {
		t.Fatalf("told %d times:\n%s", n, updateNotes(a))
	}
}

// stillFeed is a followed feed with fixed state.
type stillFeed struct {
	online []string
	joined []dirwatch.Joined
}

func (f stillFeed) State() dirwatch.State {
	return dirwatch.State{Up: true, Online: f.online, Joined: f.joined}
}
func (stillFeed) Changes() <-chan struct{} { return nil }
func (stillFeed) Probe()                   {}
func (stillFeed) Close()                   {}

func TestRequestAgeNeverSaysNowAgo(t *testing.T) {
	now := time.Now()
	for ago, want := range map[time.Duration]string{0: "asked just now · 10 min left", 5 * time.Minute: "asked 5m ago · 5 min left"} {
		r := &PendingDevice{RequestedAt: now.Add(-ago), ExpiresAt: now.Add(10*time.Minute - ago)}
		if got := requestAge(r, now); got != want {
			t.Fatalf("got %q want %q", got, want)
		}
	}
}
