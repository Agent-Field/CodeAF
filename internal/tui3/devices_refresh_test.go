package tui3

import (
	"errors"
	"strings"
	"testing"
)

// R16/R30 keep a completed removal visible when a feed-triggered list read
// fails before or after its response. A read timeout does not restore access
// or erase the successful operation; both outcomes must remain visible.
func TestR16R30DeviceRemovalSurvivesDelayedListFailure(t *testing.T) {
	for _, before := range []bool{false, true} {
		t.Run(map[bool]string{false: "after", true: "before"}[before], func(t *testing.T) {
			a := newTestApp(&fakeAgent{model: "test/model"})
			rows := []DeviceRow{{ID: "self", Name: "here", Self: true}, {ID: "other", Name: "spark", Online: true}}
			card := &deviceCard{rowsOf: rows, loaded: true, cursor: 1}
			a.showCard(card)
			failed := devicesMsg{card: card, err: errors.New("the device list did not finish loading")}
			if before {
				failed.land(a)
			}
			revokedMsg{card: card, row: rows[1]}.land(a)
			if !before {
				failed.land(a)
			}
			shown := plain(frame(a))
			for _, want := range []string{"spark was removed", "the device list did not finish loading"} {
				if !strings.Contains(shown, want) {
					t.Fatalf("R16/R30 lost %q after committed removal:\n%s", want, shown)
				}
			}
			if !card.rowsOf[1].Revoked || card.rowsOf[1].Online {
				t.Fatal("R16 failed list read restored the removed device")
			}
			devicesMsg{card: card, rows: rows}.land(a)
			shown = plain(frame(a))
			if !strings.Contains(shown, "spark was removed") || strings.Contains(shown, "did not finish loading") {
				t.Fatalf("R30 successful refresh did not clear only its own error:\n%s", shown)
			}
		})
	}
}
