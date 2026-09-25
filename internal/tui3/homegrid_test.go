package tui3

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/standing"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

// ── THE GRID'S LAWS (docs/design/home-mission-control/DESIGN.md §1) ──────────

// homeRowOf is the BODY row a word first appears on — the frame's four head
// rows are the router's, and the tab bar spells three of the panels' words —
// and the cell it starts at, or -1 for a word the body does not hold.
func homeRowOf(frame, word string) (row, col int) {
	for y, line := range strings.Split(frame, "\n") {
		if y < placeHeadRows {
			continue
		}
		if at := strings.Index(line, word); at >= 0 {
			return y, len([]rune(line[:at]))
		}
	}
	return -1, -1
}

// ACT LEFT, WATCH RIGHT: one column under a hundred and ten cells, two under a
// hundred and seventy, three past it (law 2).
func TestTheGridIsOneColumnThenTwoThenThree(t *testing.T) {
	for width, want := range map[int]int{60: 1, 109: 1, 110: 2, 169: 2, 170: 3, 240: 3} {
		if got := homeGridCols(width); got != want {
			t.Fatalf("at %d cells the grid has %d columns, want %d", width, got, want)
		}
	}
}

// AT EIGHTY CELLS EVERY PANEL STANDS IN ONE COLUMN, in the order a person reads
// the two-column page: their own panels, then the machine's.
func TestAtEightyTheGridIsOneColumnInReadingOrder(t *testing.T) {
	a := newSwitchLab(t).open(80, 24)
	frame := homeText(a)
	last := -1
	for _, word := range []string{sessionsWord, "Porting the Resume Picker", "projects"} {
		row, col := homeRowOf(frame, word)
		if row < 0 {
			t.Fatalf("%q is not on an eighty-cell home:\n%s", word, frame)
		}
		wantCol := homeGridMargin
		if word == "Porting the Resume Picker" {
			wantCol += homeGridLead
		}
		if row <= last || col != wantCol {
			t.Fatalf("%q is at row %d cell %d, after row %d in one column:\n%s", word, row, col, last, frame)
		}
		last = row
	}
	// AND EVERY HEADING THAT KEPT ITS PLACE CARRIES ITS EXPLAINER BESIDE IT, on
	// the heading's own row: the gloss is furniture of the heading, not a line of
	// its own.
	lines := strings.Split(frame, "\n")
	for _, slot := range homePanelOrder {
		if slot.explainer == "" {
			continue
		}
		row, _ := homeRowOf(frame, slot.word+rowSep)
		if row < 0 {
			continue // a twenty-four-row frame squeezes its later panels off
		}
		if !strings.Contains(lines[row], rowSep+slot.explainer) {
			t.Fatalf("%q does not carry %q on its own row:\n%s", slot.word, slot.explainer, frame)
		}
	}
}

// AT A HUNDRED AND TWENTY WHAT HAS ROWS IS THE FIELD AND EVERYTHING ELSE IS THE
// RAIL (law 2, ruled 2026-09-15), and the two columns start on the same row.
func TestAtOneTwentyWhatHasRowsTakesTheFieldAndTheQuietGatherInTheRail(t *testing.T) {
	a := newSwitchLab(t).open(120, 45)
	frame := homeText(a)
	needs, needsCol := homeRowOf(frame, "Porting the Resume Picker")
	projects, railCol := homeRowOf(frame, "projects")
	if needs < 0 || needs != projects+1 || railCol <= needsCol {
		t.Fatalf("needs you (row %d) and projects (row %d) are not the heads of the field and the rail:\n%s", needs, projects, frame)
	}
	// WHERE YOU WERE HAS ROWS, SO IT IS IN THE FIELD under the panel that
	// outranks it — the rank inside a column is the order table's as it always
	// was, and only the column is the content's to say.
	recent, recentCol := homeRowOf(frame, "Porting the Resume Picker")
	if recent != needs || recentCol != homeGridMargin+homeGridLead {
		t.Fatalf("threads has rows and is not under needs you in the field:\n%s", frame)
	}
	// AND SPEND IS PINNED UNDER PROJECTS AT THE TOP OF THE RAIL, with the quiet
	// panels under the pair rather than mixed through it.
	spend, spendCol := homeRowOf(frame, "spend")
	if spend <= projects || spendCol != railCol {
		t.Fatalf("spend is not pinned under projects at the top of the rail:\n%s", frame)
	}
	quiet, quietCol := homeRowOf(frame, "since you left")
	if quiet <= spend || quietCol != railCol {
		t.Fatalf("a quiet panel is not in the rail under the pinned pair:\n%s", frame)
	}
}

// AND THE RAIL TELLS ITS TWO GROUPS APART WITH AIR: the pinned pair that lives
// there, then a blank row that is not the ordinary one between panels, then the
// panels that are only there because they are quiet today.
func TestTheRailKeepsTheQuietPanelsAnExtraRowBelowThePinnedPair(t *testing.T) {
	a := newSwitchLab(t).open(120, 45)
	frame := homeText(a)
	lines := strings.Split(frame, "\n")
	spend, railCol := homeRowOf(frame, "spend")
	quiet, _ := homeRowOf(frame, "since you left")
	if spend < 0 || quiet < 0 {
		t.Fatalf("the rail is not drawn:\n%s", frame)
	}
	blank := 0
	for y := spend + 1; y < quiet; y++ {
		if strings.TrimSpace(string([]rune(lines[y])[min(railCol, len([]rune(lines[y]))):])) == "" {
			blank++
		}
	}
	if blank < 2 {
		t.Fatalf("the rail holds %d blank rows between the pinned pair and the quiet panels, want the ordinary one and the group's own:\n%s", blank, frame)
	}
}

