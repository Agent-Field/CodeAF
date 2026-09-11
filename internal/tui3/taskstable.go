package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// taskstable.go is THE LEFT HALF OF THE TASKS PLACE AS A TABLE: what the columns
// are, where they land at every width, and what one row puts in each of them.
//
// WHY IT IS A TABLE AND NO LONGER A RANKED TAIL. The old row offered six facts
// and degraded them by spelling from the right (rowfit.go's laws 2 and 3), which
// is the right shape for a list whose facts are OPTIONAL — a model's prices, a
// lane's numbers. It is the wrong shape for a page a person SCANS: every row gave
// up a different fact at a different width, so no two rows on one frame answered
// the same questions, most rows could only say two of the six, and the right edge
// was holes and prose. A blank cell meant either "this row has nothing to say" or
// "the frame ran out", with nothing on screen telling the two apart.
//
// So the facts are FIXED COLUMNS and the name is what flexes. rowfit.go's law 1
// is unchanged and is in fact the whole design: the name keeps every cell the two
// columns do not need, and it is the only thing on the row that is ever cut. Law
// 2 has little left to do — a fixed column cannot degrade by spelling, though the
// two cells that can say a longer and a shorter thing still do — and law 3's
// ranked prefix is answered by both columns being always filled.
//
// THE COLUMNS ARE FOUR AND THE LAST TWO ARE FIXED:
//
//	fold and family · mark · name · state · the sort key's column · one cell of air
//
// The fold and the family column in front are [tasksKin]'s and are decided in the
// layout; the mark is [tasksGlyph]'s. What is here is everything from the name
// rightwards.

// The table's own measurements, and THE ONE PLACE THEY ARE WRITTEN DOWN. The
// manual quotes them and the tests interpolate them; a number restated anywhere
// else is a number that will drift.
const (
	// tasksStateCells is the `state` column. It is the longest state word this
	// surface has with a shut fold's count after it at its shorter spelling —
	// `incomplete · holds 4` — because that is the widest thing the cell is ever
	// asked to say whole.
	tasksStateCells = 20
	// tasksColumnAir is the one cell between the last column and the frame's edge,
	// so a figure never touches the right border.
	tasksColumnAir = 1
	// tasksStateFloor is the narrowest frame that still draws the state column.
	// Under it the state goes and the sort key's column stays, because the key is
	// the column a person CHOSE and the state is the one they get for nothing.
	tasksStateFloor = 90
	// tasksNameFloor is the least a name may be left with before the row has
	// stopped naming the work at all. It is [tierTitleFloor]'s argument said again
	// for this page: about one word, under which the row has told nobody which
	// task this is.
	tasksNameFloor = tierTitleFloor
)

// tasksColumns is where the two fact columns land on a frame this wide: how many
// cells the state gets, how many the sort key's column gets, and what is left
// over for the name.
//
// IT IS ASKED BY THE PAINT AND BY THE POINTER ALIKE, which is why it is a
// function rather than arithmetic in each of them: a label a person clicks and
// the cells it stands over have to be the same cells, and two answers to where
// the `cost` column is is a click that sorts by the wrong thing.
//
// `room` is the cells the ROW has, after the place's left edge and the family
// column in front of it have been spent.
func tasksColumns(room int, key tasksSortKey) (state, second, name int) {
	second = key.cells()
	if room >= tasksStateFloor {
		state = tasksStateCells
	}
	if name = room - state - second - tasksColumnAir; name >= tasksNameFloor {
		return state, second, name
	}
	// A FRAME WITH NO ROOM FOR A NAME DROPS THE COLUMNS AND KEEPS THE NAME, in
	// that order, because a row that has spent its cells on two facts about work
	// it has not named has said nothing at all (rowfit.go, law 1).
	if state > 0 {
		state = 0
		if name = room - second - tasksColumnAir; name >= tasksNameFloor {
			return state, second, name
		}
	}
	if name = room - tasksColumnAir; name < 1 {
		name = 1
	}
	return 0, 0, name
}

// ── what one row puts in the columns ────────────────────────────────────────

