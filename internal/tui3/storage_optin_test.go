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
	if err != nil || !strings.HasPrefix(opened, syncsetup.StorageRequestURL+"?") || !strings.Contains(parsed.Query().Get("title"), code) {
		t.Fatalf("request link: %s %v", opened, err)
	}
	details := plain(frame(a))
	if !strings.Contains(details, code) || !strings.Contains(strings.Join(strings.Fields(details), ""), syncsetup.StorageRequestLink(code)) {
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
	if strings.Contains(a.sheet.devicesKeysLine(), "request access") {
		t.Fatal("enabled storage still asks for access")
	}
	a.syncSpace = blobstore.Counts{Storage: "on"}
	if strings.Contains(a.statusText(), "relay storage") {
		t.Fatal("enabled status has off line")
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
	shown := plain(frame(a))
	if a.sheet.searching() || !a.sheet.onDevices() || !strings.Contains(shown, code) || !strings.Contains(strings.Join(strings.Fields(shown), ""), syncsetup.StorageRequestLink(code)) {
		t.Fatalf("search fallback frame: %s", shown)
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
	shown := strings.Join(strings.Fields(plain(frame(a))), "")
	if !strings.Contains(shown, code) || !strings.Contains(shown, syncsetup.StorageRequestLink(code)) {
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
