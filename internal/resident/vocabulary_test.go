package resident

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/craft"

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

	// The word "craft" was worst here, because this is the line a person reads
	// the moment learned know-how takes over a job they asked for in their own
	// words — and the only escape from it used to require saying that word.
	reconciler := &Reconciler{}
	assertPlain(t, "the learned-way receipt",
		craftCompileReceipt(plainWorkflow()),
		reconciler.craftUseReceipt(plainWorkflow(), false, 0),
		reconciler.craftUseReceipt(plainWorkflow(), false, 0.38),
		reconciler.craftUseReceipt(plainWorkflow(), true, 0),
		craftSetAsideLine(),
		craftIntent(plainWorkflow(), map[string]string{"topic": "Q3"}))

	// The verbs a person aims at what has been learned: every sentence they get
	// back — the refusals included — is composed in this package and goes into
	// the thread verbatim, so the filter covers them too.
	assertPlain(t, "the verb receipts",
		craftRefusal("nothing in this window keeps the ways I have learned to work, so there is nothing to run").receipt,
		craftParamQuestion(plainWorkflow(), "investor-update",
			errors.New("craft investor-update: missing required params: topic, tone")),
		craftNamedIntent(plainWorkflow(), ""),
		skillRetiredLine(store.Fact{Artifact: "/home/skills/imgshrink"}),
		"⚒ release-notes"+craftDigestBecause(store.CraftForged{Because: "write the 0.4 notes"}),
		"⚒ release-notes"+craftDigestBecause(store.CraftForged{Refined: true}))
}

func plainWorkflow() *craft.Workflow {
	return &craft.Workflow{
		Name: "investor-update", Commit: "a1b2c3d4e5",
		Steps: []craft.Step{{ID: "gather"}, {ID: "draft"}, {ID: "check"}},
	}
}

// TestTheReceiptQuotesWhatTheLastRunActuallyCost is the trust half. The receipt
// named steps and a ceiling — both promises — while the one number that would
// have proved the relationship compounds was already on disk beside the
// survival record and quoted nowhere.
func TestTheReceiptQuotesWhatTheLastRunActuallyCost(t *testing.T) {
	reconciler := &Reconciler{}
	if line := reconciler.craftUseReceipt(plainWorkflow(), false, 0.38); !strings.Contains(line, "last time $0.38") {
		t.Fatalf("the receipt did not carry the prior run's cost: %q", line)
	}
	// No prior run recorded is no clause, never a zero. A record written before
	// the cost was measured reads as exactly that.
	if line := reconciler.craftUseReceipt(plainWorkflow(), false, 0); strings.Contains(line, "last time") {
		t.Fatalf("a receipt invented a prior run: %q", line)
	}
	// A first run has no last time to quote and says what it does have.
	first := reconciler.craftUseReceipt(plainWorkflow(), true, 0)
	if !strings.Contains(first, "first time working this way") || strings.Contains(first, "last time") {
		t.Fatalf("the first-run receipt = %q", first)
	}
	if strings.Contains(first, "\n") {
		t.Fatalf("the receipt grew past one line: %q", first)
	}
}
