package palette

import "github.com/Agent-Field/aforge-v2/internal/tui2/tokens"

// The drill trail — the back grammar (§16 OVERLAY DISMISSAL, read for depth).
//
// A palette that can open a sub-list has to be able to leave one, and the
// reported failure was that it could not: a reader who drilled into an option's
// choices had no key and no pointer target that went back, only esc, which threw
// away the whole surface along with the level. A surface you can enter and not
// leave is a trap regardless of how good the level you are trapped on is.
//
// THE GRAMMAR, and each part is answering a different reader:
//
//   - BACKSPACE on an empty filter pops one level. With text in the filter it
//     edits the text, unchanged — the filter is what the reader is looking at
//     while they type, and a key that sometimes deleted a letter and sometimes
//     threw away the list would make typing feel dangerous. Empty-then-back is
//     the same rule a shell path prompt and every file picker already teach.
//   - ESC closes the whole surface from any depth. It does NOT unwind one rung:
//     8.2.21 is that esc acts on what you are watching, and what a reader
//     watching a drilled palette wants gone is the palette. Two keys with one
//     meaning between them is how "press esc until something happens" gets
//     learned, and that is not a grammar, it is a shrug.
//   - THE TRAIL is the pointer's door and the reader's place, in one line. Every
//     ancestor segment is a click target that pops back to it; the current one
//     is a label and is not.
//
// WHY SEGMENTS AND NOT A `‹ back` ROW. Both were on the table. A back row is one
// target and costs a row of the list on every drilled level; the trail is N
// targets, costs no rows at all, and answers "where am I" — which a back row
// does not — on the line the reader is already reading because it holds their
// query. The cost is a hit test on one line, which is [trail.hitAt] and is six
// lines long. If a lane later wants the row as well, nothing here forbids it.

// RootWord names the undrilled palette on the trail. It is a word rather than a
// glyph because every other segment is a word, and a trail whose first step is
// punctuation reads as decoration rather than as a step.
//
// It is EXPORTED because the drill no longer ends at this package. A settings
// sheet or a model picker raised from a palette row is one rung deeper in the
// same path, and it draws the same trail with this word at its head — so the
// word a reader clicks to get back to the whole catalog is written down once,
// here, rather than spelled by each surface that has to name it.
const RootWord = "all"

// rootSegment is the in-package spelling. Same constant, kept so this file's
// own prose reads about a segment rather than about an export.
const rootSegment = RootWord

// trailLead opens each segment. It is [tokens.GlyphPromptChat], the same mark
// the composer draws to mean "and then", which is exactly what a step of a path
// means. The space is part of the lead so the segments never touch it.
//
// It is NOT exported, and the two surfaces that also draw this path — the model
// picker and the settings sheet — respell it rather than importing it, for the
// reason line.go states: a third package existing to hold twenty lines of path
// arithmetic would put a public API in front of something no consumer of this
// tree should call, and importing it from here would drag this package's rail
// and registry reads behind a surface that wanted one glyph. Their spellings are
// pinned equal to this one by a test on each side.
const trailLead = tokens.GlyphPromptChat + " "

// trailGap separates the trail from whatever the header draws after it.
const trailGap = "  "

// segment is one drawn step of the trail: which depth it returns to, and the
// cells it occupied in the last render so a click can be turned back into that
// depth. The columns are recorded at paint time rather than recomputed, because
// a hit test that measured the string again would be a second opinion about
// where the words are.
type segment struct {
	depth int
	at    int
	w     int
}

// hitAt maps a column on the header row back to the depth a click there returns
// to. The CURRENT level is deliberately not a target — clicking where you
// already are must do nothing rather than rebuild the list under the pointer.
func hitAt(segs []segment, x int) (int, bool) {
	for i := range segs {
		s := segs[i]
		if x >= s.at && x < s.at+s.w {
			return s.depth, true
		}
	}
	return 0, false
}

// level is one rung of the drill: the catalog it shows, the word it is named by
// on the trail, and the reader's own place in it.
//
// The place is kept because popping is a RETURN, not a fresh open. A reader who
// drilled from the middle of a long list, looked, and came back to the top would
// have been moved by the surface rather than by themselves — 7.2's rule that a
// visible row never moves, applied to the row you were standing on.
type level struct {
	word   string
	cat    Catalog
	query  string
	cursor int
	top    int
}
