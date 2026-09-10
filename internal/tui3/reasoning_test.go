package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The rows the informative-picker wave is about: one model that publishes
// everything, one that publishes nothing, and one in between.
var richCatalog = []Model{
	{
		ID: "anthropic/claude-sonnet-4.5", ContextLength: 1_000_000,
		PromptPrice: 0.000003, CompletionPrice: 0.000015,
		ArenaElo: 1243.4, Reasoning: true,
	},
	{
		ID: "openai/gpt-4.1-mini", ContextLength: 128_000,
		PromptPrice: 0.00000008, CompletionPrice: 0.00000015,
	},
	{ID: "moonshotai/kimi-k3"},
}

// pickerLines is the picker's own rows, plain — the status line below them
// names a model too, and a frame-wide search would read its price as a row's.
func pickerLines(a *app) []string {
	out := make([]string, 0, len(a.pick.hits))
	for _, line := range a.pick.rows(a.width, len(a.pick.hits), a.pal, -1, a.reasoningFor) {
		out = append(out, plain(line))
	}
	return out
}

func TestAPickerRowCarriesTheWindowThePriceAndTheScore(t *testing.T) {
	a := pickerApp(t, &fakeAgent{model: "moonshotai/kimi-k3"}, richCatalog)
	a.width = 100 // a picker's rows want a picker's terminal
	typeLine(t, a, "/model")

	got := strings.Join(pickerLines(a), "\n")
	for _, want := range []string{
		"anthropic/claude-sonnet-4.5",
		"$3/$15 per M · 1M · elo 1243",
		"openai/gpt-4.1-mini",
		"$0.08/$0.15 per M · 128k",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("the picker has to say %q:\n%s", want, got)
		}
	}
	for _, line := range pickerLines(a) {
		// A row nobody published anything about says nothing about it — no zero
		// price, no zero elo, no "0" window.
		if strings.Contains(line, "moonshotai/kimi-k3") &&
			(strings.Contains(line, "$") || strings.Contains(line, "elo")) {
			t.Fatalf("an unpublished row must draw a gap, not a zero: %q", line)
		}
		// And the score is absent from a row that carries no arena figure, even
		// though that row does carry a price.
		if strings.Contains(line, "gpt-4.1-mini") && strings.Contains(line, "elo") {
			t.Fatalf("gpt-4.1-mini publishes no elo, so the row must not: %q", line)
		}
	}
}

// The level rides the id, and NEITHER gives way when the row runs out of room:
// a clipped level is a knob that looks like it did nothing, and a clipped id is
// a row a person cannot match against the name they are hunting for. What gives
// way is the TAIL, one whole fact at a time, from the bottom of the hierarchy
// up (rowfit.go).
func TestANarrowRowKeepsTheNameAndTheLevelAndSpendsTheTail(t *testing.T) {
	agent := &fakeAgent{model: "moonshotai/kimi-k3", levels: map[string]string{
		"anthropic/claude-sonnet-4.5": "high",
	}}
	a := pickerApp(t, agent, richCatalog)
	typeLine(t, a, "/model")
	typeInto(t, a, "sonnet")
	// The session was dialled before this surface opened, so the row learns the
	// level off the draw path — one frame late, and never on the wire
	// (reasoninglevel.go).
	settleLevels(a, "anthropic/claude-sonnet-4.5")

	line := pickerLines(a)[0]
	if !strings.Contains(line, "anthropic/claude-sonnet-4.5:high") {
		t.Fatalf("a narrow row cut the name or the level off it: %q", line)
	}
	if strings.Contains(line, glyphMore) {
		t.Fatalf("a narrow row cut something in half instead of dropping it: %q", line)
	}
	// Sixty columns hold the name, the level and the two facts a person chooses
	// on; the arena score is under the fold of the row and simply is not drawn.
	if !strings.Contains(line, "$3/$15 per M · 1M") || strings.Contains(line, "elo") {
		t.Fatalf("the narrow row spent its tail in the wrong order: %q", line)
	}
}

