package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// AN APPROVAL ASKED FROM INSIDE A SCRIPT IS ASKED ON THE SCRIPT'S CALL'S ROW.
// The call began before the gate asked, so its row is already under the
// script; the question parks that row, raises the card, and draws no row of
// its own. The script and the batch's other call keep running.
func TestAnApprovalInsideAScriptIsAskedOnTheNestedRow(t *testing.T) {
	a := newWaitingApp(t, 0)
	a.width, a.height = 140, 30
	drive(t, a,
		streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventToolBegin, Tool: "script", CallID: "s", Hint: "script"}},
		streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventToolBegin, Tool: "read", CallID: "r", Hint: "go.mod"}},
		streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventToolBegin, Tool: "bash", CallID: "s-script-1", Parent: "s", Hint: "touch x", Args: `{"command":"touch x"}`}},
		streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventConsentRequest, ID: 7, Tool: "bash", CallID: "s-script-1", Args: `{"command":"touch x"}`, Rule: "bash"}},
	)
	var order []string
	status := map[string]toolState{}
	nested := map[string]bool{}
	for _, e := range a.entries {
		if e.kind == entryTool {
			order = append(order, e.callID)
			status[e.callID], nested[e.callID] = e.status, e.nested
		}
	}
	if got := strings.Join(order, " "); got != "s s-script-1 r" {
		t.Fatalf("rows = %q; want the script, its call under it, then the read, and no row drawn for the question", got)
	}
	if status["s-script-1"] != toolConsent || !nested["s-script-1"] {
		t.Errorf("the nested call's row is status %v, nested %v; want it asking, under its script", status["s-script-1"], nested["s-script-1"])
	}
	if status["s"] == toolConsent || status["r"] == toolConsent {
		t.Errorf("a row other than the asking call is asking: script %v, read %v", status["s"], status["r"])
	}
	head, ok := a.questionHead()
	if !ok || head.question.Subject.CallID != "s-script-1" {
		t.Fatalf("the card is %+v (raised %v); want it about the nested call", head.question.Subject, ok)
	}
	var touch, read = -1, -1
	for i, line := range plainRows(a) {
		if touch < 0 && strings.Contains(line, "touch x") {
			touch = i
		}
		if read < 0 && strings.Contains(line, "go.mod") {
			read = i
		}
	}
	if touch < 0 || read < 0 || touch > read {
		t.Errorf("the asking call is drawn at row %d and the read at %d; want it under the script, above the read:\n%s", touch, read, strings.Join(plainRows(a), "\n"))
	}
}
