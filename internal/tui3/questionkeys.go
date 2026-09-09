package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// THE ONE KEY GRAMMAR FOR EVERY QUESTION THIS SURFACE DRAWS.
//
// docs/design/questions/DESIGN.md states it as a law and this file is that law
// as a table: "digits 1–9 pick (letters only where task-states already fixed
// a/n/s); space toggles a checklist; a/b a pair; ←→ a dial; tab the next blank;
// c comment; x compare; ? ask back; d you decide; D decide this kind from now
// on; r make a rule; u undo while real; esc later; o/enter open. Spelled once in
// one table, read by every form and by the manual."
//
// SPELLED ONCE IS THE WHOLE POINT. A question is drawn at four sizes — a line, a
// card, this room, a sheet — and it is also answered from home, from another
// window and from a phone. Every one of those has to offer the SAME key for the
// same act or the grammar is not a grammar, it is six habits; and every one of
// them also has to say the key in the same words, because a row that reads
// `x compare` here and `x differences` on home is two features to the person
// reading them. So the word beside a key lives here too, and a caller asks for a
// row of them rather than writing its own.
//
// WHY IT IS A TABLE AND NOT A SWITCH. The rows a form offers depend on what the
// question actually carries — no dimensions, no compare; no pick, no `enter →`
// line (the emptiness law) — so the offer row is FILTERED from this table by the
// question in front of it. A switch statement cannot be filtered, cannot be
// counted and cannot be read back by a test, and the three things this table has
// to do are exactly those.

// questionAct is what a key DOES, named so the drawing and the routing agree on
// one vocabulary. It is deliberately not a func: the same act means different
// things to the room, the card and the sheet, and a table of closures would have
// to be built per form, which is the duplication this file exists to end.
type questionAct uint8

const (
	// questionActNone is the zero value and is never in the table.
	questionActNone questionAct = iota
	// questionActPick is a digit taking one of the answers.
	questionActPick
	// questionActTake is `enter` on the asker's own pick, and it is offered ONLY
	// where there is one — the emptiness law's clearest case.
	questionActTake
	// questionActDone is the SAME KEY on a question whose answer is a shape
	// rather than a list. It is a second act rather than a second word on the
	// first because the grammar is one word per act: "take the pick" on a run of
	// pairs would name something the person is not being offered.
	questionActDone
	// questionActToggle is `space` on a checklist row.
	questionActToggle
	// questionActSuggest is `a` on a checklist: take the asker's suggestion
	// whole, which is the answer most checklists actually get.
	questionActSuggest
	// questionActOrderUp and questionActOrderDown move a checklist row where the
	// ORDER is part of the answer.
	questionActOrderUp
	questionActOrderDown
	// questionActPairA and questionActPairB answer one pair; questionActSame is
	// the third answer a pair always has, and the one a forced choice loses.
	questionActPairA
	questionActPairB
	questionActSame
	// questionActDialDown and questionActDialUp move a dial one notch.
	questionActDialDown
	questionActDialUp
	// questionActNextBlank and questionActPrevBlank walk the holes in a sentence.
	questionActNextBlank
	questionActPrevBlank
	// questionActComment annotates the focused part — an option, a blank, a pair.
	questionActComment
	// questionActCompare lays the answers out against each other.
	questionActCompare
	// questionActAskBack puts one question to the asker with this one still open.
	questionActAskBack
	// questionActDecide hands this one decision back, and questionActDecideKind
	// hands back every decision of its shape from now on.
	questionActDecide
	questionActDecideKind
	// questionActRule offers to make the answer standing.
	questionActRule
	// questionActUndo takes an answer back while taking it back is still real.
	questionActUndo
	// questionActReframe is "none of these — the real question is…".
	questionActReframe
	// questionActLater folds the question to the chip. The turn or the task stays
	// paused on it; nothing is answered and nothing is lost.
	questionActLater
	// questionActOpen promotes the form one size: chip → line → card → room.
	questionActOpen
	// questionActFold walks the option sections shut and open.
	questionActFold
)

// questionSep is the separator every telemetry row on this surface writes, and
// every row a question draws writes it too — an offer row that punctuated
// differently from the status line beside it would read as a different program.
const questionSep = " · "

// questionKey is one row of the grammar: the key as [tea.KeyPressMsg.String]
// spells it, the act it runs, and the words the offer row says beside it.
type questionKey struct {
	// key is the string the key press arrives as. A digit is spelled `1–9`
	// rather than as ten rows, because the row a person reads says `1–9` too.
	key string
	// act is what pressing it does.
	act questionAct
	// word is what the offer row says after the key, in the person's own
	// vocabulary and never in the machinery's: `you decide`, never `delegate`.
	word string
}