// tasksStateField is the `state` column's cell, and IT IS ALWAYS FILLED.
//
// It is the one word every row of work has ([taskStateWord], out of the table
// tasktier.go states is the only one), with two things that can stand in its
// place or after it and nothing else:
//
//   - WORK IN ANOTHER WINDOW SAYS WHERE IT IS INSTEAD. That is the one fact that
//     can correct the mark beside it, and a row whose mark and whose words
//     disagree is worse than a row missing a fact ([tasksNote]).
//   - A SHUT FOLD SAYS WHAT IT IS HOLDING after it. A mark with no count is a
//     mark a person has to open to find out whether it was worth opening.
//
// THE REASON IS NOT HERE. A row is a name and two facts now; WHY the work ended
// as it did is the record's, and it is read in the pane beside the list or on the
// one line the cursor's own row grows where there is no room for a pane
// ([tasksReasonLine]). It was on the row for as long as this page has existed and
// it is what made the right edge prose.
func tasksStateField(line tasksLine) rowField {
	item := line.item
	if note := tasksNote(item); note != "" {
		return rowSay(note)
	}
	word := taskStateWord(item.entry, item.runs)
	if item.live != nil {
		// A NODE THIS WINDOW IS HOLDING IS READ FROM THE NODE, which is the same
		// reading the mark in front of it is drawn from ([tasksItem.status]). The
		// record's own row knows less than the node does.
		word = item.live.Word
	}
	if !line.folds || line.open || line.kids <= 0 {
		return rowSay(word)
	}
	return rowSay(word+rowSep+tasksUnderWord(line.kids), word+rowSep+tasksHoldsShort(line.kids), word)
}

// tasksHoldsShort is [tasksUnderWord] with the room a COLUMN has rather than a
// row's whole width: `holds 4` where the line says `holds 4 more`. It is law 2
// applied to the one cell on this page that still has a longer and a shorter
// thing to say — the state word itself cannot be shortened and must not be cut.
func tasksHoldsShort(kids int) string { return "holds " + itoa(kids) }

// tasksChatStateField is a CONVERSATION's cell: how much work opening it puts on
// the page, and the most urgent thing among that work.
//
// A CONVERSATION HAS NO STATE OF ITS OWN, and this is not one — it is the count
// and the word of what is under it, which is exactly the question a shut root
// raises. `5 your call` is five rows and at least one of them wants somebody;
// `9 done` is nine rows and nothing to do.
func tasksChatStateField(chat tasksChat) rowField {
	if chat.kids <= 0 || strings.TrimSpace(chat.word) == "" {
		return rowSay()
	}
	return rowSay(itoa(chat.kids)+" "+chat.word, itoa(chat.kids))
}

// ── the row ─────────────────────────────────────────────────────────────────

// tasksTableRow lays one row of the table out: the name in what the columns
// leave, then the two columns, then one cell of air.
//
// EVERY ROW OF ONE FRAME ANSWERS THE SAME TWO QUESTIONS. That is the whole
// difference from the tail it replaces — the columns are in the same cells on
// every row, so the eye reads DOWN a column instead of re-parsing each row's own
// ranked prefix, and a row with nothing to say in a column draws nothing there
// rather than pulling the next fact leftwards into the hole.
func tasksTableRow(name string, state, second rowField, secondInk func(string) string,
	room int, key tasksSortKey, pal palette, lit bool) string {
	stateCells, secondCells, nameCells := tasksColumns(room, key)
	said := fit(name, nameCells)
	out := placeSubject(said, lit, pal) + pad(nameCells-ansi.StringWidth(said))
	if stateCells > 0 {
		word := rowTail([]rowField{state}, stateCells)
		out += placeFactInk(lit, pal)(word) + pad(stateCells-ansi.StringWidth(word))
	}
	if secondCells > 0 {
		// THE SORT KEY'S COLUMN IS RIGHT-ALIGNED, because every one of its three
		// answers is a FIGURE — an age, a count, a price — and figures are read
		// down their last digit.
		figure := rowTail([]rowField{second}, secondCells)
		out += pad(secondCells-ansi.StringWidth(figure)) + secondInk(figure)
	}
	return out + pad(tasksColumnAir)
}

// pad is n spaces, and none for a negative count.
func pad(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(" ", n)
}

// ── the control row ─────────────────────────────────────────────────────────

// THE FILTER BOX IS THE FIRST ROW OF THE LIST, AND THE COLUMN LABELS STAND OVER
// THE COLUMNS THEY NAME.
//
// It replaces the note line under the list that said the filter back to a person
// who could not see the filter itself. That line existed because the box two rows
// below it was the router's message box and said nothing about narrowing
// anything: what somebody typed went somewhere they could not see, and the only
// correction available was an echo UNDER the rows their keystrokes had just
// changed. The box is on the list now, where the typing lands.

