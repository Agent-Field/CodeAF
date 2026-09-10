package catalog

// The fold a ledger keys on, held to the three rows the live catalog really
// publishes for the model this build ships as its default. The rows are copied
// from the 2026-09-01 listing rather than invented, because the whole bug is
// that a plausible-looking fold answers an id OpenRouter serves nowhere.

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// deepseekRows is the family as the router publishes it: a floating alias whose
// canonical_slug repeats itself and whose alias_target names the servable id,
// that servable id with a dated canonical slug of its own, and the bare undated
// id — which is a DIFFERENT snapshot, four months older.
const deepseekRows = `{"data":[
  {"id":"~deepseek/deepseek-v4-flash-latest","canonical_slug":"~deepseek/deepseek-v4-flash-latest",
   "alias_target":{"name":"DeepSeek V4 Flash 0731","slug":"%s"},
   "architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"0.0000000798","completion":"0.0000001596"}},
  {"id":"deepseek/deepseek-v4-flash-0731","canonical_slug":"deepseek/deepseek-v4-flash-20260731",
   "architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"0.0000000798","completion":"0.0000001596"}},
  {"id":"deepseek/deepseek-v4-flash","canonical_slug":"deepseek/deepseek-v4-flash-20260423",
   "architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"0.0000000798","completion":"0.0000001596"}}
]}`

// deepseekCatalog is that listing with whatever alias target the caller wants
// to see followed.
func deepseekCatalog(t *testing.T, target string) *Catalog {
	t.Helper()
	body := strings.Replace(deepseekRows, "%s", target, 1)
	return Load(context.Background(), Options{
		BaseURL: DefaultBaseURL, Dir: t.TempDir(),
		HTTPClient: catalogClient(t, http.StatusOK, body, nil),
	})
}

// TestServableFoldsAFloatingAliasToTheIdTheRouterActuallyServes is the fold the
// ledger keys on, and the assertion that matters most is the negative one:
// Identity's answer for this row is a dated slug OpenRouter lists nowhere and
// publishes no endpoints page for, so a ledger folded through it would hold
// beliefs about a name no request will ever wear.
func TestServableFoldsAFloatingAliasToTheIdTheRouterActuallyServes(t *testing.T) {
	c := deepseekCatalog(t, "deepseek/deepseek-v4-flash-0731")
	const servable = "deepseek/deepseek-v4-flash-0731"
	for _, spelling := range []string{
		"~deepseek/deepseek-v4-flash-latest",
		"deepseek/deepseek-v4-flash-latest",
		" ~DEEPSEEK/DeepSeek-V4-Flash-Latest ",
	} {
		if got := c.Servable(spelling); got != servable {
			t.Errorf("Servable(%q) = %q, want the id the router serves (%q)", spelling, got, servable)
		}
	}
	// The canonical chain, which is what Identity follows, ends somewhere else.
	if got := c.Identity("~deepseek/deepseek-v4-flash-latest"); got != "deepseek/deepseek-v4-flash-20260731" {
		t.Fatalf("Identity answered %q; this test is pinned to the chain it really follows", got)
	}
	if got := c.Servable("~deepseek/deepseek-v4-flash-latest"); got == "deepseek/deepseek-v4-flash-20260731" {
		t.Errorf("Servable answered %q, an id served nowhere", got)
	}
	// And never the bare undated id, which is the 0423 snapshot: a different
	// model, its own machines, its own speeds.
	if got := c.Servable("~deepseek/deepseek-v4-flash-latest"); got == "deepseek/deepseek-v4-flash" {
		t.Error("Servable answered the bare id, which is an older snapshot rather than this model")
	}
	// A row that does not float is already servable.
	if got := c.Servable("deepseek/deepseek-v4-flash-0731"); got != servable {
		t.Errorf("Servable of a concrete id = %q, want it unchanged", got)
	}
	if got := c.Servable("deepseek/deepseek-v4-flash"); got != "deepseek/deepseek-v4-flash" {
		t.Errorf("Servable of the bare id = %q, want it unchanged", got)
	}
	// A model this catalog does not carry is forwarded verbatim; nothing is
	// invented for a name nobody published.
	if got := c.Servable("nobody/nothing"); got != "nobody/nothing" {
		t.Errorf("Servable of an unknown model = %q, want it verbatim", got)
	}
}

// TestServableFollowsTheAliasTargetRatherThanKnowingTheAnswer is the whole
// difference between a fold and a special case. The alias moves the day
// DeepSeek ships the next snapshot, and if this fold were pinned to today's
// target the ledger would key every session after that on a model nobody is
// talking to any more.
func TestServableFollowsTheAliasTargetRatherThanKnowingTheAnswer(t *testing.T) {
	moved := deepseekCatalog(t, "deepseek/deepseek-v4-flash-0902")
	if got := moved.Servable("~deepseek/deepseek-v4-flash-latest"); got != "deepseek/deepseek-v4-flash-0902" {
		t.Errorf("the alias target moved and Servable answered %q, want the row's own target", got)
	}
}

// Servable never waits, and it never guesses either. It is read on the launch
// path AND on the send path, so a catalog still warming in the background must
// answer at once — and what it has to answer is NOTHING, because its reader
// remembers first answers for the life of the process. "The id as written" from
// a cold catalog is indistinguishable from "I looked and there is nothing to
// move", and a reader that could not tell them apart would key a whole run on
// the alias it asked one moment too early about.
func TestAColdCatalogSaysNothingRatherThanGuessing(t *testing.T) {
	unresolved := &Catalog{}
	if got := unresolved.Servable("~deepseek/deepseek-v4-flash-latest"); got != "" {
		t.Errorf("a cold catalog answered %q, want nothing at all", got)
	}
	var absent *Catalog
	if got := absent.Servable("deepseek/deepseek-v4-flash-latest"); got != "" {
		t.Errorf("no catalog at all answered %q", got)
	}
	// A WARM CATALOG THAT DOES NOT CARRY THE MODEL IS A DIFFERENT ANSWER, and
	// telling the two apart is the whole point: it has looked, and there is
	// nothing to move, which is a fact worth remembering.
	warm := deepseekCatalog(t, "deepseek/deepseek-v4-flash-0731")
	if got := warm.Servable("nobody/nothing"); got != "nobody/nothing" {
		t.Errorf("a warm catalog answered %q for a model it does not carry, want it verbatim", got)
	}
}