// questionKeys IS the grammar. The order is the order an offer row spells them,
// which runs from the answer a person is most likely to want to the way out.
var questionKeys = []questionKey{
	{key: "1–9", act: questionActPick, word: "pick"},
	{key: "enter", act: questionActTake, word: "take the pick"},
	{key: "enter", act: questionActDone, word: "done"},
	{key: "space", act: questionActToggle, word: "tick"},
	{key: "a", act: questionActSuggest, word: "take its suggestion"},
	{key: "a", act: questionActPairA, word: "the first"},
	{key: "b", act: questionActPairB, word: "the second"},
	{key: "=", act: questionActSame, word: "same either way"},
	{key: "←→", act: questionActDialUp, word: "move it"},
	{key: "shift+↑↓", act: questionActOrderUp, word: "order them"},
	{key: "tab", act: questionActNextBlank, word: "next blank"},
	{key: "x", act: questionActCompare, word: "compare"},
	{key: "c", act: questionActComment, word: "comment"},
	{key: "?", act: questionActAskBack, word: "ask it"},
	{key: "n", act: questionActReframe, word: "none of these"},
	{key: "d", act: questionActDecide, word: "you decide"},
	{key: "D", act: questionActDecideKind, word: "you decide these"},
	{key: "r", act: questionActRule, word: "make it a rule"},
	{key: "u", act: questionActUndo, word: "undo"},
	{key: "o", act: questionActOpen, word: "open"},
	{key: "esc", act: questionActLater, word: "later"},
}

// questionKeyWord is the word the grammar says beside one act, or "" for an act
// that is not in the table. It is how a form that draws its own row — the
// compare table's foot, the you-decide confirmation — says a key in the same
// words the offer row would.
func questionKeyWord(act questionAct) string {
	for _, k := range questionKeys {
		if k.act == act {
			return k.word
		}
	}
	return ""
}

// questionKeyOf is the same lookup by the pressed key, and it is the whole of
// the routing: a form asks what a press means, gets an act, and switches on the
// act. Two rows share `a` — the checklist's suggestion and a pair's first
// answer — so the caller says which input shape is on screen and gets the one
// that is live. Nothing else in the table collides.
func questionKeyOf(key string, on questionSurface) questionAct {
	if key >= "1" && key <= "9" {
		return questionActPick
	}
	switch key {
	case "left":
		return questionActDialDown
	case "right":
		return questionActDialUp
	case "shift+tab":
		return questionActPrevBlank
	case "shift+up":
		return questionActOrderUp
	case "shift+down":
		return questionActOrderDown
	case "a":
		// THE ONE COLLISION IN THE GRAMMAR, resolved by what is on screen rather
		// than by giving one of them a second key. `a` means "take the asker's
		// suggestion" on a checklist and "the first one" on a pair, and those are
		// the same instinct — take the thing on the left — spelled at two shapes.
		if on.pairs {
			return questionActPairA
		}
		return questionActSuggest
	}
	for _, k := range questionKeys {
		if k.key == key {
			return k.act
		}
	}
	return questionActNone
}

// questionSurface is what is on screen when a key arrives, and it is the ONLY
// context [questionKeyOf] takes. It is a struct rather than a bool so the day a
// second collision appears it is named here rather than threaded through every
// caller.
type questionSurface struct {
	// pairs is true while a this-or-that run is the live input shape.
	pairs bool
}

// questionOffer is the row of keys a question actually offers, built by
// filtering the grammar through what the question carries.
//
// THE EMPTINESS LAW IS ENFORCED HERE AND NOWHERE ELSE (DESIGN.md): "no pick → no
// `enter →` line; no reason → no dim line; no dimensions → no compare". A form
// that decided for itself which keys to name would be a second reading of the
// same object, and the two would disagree the first time a question arrived
// without dimensions.
func questionOffer(acts ...questionAct) []questionKey {
	out := make([]questionKey, 0, len(acts))
	for _, want := range acts {
		for _, k := range questionKeys {
			if k.act == want {
				out = append(out, k)
				break
			}
		}
	}
	return out
}

// questionOfferRow spells an offer as the one row a person reads, with the
// separator every other telemetry row on this surface uses.
//
// The keys are painted in the question hue and the words dim beside them, which
// is the roomapproval.go rule said once more: the KEY is the pressable target
// and the word is what it does, and a row that painted both the same weight
// makes a person read the whole sentence to find the letter.
func questionOfferRow(pal palette, keys []questionKey, width int) string {
	if len(keys) == 0 || width <= 0 {
		return ""
	}
	// THE WAY OUT IS NEVER WHAT GETS CUT, which is roomapproval.go's law about
	// its own row said once more: a row that fits by losing its last entry loses
	// `esc later`, and a question with no visible way to leave it is the one
	// thing a page that promises never to take the keyboard must not draw. So the
	// row gives up entries from the MIDDLE — the first is how you answer and the
	// last is how you leave — until what is left fits.
	shown := keys
	for len(shown) > 2 && ansi.StringWidth(questionOfferPlain(shown)) > width {
		cut := len(shown) / 2
		shown = append(append([]questionKey{}, shown[:cut]...), shown[cut+1:]...)
	}
	var b strings.Builder
	for i, k := range shown {
		if i > 0 {
			b.WriteString(pal.dim(questionSep))
		}
		b.WriteString(pal.ask(k.key) + pal.dim(" "+k.word))
	}
	return fit(b.String(), width)
}

// questionOfferPlain is the same row without paint, which is what a test reads
// and what the width of the row is measured from before it is painted.
func questionOfferPlain(keys []questionKey) string {
	words := make([]string, 0, len(keys))
	for _, k := range keys {
		words = append(words, k.key+" "+k.word)
	}
	return strings.Join(words, questionSep)
}
