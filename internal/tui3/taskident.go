package tui3

import (
	"strings"
	"time"
)

// A TASK HAS A NAME, AND THE NAME IS NOT ITS TYPE.
//
// Until this wave a node was drawn as whatever sentence the engine happened to
// put in its Title, at whatever length the model wrote it, with a state glyph
// in front. That is enough for one node and it falls apart at three: the rail
// becomes a column of half-sentences that all begin with the same verb, the
// notes in the transcript say "task" four times, and a person tracking which of
// them is the one they care about has to read every row every time.
//
// So a node gets an IDENTITY, and it is three things, all of them stable for
// the node's whole life:
//
//   - a TITLE of two or three words. Not a summary, not the first line of the
//     brief: a NAME, short enough that the eye takes it as one token rather
//     than reading it. It is cut from the engine's own title because that is
//     the model's own name for the work; the assignment is the fallback, and an
//     id is the floor, because a row that cannot say what it is must still say
//     which it is.
//   - a SUBTITLE of one line — the first sentence of the assignment, capped.
//     It answers "and what is that" once, where there is width for it (the
//     cards), and it is deliberately not on the rail, which is 24 columns wide
//     and is a presence list rather than a page.
//   - a GLYPH and a HUE, both keyed on the id and on nothing else. This is the
//     part a person actually navigates by: ◆ teal is task 3 in the rail, in the
//     note, and on the card that lands eleven minutes later, and it is ◆ teal
//     on every redraw and every resume because the id is the only thing that
//     went into it. Nothing about the node's STATE reaches this pair — state is
//     the state glyph's job, and an identity that changed when the work changed
//     would be an identity that cannot be used to follow the work.
//
// The hue is the identity ring (styles.go's [taskRing]), which is spent on the
// glyph cell alone. See that comment for why a colour with no meaning is
// allowed on a surface whose whole colour law is that colour means something.

// taskIdent is one node's identity: the cell it is drawn with, that cell's
// stand-in where there is no unicode, and which of the ring's hues it takes.
type taskIdent struct {
	glyph, ascii string
	tint         int
}

// taskGlyphs is the identity alphabet: four shapes, filled and hollow, which is
// eight marks a person can tell apart at a glance in one column. They are
// SHAPES rather than letters on purpose — a letter beside a title reads as part
// of the title, and the one job of this cell is to be read without being read.
var taskGlyphs = [...]string{"◆", "◇", "●", "○", "■", "□", "▲", "△"}

// taskGlyphsASCII is the same alphabet for a terminal with no unicode and for
// the linear tier. None of them is a mark this surface already spends: `*`,
// `o`, `.`, `x`, `?` and `+` all mean something else within a few rows of here
// (styles.go's stand-ins, the diffstat, the question), and an identity that
// collided with a state would be worse than no identity at all.
var taskGlyphsASCII = [...]string{"#", "@", "%", "&", "$", "~", "=", "!"}

// identFor derives a node's identity from its id, and from nothing else.
//
// The hash is splitmix64's finalizer, which is here for one reason: task ids
// are SEQUENTIAL. Taking `id % 8` would give the eight glyphs out in order and
// two tasks proposed one after the other would always be neighbours in the
// ring; the finalizer is the cheapest function that turns 1, 2, 3 into three
// unrelated numbers. The glyph is the low end of the hash and the tint the next
// bits up, so the two vary independently and 48 pairs are reachable.
func identFor(id uint64) taskIdent {
	h := taskHash(id)
	at := int(h % uint64(len(taskGlyphs)))
	tint := 0
	if n := len(taskRing); n > 0 {
		tint = int((h / uint64(len(taskGlyphs))) % uint64(n))
	}
	return taskIdent{glyph: taskGlyphs[at], ascii: taskGlyphsASCII[at], tint: tint}
}

func taskHash(id uint64) uint64 {
	h := id + 0x9E3779B97F4A7C15
	h ^= h >> 30
	h *= 0xBF58476D1CE4E5B9
	h ^= h >> 27
	h *= 0x94D049BB133111EB
	h ^= h >> 31
	return h
}

// taskMark is one identity, painted: the glyph in its hue, or its stand-in on a
// terminal that cannot draw it. It is one cell wide in every tier, which is
// what lets every row that carries one measure itself the same way.
func (a *app) taskMark(ident taskIdent) string {
	glyph := ident.glyph
	if a.pal.ascii || a.linear {
		glyph = ident.ascii
	}
	if glyph == "" {
		// The zero identity — a node built before one was derived. A space keeps
		// the column, which is the thing the rows around it are aligned to.
		return " "
	}
	return a.pal.markPaint(ident.tint, glyph)
}

