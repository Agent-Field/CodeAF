package tui3

import (
	"strings"
	"testing"
)

// L4-H10a, L4-H10b: A refused Enter leaves the complete draft in the message box and starts no turn.
func TestL4B2RefusedSendKeepsDraft(t *testing.T) {
	for _, sentence := range []string{
		"not sent — still reconnecting to localhost; enter sends it once the link is back",
		"not sent — the connection to desk-b is gone; your words are still in the box. Run the same command to pick the conversation back up",
	} {
		agent := &fakeAgent{model: "m"}
		a := newTestApp(agent)
		a.link.SendRefusal = func() string { return sentence }
		a.input.setText("  my words\nwith another line  ")
		a.enterLine()
		if a.input.String() != "  my words\nwith another line  " || len(agent.sent) != 0 || a.turn != 0 {
			t.Fatalf("draft spent: %q sent=%v", a.input.String(), agent.sent)
		}
		if !strings.Contains(updateNotes(a), sentence) {
			t.Fatalf("missing refusal: %s", updateNotes(a))
		}
	}
}
