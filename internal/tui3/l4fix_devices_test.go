package tui3

import (
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
	"strings"
	"testing"
	"time"
)

// L4-R9: Neither device card calls the explicit off setting a service.
func TestL4FixDeviceCardsSyncOff(t *testing.T) {
	a, r := approveApp(t, &fakeApprovals{devices: []DeviceRow{{Name: "laptop", Self: true, Platform: "linux"}}})
	a.syncService = "off"
	r.run(a.openDevices())
	r.until("devices", func() bool { c, ok := a.pair.card.(*deviceCard); return ok && c.loaded })
	if text := plain(frame(a)); strings.Contains(text, "sync service:") {
		t.Fatal(text)
	}
	a.pair.close()
	a.openSettings()
	walkToDevicesTab(t, a, r)
	r.until("settings devices", func() bool { return a.sheet.dev != nil && a.sheet.dev.loaded })
	if text := plain(frame(a)); strings.Contains(text, "sync service:") {
		t.Fatal(text)
	}
}

// L4-R10: The Add selection uses the same cursor slot in every repertoire.
func TestL4FixAddDeviceCursor(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	c := &deviceCard{loaded: true}
	for _, tier := range []tokens.GlyphSet{tokens.ASCII, tokens.Plain, tokens.NerdFont} {
		a.pal.icons = tier
		text := plain(strings.Join(c.rows(120, time.Time{}, a.pal), "\n"))
		if !strings.HasPrefix(text, a.pal.glyph(tokens.GPointer)+" ") {
			t.Fatalf("tier=%v: %s", tier, text)
		}
	}
}
