package lane

import "testing"

// resetServable puts the process-wide fold back for one test. Both the resolver
// and the memo are process state, deliberately, so a test that installs one has
// to clear what an earlier test remembered.
func resetServable(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { UseServable(nil) })
	UseServable(nil)
}

// With nothing installed the ledger key is what it has always been. A surface
// with no catalog must degrade to yesterday's behaviour and never to nothing.
func TestWithNoFoldInstalledTheLedgerKeepsTheNameItAlwaysUsed(t *testing.T) {
	resetServable(t)
	for spelling, want := range map[string]string{
		"moonshotai/kimi-k3:high":           "moonshotai/kimi-k3",
		"deepseek/deepseek-v4-flash-latest": "deepseek/deepseek-v4-flash-latest",
		"":                                  "",
	} {
		if got := LedgerModel(spelling); got != want {
			t.Errorf("LedgerModel(%q) = %q, want %q", spelling, got, want)
		}
	}
}

// The fold is applied under the tier suffix, so `alias:high` and `alias` reach
// the ledger as the one model the router is really serving.
func TestAnInstalledFoldNamesTheModelTheRouterActuallyServes(t *testing.T) {
	resetServable(t)
	UseServable(func(model string) string {
		if model == "deepseek/deepseek-v4-flash-latest" {
			return "deepseek/deepseek-v4-flash-0731"
		}
		return model
	})
	const servable = "deepseek/deepseek-v4-flash-0731"
	for _, spelling := range []string{
		"deepseek/deepseek-v4-flash-latest",
		"deepseek/deepseek-v4-flash-latest:high",
	} {
		if got := LedgerModel(spelling); got != servable {
			t.Errorf("LedgerModel(%q) = %q, want %q", spelling, got, servable)
		}
	}
	if got := LedgerModel("anthropic/claude-opus-5"); got != "anthropic/claude-opus-5" {
		t.Errorf("a model the fold does not move came back as %q", got)
	}
}

// A fold that answers nothing is ignored rather than obeyed: a blank key would
// file every model's beliefs in one heap.
func TestAFoldThatAnswersNothingIsIgnored(t *testing.T) {
	resetServable(t)
	UseServable(func(string) string { return "  " })
	if got := LedgerModel("deepseek/deepseek-v4-flash-latest"); got != "deepseek/deepseek-v4-flash-latest" {
		t.Errorf("an empty fold answer became the ledger key %q", got)
	}
}

// The answer is memoised, because the catalog behind the fold warms in the
// background: asked before it lands and again after, an honest fold gives two
// answers, and a ledger key that moved halfway through a run would split one
// session's history in two.
func TestTheLedgerKeyDoesNotMoveUnderARunningSession(t *testing.T) {
	resetServable(t)
	cold := true
	UseServable(func(model string) string {
		if cold {
			return model
		}
		return "deepseek/deepseek-v4-flash-0731"
	})
	first := LedgerModel("deepseek/deepseek-v4-flash-latest")
	cold = false
	if second := LedgerModel("deepseek/deepseek-v4-flash-latest"); second != first {
		t.Errorf("the ledger key moved mid-run: %q then %q", first, second)
	}
	// Installing a fold clears the memo, so a process that installs late is
	// consistent from that point on.
	UseServable(func(string) string { return "deepseek/deepseek-v4-flash-0731" })
	if got := LedgerModel("deepseek/deepseek-v4-flash-latest"); got != "deepseek/deepseek-v4-flash-0731" {
		t.Errorf("after a late install the key was %q", got)
	}
}