// AT A HUNDRED AND EIGHTY THE FIELD IS THE FIRST COLUMN, THE RAIL IS THE LAST,
// AND THE MIDDLE BELONGS TO THE SELECTED ROW'S DESCRIPTION — so the rail is
// flush with the right edge at every width it exists at, and no panel is ever
// drawn between them.
//
// THE FIELD NEVER SPILLS SIDEWAYS (owner, 2026-09-15). What will not fit in one
// column folds, the way it always has at the widths with no second column, so
// the middle means one thing at every size instead of being a description
// sometimes and a panel other times.
func TestAtOneEightyTheFieldIsOneColumnAndTheMiddleIsNoPanels(t *testing.T) {
	a := newSwitchLab(t).open(180, 45)
	frame := homeText(a)
	needs, needsCol := homeRowOf(frame, "Porting the Resume Picker")
	recent, recentCol := homeRowOf(frame, "Porting the Resume Picker")
	projects, railCol := homeRowOf(frame, "projects")
	if needs != projects+1 || needsCol >= railCol {
		t.Fatalf("needs you and projects do not head the field and the rail:\n%s", frame)
	}
	if recent != needs || recentCol != homeGridMargin+homeGridLead {
		t.Fatalf("threads is not under needs you in the first field column:\n%s", frame)
	}
	if needsCol != homeGridMargin+homeGridLead {
		t.Fatalf("the field does not start at the left margin (cell %d):\n%s", needsCol, frame)
	}
	// AND THE QUIET PANELS ARE ALL IN THE LAST COLUMN, none of them left behind
	// in the field beside a panel that has something to say.
	for _, word := range []string{"spend", "since you left", "standing"} {
		if _, at := homeRowOf(frame, word); at != railCol {
			t.Fatalf("%q is at cell %d, want the rail at %d:\n%s", word, at, railCol, frame)
		}
	}
	// AND NOTHING AT ALL STANDS BETWEEN THEM. The middle column is the air the
	// ruling spends to put the field at one edge and the rail at the other.
	for at := range a.home.lines {
		if got := a.home.columnOf(at); got == homeDescCol(a.home.cols) {
			t.Fatalf("line %d stands in the description column, which no panel may be drawn in:\n%s", at, frame)
		}
	}
}

// THE MIDDLE COLUMN IS WHAT THE SELECTED ROW SAYS ABOUT ITSELF, on the row's own
// line — and the row is one line, because the line it used to grow is over there
// now.
func TestTheDescriptionColumnCarriesTheSelectedRowsOwnSentence(t *testing.T) {
	a := newSwitchLab(t).open(180, 45)
	homeText(a)
	homeLineOf(t, a, func(l homeLine) bool {
		return l.cell != nil && strings.TrimSpace(l.cell.sub) != "" && l.cell.thread == ""
	})
	line, _ := a.home.focusedLine()
	said := strings.TrimSpace(line.cell.sub)
	frame := homeText(a)

	// THE SENTENCE IS IN THE MIDDLE COLUMN and not under its row. It is looked
	// for THERE — at a cell past the field — because a short sentence can be a
	// substring of a heading (`here` was inside `where you were`, the panel's
	// old word), and the
	// first place the letters happen to appear is not the place the column
	// draws them.
	_, rail := homeRowOf(frame, "projects")
	xs, _ := homeGridGeometry(a.home.gridWidth, a.home.cols)
	descX := xs[homeDescCol(a.home.cols)]
	row, at := -1, -1
	for y, text := range strings.Split(frame, "\n") {
		if y < placeHeadRows {
			continue
		}
		if i := strings.Index(text, firstWordsOf(said)); i >= 0 && ansi.StringWidth(text[:i]) >= descX {
			row, at = y, ansi.StringWidth(text[:i])
			break
		}
	}
	if row < 0 {
		t.Fatalf("the selected row's sentence %q is nowhere in the description column:\n%s", said, frame)
	}
	if at <= homeGridMargin || at >= rail {
		t.Fatalf("the sentence is at cell %d, want it between the field at %d and the rail at %d:\n%s", at, homeGridMargin, rail, frame)
	}
	// AND IT STARTS ON THE ROW IT IS ABOUT, so the two read as one thing.
	title, _ := homeRowOf(frame, line.cell.title)
	if title != row {
		t.Fatalf("the sentence is on row %d and its row is on %d:\n%s", row, title, frame)
	}
	// AND THE ROW ITSELF IS ONE LINE: the line under it is another row, not its
	// own second line.
	// (Only the field's cells of that line are read: the rail beside it may
	// say the same word in a whisper of its own — `…priced here`.)
	lines := strings.Split(frame, "\n")
	if title+1 < len(lines) && strings.Contains(ansi.Truncate(lines[title+1], descX, ""), firstWordsOf(said)) {
		t.Fatalf("the row still draws its own second line:\n%s", frame)
	}
}

