package composer

import (
	"unicode"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The `@` grammar's data half (5.18): what can be addressed, and what counts
// as a mention token in the draft.
//
// # Mentions are derived, never bookkept
//
// A mention is a PURE FUNCTION of (draft text, current targets): the span
// `@word` at a word boundary whose word is exactly some target's Word. Nothing
// here maintains offsets across edits, and that is the whole design.
//
// The alternative — recording a span at completion time and shifting it on
// every insert and delete — has to answer a question for every edit shape
// (typing inside the token, deleting half of it, pasting over it, recalling a
// ring entry, restoring a stash), and each answer is a place the span and the
// text can disagree. Derivation cannot disagree with the text, because the text
// is the only input. It also buys three of this lane's requirements for free:
// a mention survives the esc stash and the recall ring (they carry the text,
// and the text is the token), a token that is typed by hand is the same token
// as one that was completed, and a target that disappears from the rail stops
// being addressable the moment it does — the affordance cannot outlive the
// thing it addresses (5.20, 12.5).
//
// The cost is a scan of the draft per edit. It is bounded by the draft (a chat
// message, not a file), it is skipped outright when the draft holds no '@',
// and it appends into a reused slice, so a mention-free draft — the
// overwhelmingly common one — pays one byte comparison per rune and allocates
// nothing.

// Target is one thing the `@` filter can address: a live task, or — under the
// dim history group — a settled one. The wiring supplies these through
// [Options.Targets]; this package never discovers a task on its own.
type Target struct {
	// ID is the stable identifier a [Dispatch] names. It is what the wiring
	// routes on; nothing here parses it.
	ID string
	// Word is the short task word the mention completes to, WITHOUT the '@'
	// (e.g. "wisp-parity"). It is the token's text, so it must not contain
	// whitespace — a Word with a space in it can never be matched back out of
	// the draft and is skipped.
	Word string
	// Title is the one-line label shown beside the word in the filter, and the
	// second field the fuzzy query runs over (5.18: "fuzzy over task word +
	// title").
	Title string
	// Seed picks the identity hue (5.16) off the eight-hue wheel. Zero means
	// "derive it from ID", which is what [tokens.IdentityFor] does, so a wiring
	// that has not yet threaded the rail's own assignment through still gets
	// stable, distinct hues rather than eight rows of one colour.
	Seed uint64
	// Attention marks a target waiting on a human: it renders with the amber
	// `?` (5.16, 5.17) instead of its state glyph.
	Attention bool
	// Settled marks a target whose thread is closed. Settled targets are NEVER
	// direct-dispatch targets (5.18): they sort under the dim history group,
	// their chip says "about" rather than "→", and the [Dispatch] they produce
	// carries Settled so the wiring routes it to the main head as referenced
	// context. The composer never journals and never routes; it only refuses to
	// let the affordance lie about which of the two is happening.
	Settled bool
}

// hue is the target's identity token (5.16).
func (t Target) hue() tokens.Token {
	if t.Seed != 0 {
		return tokens.Identity(int(t.Seed % tokens.IdentityCount))
	}
	if t.ID == "" {
		return tokens.IdentityFor(t.Word)
	}
	return tokens.IdentityFor(t.ID)
}

// glyphToken is the pair (glyph, colour) a target's state renders as: amber `?`
// when it wants a human (5.16 gives that hue exactly one meaning), a dim ✓ when
// it is settled, and the working glyph in the target's own identity hue while
// it is alive.
func (t Target) glyphToken() (string, tokens.Token) {
	switch {
	case t.Attention:
		return tokens.GlyphNeedsHuman, tokens.Amber
	case t.Settled:
		return tokens.GlyphSettled, tokens.TextTertiary
	default:
		return tokens.GlyphWorking, t.hue()
	}
}

// wordToken is the tier a target's word is written in. Live words sit at the
// speech tier so the fuzzy highlight has somewhere to brighten to; settled
// words sit one tier lower, which is the history group's whole visual claim.
// Neither is the identity hue: hues never colourize running text (5.16), and a
// hue cannot be promoted, so a hue-painted word could not show its match chars.
func (t Target) wordToken() tokens.Token {
	if t.Settled {
		return tokens.TextTertiary
	}
	return tokens.TextSecondary
}

// mention is one `@word` token found in the draft.
type mention struct {
	start  int // rune index of the '@'
	length int // runes, '@' included
	target Target
}

// end is the rune index one past the token's last rune.
func (mn mention) end() int { return mn.start + mn.length }

// syncMentions recomputes the draft's mention spans. Every edit path calls it
// through [Model.afterEdit]; nothing else may write m.mentions.
func (m *Model) syncMentions() {
	m.mentions = m.mentions[:0]
	if m.targets == nil || !m.draftHasAt() {
		return
	}
	list := m.targets()
	if len(list) == 0 {
		return
	}
	for i := 0; i < len(m.value); i++ {
		if m.value[i] != '@' || !mentionBoundary(m.value, i) {
			continue
		}
		best, bestLen := -1, 0
		for k := range list {
			// Longest word wins, so "@wisp-parity" is never read as the
			// shorter target "wisp" with three stray runes after it.
			if n := matchWordAt(m.value, i+1, list[k].Word); n > bestLen {
				best, bestLen = k, n
			}
		}
		if best < 0 {
			continue
		}
		m.mentions = append(m.mentions, mention{start: i, length: bestLen + 1, target: list[best]})
		i += bestLen // the loop's own i++ steps past the token's last rune
	}
}

// draftHasAt is the cheap gate in front of [Model.syncMentions]: a draft with
// no '@' in it cannot hold a mention, and asking the wiring for its target list
// to discover that would be the composer spending the rail's work on a
// keystroke that could not possibly matter.
func (m *Model) draftHasAt() bool {
	for _, r := range m.value {
		if r == '@' {
			return true
		}
	}
	return false
}

// firstMention is the addressed target of a send. A draft may name several
// tasks in its prose, but a dispatch has exactly one destination (5.18: "the
// user has already done the routing"), and the first one typed is the one the
// user routed to — every later `@word` reads as prose about a task, which is
// what it looks like on the screen too.
func (m *Model) firstMention() (mention, bool) {
	if len(m.mentions) == 0 {
		return mention{}, false
	}
	return m.mentions[0], true
}

// mentionEndingAt finds the token whose last rune sits just before pos, which
// is what makes backspace atomic: one keypress against the end of `@wisp-parity`
// removes the token, not the "y".
func (m *Model) mentionEndingAt(pos int) (mention, bool) {
	for _, mn := range m.mentions {
		if mn.end() == pos {
			return mn, true
		}
	}
	return mention{}, false
}

// mentionStartingAt is the forward-delete counterpart: delete with the cursor
// on the '@' removes the whole token. Symmetry is not decoration here — a token
// that is atomic in one direction and shreddable in the other is a token the
// user cannot form a rule about (5.20).
func (m *Model) mentionStartingAt(pos int) (mention, bool) {
	for _, mn := range m.mentions {
		if mn.start == pos {
			return mn, true
		}
	}
	return mention{}, false
}

// mentionBoundary reports whether the '@' at index i opens a mention: an '@'
// mid-word is an email address or a handle in prose, not an address.
func mentionBoundary(value []rune, i int) bool {
	return i == 0 || unicode.IsSpace(value[i-1])
}

// matchWordAt returns the rune length of word if value holds it at index at and
// the token ends there, or 0. "Ends there" means the next rune is not a word
// rune, so "@wisp" does not match inside "@wispy" while "@wisp-parity," still
// matches its target — punctuation closes a token, more letters do not.
func matchWordAt(value []rune, at int, word string) int {
	if word == "" {
		return 0
	}
	n := 0
	for _, r := range word {
		if unicode.IsSpace(r) {
			return 0 // a word with whitespace can never be read back out
		}
		if at+n >= len(value) || value[at+n] != r {
			return 0
		}
		n++
	}
	if at+n < len(value) && isWordRune(value[at+n]) {
		return 0
	}
	return n
}

// isWordRune is the token alphabet: what a task word may be made of, and
// therefore what may not follow one.
func isWordRune(r rune) bool {
	return r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
