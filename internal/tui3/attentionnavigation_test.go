package tui3

import (
	"github.com/Agent-Field/aforge-v2/internal/session"
	"testing"
)

func TestPermissionQuestionAllowsClosingTheViewWithoutAnswering(t *testing.T) {
	agent, a := wired([]session.Event{toolBegin("bash", "pwd"), consentEvent(7, "bash", "pwd", "default")})
	a.file = "/tmp/permission-navigation.jsonl"
	typeLine(t, a, "permission")
	drive(t, a, key("ctrl+w"))
	if !a.closingTab() || !a.asking() {
		t.Fatal("permission swallowed close or was answered")
	}
	drive(t, a, key("esc"))
	if a.closingTab() || !a.asking() {
		t.Fatal("cancel changed the pending permission")
	}
	if len(agent.answers) != 0 {
		t.Fatal("navigation answered the permission")
	}
}
