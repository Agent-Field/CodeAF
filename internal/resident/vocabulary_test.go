package resident

import (
	"regexp"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// jargonWords is docs/JOURNEY.md's design filter as something a test can run:
// internally we may speak of leaves, crafts and charters; the user only ever
// experiences steps, the way we already do this, and a standing rule. Every
// receipt this package composes goes into the thread verbatim, so these are
// exactly the strings the filter has to cover.
var jargonWords = regexp.MustCompile(`(?i)\b(worker|workers|charter|charters|rail|rails|leaf|leaves|graph|graphs|firing|firings|craft|crafts|splice|splices|spliced|node|nodes)\b`)

func assertPlain(t *testing.T, where string, surfaces ...string) {
	t.Helper()
	for _, surface := range surfaces {
		if found := jargonWords.FindString(surface); found != "" {
			t.Errorf("%s speaks the implementation's language (%q): %q", where, found, surface)
		}
	}
}

// TestEveryComposedReceiptSpeaksPlainly walks the receipts a person actually
// reads: what a job was understood to be, what a pause or a resume or a
// cancellation did, what a redirection reached, and what a learned way of
// working is about to run. Each of these was a Go literal carrying a word out
// of the store schema.
func TestEveryComposedReceiptSpeaksPlainly(t *testing.T) {
	assertPlain(t, "the compile receipt",
		compileReceipt("benchmark the parser", []string{"main is the baseline"}, ""))

	assertPlain(t, "the redirect receipt",
		redirectReceipt(store.Node{ID: "api", Title: "v1 API client"},
			Redirection{Amended: 2, Added: 1}, 1, true))
	assertPlain(t, "the expedite receipt",
		expediteReceipt(store.Node{ID: "api", Title: "v1 API client"}, true,
			Redirection{Dropped: 1, Amended: 1}, 2, 1, true))

	assertPlain(t, "a learning moment",
		forgedCraftMoment("release-notes", false).headline,
		forgedCraftMoment("release-notes", true).headline,
		forgedSkillMoment("changelog-diff").headline)

}