// `needs you` DRAWS ITS QUESTION IN THE DESCRIPTION COLUMN ONLY WHILE ITS ROW IS
// BEING READ — under the pointer, or under the cursor — on its own row's line,
// and once; the `?` stays beside the title on every frame. It used to stand in
// the column whether or not the row was selected (owner, 2026-09-15), and the
// owner reversed that on 2026-09-17: the mark always shows, the description
// only under the mouse.
func TestTheNeedsYouQuestionIsInTheColumnOnlyWhileItsRowIsRead(t *testing.T) {
	lab := newAnswerLab(t, consentQuestion(7, "needs your ok to run bash"), time.Now())
	a := lab.a
	a.width, a.height = 180, 45
	homeText(a)
	homeLineOf(t, a, func(l homeLine) bool {
		return l.cell != nil && l.cell.mark == cellMarkNeeds && strings.TrimSpace(l.cell.sub) != ""
	})
	line, _ := a.home.focusedLine()
	said := strings.TrimSpace(line.cell.sub)
	question := a.home.cursor
	// WITH THE CURSOR SOMEWHERE ELSE THE QUESTION IS NOT ON THE FRAME, and the
	// mark still is.
	a.home.cursor = a.home.placesTop()
	homeLineOf(t, a, func(l homeLine) bool { return l.cell != nil && l.cell.panel == panelSessions })
	frame := homeText(a)
	if row, _ := homeRowOf(frame, firstWordsOf(said)); row >= 0 {
		t.Fatalf("the question is on the frame with the cursor off its row:\n%s", frame)
	}
	if !strings.Contains(frame, a.pal.glyph(tokens.GNeedsHuman)+" "+line.cell.title) {
		t.Fatalf("the mark left the row with the cursor off it:\n%s", frame)
	}
	// AND UNDER THE POINTER IT IS BACK, in the column: its thread's title line
	// on its row's line, a blank, then the question two lines under.
	a.home.hover, a.home.cursor = question, question
	frame = homeText(a)
	row, at := homeRowOf(frame, homeThreadWord+line.cell.title)
	if row < 0 {
		t.Fatalf("the read row's thread title is not on the frame with the pointer on its row:\n%s", frame)
	}
	if sentence, _ := homeRowOf(frame, firstWordsOf(said)); sentence != row+2 {
		t.Fatalf("the question is on row %d, want two under its thread title at %d:\n%s", sentence, row, frame)
	}
	_, rail := homeRowOf(frame, "projects")
	if at <= homeGridMargin || at >= rail {
		t.Fatalf("the question is at cell %d, want the description column between %d and %d:\n%s", at, homeGridMargin, rail, frame)
	}
	// AND ON ITS OWN ROW'S LINE, so which row it belongs to is where it is.
	title, _ := homeRowOf(frame, line.cell.title)
	if title != row {
		t.Fatalf("the question is on row %d and its row is on %d:\n%s", row, title, frame)
	}
	// AND ONCE ONLY. The same words twice on one frame is the reader wondering
	// which is which.
	if n := strings.Count(frame, firstWordsOf(said)); n != 1 {
		t.Fatalf("the question is on the frame %d times, want once:\n%s", n, frame)
	}
}

// A QUESTION HOME RAISES IS DRAWN IN THE DESCRIPTION COLUMN, beside the row
// whose `enter` raised it — and the foot does not say it a second time.
//
// Until the column existed the grid had nowhere to put the card: it belongs to
// the detail column beside the typed search, which is not up at rest, so a
// raised question left only its one-line foot version appended to the resting
// sentence. A person pressing enter saw the bottom of the screen change and read
// it as noise (owner, 2026-09-15).
func TestARaisedQuestionIsDrawnInTheDescriptionColumnAndNotOnTheFoot(t *testing.T) {
	lab := newAnswerLab(t, consentQuestion(7, "needs your ok to run bash"), time.Now())
	a := lab.a
	a.width, a.height = 180, 40
	homeText(a)
	homeLineOf(t, a, func(l homeLine) bool {
		return l.cell != nil && l.cell.mark == cellMarkNeeds && l.kind == homeSession
	})
	a.placeKeyPress(key("enter"))
	ask, ok := a.homeAsking()
	if !ok {
		t.Fatal("enter on a row another window holds raised no question")
	}
	frame := homeText(a)
	head := strings.TrimSpace(ask.question.Head)
	row, at := homeRowOf(frame, head)
	if row < 0 {
		t.Fatalf("the question %q is not on the frame:\n%s", head, frame)
	}
	_, rail := homeRowOf(frame, "projects")
	if at <= homeGridMargin || at >= rail {
		t.Fatalf("the question is at cell %d, want the description column between %d and %d:\n%s", at, homeGridMargin, rail, frame)
	}
	// AND THE FOOT KEEPS ITS OWN SENTENCE. One decision drawn twice on one screen
	// is the defect the question block exists to end.
	lines := strings.Split(frame, "\n")
	if foot := lines[len(lines)-1]; strings.Contains(foot, head) {
		t.Fatalf("the foot repeats the question: %q", foot)
	}
}

// THE `?` LEADS THE ROW, ALWAYS, and the question in the description column
// wears none. The mark used to move beside the question on a frame whose column
// drew the question permanently (owner, 2026-09-15); the question is drawn only
// while the row is being read now, and the owner asked (2026-09-17) that the
// mark always show — so it stands beside the title on every frame.
func TestTheNeedsMarkLeadsTheRowAndNotTheQuestion(t *testing.T) {
	lab := newAnswerLab(t, consentQuestion(7, "needs your ok to run bash"), time.Now())
	a := lab.a
	a.width, a.height = 180, 45
	homeText(a)
	homeLineOf(t, a, func(l homeLine) bool {
		return l.cell != nil && l.cell.mark == cellMarkNeeds
	})
	line, _ := a.home.focusedLine()
	mark := a.pal.glyph(tokens.GNeedsHuman)
	frame := homeText(a)
	lines := strings.Split(frame, "\n")

	row, _ := homeRowOf(frame, line.cell.title)
	if row < 0 {
		t.Fatalf("the waiting row is not on the frame:\n%s", frame)
	}
	at := strings.Index(lines[row], mark)
	if at < 0 {
		t.Fatalf("the mark is nowhere on the row's line:\n%s", frame)
	}
	// IT IS IN THE ROW'S OWN LEAD, right before the title, and not beside the
	// question in the column — which is two lines under the row, past the
	// thread's title line and its blank.
	if row+2 >= len(lines) || !strings.Contains(lines[row+2], firstWordsOf(strings.TrimSpace(line.cell.sub))) {
		t.Fatalf("the question is not two lines under the row under the cursor:\n%s", frame)
	}
	title := strings.Index(lines[row], line.cell.title)
	if title < 0 || at > title || title-at > homeGridLead+1 {
		t.Fatalf("the mark at %d does not lead the row's title at %d:\n%s", at, title, frame)
	}
	if strings.Contains(lines[row+2], mark) {
		t.Fatalf("the question in the column wears the mark too:\n%s", frame)
	}
	// AND THERE IS STILL ONLY ONE OF IT (law 8).
	if n := strings.Count(lines[row], mark); n != 1 {
		t.Fatalf("the row's line wears %d marks, want one:\n%s", n, frame)
	}
}

