package tui3

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"errors"
	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"net/url"
	"strings"
	"testing"
	"time"
)

type storageApprovals struct {
	*fakeApprovals
	code, state string
}

func (d *storageApprovals) RelayStorage() (string, string) { return d.code, d.state }

func TestStorageOptInDevicesRequestAndStatus(t *testing.T) {
	code := "id_" + strings.Repeat("a", 32)
	door := &storageApprovals{fakeApprovals: devicesFake(), code: code, state: "off"}
	a, r := openSettingsDevices(t, door, time.Now())
	a.syncService = "http://127.0.0.1:18831"
	found := false
	for at, item := range a.sheet.items {
		if item.dev != nil && item.dev.storage {
			a.sheet.cursor = at
			found = true
			break
		}
	}
	if !found || !strings.Contains(a.sheet.devicesKeysLine(), "enter request access") {
		t.Fatalf("missing storage request row: %s", plain(frame(a)))
	}
	before := processOpener
	var opened string
	processOpener = func(link string) error { opened = link; return errors.New("no browser in test") }
	t.Cleanup(func() { processOpener = before })
	r.press("enter")
	r.until("storage request opener", func() bool { return opened != "" })
	parsed, err := url.Parse(opened)
	if err != nil || !strings.HasPrefix(opened, syncsetup.StorageRequestURL+"?") || parsed.Query().Get("title") != "relay storage access: "+code || parsed.Query().Get("body") != "Please turn on relay storage for my account.\n\nRequest code: "+code {
		t.Fatalf("request link: %s %v", opened, err)
	}
	details := plain(frame(a))
	if !strings.Contains(details, code) || !strings.Contains(details, "Open request on GitHub") || !strings.Contains(frame(a), syncsetup.StorageRequestLink(code)) {
		t.Fatalf("no-browser instructions: %s", details)
	}
	a.syncSpace = blobstore.Counts{Storage: "off"}
	text := a.statusText()
	if !strings.Contains(text, "relay storage") || !strings.Contains(text, "off") || strings.Contains(text, "sync space") {
		t.Fatalf("off status: %s", text)
	}
	for _, fact := range a.statusFacts(text) {
		if fact == "off" {
			t.Fatal("off value is bright")
		}
	}
	door.state = "on"
	a.sheet.storageCode, a.sheet.storageState = a.relayStorageAccount()
	if strings.Contains(plain(frame(a)), "request code") {
		t.Fatal("approved storage still shows the request panel")
	}
	if strings.Contains(a.sheet.devicesKeysLine(), "request access") {
		t.Fatal("enabled storage still asks for access")
	}
	a.syncSpace = blobstore.Counts{Storage: "on"}
	if strings.Contains(a.statusText(), "relay storage") {
		t.Fatal("enabled status has off line")
	}
}

func TestQAStorageRequestDoesNotShowFallbackWhenBrowserOpens(t *testing.T) {
	door := &storageApprovals{fakeApprovals: devicesFake(), code: "id_" + strings.Repeat("c", 32), state: "off"}
	a, _ := openSettingsDevices(t, door, time.Now())
	old := processOpener
	processOpener = func(string) error { return nil }
	t.Cleanup(func() { processOpener = old })
	cmd := a.requestRelayStorage()
	if cmd == nil {
		t.Fatal("no browser request")
	}
	a.Update(cmd())
	if a.sheet.storageRequest || strings.Contains(plain(frame(a)), "request code") {
		t.Fatal("a successful browser open showed the no-browser fallback")
	}
	if !strings.Contains(strings.Join(strings.Fields(plain(frame(a))), " "), "GitHub may ask you to sign in") {
		t.Fatal("the browser receipt did not explain GitHub sign-in")
	}
}

func (d *storageApprovals) SyncService() string { return syncsetup.ProductionRelayURL }

func TestStorageOptInStatsAfterPairingWithoutRestart(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	door := &storageApprovals{fakeApprovals: devicesFake(), state: "off"}
	a.approvals = door
	calls := 0
	a.syncStats = func(context.Context) (blobstore.Counts, error) { calls++; return blobstore.Counts{Storage: "on"}, nil }
	if cmd := a.refreshSyncSpace(); cmd != nil || calls != 0 {
		t.Fatal("unpaired surface asked for storage")
	}
	door.code = "id_" + strings.Repeat("a", 32)
	cmd := a.refreshSyncSpace()
	if cmd == nil || calls != 0 {
		t.Fatal("pairing did not enable the background refresh")
	}
	a.Update(cmd())
	if calls != 1 || a.syncSpace.Storage != "on" {
		t.Fatal("approval not seen without restart")
	}
}