// tasksSortArrow is which way the sorted column is pointing.
//
// IT IS TYPOGRAPHY AND NOT A MARK FROM THE VOCABULARY, which is the same
// judgement every other arrow on this surface is spelled under — `→ verbs:`,
// `↑↓ choose`, the window control's own `shift+← … →`. internal/iconlaw owns the
// runes that NAME A STATE, and a direction is not one of them.
const (
	tasksSortDown = "↓"
	tasksSortUp   = "↑"
)

// tasksControlLabels is the right-hand end of the control row: the two labels,
// with the arrow on whichever column the list is sorted by.
//
// THERE ARE EXACTLY TWO LABELS BECAUSE THERE ARE EXACTLY TWO COLUMNS. `state` is
// always one of them; the other is whatever the second column is showing, which
// is the sort key itself or the age standing in for a key that has no cell
// ([tasksSortKey.column]). Sorting by name puts the arrow on neither, because
// neither column is the name — and that is honest rather than a gap: the control
// row's left half is showing the filter, and the foot names the key.
func tasksControlLabels(by tasksSort) (state, second string) {
	arrow := tasksSortDown
	if by.back {
		arrow = tasksSortUp
	}
	state, second = tasksByState.word(), by.key.column().word()
	switch {
	case by.key == tasksByState:
		return state + " " + arrow, second
	case by.key == by.key.column():
		return state, second + " " + arrow
	}
	return state, second
}

// tasksControlRow is that line: the mark, then what has been typed or the dim
// invitation to type it, and at the right the two labels.
func tasksControlRow(query string, by tasksSort, room int, pal palette) string {
	stateLabel, secondLabel := tasksControlLabels(by)
	mark := pal.glyph(tokens.GFilter)
	// THE BOX IS LAID OUT WHERE THE NAMES ARE AND THE LABELS OVER THEIR OWN
	// COLUMNS, both out of [tasksColumns] — so the label a person clicks and the
	// cells it stands over are the same cells at every width, and the two lines
	// cannot drift apart as the frame moves.
	stateCells, secondCells, nameCells := tasksColumns(room, by.key)
	boxCells := max(nameCells-ansi.StringWidth(mark)-1, 1)
	// WHAT IS TYPED IS IN THE READING INK AND THE INVITATION IS DIM. A person has
	// to be able to tell the words they typed from the words the box came with.
	said, ink := fit(tasksTypeWord, boxCells), pal.dim
	if query != "" {
		said, ink = fit(query, boxCells), pal.ink
	}
	out := pal.dim(mark) + " " + ink(said) + pad(boxCells-ansi.StringWidth(said))
	if stateCells > 0 {
		label := fit(stateLabel, stateCells)
		out += pal.dim(label) + pad(stateCells-ansi.StringWidth(label))
	}
	if secondCells > 0 {
		label := fit(secondLabel, secondCells)
		out += pad(secondCells-ansi.StringWidth(label)) + pal.dim(label)
	}
	return out + pad(tasksColumnAir)
}

// tasksControlHit is which label one cell of the control row is under, and
// whether it is under one at all.
//
// IT IS THE SAME ARITHMETIC THE PAINT USES ([tasksColumns]), asked from the other
// end. The pointer resolving a press against its own idea of where a column sits
// is exactly how a click comes to sort by the wrong thing, which is the argument
// the frame and the hit map are one function for (place_tasks.go).
//
// `x` is the cell inside the ROW — the place's left edge already spent.
func tasksControlHit(x, room int, by tasksSort) (tasksSortKey, bool) {
	stateCells, secondCells, nameCells := tasksColumns(room, by.key)
	switch {
	case stateCells > 0 && x >= nameCells && x < nameCells+stateCells:
		return tasksByState, true
	case secondCells > 0 && x >= nameCells+stateCells && x < nameCells+stateCells+secondCells:
		// THE SECOND LABEL NAMES THE COLUMN AND NOT THE KEY. Sorting by name puts
		// the age in that column, so a click on it asks for the age — which is what
		// the word under the pointer says.
		return by.key.column(), true
	}
	return 0, false
}

// ── the keys ────────────────────────────────────────────────────────────────

