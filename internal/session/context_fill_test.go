package session

import (
	"strconv"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
)

// ── A PINNED FILL IS HONOURED, AND AN UNTOUCHED ONE IS NOT ──────────────────
//
// `--context-fill` has existed and been documented as the compaction fill point
// since long before the derivation in window_policy_test.go next door, and the
// conversation's trigger read it nowhere: the fill law could not tell a sixty
// somebody typed from the sixty it ships with, so honouring the number would
// have dropped every session's fold line from the derived ~85% of its window to
// 60% — the exact regression the derivation was written to end.
//
// What makes honouring it safe is provenance, and the settings registry already
// carried it: an environment pin on the row, or a value written down in the
// profile. These tests pin both halves of the resulting law.

// pinFill puts a fill percentage in force for one test the way `--context-fill`
// does — it sets the environment variable that flag sets — and t.Setenv puts it
// back afterwards, so no other test in this package inherits a pinned law.
func pinFill(t *testing.T, percent int) {
	t.Helper()
	t.Setenv("AFORGE_CONTEXT_FILL_PCT", strconv.Itoa(percent))
}

// TestAPinnedFillFoldsWhereItSaysAndAnUnpinnedOneFollowsTheWindow is the issue's
// acceptance sentence whole: a 1M-window model with --context-fill 60 pinned
// compacts near 600k, and the same model with nothing pinned keeps the derived
// threshold.
func TestAPinnedFillFoldsWhereItSaysAndAnUnpinnedOneFollowsTheWindow(t *testing.T) {
	const window = 1_000_000

	// Nothing pinned: the derived line stands, which on a million-token window
	// is the window less fifteen percent of it.
	derived := CompactThreshold(window)
	if want := window - window*15/100; derived != want {
		t.Fatalf("with nothing pinned a %d-token window thresholds at %d, want the derived %d", window, derived, want)
	}
	if _, pinned := ContextFillPinned(); pinned {
		t.Fatal("a session nobody has touched reports a pinned fill")
	}

	// Pinned to sixty: the line is sixty percent of that same window, and the
	// agent that actually folds agrees with the function a surface draws.
	pinFill(t, 60)
	percent, pinned := ContextFillPinned()
	if !pinned || percent != 60 {
		t.Fatalf("a pinned fill reads as %d/%v, want 60/true", percent, pinned)
	}
	if got := CompactThreshold(window); got != 600_000 {
		t.Fatalf("a pinned fill of 60 on a %d-token window thresholds at %d, want 600000", window, got)
	}
	agent := &Agent{config: Config{ContextWindow: window}}
	if got := agent.compactThreshold(); got != 600_000 {
		t.Fatalf("the agent that folds thresholds at %d, want the 600000 the surface draws", got)
	}
	// And it is a real move rather than a coincidence of arithmetic: the pinned
	// line is a quarter of a million tokens below the derived one.
	if got := CompactThreshold(window); got >= derived {
		t.Fatalf("the pinned line %d did not move off the derived %d", got, derived)
	}
}

// TestAnUntouchedFillIsNotAPinHoweverOrdinaryItsValue is the other half of the
// same law, said about the number that made it necessary. Sixty is what
// ctxbudget ships with, and it is what a person is most likely to type; the
// trigger has to tell those two sixties apart or the default becomes the law.
func TestAnUntouchedFillIsNotAPinHoweverOrdinaryItsValue(t *testing.T) {
	const claimed = 1_310_720

	if percent := ctxbudget.FillPercent(); percent != ctxbudget.DefaultFillPercent {
		t.Fatalf("the shipped fill is %d, want %d — this test is about that number", percent, ctxbudget.DefaultFillPercent)
	}
	untouched := CompactThreshold(claimed)
	pinFill(t, ctxbudget.DefaultFillPercent)
	sameNumberTyped := CompactThreshold(claimed)

	if untouched == sameNumberTyped {
		t.Fatalf("a fill nobody set and the same fill typed both threshold at %d — the trigger cannot tell them apart", untouched)
	}
	if want := claimed * ctxbudget.DefaultFillPercent / 100; sameNumberTyped != want {
		t.Fatalf("the typed fill thresholds at %d, want %d", sameNumberTyped, want)
	}
	if want := claimed - claimed*15/100; untouched != want {
		t.Fatalf("the untouched fill thresholds at %d, want the derived %d", untouched, want)
	}
}

