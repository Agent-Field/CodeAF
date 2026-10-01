package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

// THE DEVICES ROW AND THE RESCUE PROMPT STAND ON HOME with no row under the
// cursor (an empty home, or a chat that lives on another device).
func TestDevicesRowAndRescueStandOnAnEmptyHome(t *testing.T) {
	a, _ := devicesRig("dev_spark")
	a.tookMachines(homeMachinesMsg{rows: []chatlist.Row{leftOffRow("a", "dumb", chatlist.Off, time.Hour)}})
	a.openHome()
	got := strings.Join(strings.Fields(homeText(a)), " ")
	for _, want := range []string{"● This Mac ● spark ○ dumb (offline)", "Continue where you left off on dumb?"} {
		if !strings.Contains(got, want) {
			t.Fatalf("empty home lacks %q:\n%s", want, homeText(a))
		}
	}
}
