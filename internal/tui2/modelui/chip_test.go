package modelui

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

func TestChipSpeaksTheGrammar(t *testing.T) {
	t.Parallel()
	chip := Chip{
		Role:   store.RoleWork,
		Model:  "anthropic/claude-sonnet-4-20250514",
		Used:   50_000,
		Window: 200_000,
	}
	// ⟨role word⟩ ⟨model word⟩ ⟨ctx gauge⟩ — 5.23, in that order and no other.
	if got, want := chip.Text(), RoleWord(store.RoleWork)+" claude-sonnet-4 "+tokens.Gauge(0.25); got != want {
		t.Fatalf("chip text = %q, want %q", got, want)
	}
}

func TestChipSaysModelWordsNeverProviderIDs(t *testing.T) {
	t.Parallel()
	for _, slug := range []string{
		"anthropic/claude-sonnet-4-20250514",
		"~anthropic/claude-sonnet-4",
		"anthropic/claude-sonnet-4:free",
	} {
		chip := Chip{Model: slug}
		if got := chip.Text(); got != "claude-sonnet-4" {
			t.Errorf("chip for %q = %q, want %q", slug, got, "claude-sonnet-4")
		}
		if strings.Contains(chip.Text(), "/") {
			t.Errorf("chip for %q leaked a provider id: %q", slug, chip.Text())
		}
	}
}

func TestBoostIsAGlyphOnTheChipAndNotASecondConcept(t *testing.T) {
	t.Parallel()
	chip := Chip{Role: store.RoleWork, Model: "openai/gpt-oss-120b", Boosted: true}
	got := chip.Text()
	if !strings.Contains(got, tokens.GlyphBoosted) {
		t.Fatalf("boosted chip = %q, want the boost glyph %q", got, tokens.GlyphBoosted)
	}
	// It rides the same chip: one role word, one model word, one mark (5.10).
	if want := RoleWord(store.RoleWork) + " gpt-oss-120b " + tokens.GlyphBoosted; got != want {
		t.Fatalf("boosted chip = %q, want %q", got, want)
	}
	// And the mark is never emoji: 5.17's width law, which ⚡ fails.
	if strings.Contains(got, "⚡") {
		t.Fatalf("chip used the emoji boost mark: %q", got)
	}
}

func TestEffortRidesTheSlugAndShowsAsASuffix(t *testing.T) {
	t.Parallel()
	chip := Chip{Role: store.RolePlan, Model: "openai/gpt-oss-120b:high"}
	if got, want := chip.Text(), RoleWord(store.RolePlan)+" gpt-oss-120b "+tokens.GlyphSeparator+" high"; got != want {
		t.Fatalf("chip = %q, want %q", got, want)
	}
}

func TestAVariantThatIsNotAnEffortIsNotRenderedAsOne(t *testing.T) {
	t.Parallel()
	chip := Chip{Model: "meta/llama-3.3-70b:free"}
	if got := chip.Text(); got != "llama-3.3-70b" {
		t.Fatalf("chip = %q, want the variant dropped and no effort invented", got)
	}
}

func TestAnUnknownWindowIsMissingAndNeverAnEstimate(t *testing.T) {
	t.Parallel()
	chip := Chip{Role: store.RoleScribe, Model: "openai/gpt-oss-20b", Used: 1_000}
	if got, want := chip.Text(), RoleWord(store.RoleScribe)+" gpt-oss-20b "+tokens.GlyphMissing; got != want {
		t.Fatalf("chip = %q, want %q", got, want)
	}
	// Nothing known at all spends no column on saying so.
	quiet := Chip{Role: store.RoleScribe, Model: "openai/gpt-oss-20b"}
	if got, want := quiet.Text(), RoleWord(store.RoleScribe)+" gpt-oss-20b"; got != want {
		t.Fatalf("chip = %q, want %q", got, want)
	}
}

func TestAnUnboundBindingRendersTheMissingMark(t *testing.T) {
	t.Parallel()
	if got, want := (Chip{Role: store.RoleVerify}).Text(), RoleWord(store.RoleVerify)+" "+tokens.GlyphMissing; got != want {
		t.Fatalf("unbound chip = %q, want %q", got, want)
	}
	// A row that already names the role still says "unbound" rather than
	// nothing: the row IS about a binding.
	if got, want := (Chip{}.WithoutRole()).Text(), tokens.GlyphMissing; got != want {
		t.Fatalf("anonymous unbound chip = %q, want %q", got, want)
	}
	// A chip about nothing at all draws nothing at all.
	if got := (Chip{}).Text(); got != "" {
		t.Fatalf("zero chip = %q, want empty", got)
	}
}

func TestTheGaugeIsAlwaysExactlyOneCell(t *testing.T) {
	t.Parallel()
	for _, used := range []int64{0, 1, 1_000, 99_999, 100_000, 199_999, 200_000, 1 << 40} {
		chip := Chip{Model: "x/y", Used: used, Window: 200_000}
		_, _, _, _, gauge := chip.fit(0)
		if w := blocks.Width(gauge); w != 1 {
			t.Fatalf("gauge for used=%d is %d cells (%q), want 1", used, w, gauge)
		}
	}
	// A negative reading cannot make the gauge disappear or panic.
	chip := Chip{Model: "x/y", Used: -5, Window: 200_000}
	if _, _, _, _, gauge := chip.fit(0); blocks.Width(gauge) != 1 {
		t.Fatalf("gauge for a negative reading is %q, want one cell", gauge)
	}
}

