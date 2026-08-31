package tui3

// THE WORK BAND: WHAT THIS CONVERSATION HAS DONE, NAME FIRST.
//
// It used to lead every row with its state — `done Port the Picker 2h`, and the
// outcome sentence jammed underneath with the next task starting immediately
// after it. A person reading that column read the word `done` eleven times
// before they read a single thing the conversation had actually done, and the
// rows ran together into one grey block because nothing separated them.
//
// So the band is turned around and given air:
//
//	Port the Picker                          2h
//	  the roster resumes cleanly · 14 files
//
//   - THE NAME IS THE ROW'S IDENTITY and it goes first, in [palette.muted]
//     rather than dim — it is the one piece of text on this band somebody is
//     scanning for. The age hangs off the right in dim, which is where every
//     other age on this surface hangs.
//   - THE NEXT LINE IS WHAT IT CAME TO, indented under the name so it reads as
//     a continuation and dim. Its sentence stays whole; file count and cost
//     move to following indented rows when the card is too narrow for them.
//   - DONE IS THE ABSENCE OF A MARK. There is no `✓` and no `done` on a landed
//     task: this surface's glyphs say what is HAPPENING, and a tick on every
//     finished row would spend the loudest ink on the rows that want nothing.
//     Everything that is NOT simply done leads the second line with its glyph
//     and its word instead — `● running`, `◌ incomplete`, `▲ needs your look`,
//     `✗ failed` — so the eye finds the exceptions and skims the rest.
//   - ONE BLANK BETWEEN TASKS AND NONE AFTER THE LAST, which is what turns the
//     band from a block into a list of things.
//
// The fold is by TASK and never by row ([app.bandFoldGroups]): a cut that fell
// between a task's name and its outcome would leave a sentence hanging under a
// fold line with nothing above it saying what it was about.

import (
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// homeWorkTasks is how many pieces of work the band shows before the rest fold.
// THREE AND NOT MORE, because each one now costs two lines and a blank: three
// tasks with what they came to is nine rows, which is already the tallest band
// on the card, and the project's whole history is what the task page is for.
const homeWorkTasks = 3

// homeWorkIndent is where the outcome line hangs under the task it belongs to —
// far enough in to read as a continuation rather than as another task.
const homeWorkIndent = 2

func init() {
	registerHomeBand(homeBand{name: "work", order: bandOrderWork, draw: drawWorkBand})
}

// drawWorkBand is the band, one group of lines per task.
func drawWorkBand(a *app, ctx bandContext) []string {
	row, width, now, pal := ctx.subject.row, ctx.width, ctx.now, ctx.pal
	var groups [][]string
	for _, entry := range row.Tasks.Rows {
		group := homeWorkName(entry, width, now, pal)
		group = append(group, homeWorkUnder(entry, row, width, pal)...)
		groups = append(groups, group)
	}
	rows := a.bandFoldGroups(ctx, "work", groups, homeWorkTasks, "tasks")
	if len(rows) == 0 {
		return nil
	}
	// THE CAPTION GOES OVER THE WHOLE BAND rather than over each fresh row: the
	// rows say WHAT landed, and this says why any of it is worth a second look.
	// It is the look stamp home writes on its way out (home.go's [homeView.seen],
	// session's look.go), and a first look — with no stamp to measure from —
	// captions nothing, which is the emptiness law applied to a whole line.
	if a.homeFresh(row) > 0 {
		rows = append([]string{pal.dim(fit(homeFreshWord, width))}, rows...)
	}
	return rows
}

// homeWorkName is a task's first line: what it is CALLED, and how long ago it
// landed, hard against the right edge.
func homeWorkName(entry session.TaskIndexEntry, width int, now time.Time, pal palette) []string {
	label := strings.TrimSpace(entry.Label)
	if label == "" {
		label = strings.TrimSpace(entry.Title)
	}
	return bandSides(width, homeWorkIndent, 8, label, sinceAt(entry.EndedAt, now), pal.muted, pal.dim)
}

// homeWorkUnder is a task's outcome rows, and nil when there is nothing true to
// put there — a landed task with no outcome, no files and no cost says nothing
// rather than drawing an empty indent (the emptiness law).
func homeWorkUnder(entry session.TaskIndexEntry, row session.SessionRow, width int, pal palette) []string {
	room := width - homeWorkIndent
	if room < 8 {
		return nil
	}
	word := homeTaskWord(entry, row)
	outcome := strings.TrimSpace(entry.Outcome)
	var parts []string
	needs := false
	if word == doneWord {
		// DONE WEARS NO MARK. The sentence is the whole of the line.
		if outcome != "" {
			parts = append(parts, outcome)
		}
	} else {
		// EVERY OTHER STATE LEADS, because it is the reason to look at this row
		// at all. What follows the word is whatever that state actually knows:
		// a running node says what it is doing, and a failed or unjudged one
		// says what it came to.
		lead := homeWorkGlyph(word, pal.ascii) + " " + word
		detail := outcome
		if word == taskRecordRunsWord {
			detail = strings.TrimSpace(entry.Activity)
			// AND WHEN THE NODE IS NOT ITS OWN WORKER, THAT IS THE MORE HONEST
			// LINE. The activity is what the node's room last did, and a check
			// runs outside the room — so through the minutes of a check and a
			// repair round this row would quote a call that finished before
			// either started (taskphase.go).
			if phase := taskPhaseWords(entry.Phase, 0, 0); phase != "" {
				detail = phase
			}
		}
		if detail != "" {
			lead += " · " + detail
		}
		parts = append(parts, lead)
		needs = word == taskUnverifiedWord
	}
	// THE TWO DIM FACTS, EACH ONLY WHEN IT IS ONE. A task that wrote no files
	// says nothing about files; one that cost nothing says nothing about cost.
	if entry.FilesChanged > 0 {
		parts = append(parts, itoa(entry.FilesChanged)+plural(" file", entry.FilesChanged))
	}
	if entry.Cost > 0 {
		parts = append(parts, dollars(entry.Cost))
	}
	if len(parts) == 0 {
		return nil
	}
	ink := pal.dim
	if needs {
		// The one thing on this band that is asking for a hand takes the accent,
		// the same way the left column brings `waiting on you` up out of the dim.
		ink = pal.accent
	}
	rows := bandClauses(room, 0, ink, parts...)
	for i := range rows {
		rows[i] = strings.Repeat(" ", homeWorkIndent) + rows[i]
	}
	return rows
}

// homeWorkGlyph is the mark that leads a task that is not simply done. They are
// HOME'S OWN GLYPHS (home.go names them) rather than the task page's, because
// this is home and a person reading the left column has already learnt these
// four shapes on the rows beside it.
func homeWorkGlyph(word string, ascii bool) string {
	switch word {
	case taskRecordRunsWord:
		if ascii {
			return homeLiveASCII
		}
		return homeLiveGlyph
	case taskRecordStoppedWord:
		if ascii {
			return homeStuckASCII
		}
		return homeStuckGlyph
	case taskUnverifiedWord:
		if ascii {
			return homeAskASCII
		}
		return homeAskGlyph
	case doneFailWord:
		if ascii {
			return glyphBadASCII
		}
		return glyphBad
	}
	return ""
}
