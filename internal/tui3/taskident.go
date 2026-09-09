package tui3

import (
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
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
//   - a MARKER, one cell, the same for every piece of work. It says "this row is
//     a task" and nothing else, and it is the same mark in the note and on the
//     card that lands eleven minutes later. IT IS NOT ON THE RAIL, and that is
//     the marker's own argument applied to one column: the rail holds nothing
//     but tasks, so there is nothing there for "this row is a task" to tell
//     apart, and the two cells are worth more to the name on a surface
//     twenty-four columns wide (task.go's [app.railLead]).
//
// THE MARKER USED TO BE EIGHT SHAPES IN SIX HUES, hashed off the id — a private
// alphabet in which ◆ teal was task 3 and ▲ amber was task 5. The argument was
// that a person navigates by it. What they actually navigate by is the NAME and
// the state, and the alphabet cost more than it paid: it had to be learned, it
// was learned per session because ids restart, it put arbitrary colour on a
// surface whose whole colour law is that colour means something, and it changed
// nothing about the row that a person could act on. Every row that carries it
// already carries the id in a form a person can say out loud (`#3`) and the
// state in its own mark and its own word — which is what tells two rows apart on
// a terminal with no colour at all.
//
// So: ONE MARKER, DRAWN AS FURNITURE. What varies between rows is what differs
// between the work.

// taskIdent is one node's marker: the cell it is drawn with and that cell's
// stand-in where there is no unicode. It is a type rather than a constant
// because every row that draws one holds one, and because the zero value is a
// real case ([app.taskMark] draws it as the space that keeps the column).
type taskIdent struct{ glyph, ascii string }

// The one marker. `◆` is a filled shape rather than a letter — a letter beside a
// title reads as part of the title — and `#` is its stand-in, which is also how
// the tree rows already spell an id, so the two agree rather than collide.
const (
	taskIdentGlyph = "◆"
	taskIdentASCII = "#"
)

// identFor is that marker, for any node. It takes the id because every call site
// has one and because the day this cell means something again, this is where it
// would be decided.
func identFor(uint64) taskIdent {
	return taskIdent{glyph: taskIdentGlyph, ascii: taskIdentASCII}
}

// taskMark is one marker, painted. It is one cell wide in every tier, which is
// what lets every row that carries one measure itself the same way.
//
// IT IS DIM, because it is furniture: it is on every task row, it distinguishes
// none of them from any other, and the ink on those rows belongs to the name and
// to the state. This is where the identity ring used to be spent.
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
	return a.pal.dim(glyph)
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

// The caps on a name.
const (
	// taskTitleWords is how long the ENGINE lets a name be, and it is kept here
	// as a reference and not as a ruler. The namer that makes a piece of work's
	// short name asks for exactly this many words (internal/session's
	// taskname.go), so a name that arrives already short arrived that way — this
	// surface does not cut it a second time, and [taskTitleOf] says why.
	taskTitleWords = session.TaskNameWords
	// taskSubtitleMax is the subtitle's width in cells. Ninety is about a line of
	// prose at the width the cards are drawn at; past it the "one line" promise
	// is being kept by the wrapper rather than by the sentence.
	taskSubtitleMax = 90
)

// taskTitleOf is the NAME: the engine's own title for the work WHOLE, or the
// first sentence of the assignment when there is no title, or the id when there
// is neither.
//
// IT DOES NOT CUT, AND THAT IS THE POINT. Until this wave it answered
// `firstWords(label, taskTitleWords)` — three words, decided here, BEFORE ANY
// WIDTH WAS KNOWN — so a hundred-and-sixty-column room header named the work no
// better than a twenty-four-column rail row did: a family of six pieces that all
// begin with a verb and a plural noun came out as `Cut every list`, `Fold the
// settled`, `Move the tab`, and the room a person opened to find out more told
// them exactly what the column already had. That is [rowfit.go]'s first law
// broken at the earliest possible moment — a fitter cannot give back cells that
// were spent before it was asked — and the answer is the one that file states:
// the identity comes to the row whole and the ROW decides what it can afford
// ([app.roomHeadWord] is the worked example, and every other site that draws a
// name already fits it to its own space).
//
// What is left here is grooming and not cutting: the words are normalised onto
// single spaces so a title with a newline in it cannot break a row, and the
// punctuation that ended the sentence it came out of is dropped.
//
// The id floor matters more than it looks. A node whose title never arrived is
// exactly the node a person is most likely to be trying to identify — something
// went wrong early — and "task 7" is a name they can say out loud, ask about,
// and match against the rail. An empty string is not.
func taskTitleOf(label, assignment string, id uint64) string {
	if name := wholeName(label); name != "" {
		return name
	}
	if name := wholeName(leadSentence(assignment)); name != "" {
		return name
	}
	return taskIDWord(id)
}

// wholeName is [firstWords] with no cap: one line, single-spaced, without the
// full stop that ended the sentence it was lifted from.
func wholeName(text string) string {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimRight(strings.Join(fields, " "), ".,:;")
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
