package catalog

import "testing"

// Identity is what an accumulated history is keyed by, and it answers a
// different question from Concrete: not "which spelling will another catalog
// accept" but "are these two spellings one model". Both the alias target and the
// canonical slug say yes, so both are applied.
func TestIdentityCollapsesEverySpellingOfOneModel(t *testing.T) {
	c := fixtureCatalog(t, t.TempDir(), Options{})
	const dated = "anthropic/claude-opus-5-20260723"
	for _, spelling := range []string{
		"anthropic/claude-opus-5", "~anthropic/claude-opus-5", " ANTHROPIC/claude-opus-5 ",
	} {
		if got := c.Identity(spelling); got != dated {
			t.Errorf("Identity(%q) = %q, want the one model behind it (%q)", spelling, got, dated)
		}
	}
	// A floating alias and its target are one model too.
	if got, want := c.Identity("~x-ai/grok-latest"), c.Identity("x-ai/grok-4.5"); got != want {
		t.Errorf("an alias and its target got two identities: %q and %q", got, want)
	}
	// A model this catalog does not carry is forwarded as written; nothing is
	// invented for a name nobody published.
	if got := c.Identity("nobody/nothing"); got != "nobody/nothing" {
		t.Errorf("Identity of an unknown model = %q, want it verbatim", got)
	}
}

// Identity never waits. It is asked on the launch path, before anything has been
// planned, and a catalog that has not resolved yet is one more way of not
// knowing rather than a fetch in front of the first frame.
func TestIdentityNeverWaitsOnAColdCatalog(t *testing.T) {
	unresolved := &Catalog{}
	if got := unresolved.Identity("~anthropic/claude-opus-5"); got != "anthropic/claude-opus-5" {
		t.Errorf("a cold catalog answered %q, want the normalised id as written", got)
	}
	var absent *Catalog
	if got := absent.Identity("anthropic/claude-opus-5"); got != "anthropic/claude-opus-5" {
		t.Errorf("no catalog at all answered %q", got)
	}
}
