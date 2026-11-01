package manual

import (
	"strings"
	"testing"
)

// THE APP'S PAGES JOIN THE CHAT'S MANUAL UNDER THEIR OWN PREFIX. A search
// reaches an app page by its words, a page read reaches it by its prefixed
// name, a later set replaces the first whole, and the chat corpus itself is
// never changed by either.
func TestAppPagesJoinTheChatManualUnderTheirPrefix(t *testing.T) {
	t.Cleanup(func() { _, _ = SetAppPages(nil) })
	names, err := SetAppPages([]AppPage{{Name: "inbox", Markdown: "# The inbox\n\n## Clearing the inbox\n\nSwipe a card left to snooze it until tomorrow morning."}})
	if err != nil || len(names) != 1 || names[0] != "workaf/inbox" {
		t.Fatalf("the set answered %v %v", names, err)
	}
	found := WithApp().Search("how do I snooze a card in the inbox", DefaultResults)
	if len(found) == 0 || found[0].Page != "workaf/inbox" {
		t.Fatalf("the search found %+v", found)
	}
	if text, ok := WithApp().Page("workaf/inbox"); !ok || !strings.Contains(text, "snooze") {
		t.Fatalf("the page read answered %q %v", text, ok)
	}
	if _, ok := Chat().Page("workaf/inbox"); ok {
		t.Fatal("the chat corpus itself took the app's page")
	}
	if len(WithApp().Search("rewind undo", DefaultResults)) == 0 {
		t.Fatal("the chat's own pages are gone from the joined manual")
	}
	if _, err := SetAppPages([]AppPage{{Name: "spaces", Markdown: "# Spaces\n\nA space holds a project's chats."}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := WithApp().Page("workaf/inbox"); ok {
		t.Fatal("a second set kept the first set's page")
	}
	if _, err := SetAppPages(nil); err != nil || WithApp() != Chat() {
		t.Fatalf("clearing the pages answered %v and left a joined manual", err)
	}
}

// What the app may register is bounded: a name is a short slug, a page and the
// set have a size, and two pages cannot share a name.
func TestAppPagesAreBounded(t *testing.T) {
	t.Cleanup(func() { _, _ = SetAppPages(nil) })
	for _, bad := range [][]AppPage{
		{{Name: "Bad Name", Markdown: "# x"}},
		{{Name: "", Markdown: "# x"}},
		{{Name: "blank", Markdown: "  "}},
		{{Name: "big", Markdown: strings.Repeat("x", MaxAppPageBytes+1)}},
		{{Name: "twice", Markdown: "# a"}, {Name: "twice", Markdown: "# b"}},
		make([]AppPage, MaxAppPages+1),
	} {
		if _, err := SetAppPages(bad); err == nil {
			t.Fatalf("%d pages starting %+v were accepted", len(bad), bad[0])
		}
	}
}
