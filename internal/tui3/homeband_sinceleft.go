package tui3

// `since you left` — WHAT HAPPENED ACROSS EVERY PROJECT WHILE YOU WERE NOT HERE.
//
// A conversation's own card has this band already ([drawNewsBand]), about the
// one conversation under the cursor. This is the machine's: every project's
// inbox and every conversation's landings in one list, newest first, which is
// the second of the three questions somebody opens this screen to ask.
//
// IT DOES NOT DERIVE THE NEWS ITSELF. [homeView.phoneNotes] already does that
// walk for the phone tier's own `since you left` section, on home's clock and
// behind its own cache, and it already holds the two rules that make a thing
// news: an inbox note is news until the conversation it belongs to drains it
// (internal/session's standing_run.go), and a task is news when it landed after
// the person last spoke in the conversation that ran it — the same derivation
// the `◆` on a standing row makes ([standNews], and home's look stamp in
// session's look.go). A second derivation here would be a second answer to what
// news is, and the day the two disagreed the phone and the desk would be showing
// two different machines.
//
// EVERY ROW IS A DOOR. Pressing one opens the thing it names — the conversation
// the news landed in, or the project whose inbox it was left in — through the
// road the left column's own rows open through (homemachine.go's
// [app.machinePress]).

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func init() {
	registerHomeBand(homeBand{
		name:  "sinceleft",
		order: bandOrderSinceLeft,
		kinds: []bandKind{bandKindMachine},
		draw:  drawSinceLeftBand,
	})
}

// machineNewsWord is the band's heading, spelled exactly as the phone tier
// spells it ([homePhoneNewsWord]) and as the conversation card's own news band
// ends its heading with. One question, one wording, three places.
const machineNewsWord = homePhoneNewsWord

// machineNewsShown is how many pieces of news the band draws before the rest
// fold. Four, because past it a card stops being a shape on the page and becomes
// a list — which is [homeShown]'s reasoning about the column beside it.
const machineNewsShown = 4

func drawSinceLeftBand(a *app, ctx bandContext) []string {
	notes := a.machineFactsAt(ctx.now).news
	if len(notes) == 0 {
		return nil
	}
	glyph := standNewsGlyph
	if ctx.pal.ascii {
		glyph = standNewsASCII
	}
	groups := make([][]string, 0, len(notes))
	for _, note := range notes {
		words := strings.TrimSpace(note.words)
		if words == "" {
			continue
		}
		// THE MARK IS THE ONE COLOURED CELL ON THE ROW, and the hue it wears is
		// the one this surface paints things that LANDED with ([hueAdd]): every
		// row here is work that finished or news that arrived while nobody was in
		// the room, which is the whole of what `◆` claims (standing.go).
		group := bandSidesWithSeparator(ctx.width, 2, standWordsFloor, "· ",
			glyph+" "+words, machineNewsPlace(note),
			machineLeadInk(ctx.pal, glyph, ctx.pal.add), ctx.pal.dim)
		// THE ROW IS RECORDED AS IT WAS DRAWN — clipped to this frame's width,
		// with the paint taken off — so a press finds the note by the text that
		// is actually on the screen rather than by counting rows into a card that
		// drops bands from the bottom ([app.bandFoldAt] finds a fold line the
		// same way).
		for _, row := range group {
			a.noteMachineDoor(ansi.Strip(row), note)
		}
		groups = append(groups, group)
	}
	if len(groups) == 0 {
		return nil
	}
	rows := a.bandFoldPacked(ctx, "sinceleft", groups, machineNewsShown, "things")
	// THE HEADING IS STRUCTURE (homeband_news.go says the whole of why): the
	// accent belongs to the one live thing on the screen, and a band's label is
	// not it.
	return append([]string{ctx.pal.muted(fit(machineNewsWord, ctx.width))}, rows...)
}
