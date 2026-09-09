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
//     and its word instead — `● working`, `◌ incomplete`, `▲ your call`,
//     `✗ incomplete` — so the eye finds the exceptions and skims the rest.
//   - ONE BLANK BETWEEN TASKS AND NONE AFTER THE LAST, which is what turns the
//     band from a block into a list of things.
//
// The fold is by FAMILY and never by row ([app.bandFoldGroupsHiding]): a cut
// that fell between a task's name and its outcome would leave a sentence
// hanging under a fold line with nothing above it saying what it was about, and
// a cut that fell between a task and the work it handed out would do the same
// thing one level up — an orphan child under a fold line, with the only row that
// explains it on the other side of the cut. So a family goes behind the fold
// whole, and the count on the fold line is the pieces of work it is hiding
// rather than the families (hometree.go).

import (
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// homeWorkTasks is how many pieces of work the band shows before the rest fold.
// THREE AND NOT MORE, because each one now costs two lines and a blank: three
// tasks with what they came to is nine rows, which is already the tallest band
// on the card, and the project's whole history is what the task page is for.
//
// IT COUNTS FAMILIES AND NOT NODES. A run that handed three pieces out is ONE
// thing that happened, and a band that spent its whole allowance on the inside
// of it would be hiding two other runs to show the parts of one. The fold line
// still says how many PIECES OF WORK are behind it, because that is the number a
// person is deciding whether to go and look at.
const homeWorkTasks = 3

// homeWorkIndent is where the outcome line hangs under the task it belongs to —
// far enough in to read as a continuation rather than as another task.
const homeWorkIndent = 2

func init() {
	registerHomeBand(homeBand{name: "work", order: bandOrderWork, draw: drawWorkBand})
}

// THE ORDER THIS BAND AND THE CARD BEHIND IT DRAW A CONVERSATION'S WORK IN is
// hometree.go's, and the argument for it is worth keeping where the two readers
// can see it: WHAT WANTS A PERSON, then what is still going, then everything
// that is over — and inside each of those, the record's own order, which is
// newest first with live work counted as now (internal/session's
// [sortTaskIndex]).
//
// THE RECORD'S ORDER ALONE WAS NOT ENOUGH, AND THE FOLD IS WHY. Both readers
// show three pieces of work and fold the rest behind `▸ N more`
// ([homeWorkTasks], place_home.go's [homeCardTasks]), and the record ranks by
// WHEN. Live work sorts to the top of that because an unfinished row is dated
// now — but a task that stopped and wants somebody's look carries the moment it
// stopped, so three tasks finishing after it push the one row on the card that
// is asking for something behind a fold. The person's own instruction was that
// work needing them be easy to find, and it was findable everywhere except the
// screen they open first.
//
// IT IS RANKED BY FAMILY ([homeWorkTwigRank]), so a CHILD that is asking for
// somebody brings its parent up the band with it. A run's own name is the row
// that says what the child was for, and promoting the child alone would put the
// question on the screen with its subject folded away.

// drawWorkBand is the band, one group of lines per FAMILY of work.
func drawWorkBand(a *app, ctx bandContext) []string {
	row, width, now, pal := ctx.subject.row, ctx.width, ctx.now, ctx.pal
	var groups [][]string
	var sizes []int
	for _, family := range homeWorkFamilies(row) {
		var group []string
		size := 0
		for _, node := range family {
			drawn := homeWorkNodeRows(node, row, width, now, pal)
			if len(drawn) == 0 {
				continue
			}
			group = append(group, drawn...)
			size++
		}
		if len(group) == 0 {
			continue
		}
		groups, sizes = append(groups, group), append(sizes, size)
	}
	rows := a.bandFoldGroupsHiding(ctx, "work", groups, homeWorkTasks, homeWorkHidden(sizes, homeWorkTasks), "tasks")
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

// homeWorkNodeRows is one piece of work as the band draws it — its name, what it
// came to, and the connectors saying what it hangs off — or nothing at all where
// the row has no cells to be drawn in.
//
// THE CONNECTORS ARE PAID FOR OUT OF THE ROW AND NOT OUT OF THE BAND. The name
// and the sentence under it are each fitted into what is left after the prefix,
// so a nested row's age still lands on the band's own right margin and its
// outcome still wraps at the band's own edge — the tree moves where a row
// STARTS and changes nothing about where the band ends.
func homeWorkNodeRows(node homeWorkNode, row session.SessionRow, width int, now time.Time, pal palette) []string {
	lead := homeWorkLeadOf(node, width, pal)
	rows := homeWorkName(node.entry, width-lead.nameCols, now, pal)
	if len(rows) == 0 {
		return nil
	}
	under := homeWorkUnder(node.entry, row, width-lead.underCols, pal)
	if lead.nameCols == 0 && lead.underCols == 0 {
		// A ROOT WITH NOTHING UNDER IT IS THE ROW THIS BAND ALWAYS DREW, to the
		// cell. A conversation whose work never branched must not pay a column of
		// air for a tree it does not have.
		return append(rows, under...)
	}
	// AND AN EMPTY PREFIX IS PAINTED WITH NOTHING, not with an empty paint: a root
	// whose family hangs below it has no elbow of its own, and wrapping "" in the
	// dim ink would write two escape sequences onto the front of a row that a
	// reader — and a test — reads as the row's first cell.
	air := func(prefix string) string {
		if prefix == "" {
			return ""
		}
		return pal.dim(prefix)
	}
	// The name's own second row — the age, where the two would not share one line
	// ([bandSides]) — hangs behind the elbow's own cells rather than behind the
	// elbow, and behind [homeWorkLead.stem] rather than [homeWorkLead.under],
	// because it was fitted into the width the NAME was given.
	for i := 1; i < len(rows); i++ {
		rows[i] = air(lead.stem) + rows[i]
	}
	rows[0] = air(lead.name) + rows[0]
	for _, said := range under {
		rows = append(rows, air(lead.under)+said)
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
	return homeWorkUnderSaid(entry, row, width, pal, false)
}

// homeWorkUnderSaid is [homeWorkUnder] for a caller whose row ABOVE has already
// drawn this task's price.
//
// ONE SOURCE OF TRUTH FOR A FIGURE DRAWN TWICE. The band above (this file's
// [homeWorkName]) right-aligns how long ago the work landed, so the money has
// nowhere else to be and belongs down here. The CARD's name row
// (place_home.go's [app.homeCardWork]) right-aligns the price itself — and then
// appended these rows under it, so a task that was not simply done drew
// `$0.52` on the name row and `$0.52` again four cells under it in a second
// ink. A figure a person sees twice is a figure they have to check against
// itself, and the second copy was what pushed the file count and the outcome
// sentence into an ellipsis.
//
// THE NAME ROW WINS. It is right-aligned in the money ink at the card's own
// margin, which puts every task's price in one column a reader can run an eye
// down; the under-block's copy sat mid-sentence between a state word and a file
// count, in the dim, where no two rows line up. So the caller that already said
// it says so, and the file count gets the cells back.
func homeWorkUnderSaid(entry session.TaskIndexEntry, row session.SessionRow, width int, pal palette, saidCost bool) []string {
	room := width - homeWorkIndent
	if room < 8 {
		return nil
	}
	status := taskEntryStatus(entry, row.Runs(entry))
	word := taskPresenceWord(status)
	outcome := strings.TrimSpace(entry.Outcome)
	var parts []string
	needs := false
	if status.Presence == session.TaskPresenceDone {
		// DONE WEARS NO MARK. The sentence is the whole of the line.
		if outcome != "" {
			parts = append(parts, outcome)
		}
	} else {
		// EVERY OTHER STATE LEADS, because it is the reason to look at this row
		// at all. What follows the word is whatever that state actually knows:
		// a running node says what it is doing, and a failed or unjudged one
		// says what it came to.
		lead := homeWorkGlyph(status, pal.ascii) + " " + word
		detail := outcome
		if !status.Settled() && status.Liveness == session.TaskLivenessHeld {
			detail = strings.TrimSpace(entry.Activity)
			// AND WHEN THE NODE IS NOT ITS OWN WORKER, THAT IS THE MORE HONEST
			// LINE. The activity is what the node's room last did, and a check
			// runs outside the room — so through the minutes of a check and a
			// repair round this row would quote a call that finished before
			// either started (taskphase.go).
			//
			// AND IT IS ASKED OF THE ROW, NOT OF THE ENTRY, so the line is the
			// same for work happening in ANOTHER window: the index row carries
			// the phase only inside the process holding the graph, and the
			// presence file is what crosses the gap ([session.SessionRow.Phase]).
			if phase := taskPhaseWords(row.Phase(entry), 0, 0); phase != "" {
				detail = phase
			}
		}
		if detail != "" {
			lead += " · " + detail
		}
		parts = append(parts, lead)
		needs = status.Attention
	}
	// THE TWO DIM FACTS, EACH ONLY WHEN IT IS ONE. A task that wrote no files
	// says nothing about files; one that cost nothing says nothing about cost.
	if entry.FilesChanged > 0 {
		parts = append(parts, itoa(entry.FilesChanged)+plural(" file", entry.FilesChanged))
	}
	if entry.Cost > 0 && !saidCost {
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
func homeWorkGlyph(status session.TaskStatus, ascii bool) string {
	switch status.Presence {
	case session.TaskPresenceWorking, session.TaskPresenceWaiting, session.TaskPresenceFinishing:
		if ascii {
			return homeLiveASCII
		}
		return homeLiveGlyph
	case session.TaskPresenceNeedsLook:
		if ascii {
			return homeAskASCII
		}
		return homeAskGlyph
	case session.TaskPresenceIncomplete:
		if status.Fault {
			if ascii {
				return glyphBadASCII
			}
			return glyphBad
		}
		if ascii {
			return homeStuckASCII
		}
		return homeStuckGlyph
	case session.TaskPresenceStopped:
		// A person's stop wears the mark it wears everywhere else and not the
		// cross (task.go's [glyphStopped]).
		if ascii {
			return glyphStoppedASCII
		}
		return glyphStopped
	}
	return ""
}
