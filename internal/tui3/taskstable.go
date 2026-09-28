package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// The Tasks table shares column measurements between rows, headings and clicks.
// Identity flexes around project, state and age; fold controls follow their
// titles in the name column.

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
	tasksFoldCells = 2
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

// tasksColumns reserves the same fact columns for every row at a given width.
// Tree connectors come out of the name cell, so nested rows remain aligned.
func tasksColumns(width int, key tasksSortKey) (state, second, name int) {
	second = key.cells()
	if width >= tasksStateFloor {
		state = tasksStateCells
	}
	if name = width - state - second - tasksColumnAir - tasksProjectCells(width); name >= tasksNameFloor {
		return state, second, name
	}
	// A FRAME WITH NO ROOM FOR A NAME DROPS THE COLUMNS AND KEEPS THE NAME, in
	// that order, because a row that has spent its cells on two facts about work
	// it has not named has said nothing at all (rowfit.go, law 1).
	if state > 0 {
		state = 0
		if name = width - second - tasksColumnAir - tasksProjectCells(width); name >= tasksNameFloor {
			return state, second, name
		}
	}
	if name = width - tasksColumnAir - tasksProjectCells(width); name < 1 {
		name = 1
	}
	return 0, 0, name
}

// The project column is shared by all rows and yields space on compact frames.
func tasksProjectCells(width int) int {
	if layoutTier(width) == tierPhone {
		return 0
	}
	return min(24, width/5)
}