// TestChipDropLadder walks the stated order: effort, role, gauge, boost, then
// the model word truncates. Each step is checked at exactly the width where it
// must happen, so a re-ordered ladder fails here and not in a screenshot.
func TestChipDropLadder(t *testing.T) {
	t.Parallel()
	chip := Chip{
		Role:    store.RoleWork,
		Model:   "openai/gpt-oss-120b:high",
		Boosted: true,
		Used:    10,
		Window:  100,
	}
	full := chip.Text()
	if want := RoleWord(store.RoleWork) + " gpt-oss-120b " + tokens.GlyphBoosted + " " + tokens.GlyphSeparator + " high " + tokens.Gauge(0.1); full != want {
		t.Fatalf("full chip = %q, want %q", full, want)
	}
	steps := []struct {
		width int
		want  string
	}{
		{blocks.Width(full), full},
		{blocks.Width(full) - 1, RoleWord(store.RoleWork) + " gpt-oss-120b " + tokens.GlyphBoosted + " " + tokens.Gauge(0.1)},
		{21, "gpt-oss-120b " + tokens.GlyphBoosted + " " + tokens.Gauge(0.1)},
		{15, "gpt-oss-120b " + tokens.GlyphBoosted},
		{13, "gpt-oss-120b"},
	}
	for _, step := range steps {
		if got := plainChip(chip, step.width); got != step.want {
			t.Errorf("at width %d chip = %q, want %q", step.width, got, step.want)
		}
	}
	// Below the model word it truncates, and never overflows on the way down.
	for w := 1; w < 12; w++ {
		got := plainChip(chip, w)
		if blocks.Width(got) > w {
			t.Fatalf("at width %d chip = %q, %d cells", w, got, blocks.Width(got))
		}
	}
}

func TestChipNeverOverflowsAtAnyWidth(t *testing.T) {
	t.Parallel()
	chips := []Chip{
		{},
		{Role: store.RoleOrchestrate},
		{Role: store.RoleWork, Model: "anthropic/claude-sonnet-4-20250514:high", Boosted: true, Used: 199_000, Window: 200_000},
		{Model: "a-very-long-vendor/an-even-longer-model-name-that-keeps-going-and-going"},
		{Model: "彼らの/モデルの名前", Used: 5, Window: 10},
		{Role: store.ModelRole("nonsense"), Model: "x"},
	}
	for _, chip := range chips {
		for w := 0; w <= 80; w++ {
			got := chip.Render(w)
			if blocks.Width(got) > w {
				t.Fatalf("chip %+v at width %d rendered %d cells: %q", chip, w, blocks.Width(got), got)
			}
		}
	}
}

func TestChipSpansCarryTheirTokens(t *testing.T) {
	t.Parallel()
	chip := Chip{Role: store.RoleWork, Model: "x/sonnet", Boosted: true, Used: 1, Window: 10}
	spans := chip.Spans(0)
	want := []struct {
		text string
		tok  tokens.Token
	}{
		{RoleWord(store.RoleWork), tokens.TextTertiary},
		{" ", tokens.TextTertiary},
		{"sonnet", tokens.TextSecondary},
		{" ", tokens.TextTertiary},
		{tokens.GlyphBoosted, tokens.Cyan},
		{" ", tokens.TextTertiary},
		{tokens.Gauge(0.1), tokens.TextTertiary},
	}
	if len(spans) != len(want) {
		t.Fatalf("spans = %v, want %d of them", spans, len(want))
	}
	for i := range want {
		if spans[i].Text != want[i].text || spans[i].Token != want[i].tok {
			t.Errorf("span %d = %q/%v, want %q/%v", i, spans[i].Text, spans[i].Token, want[i].text, want[i].tok)
		}
	}
}

func TestAContextAlarmColorsTheGauge(t *testing.T) {
	t.Parallel()
	calm := Chip{Model: "x/y", Used: 1, Window: 200_000}
	loud := Chip{Model: "x/y", Used: 199_999, Window: 200_000}
	if got := lastToken(calm.Spans(0)); got != tokens.TextTertiary {
		t.Errorf("calm gauge token = %v, want the chrome tier", got)
	}
	if got := lastToken(loud.Spans(0)); got != tokens.Amber {
		t.Errorf("alarmed gauge token = %v, want amber", got)
	}
}

func TestMinWidthIsTheModelWord(t *testing.T) {
	t.Parallel()
	chip := Chip{Role: store.RoleWork, Model: "openai/gpt-oss-120b", Used: 1, Window: 2}
	if got, want := chip.MinWidth(), blocks.Width("gpt-oss-120b"); got != want {
		t.Fatalf("MinWidth = %d, want %d", got, want)
	}
	if got := plainChip(chip, chip.MinWidth()); got != "gpt-oss-120b" {
		t.Fatalf("chip at MinWidth = %q, want the whole model word", got)
	}
}

func TestAStyledChipPaintsAndStillFits(t *testing.T) {
	t.Parallel()
	chip := Chip{
		Role:   store.RoleWork,
		Model:  "openai/gpt-oss-120b",
		Used:   1,
		Window: 2,
		Styler: tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal),
	}
	got := chip.Render(40)
	if !strings.Contains(got, "\x1b[") {
		t.Fatalf("styled chip has no escape sequences: %q", got)
	}
	if blocks.Width(got) > 40 {
		t.Fatalf("styled chip is %d cells wide: %q", blocks.Width(got), got)
	}
}

// plainChip renders with no styler and returns the bytes, which are the text.
func plainChip(c Chip, width int) string { return c.Render(width) }

func lastToken(spans []Span) tokens.Token {
	if len(spans) == 0 {
		return tokens.TextPrimary
	}
	return spans[len(spans)-1].Token
}
