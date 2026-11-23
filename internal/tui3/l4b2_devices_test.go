package tui3

import (
	"context"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
	"strings"
	"testing"
	"time"
)

// L4-H7a, L4-H7b, L4-H7c: Removal requires one explicit answer and consumes every cancellation key.
func TestL4B2RemovalQuestion(t *testing.T) {
	for _, key := range []string{"y", "esc", "n", "down", "r", "enter", "ctrl+c"} {
		t.Run(key, func(t *testing.T) {
			door := &fakeApprovals{}
			a, _ := approveApp(t, door)
			c := &deviceCard{loaded: true, cursor: 1, rowsOf: []DeviceRow{{ID: "dev_1111", Name: "desk-b"}, {ID: "dev_2222", Name: "desk-b"}}}
			a.pair.open, a.pair.card = true, c
			cmd, close := c.key(a, "r")
			if cmd != nil || close || door.did() != "" {
				t.Fatal("r changed the device")
			}
			text := plain(strings.Join(c.rows(160, time.Now(), a.pal), "\n"))
			if !strings.Contains(text, "remove desk-b #2222? it must pair again to come back  ·  y removes  ·  any other key keeps it") {
				t.Fatalf("question: %s", text)
			}
			cmd, close = c.key(a, key)
			if close || c.cursor != 1 {
				t.Fatal("answer left the selected row")
			}
			if key == "y" {
				if cmd == nil {
					t.Fatal("yes did not remove")
				}
				m := cmd().(revokedMsg)
				m.land(a)
				if door.did() != "revoke:dev_2222" || c.line != fmt.Sprintf(devicesRevoked, "desk-b") {
					t.Fatalf("result: %s %s", door.did(), c.line)
				}
			} else if cmd != nil || door.did() != "" || c.line != "" {
				t.Fatal("cancellation did more than keep the device")
			}
		})
	}
}

// L4-H8a: Approval platforms and Home presence use the terminal's selected repertoire.
func TestL4B2DeviceGlyphs(t *testing.T) {
	for _, tier := range []tokens.GlyphSet{tokens.Plain, tokens.NerdFont, tokens.ASCII} {
		a := newTestApp(&fakeAgent{model: "m"})
		a.pal.icons = tier
		req := tower(time.Unix(1_000_000, 0))
		for _, platform := range []string{"darwin", "linux", "windows", "ios", "android", "other"} {
			req.Platform = platform
			c := &approveCard{req: &req}
			text := plain(strings.Join(c.rows(160, req.RequestedAt, a.pal), "\n"))
			if strings.Contains(text, "$") {
				t.Fatalf("platform drawn as money: %s", text)
			}
			if !strings.Contains(text, a.pal.glyph(platformOf(platform).icon)+" tower") {
				t.Fatalf("wrong tier %v: %s", tier, text)
			}
		}
		a.devRow.devices = []chatlist.Device{{ID: "self", Name: "This computer", Self: true}, {ID: "peer", Name: "desk"}}
		a.dirFeed = &fakeFeed{state: dirwatch.State{Up: true, Online: []string{"peer"}}}
		text := plain(strings.Join(drawDevicesBand(a, bandContext{width: 160, pal: a.pal}), "\n"))
		if !strings.Contains(text, tier.Glyph(tokens.GStepDone)+" desk") {
			t.Fatalf("presence tier: %s", text)
		}
	}
}

// L4-M9b, L4-M10b, L4-M10c: Both lists share service and row words, including this device and the Add row.
func TestL4B2DeviceListsAgree(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	door := &fakeApprovals{devices: []DeviceRow{{ID: "self", Name: "laptop", Platform: "darwin", Self: true}, {ID: "other", Name: "desk", Platform: "linux", LastSeen: now.Add(-time.Hour)}}}
	a, r := approveApp(t, door)
	a.syncService = "http://127.0.0.1:8901"
	a.clock = func() time.Time { return now }
	r.run(a.openDevices())
	r.until("devices", func() bool { c, ok := a.pair.card.(*deviceCard); return ok && c.loaded })
	c := a.pair.card.(*deviceCard)
	listFrame := plain(frame(a))
	text := plain(strings.Join(c.rows(160, now, a.pal), "\n"))
	if !strings.Contains(text, "sync service: 127.0.0.1:8901 (your own)") || !strings.Contains(text, addDeviceWord) {
		t.Fatal(text)
	}
	a.pair.close()
	a.openSettings()
	walkToDevicesTab(t, a, r)
	r.until("settings devices", func() bool { return a.sheet.dev != nil && a.sheet.dev.loaded })
	for _, row := range c.rowsOf {
		words := plain(c.rowText(row, now, a.pal))
		if !strings.Contains(listFrame, words) || !strings.Contains(plain(frame(a)), words) {
			t.Fatalf("device row differs between the actual screens: %q\n%s", words, plain(frame(a)))
		}
	}
	if !strings.Contains(plain(frame(a)), "sync service: 127.0.0.1:8901 (your own)") {
		t.Fatal("settings omitted the service")
	}
}

// L4-M10b: A solo device can be renamed, then the Add row starts the same pairing offer.
func TestL4B2SoloDeviceAddRow(t *testing.T) {
	a, r := approveApp(t, &fakeApprovals{devices: []DeviceRow{{ID: "self", Name: "here", Self: true}}})
	r.run(a.openDevices())
	r.until("solo device", func() bool { c, ok := a.pair.card.(*deviceCard); return ok && c.loaded })
	c := a.pair.card.(*deviceCard)
	r.press("n")
	if c.naming == nil {
		t.Fatal("this device cannot be named")
	}
	r.press("esc", "down")
	if _, ok := c.pick(); ok || !strings.Contains(c.hint(), "enter add") {
		t.Fatalf("add row not selected: %s", c.hint())
	}
	a.pairing = &fakePairing{offer: func(ctx context.Context, ui pair.OfferUI) (string, error) { <-ctx.Done(); return "", ctx.Err() }}
	r.press("enter")
	if a.pair.run == nil || a.pair.card != nil {
		t.Fatal("Add did not open /pair")
	}
}

// L4-H7a, L4-H7b, L4-H7c: Settings consumes the confirmation key before navigation, search or quitting.
func TestL4B2SettingsRemovalKeys(t *testing.T) {
	for _, answer := range []string{"esc", "right", "down", "n", "ctrl+c", "y"} {
		t.Run(answer, func(t *testing.T) {
			door := devicesFake()
			a, r := openSettingsDevices(t, door, time.Now())
			r.press("down")
			c := a.sheet.dev
			row, ok := c.pick()
			if !ok || row.Self {
				t.Fatal("no removable row")
			}
			cursor, tab := a.sheet.cursor, a.sheet.tab
			r.press("r")
			if c.removing == nil || door.did() != "" {
				t.Fatal("r did not ask")
			}
			r.press(answer)
			if answer == "y" {
				r.until("removal", func() bool { return strings.Contains(c.line, "was removed") })
				if door.did() != "revoke:"+row.ID {
					t.Fatal(door.did())
				}
			} else if door.did() != "" || !a.at(pageSettings) || a.sheet.cursor != cursor || a.sheet.tab != tab || !a.sheet.query.empty() || a.quitting {
				t.Fatal("cancellation performed another action")
			}
		})
	}
}
