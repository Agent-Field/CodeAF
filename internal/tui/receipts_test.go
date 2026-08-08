package tui

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/charmbracelet/x/ansi"
)

// A receipt says what it says. The command stamp used to overwrite every one of
// them with the same sentence, so a refusal, a compile note and a reading all
// folded into "reading + N assumptions" and none was worth opening.
func TestReceiptsFoldToTheirOwnFirstLine(t *testing.T) {
	for _, probe := range []struct {
		body    string
		command int64
		want    string
	}{
		{"Read the compiled request.\nAssumed: no schema changes.", 7,
			"Read the compiled request. · 1 assumption"},
		{"Read the compiled request.\nAssumed: one.\nAssumed: two.", 7,
			"Read the compiled request. · 2 assumptions"},
		{"I couldn't apply that request: the job had already finished.", 9,
			"I couldn't apply that request: the job had already finished."},
		{"cancelled — 2 leaves cancelled", 11, "cancelled — 2 leaves cancelled"},
		{"Background sync complete.\nThis is secondary plumbing.", 0, "Background sync complete."},
		{"", 3, "update"},
	} {
		got := receiptSummary(store.Message{Body: probe.body, CommandSeq: probe.command})
		if got != probe.want {
			t.Fatalf("receipt %q folded to %q, want %q", probe.body, got, probe.want)
		}
	}

	// And it reaches the thread that way, clamped to the pane, at dock width.
	model := New(&fakeBackend{}, "receipts")
	model.setSize(60, 26)
	model.messages = []store.Message{{
		Seq: 3, SessionID: "receipts", Role: store.RoleSystem, CommandSeq: 9,
		Body: "I couldn't apply that request: the job had already finished.",
	}}
	model.threadGen++
	plain := ansi.Strip(model.renderMessages())
	if !strings.Contains(plain, "I couldn't apply that request") {
		t.Fatalf("the refusal is not readable in the thread:\n%s", plain)
	}
	if strings.Contains(plain, "reading + ") {
		t.Fatalf("the fixed label survived:\n%s", plain)
	}
	assertFitsWidth(t, model.View(), 60, "refusal receipt")
}