// THE SORT KEYS ARE CHORDS AND NOT BARE LETTERS, and that is a deliberate
// departure from the spec's own `s` / `S`.
//
// EVERY PRINTABLE KEY ON THIS PAGE IS THE FILTER (place_tasks.go's
// [app.taskSheetKeyPress] says why: the frame is the page, so there is no draft
// underneath for a keystroke to reach, and a record of four hundred tasks is
// found by remembering a word of a title). A bare `s` would take the filter's
// commonest letter away from it — `sweep`, `stop`, `site`, `session` — and the
// list a person was trying to narrow would re-sort instead. The two cannot both
// be bare, the filter box is the thing this wave put ON SCREEN, and the spec's
// own frame draws the invitation to type into it.
//
// So the chord is alt, which is what this surface already spends on a place's
// own verbs (`alt+.` the map, `alt+t` the roster, `alt+enter` a task), and the
// pointer keeps the gesture the spec leads with: the column labels are pressed.
const (
	tasksSortKeyChord  = "alt+s"
	tasksSortBackChord = "alt+shift+s"
)

// tasksSortHint is how the foot names them, in the hint slot's own grammar: the
// key, then what it does.
func tasksSortHint(by tasksSort) string {
	return tasksSortKeyChord + " sort: " + by.key.word()
}

// ── the reason, off the row and under the cursor ────────────────────────────

// THE REASON LEFT THE ROW AND IT HAS TO LAND SOMEWHERE.
//
// A table's rows are a name and two facts; why one piece of work ended as it did
// is the record's, and the record is a pane beside the list on a frame wide
// enough for one. Under that width there is no pane — and the task-states law
// says a row may never read a bare `your call`, because the whole point of that
// word is that somebody has to do something and the page owes them what.
//
// So the CURSOR'S row, and only the cursor's row, grows one dim line.

// tasksReasonLine is that line: the row's state with its reason behind it, and —
// for a landing that wrote one — the first sentence of what it came to.
//
// IT IS ONE LINE AND IT RETURNS A STRING. A wrapped answer would make the row's
// HEIGHT depend on the length of its reason, so every row under the cursor would
// move as the cursor walked, and the window that keeps a cursor's block whole
// ([tasksTop]) would be chasing a number that changed with the row it was
// measuring.
func tasksReasonLine(item tasksItem, width int, pal palette) string {
	said := strings.TrimSpace(item.status().RowWord())
	if outcome := taskFirstSentence(item.entry.Outcome); outcome != "" && outcome != said {
		said += rowSep + outcome
	}
	if said == "" {
		return ""
	}
	return pal.dim(fit(said, width))
}

// taskFirstSentence is the opening sentence of what a landing wrote down, which
// is as much of a report as a single line can honestly carry. A report with no
// sentence end in it is taken whole and left to the fitter.
func taskFirstSentence(report string) string {
	report = strings.TrimSpace(strings.SplitN(strings.TrimSpace(report), "\n", 2)[0])
	for at, r := range report {
		if r != '.' && r != '!' && r != '?' {
			continue
		}
		// A FULL STOP INSIDE A FILENAME IS NOT A SENTENCE END, which is the same
		// judgement the step caption makes about the same characters: the stop has
		// to be followed by a space or by nothing at all.
		if at+1 >= len(report) {
			break
		}
		if report[at+1] == ' ' {
			return strings.TrimSpace(report[:at+1])
		}
	}
	return report
}

// tasksReasonShowing reports whether the list should grow that line at all,
// AND IT ASKS THE WHOLE FRAME RATHER THAN THE LIST'S OWN WIDTH.
//
// The difference is the whole of the trap. Once the record stands in a pane
// beside the list, the list is drawn in the cells the pane leaves — 72 of 122 —
// which is already under the width at which a pane appears. A predicate that
// asked its own width would decide there is no pane on exactly the frames that
// have one, and draw this line underneath the pane that already says it.
func tasksReasonShowing(a *app) bool {
	width, _ := a.size()
	return layoutTier(width) != tierPhone && width < tasksPaneFloor
}

// tasksPaneFloor is the frame at which the record moves off the cursor's row and
// into a pane of its own beside the list (#884). It is named here because this
// is the file that has to know the answer today; the pane's own lane replaces
// the body of [tasksReasonShowing] with its predicate and this constant goes
// with it.
const tasksPaneFloor = 110
