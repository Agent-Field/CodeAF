package tui3

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/furrowbin"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

func openSettingsDevices(t *testing.T, door Approvals, now time.Time) (*app, *pairRig) {
	t.Helper()
	a, r := approveApp(t, door)
	a.clock = func() time.Time { return now }
	a.openSettings()
	walkToDevicesTab(t, a, r)
	if door != nil && a.cellsUnavailable == "" {
		r.until("the settings device list", func() bool {
			return a.sheet.dev != nil && (a.sheet.dev.loaded || a.sheet.dev.line != "" || a.sheet.dev.readErr != nil)
		})
	}
	return a, r
}

func walkToDevicesTab(t *testing.T, a *app, r *pairRig) {
	t.Helper()
	for guard := 0; guard < len(settingTabs) && !a.sheet.onDevices(); guard++ {
		r.press("right")
	}
	if !a.sheet.onDevices() {
		t.Fatalf("right arrows never reached %s; stopped on %s", tabDevices, settingTabs[a.sheet.tab])
	}
}

// Validation contract item 1: `/settings` shows a `Devices` tab between
// `Connections` and `Privacy`, and moving across the bar with the existing tab
// keys (`←` and `→`) reaches it.
func TestSettingsDevicesTabSitsBetweenConnectionsAndPrivacyAndIsReachedByTabKeys(t *testing.T) {
	at := -1
	for i, name := range settingTabs {
		if name == tabDevices {
			at = i
			break
		}
	}
	if at <= 0 || at+1 >= len(settingTabs) {
		t.Fatalf("%s at index %d in %v, want between %s and %s", tabDevices, at, settingTabs, tabConnections, tabPrivacy)
	}
	if settingTabs[at-1] != tabConnections || settingTabs[at+1] != tabPrivacy {
		t.Fatalf("neighbors around %s are %q and %q, want %q and %q",
			tabDevices, settingTabs[at-1], settingTabs[at+1], tabConnections, tabPrivacy)
	}

	a, r := approveApp(t, devicesFake())
	a.openSettings()
	if settingTabs[a.sheet.tab] != tabGeneral {
		t.Fatalf("settings opened on %s, want %s", settingTabs[a.sheet.tab], tabGeneral)
	}
	walkToDevicesTab(t, a, r)
	if !strings.Contains(plain(frame(a)), tabDevices) {
		t.Fatalf("frame does not draw %s on the bar:\n%s", tabDevices, plain(frame(a)))
	}
	r.press("right")
	if settingTabs[a.sheet.tab] != tabPrivacy {
		t.Fatalf("one step right of %s landed on %s, want %s", tabDevices, settingTabs[a.sheet.tab], tabPrivacy)
	}
	r.press("left")
	if !a.sheet.onDevices() {
		t.Fatalf("one step left of %s landed on %s, want %s", tabPrivacy, settingTabs[a.sheet.tab], tabDevices)
	}
}

type gatedDevicesApprovals struct {
	*fakeApprovals
	release chan struct{}
}

