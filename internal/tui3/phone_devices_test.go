package tui3

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/pair"
)

type fakePhoneApprovals struct {
	*fakePermitApprovals
	syncAddr string
	unpaired []string
}

func (f *fakePhoneApprovals) SyncService() string { return f.syncAddr }

func (f *fakePhoneApprovals) Unpair(_ context.Context, id string) error {
	f.mu.Lock()
	f.unpaired = append(f.unpaired, id)
	f.mu.Unlock()
	f.log("unpair:" + id)
	return nil
}

// U1 neighbour (design 9.2): client-class approve card, 10-character link shape,
// and phone completion toast.
func TestU1ApproveCardClientClassAndTenCharLinks(t *testing.T) {
	const tenCharCode = "k7m2q9xdr5"
	const tenCharToken = "k7m2q9xdr5.AAAAAAAAAAAAAAAAAAAAAA"
	for typed, want := range map[string]bool{
		tenCharCode:  true,
		tenCharToken: true,
		"k7m2q9xd":   true,
		"42-715-302": false,
	} {
		if got := isLinkShape(typed); got != want {
			t.Fatalf("isLinkShape(%q) = %t, want %t", typed, got, want)
		}
	}
	if !isPairLink(tenCharToken) {
		t.Fatalf("isPairLink(%q) = false, want true", tenCharToken)
	}
	if isPairLink(tenCharCode) {
		t.Fatalf("isPairLink(%q) = true, want false for keyless 10-char code", tenCharCode)
	}

	now := time.Now()
	req := PendingDevice{
		Code:        tenCharCode,
		Device:      "dev_phone11111111111111111111111111",
		Name:        "iPhone",
		Platform:    "ios",
		Check:       "4821",
		Client:      true,
		RequestedAt: now.Add(-1 * time.Minute),
		ExpiresAt:   now.Add(9 * time.Minute),
	}
	door := &fakeApprovals{pending: req}
	a, r := approveApp(t, door)
	a.width = 100
	a.clock = func() time.Time { return now }

	r.slash("/pair " + tenCharToken)
	r.until("the phone approve card", func() bool {
		c, ok := a.pair.card.(*approveCard)
		return ok && c.req != nil
	})
	shown := plain(frame(a))
	for _, want := range []string{
		"A phone wants to connect.",
		"It can start chats and answer on all your computers. It never gets your keys or your files.",
		"Check number 4821. Approve only if the phone shows the same number.",
		approveKeys,
	} {
		if !strings.Contains(shown, want) {
			t.Fatalf("phone approve card lacked %q:\n%s", want, shown)
		}
	}
	if strings.Contains(shown, pair.PairedControlLine("iPhone")) {
		t.Fatalf("phone approve card unexpectedly drew computer control line:\n%s", shown)
	}
	if hint := a.hintWord(); hint != approveKeys {
		t.Fatalf("hint = %q, want %q", hint, approveKeys)
	}

	r.press("a")
	r.until("phone card closed", func() bool { return !a.pair.open })
	if !strings.Contains(door.did(), "approve:"+tenCharCode) {
		t.Fatalf("door calls = %q, want approve:%s", door.did(), tenCharCode)
	}
	wantComplete := "iPhone is now paired as a phone. It can start chats and answer on every computer you have paired. Remove it in /devices."
	if got := pair.ApprovedPhoneLine("iPhone"); got != wantComplete {
		t.Fatalf("ApprovedPhoneLine = %q, want %q", got, wantComplete)
	}
	if notes := updateNotes(a); !strings.Contains(notes, wantComplete) {
		t.Fatalf("notes lacked %q:\n%s", wantComplete, notes)
	}
}