func TestStorageOptInSearchRequestShowsFallbackFrame(t *testing.T) {
	code := "id_" + strings.Repeat("b", 32)
	door := &storageApprovals{fakeApprovals: devicesFake(), code: code, state: "off"}
	a, r := openSettingsDevices(t, door, time.Now())
	a.syncService = "http://127.0.0.1:18831"
	old := processOpener
	processOpener = func(string) error { return errors.New("no browser") }
	t.Cleanup(func() { processOpener = old })
	r.press("ctrl+u")
	r.typeText("relay storage")
	for i, item := range a.sheet.items {
		if item.dev != nil && item.dev.storage {
			a.sheet.cursor = i
			break
		}
	}
	r.press("enter")
	r.until("no-browser fallback", func() bool { return a.sheet.storageRequest })
	shown := plain(frame(a))
	if a.sheet.searching() || !a.sheet.onDevices() || !strings.Contains(shown, code) || !strings.Contains(frame(a), syncsetup.StorageRequestLink(code)) {
		t.Fatalf("search fallback frame: %s", shown)
	}
}

// The switch is called "relay storage" now. Contract: a person who remembers
// the old name, "sync storage", still finds the same row by typing it, and the
// row is drawn under the new name.
func TestStorageRowIsStillFoundByItsOldName(t *testing.T) {
	code := "id_" + strings.Repeat("d", 32)
	door := &storageApprovals{fakeApprovals: devicesFake(), code: code, state: "off"}
	a, r := openSettingsDevices(t, door, time.Now())
	a.syncService = "http://127.0.0.1:18831"
	r.press("ctrl+u")
	r.typeText("sync storage")
	found := false
	for _, item := range a.sheet.items {
		if item.dev != nil && item.dev.storage {
			found = true
		}
	}
	if !found {
		t.Fatalf("searching the old name %q did not find the storage row: %s", "sync storage", plain(frame(a)))
	}
	if shown := plain(frame(a)); !strings.Contains(shown, "relay storage") {
		t.Fatalf("the row must be drawn as relay storage: %s", shown)
	}
}

func TestStorageOptInRequestFallbackNarrowPendingDevicesFrame(t *testing.T) {
	code := "id_" + strings.Repeat("c", 32)
	door := &storageApprovals{fakeApprovals: devicesFake(), code: code, state: "off"}
	a, r := openSettingsDevices(t, door, time.Now())
	a.syncService = "http://127.0.0.1:18831"
	a.Update(tea.WindowSizeMsg{Width: 56, Height: 40})
	for i, item := range a.sheet.items {
		if item.dev != nil && item.dev.storage {
			a.sheet.cursor = i
			break
		}
	}
	before := processOpener
	processOpener = func(string) error { return errors.New("browser missing") }
	defer func() { processOpener = before }()
	// A device-list failure must not hide the locally available request.
	a.sheet.dev.readErr = errors.New("device list unavailable")
	r.press("enter")
	r.until("narrow no-browser fallback", func() bool { return a.sheet.storageRequest })
	shown := strings.Join(strings.Fields(plain(frame(a))), "")
	if !strings.Contains(shown, code) || !strings.Contains(frame(a), syncsetup.StorageRequestLink(code)) {
		t.Fatalf("narrow request frame: %s", plain(frame(a)))
	}
}

func TestStorageOptInStatusPrefersCurrentPermission(t *testing.T) {
	door := &storageApprovals{fakeApprovals: devicesFake(), code: "id_account", state: "off"}
	a := newTestApp(&fakeAgent{model: "m"})
	a.approvals = door
	a.syncService = "http://127.0.0.1:18831"
	a.syncSpace = blobstore.Counts{Storage: "on"}
	if !strings.Contains(a.statusText(), "relay storage") {
		t.Fatal("cached refusal hidden by earlier usage answer")
	}
	door.state = "on"
	a.syncSpace.Storage = "off"
	if strings.Contains(a.statusText(), "relay storage") {
		t.Fatal("cached approval hidden by earlier usage answer")
	}
}