func (g *gatedDevicesApprovals) Devices(ctx context.Context) ([]DeviceRow, error) {
	select {
	case <-g.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return g.fakeApprovals.Devices(ctx)
}

// Validation contract item 2: the tab lists devices through `a.approvals.Devices`
// in `orderDevices` order, using the card's row words (`this device`,
// `seen … ago`, `removed`, online and offline dots from the palette's glyph
// door) and `devicesLoading` while loading, and re-reads when the directory
// version advances (`refreshOnChange`).
func TestSettingsDevicesListsDevicesInCardOrderAndRefreshesOnDirectoryChange(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	door := &fakeApprovals{devices: []DeviceRow{
		{ID: "dev_C", Name: "desk", Platform: "darwin", LastSeen: now.Add(-3 * time.Hour)},
		{ID: "dev_B", Name: "tower", Platform: "linux"},
		{ID: "dev_A", Name: "this mac", Platform: "darwin", Self: true},
	}}
	gated := &gatedDevicesApprovals{fakeApprovals: door, release: make(chan struct{})}

	a, r := approveApp(t, gated)
	a.clock = func() time.Time { return now }
	a.dirFeed = &stillFeed{online: []string{"dev_B"}, version: 1}
	a.openSettings()
	walkToDevicesTab(t, a, r)

	loading := plain(frame(a))
	if !strings.Contains(loading, devicesLoading) {
		t.Fatalf("while loading, tab lacked %q:\n%s", devicesLoading, loading)
	}
	if strings.Contains(loading, addDeviceWord) {
		t.Fatalf("while loading, tab already drew %q:\n%s", addDeviceWord, loading)
	}

	close(gated.release)
	r.until("loaded rows", func() bool { return a.sheet.dev != nil && a.sheet.dev.loaded })

	got := plain(frame(a))
	selfAt := strings.Index(got, "● this mac")
	towerAt := strings.Index(got, "● tower")
	deskAt := strings.Index(got, "○ desk")
	addAt := strings.Index(got, addDeviceWord)
	if selfAt < 0 || towerAt < 0 || deskAt < 0 || addAt < 0 {
		t.Fatalf("missing expected rows:\n%s", got)
	}
	if !(selfAt < towerAt && towerAt < deskAt && deskAt < addAt) {
		t.Fatalf("rows out of orderDevices order (self=%d tower=%d desk=%d add=%d):\n%s",
			selfAt, towerAt, deskAt, addAt, got)
	}
	for _, want := range []string{devicesSelf, "seen 3h ago"} {
		if !strings.Contains(got, want) {
			t.Fatalf("screen lacked %q:\n%s", want, got)
		}
	}

	// Dots resolve through the palette's glyph door rather than literal marks.
	asciiPal := newPalette(tokens.ANSI256, true)
	asciiSelf := plain(strings.Join(a.sheet.devRowLines(a.sheet.items[0].dev, nil, false, false, 80, asciiPal), "\n"))
	asciiAway := plain(strings.Join(a.sheet.devRowLines(a.sheet.items[2].dev, nil, false, false, 80, asciiPal), "\n"))
	if !strings.Contains(asciiSelf, "# this mac") || !strings.Contains(asciiAway, "o desk") {
		t.Fatalf("ASCII glyph repertoire not honored: self=%q away=%q", asciiSelf, asciiAway)
	}

	// Directory change triggers refreshOnChange and updates the open tab.
	door.mu.Lock()
	door.devices[1].Revoked = true
	door.mu.Unlock()
	a.dirFeed = &stillFeed{online: []string{"dev_B"}, version: 2}
	r.feed(dirWatchMsg{from: a.dirFeed})
	r.until("directory refresh", func() bool {
		shown := plain(frame(a))
		return strings.Contains(shown, "○ tower") && strings.Contains(shown, "removed")
	})
	if strings.Contains(plain(frame(a)), "● tower") {
		t.Fatalf("revoked device kept online dot after refresh:\n%s", plain(frame(a)))
	}
}

// Validation contract item 3: the tab has an `Add another device` row sharing
// the single person-facing constant, and pressing `enter` on it leaves
// `/settings` and starts bare `/pair` (`runPair("")`).
func TestSettingsDevicesAddRowLeavesSettingsAndStartsBarePair(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	door := devicesFake()
	var shown *pair.Code
	pairing := &fakePairing{
		offer: func(ctx context.Context, ui pair.OfferUI) (string, error) {
			code, err := pair.NewCode(now)
			if err != nil {
				return "", err
			}
			code = code.WithPlate("42")
			shown = code
			ui.Show(code, pair.ChatLines(code, ""))
			<-ctx.Done()
			return "", ctx.Err()
		},
	}

	a, r := approveApp(t, door)
	a.pairing = pairing
	a.clock = func() time.Time { return now }
	a.openSettings()
	walkToDevicesTab(t, a, r)
	r.until("loaded rows", func() bool { return a.sheet.dev != nil && a.sheet.dev.loaded })

	for guard := 0; guard < len(a.sheet.items) && !a.sheet.onAddDeviceRow(); guard++ {
		r.press("down")
	}
	if !a.sheet.onAddDeviceRow() {
		t.Fatal("down arrows did not reach Add another device")
	}

	r.press("enter")
	if a.at(pageSettings) {
		t.Fatal("enter on Add another device did not close /settings")
	}
	r.until("the pairing code", func() bool { return a.pair.open && a.pair.code != nil })
	if shown == nil || !strings.Contains(plain(frame(a)), "codeaf pair "+shown.Shown()) {
		t.Fatalf("bare /pair screen did not open:\n%s", plain(frame(a)))
	}
}

// Validation contract item 4: `r` calls `Revoke(ctx, id)`, marks the row
// `removed`, and puts `devicesRevoked` with the name on the foot line; on this
// device's own row `r` shows `devicesOwnRow` and calls nothing; on the add row
// or an already-removed row `r` does nothing.
func TestSettingsDevicesRKeyConfirmsRemovalRefusesOwnRowAndIgnoresAddAndRemovedRows(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	door := &fakeApprovals{devices: []DeviceRow{
		{ID: "dev_A", Name: "this mac", Platform: "darwin", Self: true},
		{ID: "dev_B", Name: "tower", Platform: "linux"},
	}}
	a, r := openSettingsDevices(t, door, now)

	// Own row: shows devicesOwnRow on the foot line and calls nothing.
	r.press("r")
	if door.did() != "" {
		t.Fatalf("r on own row called door: %s", door.did())
	}
	foot := plain(strings.Join((placeSettings{}).note(a, a.width), " "))
	if !strings.Contains(foot, devicesOwnRow) || !strings.Contains(plain(frame(a)), devicesOwnRow) {
		t.Fatalf("r on own row did not show %q on foot line (foot=%q):\n%s", devicesOwnRow, foot, plain(frame(a)))
	}

	// Add row: r does nothing.
	r.press("down", "down")
	if !a.sheet.onAddDeviceRow() {
		t.Fatal("expected cursor on add row")
	}
	a.sheet.dev.line = ""
	r.press("r")
	if door.did() != "" || a.sheet.dev.line != "" {
		t.Fatalf("r on add row acted: door=%q line=%q", door.did(), a.sheet.dev.line)
	}

	// A removable row asks once; y removes it and puts devicesRevoked on the foot line.
	r.press("up", "r", "y")
	wantRevoked := fmt.Sprintf(devicesRevoked, "tower")
	r.until("revoked outcome", func() bool {
		return strings.Contains(plain(frame(a)), wantRevoked)
	})
	if door.did() != "revoke:dev_B" {
		t.Fatalf("door calls = %q, want revoke:dev_B", door.did())
	}
	foot = plain(strings.Join((placeSettings{}).note(a, a.width), " "))
	if !strings.Contains(foot, wantRevoked) {
		t.Fatalf("foot line = %q, want %q", foot, wantRevoked)
	}
	shown := plain(frame(a))
	if !strings.Contains(shown, "○ tower") || !strings.Contains(shown, "removed") {
		t.Fatalf("row not marked removed:\n%s", shown)
	}

	// Already-removed row: a second r does nothing.
	r.press("r")
	if door.did() != "revoke:dev_B" {
		t.Fatalf("r on removed row called door again: %s", door.did())
	}
}

// Validation contract item 5: `n` renames this device through `startNaming`,
// `typed` and `rename`; while typing, `esc` cancels typing and does NOT close
// `/settings`, and only a second `esc` with no typing in progress leaves
// `/settings`.
func TestSettingsDevicesNKeyRenamesThisDeviceAndEscCancelsTypingWithoutClosingSettings(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	door := devicesFake()
	a, r := openSettingsDevices(t, door, now)

	r.press("n")
	if !a.sheet.devTyping() {
		t.Fatal("n on own row did not start naming")
	}
	lines, _, caretX, caretY, up := a.placeFrameNow(a.width, a.height)
	if !up {
		t.Fatal("settings place not up")
	}
	promptRow := -1
	for i, line := range lines {
		if strings.Contains(plain(line), devicesNameAsk+"this mac") {
			promptRow = i
			break
		}
	}
	if promptRow < 0 {
		t.Fatalf("prefilled rename field missing:\n%s", plain(strings.Join(lines, "\n")))
	}
	if caretY != promptRow || !strings.HasSuffix(ansi.Truncate(plain(lines[caretY]), caretX, ""), "this mac") {
		t.Fatalf("caret at (%d,%d), want end of prompt row %d", caretX, caretY, promptRow)
	}

	// Letters that are otherwise commands (`r`, `n`) type into the name, and esc
	// cancels only the typing without closing /settings.
	r.press("ctrl+u")
	r.typeText("rnd")
	if door.did() != "" {
		t.Fatalf("typing into rename field called door: %s", door.did())
	}
	r.press("esc")
	if a.sheet.devTyping() {
		t.Fatal("first esc did not cancel naming")
	}
	if !a.at(pageSettings) || !a.sheet.onDevices() {
		t.Fatal("first esc closed /settings instead of only cancelling the rename")
	}
	if door.did() != "" {
		t.Fatalf("cancelling rename wrote to door: %s", door.did())
	}

	// Saving a new name goes through Rename and refreshes the row.
	r.press("n", "ctrl+u")
	r.typeText("studio mac")
	r.press("enter")
	r.until("renamed row", func() bool {
		return !a.sheet.devTyping() && strings.Contains(plain(frame(a)), "● studio mac")
	})
	if door.did() != "rename:studio mac" {
		t.Fatalf("door = %q, want rename:studio mac", door.did())
	}

	// With no typing in progress, esc leaves /settings.
	r.press("esc")
	if a.at(pageSettings) {
		t.Fatal("esc at rest on Devices did not close /settings")
	}
}

// Validation contract item 6: the keys line uses `devicesNameKey`,
// `devicesGoneKey`, `devicesMoveKey` and `devicesNameKeys`, shrinking so
// `r remove` appears only on a removable row, `↑↓ choose` only when more than
// one row can be walked, and `enter add` on the add row.
func TestSettingsDevicesKeysLineShrinksToWhatWorksOnTheCurrentRow(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

	// Single selectable row (empty directory + Add another device): no ↑↓ choose,
	// enter add present, no r remove.
	single, _ := openSettingsDevices(t, &fakeApprovals{}, now)
	keys := single.sheet.keysLine()
	if strings.Contains(keys, devicesMoveKey) {
		t.Fatalf("single-row Devices tab offered %q: %q", devicesMoveKey, keys)
	}
	if !strings.Contains(keys, devicesAddKey) || strings.Contains(keys, devicesGoneKey) {
		t.Fatalf("single add row keys wrong: %q", keys)
	}

	// Multi-row tab: self, removable, and already-removed rows (orderDevices
	// sorts offline computers alphabetically, so "old" precedes "tower").
	door := &fakeApprovals{devices: []DeviceRow{
		{ID: "dev_A", Name: "this mac", Platform: "darwin", Self: true},
		{ID: "dev_B", Name: "tower", Platform: "linux"},
		{ID: "dev_C", Name: "old", Platform: "linux", Revoked: true},
	}}
	a, r := openSettingsDevices(t, door, now)

	// Row 0 (Self): ↑↓ choose + n rename, no r remove.
	keys = a.sheet.keysLine()
	if !strings.Contains(keys, devicesMoveKey) || !strings.Contains(keys, devicesNameKey) || strings.Contains(keys, devicesGoneKey) {
		t.Fatalf("own row keys = %q", keys)
	}

	// Typing a name: devicesNameKeys.
	r.press("n")
	if got := a.sheet.keysLine(); got != devicesNameKeys {
		t.Fatalf("typing keys = %q, want %q", got, devicesNameKeys)
	}
	r.press("esc")

	// Row 1 ("old", already removed): ↑↓ choose + n rename, no r remove.
	r.press("down")
	keys = a.sheet.keysLine()
	if !strings.Contains(keys, devicesMoveKey) || !strings.Contains(keys, devicesNameKey) || strings.Contains(keys, devicesGoneKey) {
		t.Fatalf("removed row keys = %q", keys)
	}

	// Row 2 ("tower", removable): ↑↓ choose + n rename + r remove.
	r.press("down")
	keys = a.sheet.keysLine()
	if !strings.Contains(keys, devicesMoveKey) || !strings.Contains(keys, devicesNameKey) || !strings.Contains(keys, devicesGoneKey) {
		t.Fatalf("removable row keys = %q", keys)
	}

	// Row 3 (Add another device): ↑↓ choose + enter add, no r remove.
	r.press("down")
	keys = a.sheet.keysLine()
	if !strings.Contains(keys, devicesMoveKey) || !strings.Contains(keys, devicesAddKey) || strings.Contains(keys, devicesGoneKey) {
		t.Fatalf("add row keys = %q", keys)
	}
}

// Validation contract item 7: when devices cannot work here, the tab shows the
// same single sentence `/devices` would say — `cellsUnavailable` (including on
// platforms where `commandOnPlatform` hides `/devices`), `pairUnavailableWord`
// with a nil approvals door, or the card's own error wording on a failed list
// read — and never shows an empty list, `0 devices`, or a working-looking add
// row.
func TestSettingsDevicesUnavailableAndFailedReadSayWhyAndNothingElse(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

	t.Run("nil approvals door", func(t *testing.T) {
		a, _ := openSettingsDevices(t, nil, now)
		got := plain(frame(a))
		if !strings.Contains(got, pairUnavailableWord) {
			t.Fatalf("nil door lacked %q:\n%s", pairUnavailableWord, got)
		}
		if strings.Contains(got, addDeviceWord) || strings.Contains(got, "0 devices") {
			t.Fatalf("nil door drew forbidden list or add row:\n%s", got)
		}
	})

	t.Run("cells unavailable reason", func(t *testing.T) {
		reason := furrowbin.CellsUnavailable
		a, r := approveApp(t, devicesFake())
		a.cellsUnavailable = reason
		a.clock = func() time.Time { return now }
		// Confirm a typed /devices says this exact sentence.
		a.slash("/devices")
		if !strings.Contains(updateNotes(a), reason) {
			t.Fatalf("typed /devices did not say %q: %q", reason, updateNotes(a))
		}
		a.openSettings()
		walkToDevicesTab(t, a, r)
		got := plain(frame(a))
		if !strings.Contains(got, reason) {
			t.Fatalf("tab lacked cells-unavailable sentence %q:\n%s", reason, got)
		}
		if strings.Contains(got, addDeviceWord) || strings.Contains(got, "this mac") {
			t.Fatalf("unavailable tab drew rows or add door:\n%s", got)
		}
	})

	t.Run("failed list read", func(t *testing.T) {
		readErr := errors.New("the device directory did not answer")
		a, _ := openSettingsDevices(t, &fakeApprovals{err: readErr}, now)
		got := plain(frame(a))
		if !strings.Contains(got, readErr.Error()) {
			t.Fatalf("failed read lacked card error %q:\n%s", readErr.Error(), got)
		}
		if strings.Contains(got, addDeviceWord) {
			t.Fatalf("failed read still offered %q:\n%s", addDeviceWord, got)
		}
	})
}

// Validation contract item 8: typing `devices`, `remove`, `revoke`, `pair` or
// `add device` into the `/settings` search finds the `Devices` tab.
func TestSettingsDevicesSearchFindsTheTabAcrossQueries(t *testing.T) {
	queries := []struct {
		query string
		top   bool
	}{
		{"devices", true},
		{"remove", false},
		{"revoke", true},
		{"pair", true},
		{"add device", true},
	}

	for _, tc := range queries {
		t.Run(tc.query, func(t *testing.T) {
			a, r := approveApp(t, devicesFake())
			a.openSettings()
			r.press("ctrl+u")
			r.typeText(tc.query)
			if tc.top && !a.sheet.onDevices() {
				t.Fatalf("query %q landed on %s, want %s", tc.query, settingTabs[a.sheet.tab], tabDevices)
			}
			var hasHeading, hasRow bool
			for _, item := range a.sheet.items {
				if item.head == tabDevices {
					hasHeading = true
				}
				if item.dev != nil {
					hasRow = true
				}
			}
			if !hasHeading || !hasRow {
				t.Fatalf("query %q missing %s results (heading=%t row=%t)", tc.query, tabDevices, hasHeading, hasRow)
			}
			if shown := plain(frame(a)); !strings.Contains(shown, addDeviceWord) {
				t.Fatalf("query %q did not draw %q on screen:\n%s", tc.query, addDeviceWord, shown)
			}
		})
	}
}
