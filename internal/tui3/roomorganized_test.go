package tui3

import (
	"strings"
	"testing"
)

// AN ORGANIZED ROOM DROPS THE LEGEND'S `room ·` SPELLING. At the e2e suite's
// 120×40 with the rail open, [app.roomOrganized] is true: the focus header
// carries `esc/← main` (roomBackWord) and the legend's left end is empty
// (render.go's [app.legendLeftSpan]). A needle waiting for `room ·` on that
// frame never fires — which is why roomfeed waits for roomBackWord instead.
func TestAnOrganizedRoomPutsTheWayBackOnTheHeaderNotTheLegend(t *testing.T) {
	a, _, _ := roomApp(t)
	a.width, a.height = 120, 40
	a.touch()
	clickRail(t, a, 0)
	if !a.roomOpen() {
		t.Fatal("the rail click did not open the node's room")
	}
	if !a.roomOrganized() {
		t.Fatal("120×40 with the rail open must be the organized layout")
	}
	legend := plain(a.legend(a.width))
	if strings.Contains(legend, "room ·") {
		t.Fatalf("an organized room still put room · on the legend:\n%s", legend)
	}
	screen := plain(frame(a))
	if !strings.Contains(screen, roomBackWord) {
		t.Fatalf("an organized room lost its way back (%q):\n%s", roomBackWord, screen)
	}

	// Compact height still uses the legend spelling the suite used to wait for.
	a.height = 24
	a.touch()
	if a.roomOrganized() {
		t.Fatal("24 rows must not organize the room")
	}
	if !strings.Contains(plain(a.legend(a.width)), roomLegendWord) {
		t.Fatalf("a compact room lost the legend way out:\n%s", plain(a.legend(a.width)))
	}
}