func TestTheRowsTailIsDimAndTheIDIsNot(t *testing.T) {
	a := pickerApp(t, &fakeAgent{model: "moonshotai/kimi-k3"}, richCatalog)
	typeLine(t, a, "/model")
	typeInto(t, a, "sonnet")

	rows := a.pick.rows(a.width, 1, a.pal, -1, a.reasoningFor)
	if len(rows) != 1 {
		t.Fatalf("filtered to %d rows, want the one", len(rows))
	}
	// The tail is painted separately from the label: the row's ink is the id,
	// and everything after it is the note. Colour is asserted here because
	// colour is the subject.
	//
	// UNDER THE CURSOR THE NOTE IS INK, not dim, and that is the whole-row
	// highlight (palette.go's [overlayRow]): the selected row is one emphasised
	// band from lead to note, and a dim tail inside it would be grey on grey —
	// the three facts a person is comparing, greyed out on the one row they are
	// comparing them on. Off the cursor the tail is dim, which is asserted below.
	id, note := "anthropic/claude-sonnet-4.5", "$3/$15 per M · 1M · elo 1243"
	if !strings.Contains(rows[0], a.pal.ink(note)) {
		t.Fatalf("the selected row's tail is not inside the band:\n%q", rows[0])
	}
	if strings.Contains(rows[0], a.pal.dim(id)) {
		t.Fatalf("the id under the cursor must not be dim:\n%q", rows[0])
	}

	// A row the cursor is not on keeps the dim tail: that contrast is what makes
	// the band read as a selection rather than as the list's ordinary paint.
	drive(t, a, key("ctrl+u"))
	rows = a.pick.rows(a.width, len(a.pick.hits), a.pal, -1, a.reasoningFor)
	if !strings.Contains(rows[1], a.pal.dim("$0.08/$0.15 per M · 128k")) {
		t.Fatalf("an unselected row's tail has to be dim:\n%q", rows[1])
	}
}

// ctrl+t and not t: the filter box takes every printable key, so a bare t would
// cost the list the letter every third model id contains.
func ctrlT() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl} }

func TestCtrlTCyclesTheLevelAndSkipsModelsThatTakeNone(t *testing.T) {
	agent := &fakeAgent{model: "moonshotai/kimi-k3"}
	a := pickerApp(t, agent, richCatalog)
	typeLine(t, a, "/model")
	typeInto(t, a, "sonnet")

	want := []string{"low", "medium", "high", ""}
	for _, level := range want {
		drive(t, a, ctrlT())
		if got := agent.ReasoningFor("anthropic/claude-sonnet-4.5"); got != level {
			t.Fatalf("the cycle reached %q, want %q", got, level)
		}
	}

	// The level shows on the row, beside the id, while it is set.
	drive(t, a, ctrlT()) // → low
	a.width = 100
	if got := strings.Join(pickerLines(a), "\n"); !strings.Contains(got, "anthropic/claude-sonnet-4.5:low") {
		t.Fatalf("the row has to carry the level:\n%s", got)
	}

	// A model whose catalog row accepts no reasoning knob is not offered one:
	// ctrl+t on it changes nothing at all.
	drive(t, a, key("ctrl+u"))
	typeInto(t, a, "gpt-4.1-mini")
	drive(t, a, ctrlT())
	if got := agent.ReasoningFor("openai/gpt-4.1-mini"); got != "" {
		t.Fatalf("a model that takes no reasoning knob was dialled to %q", got)
	}
	if got := plain(frame(a)); strings.Contains(got, "gpt-4.1-mini:") {
		t.Fatalf("an unsupported row must draw no level:\n%s", got)
	}
}

