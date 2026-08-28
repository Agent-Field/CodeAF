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
	//
	// ── `alt+1`, `alt+2` AND `alt+3` WERE ON THIS LIST AND WERE TAKEN OFF ─────
	//
	// They were banned because they were the RESIDENT's place keys and the chat
	// had no such thing — so a chat page spelling one could only be a page that
	// had drifted into the wrong product's vocabulary. The places wave (pages.go)
	// binds `alt+1` … `alt+7` in the chat itself, as the jump to each of the seven
	// places, which means the ban stopped being a fact about this product and
	// became a stale rule forcing the manual to lie: CLAUDE.md requires every new
	// key to be documented, and the only way to document these three while the
	// ban stood was to describe the class and quietly omit its first three
	// members — which is exactly the "named in a section that denies it" failure
	// the manual law warns about.
	//
	// So the three strings are gone and NOTHING ELSE IS. The phrases left are the
	// ones that are still resident-only, and `alt+4` … `alt+7` were never on the
	// list in the first place. If a later wave gives the chat a board or a self
	// page, the same reasoning applies to that word and to no other.
	foreign := []string{
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
