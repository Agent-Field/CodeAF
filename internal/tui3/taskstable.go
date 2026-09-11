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
func tasksControlLabels(key tasksSortKey, back bool) (state, second string) {
	arrow := tasksSortDown
	if back {
		arrow = tasksSortUp
	}
	state, second = tasksByState.word(), key.column().word()
	if key == tasksByState {
		return state + " " + arrow, second
	}
	if key == key.column() {
		return state, second + " " + arrow
	}
	return state, second
}

// tasksControlRow is that line: the mark, then what has been typed or the dim
// invitation to type it, and at the right the two labels.
func tasksControlRow(query string, key tasksSortKey, back bool, room int, pal palette) string {
	stateLabel, secondLabel := tasksControlLabels(key, back)
	mark := pal.glyph(tokens.GFilter)
	lead := pal.dim(mark) + " "
	box := pal.dim(tasksTypeWord)
	if query != "" {
		box = pal.ink(query)
	}
	// THE LABELS ARE FITTED TO THEIR OWN COLUMNS AND THE BOX TAKES THE REST, which
	// is the row under it laid out with a label where its name goes — so the two
	// lines cannot drift apart as the frame moves.
	stateCells, secondCells, nameCells := tasksColumns(room, key)
	boxCells := nameCells - ansi.StringWidth(mark) - 1
	if boxCells < 1 {
		boxCells = 1
	}
	said := fit(ansi.Strip(query), boxCells)
	if query == "" {
		said = fit(tasksTypeWord, boxCells)
	}
	_ = box
	out := lead + pal.dim(said) + pad(boxCells-ansi.StringWidth(said))
	if query != "" {
		out = lead + pal.ink(said) + pad(boxCells-ansi.StringWidth(said))
	}
	if stateCells > 0 {
		out += pal.dim(fit(stateLabel, stateCells)) + pad(stateCells-ansi.StringWidth(fit(stateLabel, stateCells)))
	}
	if secondCells > 0 {
		label := fit(secondLabel, secondCells)
		out += pad(secondCells-ansi.StringWidth(label)) + pal.dim(label)
	}
	return out + pad(tasksColumnAir)
}
