package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/manual"
)

// THE MANUAL IS A BUILD ARTEFACT, NOT A DOCUMENT SOMEBODY REMEMBERS TO UPDATE.
//
// The chat answers "what can you do?" out of internal/manual's chat/ pages, and
// the whole value of that arrangement is that the answer cannot drift from the
// program. A command added to the table without a line in a page would make the
// chat confidently deny having a feature it ships — the exact failure the pages
// exist to prevent, and one nobody would notice until a user hit it. So the
// table is checked against the pages here, and the build fails first.
//
// The bar is deliberately low — the page must MENTION the command, not describe
// it well. A test that graded prose would be a test nobody could keep green;
// this one only insists that whoever added the feature also opened the manual.
func TestTheManualMentionsEveryCommandTheTableOffers(t *testing.T) {
	for _, c := range commands {
		if !manual.Chat().Mentions("/" + c.name) {
			t.Errorf("no chat manual page mentions /%s — add it to internal/manual/chat/", c.name)
		}
		// Aliases are checked too, because an alias is a word a person will
		// type and then ask about. A manual that knows /clear but not /reset
		// answers "I have no such command" to somebody looking at a row that
		// names it.
		for _, word := range c.alias {
			if !manual.Chat().Mentions("/" + word) {
				t.Errorf("no chat manual page mentions /%s (an alias of /%s)", word, c.name)
			}
		}
	}
}

// The chat's corpus must not answer out of the resident's vocabulary. They are
// two products in one binary (internal/manual's package comment says why), and
// the failure this guards against is subtle: a page that drifts into resident
// words would have the chat telling a person about places it does not have and
// work it cannot do.
func TestTheChatManualDoesNotSpeakOfTheResident(t *testing.T) {
	// Each of these is a v1 resident concept with no counterpart in the chat.
	// A page needing one of these words is a page written about the wrong
	// product.
	foreign := []string{
		"alt+1", "alt+2", "alt+3",
		"the board", "the self page", "standing watch",
		"resident employee", "front desk",
	}
	for _, name := range manual.Chat().Pages() {
		text, ok := manual.Chat().Page(name)
		if !ok {
			t.Fatalf("page %s vanished between listing and reading", name)
		}
		lower := strings.ToLower(text)
		for _, word := range foreign {
			if strings.Contains(lower, word) {
				t.Errorf("chat manual page %q uses the resident's word %q — the chat has no such thing", name, word)
			}
		}
	}
}

// A page nobody can reach is a page that will rot. Every page has to be found
// by asking for it in the words it is titled with, which is also the cheapest
// possible check that the index is actually built and searchable.
func TestEveryChatManualPageIsReachableByItsOwnName(t *testing.T) {
	pages := manual.Chat().Pages()
	if len(pages) == 0 {
		t.Fatal("the chat manual has no pages at all")
	}
	for _, name := range pages {
		query := strings.ReplaceAll(name, "-", " ")
		found := manual.Chat().Search(query, 8)
		if len(found) == 0 {
			t.Errorf("searching the chat manual for %q found nothing", query)
			continue
		}
		var reached bool
		for _, section := range found {
			if section.Page == name {
				reached = true
				break
			}
		}
		if !reached {
			t.Errorf("searching the chat manual for %q never reaches page %s", query, name)
		}
	}
}