// TestAPinnedLineStillKeepsTheAnswerRoom is the ceiling: a pin may not push the
// conversation so far up the window that the reply it is waiting for has nowhere
// to land. The room is ctxbudget's completion reserve, which is the
// `--completion-reserve` flag's own number.
func TestAPinnedLineStillKeepsTheAnswerRoom(t *testing.T) {
	// Half a million tokens is where the two ceilings cross for a fill of
	// ninety: the reserve is the binding one from about 437,000 up, and ninety
	// percent of this window asks for more than it leaves.
	const window = 500_000

	pinFill(t, 90)
	threshold := CompactThreshold(window)
	if room := window - threshold; room < ctxbudget.CompletionReserve() {
		t.Fatalf("a pinned line at %d leaves %d tokens of answer room, want at least %d",
			threshold, room, ctxbudget.CompletionReserve())
	}
	// And the clamp is the reserve rather than a rounder number: what the fill
	// asked for was higher, so the ceiling is what came back.
	if want := window - ctxbudget.CompletionReserve(); threshold != want {
		t.Fatalf("the clamped line is %d, want %d", threshold, want)
	}
}

// TestAPinAskingForMoreRoomNeverGetsLessThanAnUnpinnedSession is the second half
// of that ceiling, and the reason it is a maximum of two lines rather than one.
//
// The completion reserve is a constant and the derived reserve is a share, so on
// any window under roughly 437,000 tokens the reserve is the larger of the two.
// Clamped to the reserve alone, a person asking for ninety percent of a
// 128,000-token window would have been folded at 62,464 — earlier than the
// 108,800 they would have got by saying nothing at all, which is a knob that
// punishes the person who turns it.
func TestAPinAskingForMoreRoomNeverGetsLessThanAnUnpinnedSession(t *testing.T) {
	for _, window := range []int{32_000, 128_000, 200_000, 400_000, 437_000} {
		derived := CompactThreshold(window)
		pinFill(t, 90)
		pinned := CompactThreshold(window)
		if pinned < derived {
			t.Fatalf("on a %d-token window a pinned 90 folds at %d, earlier than the unpinned %d",
				window, pinned, derived)
		}
		t.Setenv("AFORGE_CONTEXT_FILL_PCT", "")
	}
}

// TestAPinnedLineNeverFallsUnderTheTailItCannotFold is the floor. The most
// recent tokens are never candidates for a fold, so a threshold at or below that
// tail fires on every step and finds nothing to take — the forever-compacting
// failure compactTarget's own comment records. The floor is twice the tail.
func TestAPinnedLineNeverFallsUnderTheTailItCannotFold(t *testing.T) {
	pinFill(t, 10)
	for _, window := range []int{32_000, 128_000, 260_000, 1_000_000} {
		threshold := CompactThreshold(window)
		tail := keepRecent(window)
		if threshold <= tail {
			t.Fatalf("on a %d-token window a pinned 10 folds at %d, at or under the %d-token tail",
				window, threshold, tail)
		}
	}
}

// TestTheFoldChainHoldsUnderAPinnedLine is the invariant compactTarget states,
// checked under the law that can move the line out from under it:
//
//	threshold > compactTarget > keepRecent
//
// It holds because compactTarget derives from whichever threshold governs rather
// than from the derivation alone. A target above its threshold would fold
// nothing; one under the tail could never be reached.
func TestTheFoldChainHoldsUnderAPinnedLine(t *testing.T) {
	for _, fill := range []int{10, 25, 60, 75, 90} {
		pinFill(t, fill)
		for _, window := range []int{32_000, 128_000, 260_000, 1_000_000, 1_310_720, 4_000_000} {
			threshold, target, tail := CompactThreshold(window), compactTarget(window), keepRecent(window)
			if !(threshold > target && target > tail) {
				t.Fatalf("fill %d on a %d-token window: threshold %d, target %d, tail %d — the chain broke",
					fill, window, threshold, target, tail)
			}
		}
	}
}

// TestAPinnedLineIsStillCappedByWhatAnEndpointRefused keeps #158's law on top of
// this one: the fill is taken of the window this process TRUSTS, so a model
// whose endpoint has refused a prompt cannot have that refusal undone by pinning
// a percentage.
func TestAPinnedLineIsStillCappedByWhatAnEndpointRefused(t *testing.T) {
	const claimed = 1_000_000
	const refused = 200_000

	pinFill(t, 60)
	agent := &Agent{config: Config{ContextWindow: claimed}}
	if got := agent.compactThreshold(); got != claimed*60/100 {
		t.Fatalf("before any refusal a pinned 60 thresholds at %d, want 60%% of the claim", got)
	}
	agent.servedWindow.Store(refused)
	if got := agent.compactThreshold(); got != refused*60/100 {
		t.Fatalf("a pinned fill on a refused model thresholds at %d, want 60%% of the refused %d", got, refused)
	}
	// The claim itself is still reported honestly, which is #158's law and not
	// this one's to change.
	if got := agent.window(); got != claimed {
		t.Fatalf("window() = %d, want the model's own claim %d", got, claimed)
	}
}