// taskMarkSel is the same cell on a row the keyboard has picked.
//
// SELECTION IS A BRIGHTNESS, which is the tool line's own law for it
// (toolview.go: no band, no marker column, nothing that changes the width) —
// applied here to the one cell every task row has. It is spent on the identity
// rather than on the state mark beside it because the state mark may be a
// FAILURE, and a cursor that recoloured a failure would be the surface losing a
// fact to say where the cursor is.
func (a *app) taskMarkSel(ident taskIdent, sel bool) string {
	if !sel {
		return a.taskMark(ident)
	}
	return a.pal.bold(a.taskMark(ident))
}

// The two caps on a name.
const (
	// taskTitleWords is how long a name is allowed to be. Three is the length a
	// person reads as a label rather than as a sentence — "Fix nil-map crash",
	// "Collect the sources" — and it is a cap and not a target: a two-word title
	// is left at two.
	taskTitleWords = 3
	// taskSubtitleMax is the subtitle's width in cells. Ninety is about a line of
	// prose at the width the cards are drawn at; past it the "one line" promise
	// is being kept by the wrapper rather than by the sentence.
	taskSubtitleMax = 90
)

// taskTitleOf is the NAME: the first few words of the engine's own title, or of
// the assignment when there is no title, or the id when there is neither.
//
// The id floor matters more than it looks. A node whose title never arrived is
// exactly the node a person is most likely to be trying to identify — something
// went wrong early — and "task 7" is a name they can say out loud, ask about,
// and match against the rail. An empty string is not.
func taskTitleOf(label, assignment string, id uint64) string {
	if name := firstWords(label, taskTitleWords); name != "" {
		return name
	}
	if name := firstWords(leadSentence(assignment), taskTitleWords); name != "" {
		return name
	}
	return taskIDWord(id)
}

// taskIDWord is the LAST-RESORT name: the word a person uses for a node nobody
// has told this surface the name of.
//
// IT IS ONE FUNCTION BECAUSE IT IS ALSO A QUESTION. Two places make this string
// — the name above and a room opened on a node with no name yet (room.go) — and
// one place has to be able to ASK whether a name it is holding is really just
// this ([app.taskUpdate] refreshes a room's header when the node's real name
// finally arrives). A second spelling of it would be a header that never
// noticed.
func taskIDWord(id uint64) string { return "task " + itoa(int(id)) }

// taskSubtitleOf is the one line under a name: the first sentence of what the
// node was asked to do, capped.
//
// It answers nothing when it would only repeat the title, which is the common
// case for a short engine title — a card that said "Fix nil-map crash" twice,
// once in ink and once in dim, would be spending a row on nothing.
func taskSubtitleOf(title, assignment string) string {
	sentence := fit(leadSentence(assignment), taskSubtitleMax)
	if sentence == "" || strings.EqualFold(strings.TrimRight(sentence, "."), strings.TrimRight(title, ".")) {
		return ""
	}
	return sentence
}

// firstWords cuts a phrase to its first n words and drops the punctuation that
// ended the sentence it came out of. The trailing cut is deliberately narrow —
// a full stop, a comma, a colon — because a title ending in `)` or `"` ends
// that way for a reason.
func firstWords(text string, n int) string {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return ""
	}
	if len(fields) > n {
		fields = fields[:n]
	}
	return strings.TrimRight(strings.Join(fields, " "), ".,:;")
}

// leadSentence is the sentence a person reads to know what this is.
//
// A NEWLINE ENDS A SENTENCE HERE, ahead of any full stop. The assignments this
// reads are written by a model into a field it knows is a summary, and the
// shape they arrive in is a lead line followed by detail — so the first line is
// the sentence far more reliably than the first period is. Within that line the
// terminator is the ordinary one, and an abbreviation in it ("e.g. the parser")
// will cut early: that is the known cost of not shipping a sentence tokenizer
// for a field that is capped at ninety cells anyway.
func leadSentence(text string) string {
	text = strings.TrimSpace(text)
	if line, _, found := strings.Cut(text, "\n"); found {
		text = strings.TrimSpace(line)
	}
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '.', '!', '?':
		default:
			continue
		}
		if rest := text[i+1:]; rest == "" || strings.HasPrefix(rest, " ") {
			return strings.TrimSpace(text[:i+1])
		}
	}
	return text
}

// taskSpanWord spells how long a task took, in the completion card's own
// spelling: `47s`, `4m12s`, `1h04m`.
//
// It is NOT [countUpWord], and the difference is the space. That figure is read
// while it moves and is spaced so the eye takes it as two parts; this one is a
// finished measurement sitting in a row of other finished facts, where it is
// read as one token and a space inside it would make it two.
func taskSpanWord(d time.Duration) string {
	seconds := int(d.Round(time.Second) / time.Second)
	if seconds < 0 {
		seconds = 0
	}
	switch {
	case seconds < 60:
		return itoa(seconds) + "s"
	case seconds < 3600:
		return itoa(seconds/60) + "m" + pad2(seconds%60) + "s"
	default:
		return itoa(seconds/3600) + "h" + pad2(seconds%3600/60) + "m"
	}
}
