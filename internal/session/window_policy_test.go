package session

import (
	"strings"
	"testing"
)

// ── COMPACTION FOLLOWS THE WINDOW ───────────────────────────────────────────
//
// Measured 2026-08-31: a two-and-a-half-hour run on a model advertising 1.3M
// tokens compacted NINETEEN times, every pass at around a hundred thousand — a
// twentieth of the room the model said it had. Every fold threw away the prefix
// cache the run was otherwise getting 57–61% of its prompt back from, and the
// model coped by writing itself a notes file, which is a harness asking to be
// worked around.
//
// Two things were wrong and both are pinned here: a flat ceiling that
// disbelieved every claim above 256k, and a child agent on another model being
// handed nothing at all instead of that model's own card.

// TestAMillionTokenWindowIsNotFoldedLikeASmallOne is the acceptance sentence:
// a model with room does not compact at a hundred thousand.
func TestAMillionTokenWindowIsNotFoldedLikeASmallOne(t *testing.T) {
	// The row the dogfood run was on, and the size every one of its folds fired
	// at.
	const claimed = 1_310_720
	const foldedAt = 100_000

	if got := CompactThreshold(claimed); got <= foldedAt {
		t.Fatalf("a %d-token model still folds at %d, want a threshold past %d", claimed, got, foldedAt)
	}
	// And an agent carrying that window agrees, whichever road it came in by.
	agent := &Agent{config: Config{ContextWindow: claimed}}
	if got := agent.compactThreshold(); got <= foldedAt {
		t.Fatalf("an agent on a %d-token model thresholds at %d", claimed, got)
	}
	// A plain million, which is what the acceptance actually names.
	if got := CompactThreshold(1_000_000); got <= foldedAt {
		t.Fatalf("a 1M-token model thresholds at %d, want past %d", got, foldedAt)
	}
}

// TestTheCompactionThresholdFollowsTheWindow is the law itself: the trigger is a
// function of the window rather than a figure that stops moving once the window
// gets big.
func TestTheCompactionThresholdFollowsTheWindow(t *testing.T) {
	// From the default up. Below about 110k the 16k reserve floor dominates the
	// percentage and the share stops being a fixed one, which is deliberate and
	// is pinned by the headroom tests next door; what this is asking is whether
	// the trigger goes on following the window once it is past that.
	windows := []int{128_000, 200_000, 256_000, 400_000, 1_000_000, 1_310_720, 2_000_000}
	previous := 0
	for _, window := range windows {
		threshold := CompactThreshold(window)
		if threshold <= previous {
			t.Fatalf("window %d thresholds at %d, no higher than the smaller window's %d — the law stopped following",
				window, threshold, previous)
		}
		// The reserve is a share of the window with a floor under it, so every
		// threshold sits between four fifths and the whole of its own window.
		if threshold >= window {
			t.Fatalf("window %d thresholds at %d, which leaves no room for a reply", window, threshold)
		}
		if threshold*100/window < 80 {
			t.Fatalf("window %d thresholds at %d, only %d%% of the window",
				window, threshold, threshold*100/window)
		}
		previous = threshold
	}
}

// TestAChildOnAnotherModelIsGivenThatModelsWindow is the second half of the
// same failure, and the one the run actually paid for: the conversation knew
// the window, and every worker it started did not.
func TestAChildOnAnotherModelIsGivenThatModelsWindow(t *testing.T) {
	const own = "vendor/conversation-model"
	const other = "vendor/worker-model"
	const ownWindow = 200_000
	const otherWindow = 1_000_000

	card := func(model string) int {
		switch strings.TrimSpace(model) {
		case own:
			return ownWindow
		case other:
			return otherWindow
		}
		return 0
	}
	agent := &Agent{model: own, config: Config{ContextWindow: ownWindow, ContextWindowFor: card}}

	if got := agent.childWindow(own); got != ownWindow {
		t.Fatalf("a child on the same model got %d, want the conversation's own %d", got, ownWindow)
	}
	if got := agent.childWindow(other); got != otherWindow {
		t.Fatalf("a child on another model got %d, want that model's card figure %d", got, otherWindow)
	}
	// A model nobody can say anything about is zero rather than a guess: this
	// package's conservative default then stands, which is the honest answer.
	if got := agent.childWindow("vendor/unknown-model"); got != 0 {
		t.Fatalf("a child on an unknown model got %d, want nothing known", got)
	}
	// And a session with no catalog at all behaves exactly as it did before any
	// of this: the parent's window for its own model, nothing for another.
	blind := &Agent{model: own, config: Config{ContextWindow: ownWindow}}
	if got := blind.childWindow(own); got != ownWindow {
		t.Fatalf("a catalogless session gave its own child %d, want %d", got, ownWindow)
	}
	if got := blind.childWindow(other); got != 0 {
		t.Fatalf("a catalogless session guessed %d for another model", got)
	}
}

// TestAChildInheritsTheWindowItsParentLearned closes the other gap: a window the
// surface handed down AFTER construction has to reach the children too, or a
// conversation that learned its model's real size still starts every worker on
// the figure Config was built with.
func TestAChildInheritsTheWindowItsParentLearned(t *testing.T) {
	const model = "vendor/late-catalog-model"
	agent := &Agent{model: model, config: Config{ContextWindow: 0}}
	if got := agent.childWindow(model); got != defaultContextWindow {
		t.Fatalf("before the catalog answered, a child got %d, want the default %d", got, defaultContextWindow)
	}
	agent.SetContextWindow(1_000_000)
	if got := agent.childWindow(model); got != 1_000_000 {
		t.Fatalf("after the catalog answered, a child got %d, want the learned %d", got, 1_000_000)
	}
}

// TestARefusedPromptCapsTheModelsClaimFromThenOn is the loop closing. The one
// thing that may contradict a model card is an endpoint refusing a prompt for
// being too long, and until this wave that refusal taught the harness nothing
// at all: the loop compacted, re-sent, and built the same over-long request on
// the next long turn.
func TestARefusedPromptCapsTheModelsClaimFromThenOn(t *testing.T) {
	const claimed = 1_310_720
	const refused = 300_000

	agent := &Agent{config: Config{ContextWindow: claimed}}
	if got := agent.trustedWindow(); got != claimed {
		t.Fatalf("before any refusal the trusted window is %d, want the claim %d", got, claimed)
	}
	agent.servedWindow.Store(refused)
	if got := agent.trustedWindow(); got != refused {
		t.Fatalf("after a refusal the trusted window is %d, want what was refused %d", got, refused)
	}
	if got := agent.compactThreshold(); got != CompactThreshold(refused) {
		t.Fatalf("the threshold is %d, want the refused window's %d", got, CompactThreshold(refused))
	}
	// The claim itself is still reported honestly — the status meter describes
	// the model, and the model really does say that.
	if got := agent.window(); got != claimed {
		t.Fatalf("window() = %d, want the model's own claim %d", got, claimed)
	}
	// And the whole chain still holds under the cap, so a pass can still reach
	// its target and the target still sits above the tail.
	threshold, target, keep := agent.compactThreshold(), agent.compactTargetTokens(), agent.keepRecentTokens()
	if !(threshold > target && target > keep) {
		t.Fatalf("the chain broke under the cap: threshold %d, target %d, keep %d", threshold, target, keep)
	}
}
