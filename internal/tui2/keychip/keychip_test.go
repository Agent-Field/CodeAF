package keychip_test

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/keychip"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE TWO TIERS, WHICH ARE THE WHOLE LAW (§16's verb·key chip).
//
// The bug that named it was a settings sheet reading `esc close`: two greys, two
// words, and nothing in the row saying which one is the label and which one is
// the thing to press. Verb-first at the brighter tier fixes it structurally
// rather than by explanation — so both halves of that sentence are pinned here,
// the ORDER and the TIERS, because either one alone leaves the row ambiguous.
func TestAChipIsVerbFirstAndKeyOneTierDown(t *testing.T) {
	spans := keychip.Of(registry.ChipFor("close", "esc"), tokens.TextSecondary)
	if len(spans) != 2 {
		t.Fatalf("a two-halved chip drew %d spans: %+v", len(spans), spans)
	}
	if spans[0].Text != "close" {
		t.Errorf("the chip leads with %q, not the verb", spans[0].Text)
	}
	if spans[0].Tok != tokens.TextSecondary {
		t.Errorf("the verb wears %v, not the tier the caller asked for", spans[0].Tok)
	}
	if spans[1].Text != registry.ChipGap+"esc" {
		t.Errorf("the key half is %q", spans[1].Text)
	}
	if spans[1].Tok != tokens.TextTertiary {
		t.Errorf("the key wears %v, want one tier down (%v)", spans[1].Tok, tokens.TextTertiary)
	}
	// AND NEVER THE DIMMEST TIER FOR THE VERB: an interactive chip may not live
	// permanently in the tier the eye skips (5.22's amendment). The tier is the
	// caller's to choose, so what is pinned is that the key is strictly dimmer
	// than the verb, whichever the caller picked.
	for _, verb := range []tokens.Token{tokens.TextPrimary, tokens.TextSecondary} {
		got := keychip.Of(registry.ChipFor("open", "ctrl+r"), verb)
		if got[0].Tok == got[1].Tok {
			t.Errorf("at verb tier %v both halves wear one tier: the row is two unmarked words again", verb)
		}
	}
}

// EMPTY IS A REAL ANSWER on both halves, and each has its own honest shape.
func TestAHalfChipIsStillAHonestRow(t *testing.T) {
	// A belt-only verb is reached through the user's own words and has no key.
	spans := keychip.Of(registry.ChipFor("reflect", ""), tokens.TextSecondary)
	if len(spans) != 1 || spans[0].Text != "reflect" {
		t.Fatalf("a keyless verb drew %+v", spans)
	}
	// A raw exit hint is a key with nothing to call it, and it reads as chrome.
	spans = keychip.Of(registry.ChipFor("", "esc"), tokens.TextSecondary)
	if len(spans) != 1 || spans[0].Text != "esc" || spans[0].Tok != tokens.TextTertiary {
		t.Fatalf("a verbless key drew %+v", spans)
	}
	if got := keychip.Of(registry.ChipFor("", ""), tokens.TextSecondary); len(got) != 0 {
		t.Fatalf("an empty chip drew %+v, want nothing at all", got)
	}
}

// A LINE IS CHIPS AND SEPARATORS, and the separator is chrome while the chips
// are not — which is the whole reason the join lives here. A surface that
// painted its strip at one tier would flatten the two-tier rule the chips exist
// to state, and that is exactly what the consent dialog's key strip did.
func TestALineKeepsTheChipsTwoTiersAndDimsOnlyTheSeparator(t *testing.T) {
	chips := []registry.Chip{
		registry.ChipFor("allow", "enter"),
		registry.ChipFor("edit", "e"),
		registry.ChipFor("back", "esc"),
	}
	spans := keychip.Line(chips, tokens.TextSecondary)
	var plain strings.Builder
	verbs := 0
	for _, span := range spans {
		plain.WriteString(span.Text)
		if span.Tok == tokens.TextSecondary {
			verbs++
		}
		if span.Text == keychip.Sep && span.Tok != tokens.TextTertiary {
			t.Errorf("the separator wears %v, want chrome", span.Tok)
		}
	}
	if verbs != len(chips) {
		t.Errorf("%d of %d verbs reached the brighter tier", verbs, len(chips))
	}
	want := "allow enter · edit e · back esc"
	if got := plain.String(); got != want {
		t.Fatalf("the line reads %q, want %q", got, want)
	}
	// The width a surface fits against is the width the spans actually are.
	if got := keychip.Width(chips); got != blocks.Width(want) {
		t.Errorf("Width says %d cells, the line is %d", got, blocks.Width(want))
	}
	if got := keychip.Text(chips...); got != want {
		t.Errorf("Text reads %q, want %q", got, want)
	}
}

// An empty chip contributes nothing AND no separator, so a strip that drops a
// verb it cannot honestly offer does not leave a dangling dot behind it.
func TestADroppedChipLeavesNoDanglingSeparator(t *testing.T) {
	line := keychip.Text(
		registry.ChipFor("answer", "enter"),
		registry.ChipFor("", ""),
		registry.ChipFor("later", "esc"),
	)
	if line != "answer enter · later esc" {
		t.Fatalf("the line reads %q", line)
	}
	if got := keychip.Text(); got != "" {
		t.Fatalf("an empty line reads %q", got)
	}
	if got := keychip.Text(registry.ChipFor("", "")); got != "" {
		t.Fatalf("a line of nothing reads %q", got)
	}
}
