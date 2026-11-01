package session

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/manual"
)

// THE MANUAL TOOL READS THE APP'S PAGES TOO, named as the app's: a search
// reaches one, a page read by its prefixed name returns it, and once the app
// clears them the tool is back to the chat's pages alone.
func TestTheManualToolReachesAnAppPage(t *testing.T) {
	t.Cleanup(func() { _ = manual.SetAppPages(nil) })
	if err := manual.SetAppPages([]manual.AppPage{{Name: "inbox", Markdown: "# The inbox\n\n## Snoozing an inbox card\n\nSwipe a card left to snooze it until tomorrow morning."}}); err != nil {
		t.Fatal(err)
	}
	found := askManualOf(t, &Agent{}, map[string]string{"query": "snooze an inbox card"})
	if !strings.Contains(found, "[workaf/inbox · ") || !strings.Contains(found, "tomorrow morning") {
		t.Fatalf("the search answered:\n%s", found)
	}
	if page := askManualOf(t, &Agent{}, map[string]string{"page": "workaf/inbox"}); !strings.Contains(page, "tomorrow morning") {
		t.Fatalf("the page read answered:\n%s", page)
	}
	if err := manual.SetAppPages(nil); err != nil {
		t.Fatal(err)
	}
	if page := askManualOf(t, &Agent{}, map[string]string{"page": "workaf/inbox"}); !strings.Contains(page, "There is no manual page named workaf/inbox") {
		t.Fatalf("a cleared page still answered:\n%s", page)
	}
}