// AND A FRAME TOO SHORT FOR THE CARD FALLS BACK TO THE FOOT rather than drawing
// a cut one. A card with its closing edge, its keys and usually an answer off
// the bottom of the screen is a decision a person cannot answer; the one-line
// foot version exists for exactly the frames with no room for a card, and this
// is one of them (owner, 2026-09-15: "what happens if we run out of vertical
// space?").
func TestAFrameTooShortForTheQuestionCardSaysItOnTheFootInstead(t *testing.T) {
	lab := newAnswerLab(t, consentQuestion(7, "needs your ok to run bash"), time.Now())
	a := lab.a
	a.width, a.height = 180, 45
	homeText(a)
	homeLineOf(t, a, func(l homeLine) bool {
		return l.cell != nil && l.cell.mark == cellMarkNeeds && l.kind == homeSession
	})
	a.placeKeyPress(key("enter"))
	ask, ok := a.homeAsking()
	if !ok {
		t.Fatal("enter raised no question")
	}
	head := strings.TrimSpace(ask.question.Head)

	// TALL: the card is in the column and the foot keeps its own sentence.
	tall := homeText(a)
	if !a.homeAskFitsColumn() {
		t.Fatalf("the card does not fit a 45-row frame:\n%s", tall)
	}
	if row, _ := homeRowOf(tall, head); row < 0 {
		t.Fatalf("the question is not in the column on a tall frame:\n%s", tall)
	}
	tallFoot := lastLineOf(tall)
	if strings.Contains(tallFoot, head) {
		t.Fatalf("the foot repeats a question the column drew: %q", tallFoot)
	}

	// SHORT: no card at all, and the foot picks it up.
	a.width, a.height = 180, 12
	short := homeText(a)
	if a.homeAskFitsColumn() {
		t.Fatalf("a 12-row frame claims room for the card:\n%s", short)
	}
	for _, line := range strings.Split(short, "\n") {
		if strings.Contains(line, "╭") || strings.Contains(line, "╰") {
			t.Fatalf("a cut card was drawn on a frame with no room for it:\n%s", short)
		}
	}
	if foot := lastLineOf(short); !strings.Contains(foot, head) {
		t.Fatalf("the short frame says the question nowhere; its foot is %q:\n%s", foot, short)
	}
}

// lastLineOf is a frame's foot.
func lastLineOf(frame string) string {
	lines := strings.Split(frame, "\n")
	return lines[len(lines)-1]
}

// firstWordsOf is enough of a sentence to find it on a frame that may have
// wrapped the rest of it.
func firstWordsOf(said string) string {
	words := strings.Fields(said)
	if len(words) > 4 {
		words = words[:4]
	}
	return strings.Join(words, " ")
}

// A SHORT TERMINAL SQUEEZES IN PRIORITY ORDER (law 5): next up gives way first,
// then spend, and a squeezed panel keeps its heading and its fold.
func TestAShortColumnShrinksTheLowestPriorityPanelFirst(t *testing.T) {
	rows := func(n int) homePanelRows {
		lines := make([]homeLine, n)
		for i := range lines {
			lines[i] = homeLine{kind: homeLedger, cell: &homeCell{title: "row"}}
		}
		return homePanelRows{lines: lines}
	}
	column := []*homeGridPanel{
		{slot: homeSlotOf(panelSessions), read: rows(4), shown: 4},
		{slot: homeSlotOf(panelSpend), read: rows(4), shown: 4},
		{slot: homeSlotOf(panelNext), read: rows(4), shown: 4},
	}
	// Three panels of a heading and four rows, and two blanks between: 17.
	// Next up at its floor of three frees two.
	squeezeColumn(column, 15)
	if column[2].shown == 4 || column[1].shown != 4 || column[0].shown != 4 {
		t.Fatalf("next up should give way alone: shown %d %d %d", column[0].shown, column[1].shown, column[2].shown)
	}
	if got := column[2].height(); got != homeSlotOf(panelNext).least {
		t.Fatalf("next up squeezed to %d rows, want its floor of %d", got, homeSlotOf(panelNext).least)
	}
	squeezeColumn(column, 8)
	if !column[2].dropped || column[0].dropped {
		t.Fatalf("at eight rows next up should be gone and running kept: %+v %+v", column[0], column[2])
	}
	if homeColumnHeight(column) > 8 {
		t.Fatalf("the column is %d rows in a room of 8", homeColumnHeight(column))
	}
}