// U1: /devices lists a phone with its name, "phone" and "starts chats, answers",
// sorts phones after computers, refuses w/t toggles with "this is set on the phone",
// renames this computer on n, and removes the phone on r+y with existing wording
// and Unpair notification.
func TestU1DevicesPhoneRowOrderRemoveRenameAndNoPermitToggles(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	base := &fakeApprovals{
		devices: []DeviceRow{
			{ID: "dev_phone", Name: "alpha-phone", Platform: "ios", Kind: clientproto.ClientLinkClass, Client: true},
			{ID: "dev_away", Name: "zeta-desk", Platform: "linux", LastSeen: now.Add(-2 * time.Hour), Permit: &DevicePermit{Watch: true, Drive: false}},
			{ID: "dev_online", Name: "beta-tower", Platform: "linux", Permit: &DevicePermit{Watch: true, Drive: true}},
			{ID: "dev_self", Name: "this mac", Platform: "darwin", Self: true},
		},
	}
	door := &fakePhoneApprovals{
		fakePermitApprovals: &fakePermitApprovals{fakeApprovals: base},
		syncAddr:            "http://127.0.0.1:8901",
	}
	a, r := approveApp(t, door)
	a.width = 110
	a.clock = func() time.Time { return now }
	a.dirFeed = stillFeed{online: []string{"dev_online"}}

	r.slash("/devices")
	r.until("the device list", func() bool {
		c, ok := a.pair.card.(*deviceCard)
		return ok && c.loaded
	})

	screen := plain(frame(a))
	selfAt := strings.Index(screen, "this mac")
	onlineAt := strings.Index(screen, "beta-tower")
	awayAt := strings.Index(screen, "zeta-desk")
	phoneAt := strings.Index(screen, "alpha-phone")
	addAt := strings.Index(screen, addDeviceWord)
	if selfAt < 0 || onlineAt < 0 || awayAt < 0 || phoneAt < 0 || addAt < 0 {
		t.Fatalf("missing expected rows on screen:\n%s", screen)
	}
	if !(selfAt < onlineAt && onlineAt < awayAt && awayAt < phoneAt && phoneAt < addAt) {
		t.Fatalf("phones did not sort after computers (self=%d online=%d away=%d phone=%d add=%d):\n%s",
			selfAt, onlineAt, awayAt, phoneAt, addAt, screen)
	}
	for _, line := range strings.Split(screen, "\n") {
		if strings.Contains(line, "alpha-phone") {
			if !strings.Contains(line, "phone") || !strings.Contains(line, "starts chats, answers") {
				t.Fatalf("phone row = %q, want 'phone' and 'starts chats, answers'", line)
			}
		}
	}

	// n on row 0 renames this device.
	r.press("n", "ctrl+u")
	r.typeText("studio-mac")
	r.press("enter")
	r.until("renamed", func() bool { return strings.Contains(plain(frame(a)), "● studio-mac") })

	// Walk down to the phone row (index 3: self=0, beta-tower=1, zeta-desk=2, alpha-phone=3).
	r.press("down", "down", "down")
	c := a.pair.card.(*deviceCard)
	picked, ok := c.pick()
	if !ok || picked.ID != "dev_phone" {
		t.Fatalf("cursor on %+v, want dev_phone", picked)
	}
	hint := a.hintWord()
	if strings.Contains(hint, devicesWatchKey) || strings.Contains(hint, devicesTypeKey) {
		t.Fatalf("phone row offered w/t toggles in hint: %q", hint)
	}
	if !strings.Contains(hint, devicesGoneKey) {
		t.Fatalf("phone row hint lacked %q: %q", devicesGoneKey, hint)
	}

	// Pressing w and t on a phone row says "this is set on the phone" and calls no PermitSetter.
	door.calls = nil
	r.press("w")
	if door.did() != "" || !strings.Contains(plain(frame(a)), "this is set on the phone") {
		t.Fatalf("w on phone row: calls=%q screen:\n%s", door.did(), plain(frame(a)))
	}
	c.line = ""
	r.press("t")
	if door.did() != "" || !strings.Contains(plain(frame(a)), "this is set on the phone") {
		t.Fatalf("t on phone row: calls=%q screen:\n%s", door.did(), plain(frame(a)))
	}

	// r asks with existing wording; y revokes at sync service and calls Unpair.
	r.press("r")
	wantAsk := fmt.Sprintf(devicesRemoveAsk, "alpha-phone")
	if !strings.Contains(plain(frame(a)), wantAsk) {
		t.Fatalf("r on phone row did not show %q:\n%s", wantAsk, plain(frame(a)))
	}
	r.press("y")
	wantRevoked := fmt.Sprintf(devicesRevoked, "alpha-phone")
	r.until("phone removed", func() bool { return strings.Contains(plain(frame(a)), wantRevoked) })
	if got := door.did(); got != "revoke:dev_phone,unpair:dev_phone" {
		t.Fatalf("remove calls = %q, want revoke:dev_phone,unpair:dev_phone", got)
	}
}