func TestTheLevelIsPerModelAndSurvivesASwitchAwayAndBack(t *testing.T) {
	agent := &fakeAgent{model: "moonshotai/kimi-k3"}
	a := pickerApp(t, agent, richCatalog)

	typeLine(t, a, "/model")
	typeInto(t, a, "sonnet")
	drive(t, a, ctrlT()) // low
	drive(t, a, ctrlT()) // medium
	drive(t, a, key("enter"))
	if agent.model != "anthropic/claude-sonnet-4.5" {
		t.Fatalf("model is %q, want the sonnet row", agent.model)
	}
	// The level is held against THIS model id, and the sheet — where an id is
	// recorded whole — spells it as `<id>:<level>` (statusdeck.go).
	//
	// THE SEAM DOES NOT, SINCE 2026-09-09. That line carries one thinking rung
	// and it is the RESOLVED one, which a level dialled here is folded into
	// (foot.go's [app.seamIdentity], effortchip.go). A colon there said only this
	// scope while the cell beside it said the answer, which was one ladder spelled
	// two ways on one line.
	if got := agent.ReasoningFor("anthropic/claude-sonnet-4.5"); got != "medium" {
		t.Fatalf("the level is %q on the model it was dialled on, want medium", got)
	}
	if got := plain(frame(a)); strings.Contains(got, "claude-sonnet-4.5:medium") {
		t.Fatalf("the seam still spells a level onto the model id:\n%s", got)
	}

	// Away: the other model has a level of its own, which is none.
	typeLine(t, a, "/model moonshotai/kimi-k3")
	if got := agent.ReasoningFor("moonshotai/kimi-k3"); got != "" {
		t.Fatalf("a model nobody dialled carries %q", got)
	}
	got := plain(frame(a))
	if strings.Contains(got, "kimi-k3:") {
		t.Fatalf("a model nobody dialled must show no level:\n%s", got)
	}
	if strings.Contains(got, ":medium") {
		t.Fatalf("one model's level must not travel to another:\n%s", got)
	}

	// And back: the level was the sonnet's, and it is still the sonnet's.
	typeLine(t, a, "/model anthropic/claude-sonnet-4.5")
	if got := agent.ReasoningFor("anthropic/claude-sonnet-4.5"); got != "medium" {
		t.Fatalf("the level has to survive a switch away and back: %q", got)
	}
	// And the picker's own row, which is where an id and its level are read
	// together, says so.
	typeLine(t, a, "/model")
	typeInto(t, a, "sonnet")
	settleLevels(a, "anthropic/claude-sonnet-4.5")
	if got := strings.Join(pickerLines(a), "\n"); !strings.Contains(got, "anthropic/claude-sonnet-4.5:medium") {
		t.Fatalf("the row lost the level across the switch:\n%s", got)
	}
}

func TestPricesReadPerMillionToTwoFigures(t *testing.T) {
	cases := []struct {
		prompt, completion float64
		want               string
	}{
		{0.00000008, 0.00000015, "$0.08/$0.15 per M"},
		{0.000003, 0.000015, "$3/$15 per M"},
		{0.000_000_15, 0.000_000_6, "$0.15/$0.6 per M"},
		{0.000153, 0.000153, "$150/$150 per M"}, // two figures, not three
		{0, 0.000015, ""},                       // half a price is no price
		{0.000003, 0, ""},
		{0, 0, ""},
	}
	for _, c := range cases {
		if got := priceWord(c.prompt, c.completion); got != c.want {
			t.Fatalf("priceWord(%v, %v) = %q, want %q", c.prompt, c.completion, got, c.want)
		}
	}
}

func TestTheNoteLeavesOutWhatNobodyPublished(t *testing.T) {
	cases := []struct {
		model Model
		want  string
	}{
		{Model{ID: "a", ContextLength: 128_000}, "128k"},
		{Model{ID: "b", ArenaElo: 1243.4}, "elo 1243"},
		{Model{ID: "c"}, ""},
		{
			Model{ID: "d", ContextLength: 1_000_000, PromptPrice: 0.000003, CompletionPrice: 0.000015, ArenaElo: 1300},
			// The tail is RANKED, not catalog order: the price a person pays
			// every turn stands in front of the window they meet once a week
			// (models.go's modelFields states the whole hierarchy).
			"$3/$15 per M · 1M · elo 1300",
		},
	}
	for _, c := range cases {
		if got := modelNote(c.model); got != c.want {
			t.Fatalf("modelNote(%+v) = %q, want %q", c.model, got, c.want)
		}
	}
}