// A TALL COLUMN HANDS ITS SPARE ROWS OUT IN THE SQUEEZE'S ORDER REVERSED, one
// at a time: needs you first, then threads, round again — and never a
// row past a panel's budget, never a row the room cannot hold.
func TestATallColumnGrowsWhatAPersonCameForFirst(t *testing.T) {
	rows := func(n int) homePanelRows {
		lines := make([]homeLine, n)
		for i := range lines {
			lines[i] = homeLine{kind: homeLedger, cell: &homeCell{title: "row"}}
		}
		return homePanelRows{lines: lines, more: 20}
	}
	needs := &homeGridPanel{slot: homeSlotOf(panelNeeds), read: rows(8), shown: 4}
	recent := &homeGridPanel{slot: homeSlotOf(panelSessions), read: rows(10), shown: 5}
	column := []*homeGridPanel{needs, recent}
	// A heading, the rows and a fold each, and a blank between: 6 + 1 + 7 = 14.
	// Sessions gets two spare rows and the auxiliary questions get one.
	fitColumn(column, 16)
	if needs.shown != 5 || recent.shown != 7 {
		t.Fatalf("three spare rows grew needs to %d and recent to %d, want 5 and 7", needs.shown, recent.shown)
	}
	fitColumn(column, 200)
	if needs.shown != 8 || recent.shown != 10 {
		t.Fatalf("a room of 200 grew needs to %d and recent to %d, want their budgets 8 and 10", needs.shown, recent.shown)
	}
}

// A tall frame shows the whole bounded tab list; a short one folds what cannot fit.
func TestATallFrameShowsOpenTabsAndAShortFrameFoldsThem(t *testing.T) {
	lab := newHomeLab(t)
	workspace := lab.workspace("alpha")
	now := time.Now()
	mine := lab.session("-alpha", "aaaa000000000000", "this very chat", workspace, now)
	for i := 1; i < tabsCap; i++ {
		lab.session("-alpha", fmt.Sprintf("aaaa%012d", i), fmt.Sprintf("older chat %d", i), workspace, now.Add(-time.Duration(i)*time.Hour))
	}
	a := lab.app(mine)
	a.width, a.height = 120, 55
	openHomeFixtureTabs(a)
	a.openHome()
	homeText(a)
	if got := len(panelRows(a, panelSessions)); got != min(tabsCap, homeSessionsLimit) {
		t.Fatalf("tall Home has %d rows, want %d tabs", got, tabsCap)
	}
	a.height = 20
	frame := homeText(a)
	if len(strings.Split(frame, "\n")) != 20 || len(panelRows(a, panelSessions)) >= min(tabsCap, homeSessionsLimit) {
		t.Fatalf("short frame did not fold its tabs:\n%s", frame)
	}
	homeFoldDoor(t, a, panelSessions)
}

// AND A REAL FRAME THAT SHORT STILL DRAWS EVERY HEADING IT KEPT, with the
// person's two panels first to stay.
func TestTheSqueezeAtOneTwentyByTwentyFourKeepsNeedsAndRecent(t *testing.T) {
	a := newSwitchLab(t).open(120, 24)
	frame := homeText(a)
	for _, word := range []string{"Porting the Resume Picker"} {
		if row, _ := homeRowOf(frame, word); row < 0 {
			t.Fatalf("%q was squeezed off a 120×24 home:\n%s", word, frame)
		}
	}
	if len(strings.Split(frame, "\n")) != 24 {
		t.Fatalf("the frame is not 24 rows")
	}
}

// A HEADING'S EXPLAINER IS DRAWN ONE SHADE UNDER IT, AND IT GIVES WAY WHOLE.
// Beside a heading with the room for it, the panel's gloss follows the heading's
// own ink in the dim ink; where the column cannot hold the heading, the
// separator and the gloss TOGETHER, the gloss is dropped and the heading is left
// exactly as it read before explainers existed — never cut to make room for a
// gloss. A heading with a right-hand clause measures its gloss against the room
// the clause leaves rather than the whole column.
func TestAHeadingExplainerIsDimAndGivesWayBeforeTheHeadingIsCut(t *testing.T) {
	pal := newTestPalette()
	cell := &homeCell{kind: cellHead, panel: panelSessions, title: "tasks", note: "work you sent off"}
	tail := rowSep + cell.note
	whole := ansi.StringWidth(cell.title) + ansi.StringWidth(tail)

	// Room for all of it: the gloss is dim, and the heading keeps its own ink.
	drawn := homeCellHead(cell, whole, pal, false, false)
	if !strings.Contains(drawn, placeHeadingInk(pal)(cell.title)+pal.dim(tail)) {
		t.Fatalf("the explainer is not painted dim after the heading's own ink:\n%q", drawn)
	}
	// One cell short of the whole: the gloss goes, and the heading is untouched.
	if got := plain(homeCellHead(cell, whole-1, pal, false, false)); got != cell.title {
		t.Fatalf("a column one cell short drew %q, want the heading alone", got)
	}
	// A column too narrow for even the heading cuts it, exactly as it always
	// did — the gloss never turns that cut into something worse.
	cut := plain(homeCellHead(cell, ansi.StringWidth(cell.title)-1, pal, false, false))
	if !strings.Contains(cut, glyphMore) || strings.Contains(cut, cell.note) {
		t.Fatalf("a too-narrow column drew %q, want the heading cut with no gloss", cut)
	}
	// A heading with a right-hand clause drops its gloss past the room the
	// clause leaves, not the whole column.
	clause := &homeCell{kind: cellHead, title: "spend", note: "spent today", right: "today $6.51 of $500"}
	if !strings.Contains(plain(homeCellHead(clause, 39, pal, false, false)), clause.note) {
		t.Fatalf("the gloss does not fit the room its right-hand clause leaves at 39 cells")
	}
	if got := plain(homeCellHead(clause, 38, pal, false, false)); strings.Contains(got, clause.note) {
		t.Fatalf("the gloss was kept past the room its right-hand clause leaves: %q", got)
	}
}

