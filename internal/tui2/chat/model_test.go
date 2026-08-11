package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// 5.10 and 5.23's chip grammar, on the one line that used to spell a model by
// hand: role word, model word, and the effort that rides the slug. The vendor
// prefix and the release stamp are provenance and must not reach a cell.
func TestTheMetaStripSpeaksTheChipGrammar(t *testing.T) {
	strip := &metaStrip{
		style: tokens.NewStyler(tokens.NoColor, tokens.FocusNormal),
		model: "anthropic/claude-sonnet-4-20250514:high",
	}
	row := ansi.Strip(strip.render(60))
	for _, want := range []string{store.RoleOrchestrate.Word(), "claude-sonnet-4", "high"} {
		if !strings.Contains(row, want) {
			t.Fatalf("the meta strip's chip does not say %q: %q", want, row)
		}
	}
	for _, refused := range []string{"anthropic/", "20250514"} {
		if strings.Contains(row, refused) {
			t.Fatalf("provenance reached the chip (%q): %q", refused, row)
		}
	}
}

// One word, everywhere. The chip, a reply header's meta cell and the receipt a
// model switch posts all go through the same shortening, so a reader never sees
// one model under two names.
func TestOneModelWordForTheChipAndTheHeader(t *testing.T) {
	const slug = "anthropic/claude-sonnet-4-20250514:high"
	if got := modelWord(slug); got != "claude-sonnet-4" {
		t.Fatalf("modelWord(%q) = %q", slug, got)
	}
	strip := &metaStrip{model: slug}
	if got := strip.chip().Model; got != slug {
		t.Fatalf("the chip was handed %q rather than the slug it must shorten itself", got)
	}
}
