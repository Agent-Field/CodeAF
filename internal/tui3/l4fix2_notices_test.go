package tui3

import (
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncwords"
	"strings"
	"testing"
)

// L4-B13, L4-B16: An older engine's wording selects the same recovery door and
// outage ledger, rather than displaying duplicate or terminal-only guidance.
func TestL4Fix2LegacyNotices(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.syncService = "https://sync.example"
	a.apply(session.Event{Kind: session.EventNotice, Text: "this chat is not syncing because other machines are unreachable; it keeps working here"})
	a.note(syncwords.Outage(a.syncService))
	notes := strings.Join(noteTexts(a), "\n")
	if strings.Count(notes, "this chat keeps working here") != 1 || strings.Contains(notes, "other machines") {
		t.Fatal(notes)
	}
	a.outsideDesk.Say("this device was removed by another of your devices, so this chat stays here only — run `codeaf pair` to bring it back")
	a.saidNotices()
	notes = strings.Join(noteTexts(a), "\n")
	if !strings.Contains(notes, "/pair brings it back") || strings.Contains(notes, "run `codeaf pair`") {
		t.Fatal(notes)
	}
	if a.homeAccountNotice != removedPairNotice {
		t.Fatal(a.homeAccountNotice)
	}
}
