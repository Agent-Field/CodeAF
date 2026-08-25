package tui3

import (
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/session"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// placeMoneyInk is the one semantic door onto money's ink, so a palette move
// cannot leave one reading behind with the old meaning.
func placeMoneyInk(pal palette) func(string) string { return pal.add }

// ── the standing vocabulary ─────────────────────────────────────────────────
//
// EVERY WORD A PERSON READS ABOUT A STANDING ORDER IS SPELLED ONCE, HERE. The
// same facts are said by four surfaces — the ratification card in the
// transcript (standing.go), the shelves of the standing place (standingplace.go),
// the column down the right (margin.go) and home's own item rows
// (homestanding.go) — and a fact spelled twice is a fact that will drift the
// first time one of the four is edited. Each of them is quoted in
// internal/manual/chat/standing-orders.md exactly as it is spelled here.
const (
	// standHeading is the standing place's one sentence, drawn above the shelves.
	// It NAMES the page and counts nothing: a heading with a tally beside it
	// would be the screen counting what a person can already see.
	standHeading = "standing orders"
	// The three shelves, in the order they are drawn.
	standInHereWord     = "in this conversation"
	standProjectWord    = "for this project"
	standEverywhereWord = "everywhere"
	// standOtherWord heads the FOURTH shelf: what this machine holds that does
	// not reach the conversation the person is sitting in.
	//
	// IT IS A PLACE AND NOT A REACH, which is why it is not one of the three
	// above it. The three name how far an order was agreed to reach; this one
	// names where the orders under it live, because that is the only thing they
	// have in common — some are one project's, some are another conversation's,
	// and all of them are somewhere else. And it is said in a person's words:
	// `elsewhere on this machine` would put the machinery's own word for the
	// widest reach onto a heading that does not mean it.
	standOtherWord = "in other projects"
	// standJustHereWord is the conversation reach as the RATIFICATION CARD says
	// it, and it is deliberately not [standInHereWord]. A shelf heading names
	// the place a person is standing and files rows under it; the card names how
	// far the thing they are about to agree to will reach, and "in this
	// conversation" on a card reads as where the order was said rather than as
	// the whole of what it will govern.
	standJustHereWord = "just this conversation"
	// standJustHereTag is the conversation reach as a ROW'S TAIL says it, and it
	// is a third spelling of one fact for the reason [standJustHereWord] is a
	// second. A shelf heading files rows under a place and a card names how far
	// something will reach; a tail is two or three cells at the end of a row in a
	// column twenty-eight wide (margin.go), where `just this conversation` is the
	// whole row and `in this conversation` is most of it. What is left is the
	// half that carries the meaning: just here.
	standJustHereTag = "just here"
	// standWhereTag is the card's third band label, in the grammar of the two
	// beside it ([standWhenTag], [standCostTag]): a lower-case noun and not a
	// heading, because a card in a conversation with a heading on every row is a
	// form.
	standWhereTag = "where · "
	// standNotHereWord is the exception, said in the same three words wherever
	// it appears: the dim line under a shelf, the verb on the row's strip, and
	// the receipt for the key that wrote it.
	standNotHereWord = "not here"
	// standNothingWord is /standing on a machine nothing stands on at all. It is
	// SAID rather than dropped, for [filesNothingWord]'s reason — silence after
	// a deliberate command reads as a command that broke — and no page is opened
	// behind it, for that list's other reason: a place with no rows is a screen
	// that has to be dismissed before it can be told it was useless.
	standNothingWord = "nothing stands here yet — say what should always be true, and I'll hold it."
	// standHereWord is enter on the order this very conversation asked for.
	// There is a door and it leads exactly where the person already is, so the
	// page says the fact instead of moving them nowhere.
	standHereWord = "you are already in it"
	// standResumedWord is what a paused order that has been started again is
	// called, on the place's receipt and on the one line of news in the
	// transcript ([standUpdateWord] says the same word about the same event).
	standResumedWord = "going again"
	// standResumeWord is that same event as a VERB on the strip — what the key
	// will do rather than what it did. [standResumedWord] is the receipt and
	// reads as a report ("going again · draft the weekly update"), which is the
	// wrong half of the sentence to offer somebody a key with.
	standResumeWord = "start again"
	// standNotOursWord is what the conversation's own three verbs say if one is
	// ever asked of an order in another project. The strip does not offer them
	// there ([standPage.verbs]), so this is the second lock on the same door —
	// and it is a SENTENCE rather than a silent return, because a key that did
	// nothing and said nothing is indistinguishable from a key that is broken.
	standNotOursWord = "that one does not stand over this conversation"
)

// placeWindowStep is SCREEN 3d'S FOUR KEYS, and it is one function because
// there is one answer.
//
// Time is two dimensions — which window, and how coarse — so it gets two arrow
// axes rather than three letters: `shift+←→` pages the window by its own length
// and `shift+↑↓` zooms the grain. Every place that has a window answers exactly
// these four and no others, and a place that wrote its own switch would be a
// fourth chance for one of them to disagree about what `shift+↑` means. The
// arithmetic itself is [session.UsageWindow]'s, which is the one window
// vocabulary this surface has.
//
// A KEY THAT IS NOT ONE OF THE FOUR ANSWERS THE WINDOW IT WAS GIVEN, so a caller
// can compare and learn whether anything actually moved — which is what lets a
// place refuse to redraw for a keystroke that changed nothing.
func placeWindowStep(win session.UsageWindow, key string) session.UsageWindow {
	switch key {
	case "shift+left":
		return win.Step(-1)
	case "shift+right":
		return win.Step(1)
	case "shift+up":
		return win.Coarser()
	case "shift+down":
		return win.Finer()
	}
	return win
}

// The window control as screen 3d draws it: the label BETWEEN THE ARROWS, which
// is the control and the reading at once. A person reads the span they are
// looking at and the keys that move it in one glance, on the row that reports
// it, rather than learning a chord from a foot note somewhere else.
//
// A WINDOW WITH NO SPAN DRAWS NOTHING — not the arrows, not an empty pair of
// them. [session.UsageWindow.Label] answers "" for a window nobody has chosen
// yet, and arrows around nothing would be a control over no reading.
const (
	placeWindowBack = "shift+← "
	placeWindowOn   = " →"
)

// placeWindowWords is the control as PLAIN TEXT, which is what a caller measures
// against the frame it has. It is separate from the painted form because a
// string with escape sequences in it cannot be clipped or counted safely, and a
// header deciding whether the control fits has to do both.
func placeWindowWords(win session.UsageWindow) string {
	label := win.Label()
	if label == "" {
		return ""
	}
	return placeWindowBack + label + placeWindowOn
}

// placeWindowRow is that same control, painted: the arrows dim because they are
// the instruction, the label in the reading tier because it is the fact.
func placeWindowRow(win session.UsageWindow, pal palette) string {
	label := win.Label()
	if label == "" {
		return ""
	}
	return pal.dim(placeWindowBack) + pal.ink(label) + pal.dim(placeWindowOn)
}

// foldLine keeps every collapsed count in one sentence grammar. A clause is
// already prose and therefore follows the count after one comma.
func foldLine(n int, clause string) string {
	line := tokens.GlyphCollapsed + " " + groupedInt(n) + " more"
	if clause = strings.TrimSpace(strings.TrimPrefix(clause, ",")); clause != "" {
		line += ", " + clause
	}
	return line
}

// appendPlaceSection gives consecutive blocks exactly one breath without
// growing a second blank when two callers describe the same boundary.
func appendPlaceSection(rows []string, heading string) []string {
	for len(rows) > 0 && rows[len(rows)-1] == "" {
		rows = rows[:len(rows)-1]
	}
	if len(rows) > 0 {
		rows = append(rows, "")
	}
	return append(rows, heading)
}

// groupedInt is the one thousands spelling for reading-layer counts.
func groupedInt(n int) string {
	plain := strconv.Itoa(n)
	for at := len(plain) - 3; at > 0; at -= 3 {
		plain = plain[:at] + "," + plain[at:]
	}
	return plain
}
