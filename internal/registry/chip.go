package registry

// The verb·key chip — one grammar for every surface that draws an action word
// beside the key that reaches it (design-law-v2 §16).
//
// THE RULE: the VERB comes first and carries the brighter tier; the KEY follows
// it, one tier down. `close esc`, `open ⏎`. Never a bare key beside a verb as
// two unmarked words.
//
// The bug that named the rule was a settings sheet whose hint read `esc close`.
// Two greys, two words, and nothing in the row saying which one is the label and
// which one is the thing to press — so a reader parses it by already knowing the
// answer, which is the definition of the memory test 5.22 exists to remove.
// Verb-first fixes it structurally rather than by explanation: the word that
// names the act is the word the eye lands on, and the key reads as annotation
// because it is drawn as one.
//
// WHAT THIS PACKAGE OWNS AND WHAT IT DOES NOT. There is no paint here and there
// must not be: this package is pure data and the dependency runs one way,
// tui → registry (see the package doc). A Styler, a token or a lipgloss style
// arriving in this file would drag a terminal renderer behind every consumer of
// the catalog, including the future web surface.
//
// What WAS being duplicated is the part that lives here: the ORDER of the two
// words, and the FALLBACK LADDER that decides what the key half says at all —
// the key, else the slash alias, else nothing. internal/tui2/footer's verbLabel,
// internal/tui2/palette's accelOf and internal/tui2/chat's teachKey were three
// copies of that ladder, and two of them had already drifted into key-first.
// One ladder, three painters, each spending its own two tiers on the two halves
// it is handed.

// Chip is one action word and the accelerator that reaches it, in the order
// every surface draws them.
//
// The whole chip is the click target — a reader points at the words, not at the
// key — but only the verb is drawn at the tier that says so.
type Chip struct {
	// Verb is the action word. It is drawn FIRST, at the brighter of the
	// surface's two tiers, and it is never dropped: a chip that cut its verb to
	// spell its key would be teaching a keystroke with no name on it.
	Verb string
	// Key is the accelerator, drawn after the verb and one tier down. EMPTY IS
	// A REAL ANSWER, not a gap — a belt-only verb is reached through the user's
	// own words and has no key by construction — so a painter must test it
	// rather than assume the chip has two halves.
	Key string
}

// ChipGap is the single space between the two halves. It is named because the
// painters draw the halves in separate calls and a gap that lived in each of
// them would be a gap that could differ between them.
const ChipGap = " "

// AskKey is what a surface may put in the key half of a verb whose only door is
// prose — a [ScopeTalk] entry, reached by asking. It is offered rather than
// applied by [ChipOn]: a footer with a key column drops such a row, and a
// palette that lists every capability prints the word, and both are right for
// their own surface. Printing nothing in a list that teaches doors would read
// as "no way to do this", which is the opposite of true.
const AskKey = "ask"

// ChipOn is the chip for a registry entry as one surface can honestly draw it.
//
// The key half is [Entry.KeyOn] — so a bare letter resolves to nothing on a
// composer-first surface, where that letter is draft text — then the slash
// alias, then nothing. A caller that wants the prose door named in the empty
// case fills it with [AskKey] itself.
func ChipOn(e Entry, surface Surface) Chip {
	chip := Chip{Verb: e.Verb, Key: e.KeyOn(surface)}
	if chip.Key == "" && e.Slash != "" {
		chip.Key = "/" + e.Slash
	}
	return chip
}

// ChipFor is the chip for an act that is not a catalog row: an overlay's own
// exit, a dialog's confirm. Those are real verb·key pairs on real surfaces and
// they obey the same grammar — the rule is about how a reader parses two words,
// not about where the words came from — so they are built as the same type
// rather than assembled by hand at each call site, which is how `esc close` got
// written three times in the first place.
func ChipFor(verb, key string) Chip { return Chip{Verb: verb, Key: key} }

// Empty reports whether there is nothing to draw. A chip with no verb is
// nothing at all; a chip with no key is a verb, which is a legitimate row.
func (c Chip) Empty() bool { return c.Verb == "" }

// String is the chip as one plain string, in the drawn order. It is what a
// caller measures the chip with — cell width lives in the render tree and may
// not be imported here — and what a plain-text or `--color none` surface can
// print directly.
func (c Chip) String() string {
	if c.Verb == "" {
		return c.Key
	}
	if c.Key == "" {
		return c.Verb
	}
	return c.Verb + ChipGap + c.Key
}
