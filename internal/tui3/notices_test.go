package tui3

import (
	"strings"
	"testing"
)

func TestSayOnceSpeaksASentenceOncePerRun(t *testing.T) {
	n := NewNotices()
	n.SayOnce("removed")
	n.SayOnce("removed")
	n.Say("removed")
	n.SayOnce("other")
	if got := n.take(); len(got) != 3 {
		t.Fatalf("desk = %v, want removed, removed (plain Say), other", got)
	}
}

// TestC3RemovedComputerIsToldOnHomeOnce proves C3 at the surface's notice
// door, including a launch-time refusal queued before home starts listening.
func TestC3RemovedComputerIsToldOnHomeOnce(t *testing.T) {
	const line = "this device was removed by another of your devices, so this chat stays here only — /pair brings it back"
	desk := NewNotices()
	desk.SayHomeOnce(line)
	a, _ := homeTabsFixture(t)
	a.useNotices(desk)
	a.route(noticesMsg{})
	if !strings.Contains(homeText(a), "removed by another of your devices — pair again to sync") {
		t.Fatalf("removal hidden on home:\n%s", homeText(a))
	}
	desk.SayHomeOnce(line)
	a.route(noticesMsg{})
	count := 0
	for _, entry := range a.entries {
		if entry.kind == entryNote && entry.text == line {
			count++
		}
	}
	if count != 1 || a.echoHome {
		t.Fatalf("removal said %d times, echo flag %v", count, a.echoHome)
	}
}
