package pair

import (
	"strings"
	"testing"
	"time"
)

// L4-M3, L4-B7 stability: The terminal opts into the approver's explanation,
// while the unchanged conversation integration checks the base callback sequence.
func TestL4Fix2WeightOnApprover(t *testing.T) {
	rig := newServeRig(t, func(string, string) bool { return true })
	reach, screen := rig.reach("laptop", rig.latest)
	reach.WeightOnApprover = true
	tunnel, err := reach.Open(bounded(t, 5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer tunnel.Close()
	said := screen.all()
	if len(said) != 3 || said[0] != PairingPreamble(reach.Name) || !strings.HasPrefix(said[1], WaitingLine(reach.Name, "")) || said[2] != PairedLine(reach.Name) {
		t.Fatalf("terminal notices: %q", said)
	}
}