func tasksAgeHeaderHit(x, width int) bool {
	_, cells, _ := tasksColumns(width, tasksByAge)
	right := width - tasksColumnAir
	return cells > 0 && x >= right-cells && x < right
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
//   - WORK THAT IS RUNNING RIGHT NOW SAYS WHERE IT HAS GOT TO after it
//     ([session.TaskIndexEntry.Activity], `working · 18 of 40`). That figure is
//     the one thing on a running row that CHANGES while somebody watches it, and
//     a person watching a long run is watching for it and nothing else. It is a
//     rung and not the cell: where twenty cells cannot hold both, the state word
//     stays and the figure goes (rowfit.go law 2), because a row that has stopped
//     saying what it IS has stopped being a row of this list.
//
// THE REASON IS NOT HERE. A row is a name and two facts now; WHY the work ended
// as it did is the record's, and it is read in the pane beside the list or on the
// one line the cursor's own row grows where there is no room for a pane
// ([tasksReasonLine]). It was on the row for as long as this page has existed and
// it is what made the right edge prose.
func tasksStateField(line tasksLine) rowField {
	item := line.item
	if note := tasksNote(item); note != "" {
		// THE WORDS FIRST AND THE WINDOW'S NAME AFTER THEM. `another window ·
		// Fix the nil-map crash` is thirty-eight cells and this column has twenty,
		// and a field with only one spelling either fits or goes (rowfit.go law 4)
		// — so offering the pair alone left the cell BLANK on exactly the rows
		// whose mark most needs correcting. WHICH window is on the card `enter`
		// opens and on the cursor's own grown line; THAT it is not this one is the
		// fact the column exists for.
		short := taskAwayWord
		if item.here {
			short = taskOpenHereWord
		}
		return rowSay(note, short)
	}
	word := taskStateWord(item.entry, item.runs)
	if item.live != nil {
		// A NODE THIS WINDOW IS HOLDING IS READ FROM THE NODE, which is the same
		// reading the mark in front of it is drawn from ([tasksItem.status]). The
		// record's own row knows less than the node does.
		word = item.live.Word
	}
	if doing := strings.TrimSpace(item.entry.Activity); doing != "" && !item.status().Settled() {
		return rowSay(word+rowSep+doing, word)
	}
	if !line.folds || line.open || line.kids <= 0 {
		return rowSay(word)
	}
	if item.plan != nil {
		ending := "done"
		if item.plan.Status == "failed" || item.plan.Status == "cancelled" {
			ending = "failed"
		}
		return rowSay(word+railSep+itoa(line.kids)+" "+ending, word)
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
	if chat.whole <= 0 || strings.TrimSpace(chat.word) == "" {
		return rowSay()
	}
	return rowSay(itoa(chat.whole)+" "+chat.word, itoa(chat.whole))
}

// ── the row ─────────────────────────────────────────────────────────────────

// tasksTableRow lays one row of the table out: the row's lead — its family
// connectors and its mark — then the name in what the columns leave, then the
// inline fold control, project, state and age columns, and one cell of air.
//
// EVERY ROW OF ONE FRAME ANSWERS THE SAME TWO QUESTIONS IN THE SAME CELLS. That
// is the whole difference from the tail it replaces — the eye reads DOWN a
// column instead of re-parsing each row's own ranked prefix, and a row with
// nothing to say in a column draws nothing there rather than pulling the next
// fact leftwards into the hole.
//
// WHICH IS WHY THE LEAD IS SPENT OUT OF THE NAME AND NOT OUT OF THE ROOM. The
// connectors are two cells wider three levels down a family and a root has no
// mark at all, so a row that measured its columns from what its own lead left
// would put them in a different place on every line of the page — a table whose
// columns move is a tail with extra steps. Only the NAME flexes (rowfit.go law
// 1), and the lead eats into the name.
//
// A PROGRAM'S WORK WEARS ITS BADGE AFTER THE NAME, inside the name's own column
// (programbadge.go): program is the name of the program the row's work was
// handed to, "" for every other row, and the badge is paid for out of the name
// the way the lead is, so no column moves for it.
func tasksTableRow(lead string, leadCells int, name, program string, state, second rowField,
	secondInk func(string) string, width int, key tasksSortKey, pal palette, lit bool, project, fold string) string {
	stateCells, secondCells, nameCells := tasksColumns(width, key)
	nameCells = max(nameCells-leadCells, 1)
	foldCells := 0
	if fold != "" {
		foldCells = 1 + ansi.StringWidth(fold)
	}
	room := max(nameCells-foldCells-tasksColumnAir, 1)
	wears := programSpelling(programBadge(program), name, room, railTitleFloor)
	said := fit(name, max(room-programCells(wears), 1))
	out := lead + placeSubject(said, lit, pal) + pal.programAfter(wears)
	if fold != "" {
		out += " " + pal.dim(fold)
	}
	out += pad(nameCells - ansi.StringWidth(said) - programCells(wears) - foldCells)
	if cells := tasksProjectCells(width); cells > 0 {
		word := fit(project, cells-1)
		out += placeFactInk(lit, pal)(word) + pad(cells-ansi.StringWidth(word))
	}
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

// tasksControlLabels supplies state and age labels with the current age direction.
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
//
// IT ALSO ANSWERS THE COLUMN THE CARET SITS IN, on the same terms the key box
// does (connect.go's keyLine): the first cell of the box when nothing is typed,
// the cell after the last shown character when something is — and what is
// shown is what FIT, because a caret past the box's edge would be a cursor
// sitting on the sort labels. The answer is returned rather than re-derived
// anywhere else because this is the one place the box's own layout is decided
// ([placeTasks.caretRow] re-asks the same function to find the line and its
// column; a hand-rolled twin would drift the moment this layout moved).
func tasksControlRow(query string, by tasksSort, width int, pal palette) (string, int) {
	stateLabel, secondLabel := tasksControlLabels(by)
	mark := pal.glyph(tokens.GFilter)
	// THE BOX IS LAID OUT WHERE THE NAMES ARE AND THE LABELS OVER THEIR OWN
	// COLUMNS, both out of [tasksColumns] asked of the same LIST width every row
	// is — so the label a person clicks and the cells it stands over are the same
	// cells at every width, and the two lines cannot drift apart as the frame
	// moves. The place's left edge is spent out of the box, exactly as a row
	// spends it out of the name.
	stateCells, secondCells, nameCells := tasksColumns(width, by.key)
	boxCells := max(nameCells-ansi.StringWidth(tasksBareLead)-ansi.StringWidth(mark)-1, 1)
	// WHAT IS TYPED IS IN THE READING INK AND THE INVITATION IS DIM. A person has
	// to be able to tell the words they typed from the words the box came with.
	// The invitation is [tasksFilterHint] and not the foot's longer sentence
	// because the box is narrow at every width and a placeholder with its end cut
	// off reads as a bug in the box rather than as words the box came with.
	column := ansi.StringWidth(tasksBareLead) + ansi.StringWidth(mark) + 1
	said, ink := fit(tasksFilterHint, boxCells), pal.dim
	if query != "" {
		said, ink = fit(query, boxCells), pal.ink
		column += ansi.StringWidth(said)
	}
	out := tasksBareLead + pal.dim(mark) + " " + ink(said) + pad(boxCells-ansi.StringWidth(said))
	if cells := tasksProjectCells(width); cells > 0 {
		label := fit("project", cells-1)
		out += pal.dim(label) + pad(cells-ansi.StringWidth(label))
	}
	if stateCells > 0 {
		label := fit(stateLabel, stateCells)
		out += pal.dim(label) + pad(stateCells-ansi.StringWidth(label))
	}
	if secondCells > 0 {
		label := fit(secondLabel, secondCells)
		out += pad(secondCells-ansi.StringWidth(label)) + pal.dim(label)
	}
	return out + pad(tasksColumnAir), column
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
	// WORK IN ANOTHER WINDOW SAYS WHERE IT IS, WHOLE. The state column has twenty
	// cells and keeps only the words that correct the mark ([tasksStateField]);
	// this line has the width of the list, and WHICH window is exactly the half a
	// person needs to act — it is the difference between a terminal they can
	// switch to and one they have to go and find.
	said := strings.TrimSpace(tasksNote(item))
	if said == "" {
		said = strings.TrimSpace(item.status().RowWord())
	}
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

// tasksReasonShowing reports whether the list should grow that line at all: it
// is drawn WHERE THERE IS NO PANE, and nowhere else.
//
// IT ASKS THE PANE ITSELF ([app.taskPaneShowing]) rather than measuring a floor
// of its own. Two answers to "is the record already beside the list" is how a
// frame ends up saying one thing twice, and the pane's predicate knows something
// this file cannot: it asks the WHOLE FRAME, where a floor asked of the list's
// own width would decide there is no pane on exactly the frames that have one —
// the list is drawn in 72 of 122 cells while the pane stands beside it.
func tasksReasonShowing(a *app) bool {
	width, _ := a.size()
	return layoutTier(width) != tierPhone && !a.taskPaneShowing()
}