// A WHISPER NEVER OUTRANKS A ROW. When a short frame has to drop a whole
// panel, the panels that are only whispering go first, whatever their keep:
// a person can open a row and cannot open a whisper. Lane R2 found a 120×14
// home that was two whispers and no conversation at all.
func TestAShortFrameDropsWhisperingPanelsBeforeAnyPanelWithRows(t *testing.T) {
	lab := newHomeLab(t)
	here := lab.workspace("alpha")
	mine := lab.session("-alpha", "aaaa000000000001", "the one I am in", here, time.Now())
	for i := 2; i <= 6; i++ {
		lab.session(fmt.Sprintf("-alpha-%d", i), fmt.Sprintf("aaaa00000000000%d", i), fmt.Sprintf("quiet chat %d", i), here, time.Now().Add(-time.Duration(i)*time.Hour))
	}
	a := lab.app(mine)
	a.width, a.height = 120, 14
	a.openHome()
	frame := homeText(a)
	if row, _ := homeRowOf(frame, homeName(a.home.focused())); row < 0 {
		t.Fatalf("a 120×14 home kept a whisper and dropped every conversation:\n%s", frame)
	}
	if row, _ := homeRowOf(frame, "questions from any chat or task land here"); row >= 0 {
		t.Fatalf("a 120×14 home spent its rows on the needs-you whisper:\n%s", frame)
	}
}

// AN EMPTY PANEL WHISPERS (law 4): its heading and one dim line naming what
// arrives there — and never a sentence saying it is empty.
func TestAnEmptyPanelWhispersWhatArrivesAndNeverThatItIsEmpty(t *testing.T) {
	for id, words := range homeWhisper {
		for _, banned := range []string{"nothing", "no ", "empty", "none"} {
			if strings.HasPrefix(words, banned) || strings.Contains(words, " "+banned) {
				t.Fatalf("panel %d whispers %q, which announces an absence", id, words)
			}
		}
	}
	lab := newHomeLab(t)
	mine := lab.session("-alpha", "aaaa000000000001", "the only chat", lab.workspace("alpha"), time.Now())
	a := lab.app(mine)
	a.width, a.height = 120, 45
	a.openHome()
	a.home.world.Projects = nil
	a.home.tabs = nil
	a.home.closedTabs = nil
	a.home.build()
	frame := homeText(a)
	if row, _ := homeRowOf(frame, homeWhisper[panelSessions]); row < 0 {
		t.Fatalf("an empty running panel does not whisper:\n%s", frame)
	}
}

// A WHISPER WRAPS; IT IS NEVER CUT. At every column width a person meets —
// fifty-eight at 120 cells, eighty, a hundred and twenty — every whisper is all
// of its words, on lines that fit inside the row's lead, with no ellipsis.
func TestAWhisperWrapsAtItsColumnAndIsNeverCut(t *testing.T) {
	for _, width := range []int{58, 80, 120} {
		for id, words := range homeWhisper {
			lines := homeWhisperLines(words, width)
			for _, line := range lines {
				if strings.Contains(line, glyphMore) || len([]rune(line)) > width-homeGridLead {
					t.Fatalf("panel %d at %d cells whispers %q", id, width, line)
				}
			}
			if got := strings.Join(lines, " "); got != words {
				t.Fatalf("panel %d at %d cells whispers %q, want every word of %q", id, width, got, words)
			}
		}
	}
	// AND A REAL FRAME DRAWS THE SECOND LINE: at 120 cells a column is 58 wide,
	// and the end of the needs whisper is on the line under its first half,
	// standing in the row's lead. A quiet needs you is in the rail (law 2), so
	// the lead it stands in is the rail's and not the margin's.
	a := newLiveLab(t).open()
	frame := homeText(a)
	_, rail := homeRowOf(frame, "projects")
	first, head := homeRowOf(frame, "reminders, routines")
	second, at := homeRowOf(frame, `6" or`)
	if first < 0 || second != first+1 || at != head || at != rail+homeGridLead {
		t.Fatalf("the scheduled whisper is cut rather than wrapped in the rail:\n%s", frame)
	}
}

// firstRowOf is the line of the first row a panel draws under its heading —
// an errand's row included, which wears no cell of its own.
func firstRowOf(a *app, panel homePanelID) (int, bool) {
	under := false
	for at, line := range a.home.lines {
		if line.cell != nil && line.cell.kind == cellHead {
			under = line.cell.panel == panel
			continue
		}
		if under && line.stop() {
			return at, true
		}
	}
	return homeNoLine, false
}

// homeEmptyWhispers are what an empty machine's home always whispers: the two
// panels the squeeze never drops.
func homeEmptyWhispers() []string {
	return []string{homeWhisper[panelSessions]}
}

// focusedTitle is the title of the row the cursor is on.
func focusedTitle(a *app) string {
	if line, ok := a.home.focusedLine(); ok && line.cell != nil {
		return line.cell.title
	}
	return ""
}