// U1 (Add a phone): pressing `a` in /devices shows the setup link instructions
// when sync is on, and the sync-off sentence with no link when sync is off.
func TestU1DevicesAddPhoneSetupLinkAndSyncOff(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)

	// 1. Sync on: shows setup link with percent-encoded sync address.
	syncURL := "https://sync.example.com:8443"
	doorOn := &fakePhoneApprovals{
		fakePermitApprovals: &fakePermitApprovals{fakeApprovals: devicesFake()},
		syncAddr:            syncURL,
	}
	a, r := approveApp(t, doorOn)
	a.width = 110
	a.clock = func() time.Time { return now }
	r.slash("/devices")
	r.until("the device list", func() bool {
		c, ok := a.pair.card.(*deviceCard)
		return ok && c.loaded
	})
	r.press("a")
	screen := plain(frame(a))
	wantLink := pair.SetupLink(syncURL)
	for _, want := range []string{
		"add a phone: open this link on your phone, then choose a computer in the app:",
		wantLink,
		"The phone then shows its own link; paste it here with /pair.",
	} {
		if !strings.Contains(screen, want) {
			t.Fatalf("/devices after 'a' lacked %q:\n%s", want, screen)
		}
	}

	// 2. Sync off (empty sync address): shows sync-off sentence and no link.
	doorOff := &fakePhoneApprovals{
		fakePermitApprovals: &fakePermitApprovals{fakeApprovals: devicesFake()},
		syncAddr:            "",
	}
	aOff, rOff := approveApp(t, doorOff)
	aOff.width = 110
	aOff.clock = func() time.Time { return now }
	rOff.slash("/devices")
	rOff.until("the device list", func() bool {
		c, ok := aOff.pair.card.(*deviceCard)
		return ok && c.loaded
	})
	rOff.press("a")
	screenOff := plain(frame(aOff))
	wantOff := "sync is off on this computer, so a phone cannot connect yet."
	if !strings.Contains(screenOff, wantOff) {
		t.Fatalf("/devices with sync off lacked %q:\n%s", wantOff, screenOff)
	}
	if strings.Contains(screenOff, "workaf://setup") {
		t.Fatalf("/devices with sync off still showed a setup link:\n%s", screenOff)
	}
}

func TestU1PhoneStringsContainNoForbiddenWords(t *testing.T) {
	added := []string{
		pair.PhoneWantsToConnectLine,
		pair.PhoneAccessLine,
		pair.PhoneCheckQuestion("4821"),
		pair.ApprovedPhoneLine("iPhone"),
		pair.PhoneWord,
		pair.PhoneAccessWord,
		pair.PhonePermitSetLine,
		pair.PhoneRevokedLine("iPhone"),
		devicesPhoneWord,
		devicesPhoneAccess,
		devicesPhonePermitSet,
		devicesAddPhoneHead,
		devicesAddPhoneTail,
		devicesAddPhoneOff,
	}
	forbidden := []string{"relay", "auditor", "verdict", "verified", "refuted"}
	for _, s := range added {
		lower := strings.ToLower(s)
		for _, bad := range forbidden {
			if strings.Contains(lower, bad) {
				t.Fatalf("person-facing string %q contains forbidden word %q", s, bad)
			}
		}
	}
}
