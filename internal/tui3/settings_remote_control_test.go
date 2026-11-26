package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
)

// remoteControlRowApp opens Settings on the Devices tab with the computers read
// and the environment pin out of the way, so the row reads what its file says.
func remoteControlRowApp(t *testing.T, door Approvals) (*app, *pairRig) {
	t.Helper()
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_REMOTE_CONTROL", "")
	a, r := openSettingsDevices(t, door, timeNow())
	return a, r
}

// T15 / D12: Settings, Devices, shows a row called `remote control` that reads
// off until the person turns it on, and one press of enter turns it on and
// writes it, so another computer can open this one's chats without a restart.
func TestTheDevicesTabOffersRemoteControlOffUntilItIsTurnedOn(t *testing.T) {
	a, r := remoteControlRowApp(t, devicesFake())
	if config.RemoteControlAt(a.profileDir) {
		t.Fatal("remote control read on in a fresh profile")
	}
	r.press("end")
	item, ok := a.sheet.current()
	if !ok || item.row.Key != config.KeyRemoteControl {
		t.Fatalf("the cursor at the end of the Devices tab is on %+v, want the remote control row", item)
	}
	got := plain(frame(a))
	if !strings.Contains(got, "remote control") {
		t.Fatalf("the Devices tab does not draw the row:\n%s", got)
	}
	if !strings.Contains(got, "off") {
		t.Fatalf("the row does not say it is off:\n%s", got)
	}
	if keys := a.sheet.devicesKeysLine(); !strings.Contains(keys, "enter change") {
		t.Fatalf("the legend on the row does not offer enter: %q", keys)
	}

	r.press("enter")
	if !config.RemoteControlAt(a.profileDir) {
		t.Fatalf("enter on the row did not turn remote control on:\n%s", plain(frame(a)))
	}
	if got := plain(frame(a)); !strings.Contains(got, "on") {
		t.Fatalf("the row does not say it is on after the press:\n%s", got)
	}

	r.press("enter")
	if config.RemoteControlAt(a.profileDir) {
		t.Fatal("a second enter did not turn remote control back off")
	}
}

// T15: the computers keep the top of the tab. The row stands after the add row,
// so the cursor that starts on this computer starts there still.
func TestTheRemoteControlRowStandsAfterTheComputersAndTheAddRow(t *testing.T) {
	a, _ := remoteControlRowApp(t, devicesFake())
	items := a.sheet.items
	if len(items) < 5 {
		t.Fatalf("the Devices tab has %d items, want three computers, the add row and the switch", len(items))
	}
	if items[0].dev == nil || items[0].dev.index != 0 {
		t.Fatalf("the first item is not the first computer: %+v", items[0])
	}
	last := items[len(items)-1]
	if last.row.Key != config.KeyRemoteControl {
		t.Fatalf("the last item is %+v, want the remote control row", last)
	}
	before := items[len(items)-2]
	if before.dev == nil || !before.dev.add {
		t.Fatalf("the item before the switch is %+v, want the add row", before)
	}
}

// T15 / the capability law: when devices cannot work in this session the tab
// has no computers and no switch for reaching them, because a switch that
// cannot do anything is absent and not broken.
func TestTheRemoteControlRowIsAbsentWhenDevicesCannotWork(t *testing.T) {
	a, _ := remoteControlRowApp(t, nil)
	for _, item := range a.sheet.items {
		if item.row.Key == config.KeyRemoteControl {
			t.Fatalf("the Devices tab drew the remote control row with no way to reach another computer:\n%s", plain(frame(a)))
		}
	}
}

// T15: the row is findable by the words a person would type, from any tab.
func TestSettingsSearchFindsTheRemoteControlRow(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_REMOTE_CONTROL", "")
	a, r := approveApp(t, devicesFake())
	a.openSettings()
	for _, ch := range "remote control" {
		r.press(string(ch))
	}
	got := plain(frame(a))
	if !strings.Contains(got, "remote control") {
		t.Fatalf("searching for remote control found nothing:\n%s", got)
	}
	if !a.sheet.onDevices() {
		t.Fatalf("searching for remote control landed on %s, want %s", settingTabs[a.sheet.tab], tabDevices)
	}
}

// T15: a search that matches both the switch and the computers draws the
// Devices heading once and keeps the computers in the same group.
func TestASearchThatFindsRemoteControlAndTheComputersHasOneDevicesHeading(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_REMOTE_CONTROL", "")
	a, r := approveApp(t, devicesFake())
	a.openSettings()
	r.press("ctrl+u")
	r.typeText("devices")
	heads, row := 0, false
	for _, item := range a.sheet.items {
		if item.heading() && item.head == tabDevices {
			heads++
		}
		if item.row.Key == config.KeyRemoteControl {
			row = true
		}
	}
	if heads != 1 {
		t.Fatalf("the search drew the %s heading %d times, want once", tabDevices, heads)
	}
	if !row {
		t.Fatal("the search for devices did not offer the remote control row")
	}
}
