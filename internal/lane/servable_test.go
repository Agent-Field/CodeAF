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

// A FOLD THAT CANNOT ANSWER YET IS ASKED AGAIN, and this is the half of the
// memo that had to be taken back out. The catalog behind the fold warms in the
// background and a process may ask before it lands — headless runs do, every
// time — so remembering "the name as written" from a cold fold would key the
// whole run on the alias and never fold again, which is issue #289 made
// permanent by the thing meant to fix it.
func TestAFoldThatCannotAnswerYetIsAskedAgain(t *testing.T) {
	resetServable(t)
	asked := 0
	cold := true
	UseServable(func(model string) string {
		asked++
		if cold {
			return ""
		}
		return "deepseek/deepseek-v4-flash-0731"
	})
	const alias = "deepseek/deepseek-v4-flash-latest"
	if got := LedgerModel(alias); got != alias {
		t.Fatalf("while the fold could not answer the key was %q, want the name as written", got)
	}
	if got := LedgerModel(alias); got != alias {
		t.Fatalf("a second ask before the catalog landed gave %q", got)
	}
	if asked != 2 {
		t.Fatalf("the fold was asked %d times; an answer it could not give must not be remembered", asked)
	}
	cold = false
	if got := LedgerModel(alias); got != "deepseek/deepseek-v4-flash-0731" {
		t.Fatalf("once the catalog landed the key was %q, want the id the router serves", got)
	}
	// And THEN it is remembered, because from here on the answer is real.
	before := asked
	if got := LedgerModel(alias); got != "deepseek/deepseek-v4-flash-0731" || asked != before {
		t.Fatalf("the answered fold was asked again (%d then %d) and gave %q", before, asked, got)
	}
}

// The fold is idempotent, and it has to be: the same name reaches the ledger
// from a config slot, from a wire answer and from a row already on disk, so a
// key that moved on the second application would re-split what the first folded
// together. Held against the rows the live catalog really publishes.
func TestTheLedgerKeyIsTheSameOnTheSecondApplication(t *testing.T) {
	resetServable(t)
	UseServable(func(model string) string {
		if model == "deepseek/deepseek-v4-flash-latest" {
			return "deepseek/deepseek-v4-flash-0731"
		}
		return model
	})
	for _, spelling := range []string{
		"deepseek/deepseek-v4-flash-latest",
		"deepseek/deepseek-v4-flash-latest:high",
		"deepseek/deepseek-v4-flash-0731",
		// The bare undated id is a DIFFERENT snapshot and stays its own key.
		"deepseek/deepseek-v4-flash",
		"anthropic/claude-opus-5",
	} {
		once := LedgerModel(spelling)
		if twice := LedgerModel(once); twice != once {
			t.Errorf("LedgerModel(%q) = %q, and folding that again gave %q", spelling, once, twice)
		}
	}
	if got := LedgerModel("deepseek/deepseek-v4-flash"); got != "deepseek/deepseek-v4-flash" {
		t.Errorf("the bare id folded to %q; it is the older snapshot and a model of its own", got)
	}
}
