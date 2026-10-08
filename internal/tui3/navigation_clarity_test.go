package tui3

import "testing"

// Display labels and visual order must not migrate saved visit keys or learned keys.
func TestNavigationClarityPreservesExistingIdentities(t *testing.T) {
	for _, c := range []struct {
		id         page
		label, key string
		digit      int
	}{
		{pageTeams, "AI teams", "teams", 2},
		{pageTasks, "activity", "tasks", 4},
		{pageChats, "chats", "chats", 3},
	} {
		if c.id.word() != c.label || c.id.lookKey() != c.key || placeDigitOf(c.id) != c.digit {
			t.Fatalf("destination %v changed compatibility identity: %q %q %d", c.id, c.id.word(), c.id.lookKey(), placeDigitOf(c.id))
		}
	}
	shown := barPages(pageHome, false)
	want := []page{pageHome, pageChats, pageFactory, pageTeams, pageTasks, pageMemory, pageSpend, pageSettings}
	if len(shown) != len(want) {
		t.Fatalf("main destinations: %v", shown)
	}
	for i := range want {
		if shown[i] != want[i] {
			t.Fatalf("main destinations: %v", shown)
		}
	}
}