// THE ARROWS STAY IN THE FIELD AND THE FOOT IS THE RESTING SENTENCE (owner,
// 2026-09-17). `→` on a field row opens that row's own strip rather than
// crossing to the rail, `←` closes it, and `ctrl+o` still opens the row's
// folder without the strip. A project's row is not a stop and has no folder
// door of its own any more.
func TestTheArrowsStayInTheFieldAndTheFootIsTheRestingSentence(t *testing.T) {
	var opened string
	was := processOpener
	processOpener = func(target string) error { opened = target; return nil }
	t.Cleanup(func() { processOpener = was })

	a := newSwitchLab(t).open(120, 45)
	if hint := a.homeHint(); strings.Contains(hint, "ctrl+o") || strings.Contains(hint, "tab next place") || hint != restingFoot(a) {
		t.Fatalf("a left-column conversation's foot is %q, want the shared home hints", hint)
	}
	a.placeKeyPress(key("ctrl+o"))
	if opened == "" || !strings.HasSuffix(opened, "alpha") {
		t.Fatalf("ctrl+o opened %q, want the conversation's folder", opened)
	}
	from := a.home.cursor
	a.placeKeyPress(key("right"))
	if a.home.cursor != from || !a.strip.open {
		t.Fatalf("→ on a field row moved the cursor from %d to %d (strip %v), want the row's own strip", from, a.home.cursor, a.strip.open)
	}
	a.placeKeyPress(key("left"))
	if a.home.cursor != from || a.strip.open {
		t.Fatalf("← did not close the strip and stay on the row (cursor %d, strip %v)", a.home.cursor, a.strip.open)
	}
	for _, line := range a.home.lines {
		if line.kind == homeProjectRow && line.stop() {
			t.Fatalf("a project's row is a cursor stop: %+v", line.cell)
		}
	}
}

// AND THE SAME ON EVERY KIND OF FIELD ROW at three columns: a conversation, a
// `standing` order and a `since you left` line all rest on the one sentence,
// and `ctrl+o` opens the folder each belongs to.
func TestEveryFieldRowRestsOnTheOneFootAndOpensItsFolder(t *testing.T) {
	var opened string
	was := processOpener
	processOpener = func(target string) error { opened = target; return nil }
	t.Cleanup(func() { processOpener = was })

	lab := newSwitchLab(t)
	a := lab.open(180, 45)
	dir := a.home.world.Projects[0].Dir
	a.home.items = map[string][]StandingItemView{dir: {{Item: standing.Item{ID: "w1", Words: "water the plants",
		Workspace: "/w/alpha", Status: standing.StatusActive, When: standing.When{Kind: standing.WhenAt},
		NextDue: lab.now.Add(2 * time.Hour)}}}}
	a.home.build()
	want := restingFoot(a)

	homeLineOf(t, a, func(l homeLine) bool { return l.kind == homeSession && l.cell != nil && l.cell.panel == panelSessions })
	if hint := a.homeHint(); hint != want {
		t.Fatalf("a conversation's foot is %q, want %q", hint, want)
	}
	homeLineOf(t, a, func(l homeLine) bool { return l.cell != nil && l.cell.panel == panelNext && l.stop() })
	if hint := a.homeHint(); hint != want {
		t.Fatalf("a next up row's foot is %q, want %q", hint, want)
	}
	a.placeKeyPress(key("ctrl+o"))
	if opened != "/w/alpha" {
		t.Fatalf("ctrl+o on a standing order opened %q, want the workspace it stands over", opened)
	}
}

// AND A LANDING ON `since you left` IS ONE OF THEM: its chord opens the folder of
// the conversation that ran the work.
func TestASinceYouLeftRowRestsOnTheOneFootAndOpensItsConversationsFolder(t *testing.T) {
	var opened string
	was := processOpener
	processOpener = func(target string) error { opened = target; return nil }
	t.Cleanup(func() { processOpener = was })

	l := newLiveLab(t)
	l.task("-alpha", session.TaskIndexEntry{ID: "1", SessionID: "aaaa000000000002", Label: "spark fleet ssh audit", Title: "spark fleet ssh audit",
		Status: string(session.TaskDone), Outcome: "all up", EndedAt: l.now.Add(-time.Hour)})
	a := l.open()
	a.width, a.height = 180, 45
	a.home.seen = l.now.Add(-4 * time.Hour)
	a.home.build()
	homeLineOf(t, a, func(l homeLine) bool { return l.cell != nil && l.cell.panel == panelLeft && l.cell.kind == cellRow })
	if hint, want := a.homeHint(), restingFoot(a); hint != want {
		t.Fatalf("a since you left row's foot is %q, want %q", hint, want)
	}
	a.placeKeyPress(key("ctrl+o"))
	if opened == "" || !strings.HasSuffix(opened, "alpha") {
		t.Fatalf("ctrl+o on a landing opened %q, want the folder of the conversation that ran it", opened)
	}
}

// THE ARROWS WALK THE FIELD AND NEVER LEAVE IT (owner, 2026-09-17): `↓` is
// the next row of the column, `→` is the row's own strip and not the rail,
// `←` closes it, and `↑` off the top row stays there rather than climbing onto
// the tab bar.
func TestTheArrowsWalkTheFieldAndNeverLeaveIt(t *testing.T) {
	a := newSwitchLab(t).open(120, 45)
	if got := focusedTitle(a); got != "Porting the Resume Picker" {
		t.Fatalf("home opened on %q", got)
	}
	a.placeKeyPress(key("down"))
	if got := focusedTitle(a); got != "Swarm Task Splitting" {
		t.Fatalf("↓ went to %q, want the next row of threads", got)
	}
	here := a.home.cursor
	a.placeKeyPress(key("right"))
	if a.home.cursor != here || a.home.columnOf(a.home.cursor) != 0 || !a.strip.open {
		t.Fatalf("→ went to line %d in column %d (strip %v), want the row's own strip", a.home.cursor, a.home.columnOf(a.home.cursor), a.strip.open)
	}
	a.placeKeyPress(key("left"))
	if a.home.cursor != here || a.strip.open {
		t.Fatalf("← left the cursor on line %d with the strip %v, want the row with its strip closed", a.home.cursor, a.strip.open)
	}
	for i := 0; i < 20; i++ {
		a.placeKeyPress(key("up"))
		if a.home.columnOf(a.home.cursor) != 0 {
			t.Fatal("↑ walked out of the field sideways")
		}
	}
	if a.bar.on || a.home.cursor != a.home.placesTop() {
		t.Fatalf("↑ off the top of the field left the cursor on line %d (bar %v), want the top row at %d", a.home.cursor, a.bar.on, a.home.placesTop())
	}
}

