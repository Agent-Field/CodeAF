package tui3

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type fakePermitApprovals struct {
	*fakeApprovals
	setErr error
}

func (f *fakePermitApprovals) SetPermit(id string, watch, drive *bool) error {
	w := "nil"
	if watch != nil {
		if *watch {
			w = "true"
		} else {
			w = "false"
		}
	}
	d := "nil"
	if drive != nil {
		if *drive {
			d = "true"
		} else {
			d = "false"
		}
	}
	f.log("permit:" + id + ":w=" + w + ":d=" + d)
	if f.setErr != nil {
		return f.setErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.devices {
		if f.devices[i].ID == id && f.devices[i].Permit != nil {
			p := *f.devices[i].Permit
			if watch != nil {
				p.Watch = *watch
			}
			if drive != nil {
				p.Drive = *drive
			}
			f.devices[i].Permit = &p
		}
	}
	return nil
}

func permitDevicesDoor(now time.Time) *fakePermitApprovals {
	return &fakePermitApprovals{
		fakeApprovals: &fakeApprovals{
			devices: []DeviceRow{
				{ID: "dev_A", Name: "this mac", Platform: "darwin", Self: true, Permit: &DevicePermit{Watch: true, Drive: true}},
				{ID: "dev_B", Name: "tower", Platform: "linux", Permit: &DevicePermit{Watch: true, Drive: true}},
				{ID: "dev_C", Name: "desk", Platform: "darwin", LastSeen: now.Add(-3 * time.Hour), Permit: &DevicePermit{Watch: true, Drive: false}},
				{ID: "dev_D", Name: "vault", Platform: "linux", LastSeen: now.Add(-5 * time.Hour), Permit: &DevicePermit{Watch: false, Drive: false}},
				{ID: "dev_E", Name: "plain", Platform: "linux", LastSeen: now.Add(-6 * time.Hour)},
				{ID: "dev_F", Name: "old", Platform: "linux", Revoked: true, Permit: &DevicePermit{Watch: true, Drive: true}},
			},
		},
	}
}

// V11/V25/T13 (contract 1): another computer's row with Permit != nil shows its
// access word (`watch and type`, `watch only`, or `not shared`) and adds
// `w watch` and `t type` before `esc close`, while this computer's own row, an
// `Add another device` row, a removed row, and rows with Permit == nil show neither.
func TestDevicesPermitRowsAndKeysAppearOnlyForAnotherComputerWithPermit(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	door := permitDevicesDoor(now)
	a, r := approveApp(t, door)
	a.width = 110
	a.clock = func() time.Time { return now }
	a.dirFeed = stillFeed{online: []string{"dev_B"}}

	r.slash("/devices")
	r.until("the device list", func() bool { c, ok := a.pair.card.(*deviceCard); return ok && c.loaded })

	screen := plain(frame(a))
	for _, want := range []string{
		"tower", devicesAccessBoth,
		"desk", devicesAccessWatch,
		"vault", devicesAccessNone,
	} {
		if !strings.Contains(screen, want) {
			t.Fatalf("/devices screen lacked %q:\n%s", want, screen)
		}
	}
	for _, line := range strings.Split(screen, "\n") {
		if strings.Contains(line, "this mac") || strings.Contains(line, "plain") || strings.Contains(line, "old") || strings.Contains(line, addDeviceWord) {
			for _, access := range []string{devicesAccessBoth, devicesAccessWatch, devicesAccessNone} {
				if strings.Contains(line, access) {
					t.Fatalf("row %q unexpectedly drew access word %q", line, access)
				}
			}
		}
	}

	// Row 0: this mac (Self) -> no w watch or t type.
	if hint := a.hintWord(); strings.Contains(hint, devicesWatchKey) || strings.Contains(hint, devicesTypeKey) {
		t.Fatalf("own row offered watch/type keys: %q", hint)
	}

	// Row 1: tower (online, Permit != nil) -> w watch and t type before esc close.
	r.press("down")
	hint := a.hintWord()
	wAt := strings.Index(hint, devicesWatchKey)
	tAt := strings.Index(hint, devicesTypeKey)
	closeAt := strings.Index(hint, devicesCloseKey)
	if wAt < 0 || tAt < 0 || closeAt < 0 || !(wAt < tAt && tAt < closeAt) {
		t.Fatalf("tower row keys = %q, want %q and %q before %q", hint, devicesWatchKey, devicesTypeKey, devicesCloseKey)
	}

	// Row 2: desk (offline, Permit != nil) -> offers w watch and t type.
	r.press("down")
	if hint := a.hintWord(); !strings.Contains(hint, devicesWatchKey) || !strings.Contains(hint, devicesTypeKey) {
		t.Fatalf("desk row keys = %q, want watch and type keys", hint)
	}

	// Row 3: old (Revoked) -> no w watch or t type.
	r.press("down")
	if hint := a.hintWord(); strings.Contains(hint, devicesWatchKey) || strings.Contains(hint, devicesTypeKey) {
		t.Fatalf("revoked row offered watch/type keys: %q", hint)
	}

	// Row 4: plain (Permit == nil) -> no w watch or t type.
	r.press("down")
	if hint := a.hintWord(); strings.Contains(hint, devicesWatchKey) || strings.Contains(hint, devicesTypeKey) {
		t.Fatalf("row with Permit == nil offered watch/type keys: %q", hint)
	}

	// Row 5: vault (Permit != nil) -> offers w watch and t type.
	r.press("down")
	if hint := a.hintWord(); !strings.Contains(hint, devicesWatchKey) || !strings.Contains(hint, devicesTypeKey) {
		t.Fatalf("vault row keys = %q, want watch and type keys", hint)
	}

	// Row 6: Add another device -> no w watch or t type.
	r.press("down")
	if hint := a.hintWord(); strings.Contains(hint, devicesWatchKey) || strings.Contains(hint, devicesTypeKey) {
		t.Fatalf("Add another device row offered watch/type keys: %q", hint)
	}
}

// V11/V25/T13 (contract 2 and 3): `w` toggles watch on and off at once, and `t`
// turns type off at once and asks for `y` confirmation before turning type on.
func TestDevicesPermitWatchAndTypeSwitchesUpdateAtOnceAndConfirmTurningTypeOn(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	door := &fakePermitApprovals{
		fakeApprovals: &fakeApprovals{
			devices: []DeviceRow{
				{ID: "dev_A", Name: "this mac", Platform: "darwin", Self: true},
				{ID: "dev_B", Name: "tower", Platform: "linux", Permit: &DevicePermit{Watch: true, Drive: true}},
			},
		},
	}
	a, r := approveApp(t, door)
	a.width = 110
	a.clock = func() time.Time { return now }
	a.dirFeed = stillFeed{online: []string{"dev_B"}}

	r.slash("/devices")
	r.until("the device list", func() bool { c, ok := a.pair.card.(*deviceCard); return ok && c.loaded })

	// Move to tower (watch and type).
	r.press("down")
	if !strings.Contains(plain(frame(a)), devicesAccessBoth) {
		t.Fatalf("expected tower to start as %q:\n%s", devicesAccessBoth, plain(frame(a)))
	}

	// 1. w: watch on -> off calls SetPermit(id, &false, &false), updates row to not shared.
	r.press("w")
	if got := door.did(); got != "permit:dev_B:w=false:d=false" {
		t.Fatalf("w on->off calls = %q, want permit:dev_B:w=false:d=false", got)
	}
	screen := plain(frame(a))
	if !strings.Contains(screen, devicesAccessNone) || !strings.Contains(screen, devicesWatchOffFor("tower")) {
		t.Fatalf("after watch off screen lacked %q or %q:\n%s", devicesAccessNone, devicesWatchOffFor("tower"), screen)
	}

	// 2. w: watch off -> on calls SetPermit(id, &true, nil), updates row to watch only.
	door.calls = nil
	r.press("w")
	if got := door.did(); got != "permit:dev_B:w=true:d=nil" {
		t.Fatalf("w off->on calls = %q, want permit:dev_B:w=true:d=nil", got)
	}
	screen = plain(frame(a))
	if !strings.Contains(screen, devicesAccessWatch) || !strings.Contains(screen, devicesWatchOnFor("tower")) {
		t.Fatalf("after watch on screen lacked %q or %q:\n%s", devicesAccessWatch, devicesWatchOnFor("tower"), screen)
	}

	// 3. t: type off -> on first asks confirmation; any key other than y (including n and esc) cancels.
	door.calls = nil
	r.press("t")
	if door.did() != "" {
		t.Fatalf("t off->on called SetPermit before confirmation: %s", door.did())
	}
	wantAsk := devicesTypeAskFor("tower")
	if !strings.Contains(plain(frame(a)), wantAsk) || a.hintWord() != devicesTypeKeys {
		t.Fatalf("t off->on did not show confirmation %q (hint=%q):\n%s", wantAsk, a.hintWord(), plain(frame(a)))
	}
	r.press("n")
	if door.did() != "" || !a.pair.open || !strings.Contains(plain(frame(a)), devicesAccessWatch) {
		t.Fatalf("n did not cancel cleanly: calls=%q open=%t\n%s", door.did(), a.pair.open, plain(frame(a)))
	}

	// esc also cancels the confirmation without closing /devices.
	r.press("t", "esc")
	if door.did() != "" || !a.pair.open || !strings.Contains(plain(frame(a)), devicesAccessWatch) {
		t.Fatalf("esc during type confirmation closed /devices or changed permit: calls=%q open=%t\n%s", door.did(), a.pair.open, plain(frame(a)))
	}

	// 4. t then y confirms: calls SetPermit(id, &true, &true) and says tower can type.
	r.press("t", "y")
	if got := door.did(); got != "permit:dev_B:w=true:d=true" {
		t.Fatalf("t+y calls = %q, want permit:dev_B:w=true:d=true", got)
	}
	screen = plain(frame(a))
	if !strings.Contains(screen, devicesAccessBoth) || !strings.Contains(screen, devicesTypeOnFor("tower")) {
		t.Fatalf("after t+y screen lacked %q or %q:\n%s", devicesAccessBoth, devicesTypeOnFor("tower"), screen)
	}

	// 5. t: type on -> off is immediate: calls SetPermit(id, nil, &false) and says tower can watch, not type.
	door.calls = nil
	r.press("t")
	if got := door.did(); got != "permit:dev_B:w=nil:d=false" {
		t.Fatalf("t on->off calls = %q, want permit:dev_B:w=nil:d=false", got)
	}
	screen = plain(frame(a))
	if !strings.Contains(screen, devicesAccessWatch) || !strings.Contains(screen, devicesTypeOffFor("tower")) {
		t.Fatalf("after t on->off screen lacked %q or %q:\n%s", devicesAccessWatch, devicesTypeOffFor("tower"), screen)
	}
}

// V11/T13 (contract 4): when the Approvals door has no PermitSetter or
// SetPermit returns an error, `w` and `t` say the error (or `this build cannot
// change that`) and nothing changes on screen.
func TestDevicesPermitErrorsAndMissingSetterLeaveTheScreenUnchanged(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

	// 1. Door without PermitSetter.
	noSetter := &fakeApprovals{
		devices: []DeviceRow{
			{ID: "dev_A", Name: "this mac", Platform: "darwin", Self: true},
			{ID: "dev_B", Name: "tower", Platform: "linux", Permit: &DevicePermit{Watch: true, Drive: false}},
		},
	}
	a, r := approveApp(t, noSetter)
	a.width = 110
	a.clock = func() time.Time { return now }
	r.slash("/devices")
	r.until("the device list", func() bool { c, ok := a.pair.card.(*deviceCard); return ok && c.loaded })

	r.press("down", "w")
	if !strings.Contains(plain(frame(a)), devicesPermitNoSetter) || !strings.Contains(plain(frame(a)), devicesAccessWatch) {
		t.Fatalf("w without PermitSetter did not say %q or changed row:\n%s", devicesPermitNoSetter, plain(frame(a)))
	}
	a.openDevicesCard().line = ""
	r.press("t")
	if !strings.Contains(plain(frame(a)), devicesPermitNoSetter) || !strings.Contains(plain(frame(a)), devicesAccessWatch) {
		t.Fatalf("t without PermitSetter did not say %q or changed row:\n%s", devicesPermitNoSetter, plain(frame(a)))
	}

	// 2. Door whose SetPermit returns an error.
	wantErr := "could not save device permissions"
	failing := &fakePermitApprovals{
		fakeApprovals: &fakeApprovals{
			devices: []DeviceRow{
				{ID: "dev_A", Name: "this mac", Platform: "darwin", Self: true},
				{ID: "dev_B", Name: "tower", Platform: "linux", Permit: &DevicePermit{Watch: true, Drive: false}},
			},
		},
		setErr: errors.New(wantErr),
	}
	a2, r2 := approveApp(t, failing)
	a2.width = 110
	a2.clock = func() time.Time { return now }
	r2.slash("/devices")
	r2.until("the device list", func() bool { c, ok := a2.pair.card.(*deviceCard); return ok && c.loaded })

	r2.press("down", "w")
	if !strings.Contains(plain(frame(a2)), wantErr) || !strings.Contains(plain(frame(a2)), devicesAccessWatch) {
		t.Fatalf("w error did not show %q or changed row:\n%s", wantErr, plain(frame(a2)))
	}
	a2.openDevicesCard().line = ""
	r2.press("t", "y")
	if !strings.Contains(plain(frame(a2)), wantErr) || !strings.Contains(plain(frame(a2)), devicesAccessWatch) {
		t.Fatalf("t+y error did not show %q or changed row:\n%s", wantErr, plain(frame(a2)))
	}
}

// V11/V25/T13 (contract 1-4): the Settings > Devices tab shares the same words,
// key legend, confirmation and permit updates as the `/devices` card.
func TestTheDevicesCardAndTheSettingsTabOfferTheSameSwitches(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	door := &fakePermitApprovals{
		fakeApprovals: &fakeApprovals{
			devices: []DeviceRow{
				{ID: "dev_A", Name: "this mac", Platform: "darwin", Self: true},
				{ID: "dev_B", Name: "tower", Platform: "linux", Permit: &DevicePermit{Watch: true, Drive: false}},
			},
		},
	}
	a, r := openSettingsDevices(t, door, now)
	a.width = 120

	// Own row has no watch or type keys.
	if keys := a.sheet.keysLine(); strings.Contains(keys, devicesWatchKey) || strings.Contains(keys, devicesTypeKey) {
		t.Fatalf("Settings Devices own row offered watch/type keys: %q", keys)
	}

	// Move to tower (watch only).
	r.press("down")
	keys := a.sheet.keysLine()
	if !strings.Contains(keys, devicesWatchKey) || !strings.Contains(keys, devicesTypeKey) {
		t.Fatalf("Settings Devices tower row keys = %q", keys)
	}
	if !strings.Contains(plain(frame(a)), devicesAccessWatch) {
		t.Fatalf("Settings Devices lacked %q:\n%s", devicesAccessWatch, plain(frame(a)))
	}

	// t asks confirmation; esc cancels without closing Settings.
	r.press("t")
	if got := a.sheet.keysLine(); got != devicesTypeKeys {
		t.Fatalf("Settings Devices type confirmation keys = %q, want %q", got, devicesTypeKeys)
	}
	if !strings.Contains(plain(frame(a)), devicesTypeAskFor("tower")) {
		t.Fatalf("Settings Devices lacked confirmation ask:\n%s", plain(frame(a)))
	}
	r.press("esc")
	if !a.at(pageSettings) || !a.sheet.onDevices() || door.did() != "" {
		t.Fatalf("esc during Settings Devices type confirmation closed settings or called door: atSettings=%t calls=%q", a.at(pageSettings), door.did())
	}

	// t then y turns type on.
	r.press("t", "y")
	if got := door.did(); got != "permit:dev_B:w=true:d=true" {
		t.Fatalf("Settings Devices t+y calls = %q", got)
	}
	screen := plain(frame(a))
	if !strings.Contains(screen, devicesAccessBoth) || !strings.Contains(screen, devicesTypeOnFor("tower")) {
		t.Fatalf("Settings Devices after t+y lacked %q or %q:\n%s", devicesAccessBoth, devicesTypeOnFor("tower"), screen)
	}

	// w turns watch (and type) off.
	door.calls = nil
	r.press("w")
	if got := door.did(); got != "permit:dev_B:w=false:d=false" {
		t.Fatalf("Settings Devices w calls = %q", got)
	}
	screen = plain(frame(a))
	if !strings.Contains(screen, devicesAccessNone) || !strings.Contains(screen, devicesWatchOffFor("tower")) {
		t.Fatalf("Settings Devices after w lacked %q or %q:\n%s", devicesAccessNone, devicesWatchOffFor("tower"), screen)
	}
}

// V14/T13 (absent path): when DeviceRow.Permit is nil on every row, both the
// `/devices` card and the Settings > Devices tab are unchanged.
func TestWithoutPermitInfoTheDevicesScreensAreUnchanged(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	door := devicesFake()
	a, r := approveApp(t, door)
	a.clock = func() time.Time { return now }

	r.slash("/devices")
	r.until("the device list", func() bool { c, ok := a.pair.card.(*deviceCard); return ok && c.loaded })
	r.press("down")

	screen := plain(frame(a))
	for _, access := range []string{devicesAccessBoth, devicesAccessWatch, devicesAccessNone} {
		if strings.Contains(screen, access) {
			t.Fatalf("/devices without Permit drew %q:\n%s", access, screen)
		}
	}
	if hint := a.hintWord(); strings.Contains(hint, devicesWatchKey) || strings.Contains(hint, devicesTypeKey) {
		t.Fatalf("/devices without Permit offered watch/type keys: %q", hint)
	}
	r.press("w", "t")
	if door.did() != "" || a.openDevicesCard().line != "" {
		t.Fatalf("w/t on row with nil Permit acted: calls=%q line=%q", door.did(), a.openDevicesCard().line)
	}

	sApp, sRig := openSettingsDevices(t, devicesFake(), now)
	sRig.press("down")
	if keys := sApp.sheet.keysLine(); strings.Contains(keys, devicesWatchKey) || strings.Contains(keys, devicesTypeKey) {
		t.Fatalf("Settings Devices without Permit offered watch/type keys: %q", keys)
	}
}