// A CLICK IN THE RIGHT COLUMN LANDS ON THE ROW DRAWN THERE, not the left
// column's row that shares its screen line — and opens it, as `enter` would:
// a task row opens that task inside the tasks place (homepanel_running.go's
// [app.openTaskDoor]).
func TestAClickResolvesTheColumnItLandedIn(t *testing.T) {
	a := newSwitchLab(t).open(120, 45)
	x, y, ok := homeHeadingAt(a, sessionsWord)
	if !ok {
		t.Fatal("no sessions heading")
	}
	a.homePress(x, y)
	if !a.at(pageTasks) {
		t.Fatal("click did not open sessions")
	}

}

// THE AGE OUTRANKS THE TAIL OF A TITLE: a seventy-character title in a
// fifty-eight-cell column is cut, and the row still says how long ago it was.
func TestALongTitleIsCutBeforeItsAge(t *testing.T) {
	const width = 58
	title := strings.Repeat("Generate and Display First 200 Primes ", 2)[:70]
	cell := &homeCell{panel: panelSessions, title: title, right: "1h"}
	row := plain(homeCellBody(cell, width-homeGridLead, newTestPalette(), false))
	if !strings.HasSuffix(row, " 1h") || strings.Contains(row, title) || !strings.Contains(row, "...") {
		t.Fatalf("the title was not cut to keep its age: %q", row)
	}
	if got := len([]rune(row)); got > width-homeGridLead {
		t.Fatalf("the row is %d cells wide, its column holds %d", got, width-homeGridLead)
	}
}

// A PANEL'S FOLD IS A TOGGLE (owner, 2026-09-15). `66 more` under `where you
// were` is a stop; enter on it opens the panel — every conversation the column
// can hold, the other panels squeezed to their floors — and the fold now reads
// `N fewer`, with the cursor still on it. Enter again shuts it. It used to be
// `66 more · type to find one`, which could not be stood on at all.
func TestEnterOnAFoldOpensThePanelAndAgainShutsIt(t *testing.T) {
	a, _ := homeTabsFixture(t)
	a.height = 14
	homeText(a)
	a.home.cursor = homeFoldDoor(t, a, panelSessions)
	drive(t, a, key("enter"))
	if !a.at(pageHome) || !a.home.openedOn || a.home.opened != panelSessions {
		t.Fatal("Enter did not open the conversation fold")
	}
	a.home.cursor = homeFoldDoor(t, a, panelSessions)
	drive(t, a, key("enter"))
	if a.home.openedOn {
		t.Fatal("Enter did not close the fold")
	}
}

// ONE PANEL IS OPEN AT A TIME. Opening a second shuts the first, and the first's
// fold says `more` again. Ten questions fold `needs you` and twelve running
// tasks fold `tasks`, on one frame.
func TestOpeningASecondFoldShutsTheFirst(t *testing.T) {
	lab := newSwitchLab(t)
	for i := 0; i < 9; i++ {
		lab.task("-beta", session.TaskIndexEntry{ID: itoa(i + 1), SessionID: "bbbb000000000001", Label: "question " + itoa(i), Title: "question " + itoa(i), Status: string(session.TaskUnverified), EndedAt: lab.now.Add(-time.Minute)})
	}
	a := lab.open(120, 24)
	a.home.cursor = homeFoldDoor(t, a, panelNeeds)
	drive(t, a, key("enter"))
	if !a.home.openedOn || a.home.opened != panelNeeds {
		t.Fatal("first fold did not open")
	}
	a.home.cursor = homeFoldDoor(t, a, panelSessions)
	drive(t, a, key("enter"))
	if !a.home.openedOn || a.home.opened != panelSessions {
		t.Fatal("second fold did not replace first")
	}

}

// AN OPEN PANEL TALLER THAN THE COLUMN NAMES WHERE THE REST ARE. On a short
// frame the opened `needs you` shows what fits and its fold reads `N fewer · M
// more · tasks` — the way back first, then the place that holds the rest — so
// nothing an open fold could not show is left with no door.
// An open panel taller than the column still counts what it cannot show, and
// names no place for it: `enter` on the fold toggles the panel, so a line that
// said `· tasks` would be a door `enter` does not take. The heading is that door.
func TestAnOpenFoldOnAShortFrameCountsTheRestAndNamesNoPlace(t *testing.T) {
	lab := newSwitchLab(t)
	for i := 0; i < 9; i++ {
		lab.presence("-beta", "cccc00000000000"+string(rune('1'+i)), session.PresenceWaiting, "question "+itoa(i), lab.now)
	}
	a := lab.open(120, 20)
	a.home.cursor = homeFoldDoor(t, a, panelSessions)
	drive(t, a, key("enter"))
	fold := a.home.lines[homeFoldDoor(t, a, panelSessions)].cell.title
	if !strings.Contains(fold, " fewer · ") || !strings.HasSuffix(fold, " "+homeFoldMoreWord) {
		t.Fatalf("the open fold on a short frame reads %q, want `N fewer · M more`", fold)
	}
	if strings.Contains(fold, rowSep+pageTasks.word()) {
		t.Fatalf("the open fold names a place enter does not go to: %q", fold)
	}
}

// restingFoot is the list's two keys followed by the available draft controls.
func restingFoot(a *app) string {
	return dotted(homeOptionsWord, a.targetChordWords())
}
