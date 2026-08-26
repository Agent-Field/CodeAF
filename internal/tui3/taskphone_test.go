package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// TASKS ON A PHONE ARE A VISIBLE DOOR, A SCROLLABLE LIST OF CARDS, AND A WAY
// BACK. None of the surfaces is new — the strip, the roster page and the record
// card all exist and all answer a key — but under 60 columns each is reshaped so
// a thumb can do the whole flow with no keyboard anywhere in it.

// taskSheetLines is the roster page row by row, as a reader sees it.
func taskSheetLines(a *app) []string {
	width, height := a.size()
	lines, _, _, _ := a.taskSheetFrame(width, height)
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = plain(line)
	}
	return out
}

// taskSheetHitY is the first screen row that answers to a kind of hit, and
// whether there is one.
func taskSheetHitY(a *app, want taskSheetHitKind) (int, bool) {
	width, height := a.size()
	_, hits, _, _ := a.taskSheetFrame(width, height)
	for y, hit := range hits {
		if hit.kind == want {
			return y, true
		}
	}
	return 0, false
}

// THE STRIP IS ONE DOOR AT PHONE WIDTH. It is not a row of chips a thumb cannot
// land between — it is a single full-width row that says how many tasks there are
// and that it opens, and a tap anywhere on it opens the roster PAGE.
func TestThePhoneStripIsOneTasksDoorThatOpensTheRoster(t *testing.T) {
	a, _, _ := taskApp(t)
	a.width, a.height = 50, 28
	railRun(a)

	// railRun leaves two running and two queued nodes live, and done work off the
	// live set — four tasks, two of them running.
	door := stripText(a)
	if !strings.Contains(door, "▸ 4 tasks") {
		t.Fatalf("the phone strip is not the tasks door:\n%q", door)
	}
	if !strings.Contains(door, "2 running") {
		t.Fatalf("the door does not name the most urgent state:\n%q", door)
	}
	// It is one row, and it never draws a chip's name.
	if strings.Contains(door, "Ship the port") {
		t.Fatalf("the phone door drew a chip's name:\n%q", door)
	}
	if w := ansi.StringWidth(door); w > a.width {
		t.Fatalf("the door is %d cells wide on a %d-column frame:\n%q", w, a.width, door)
	}

	// A tap ANYWHERE on the row opens the roster page — not the overlay column —
	// in one gesture.
	drive(t, a, tea.MouseClickMsg{X: a.width - 2, Y: a.headHeight(), Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: a.width - 2, Y: a.headHeight(), Button: tea.MouseLeft})
	if !a.at(pageTasks) {
		t.Fatal("a tap on the phone door did not open the roster page")
	}
	if a.railFull() {
		t.Fatal("the phone door opened the overlay column instead of the page")
	}
}

// ONE TASK IS STILL A DOOR. A person on a phone reaches every task the same way,
// so the row is drawn — singular — even when there is only one.
func TestThePhoneStripDrawsTheDoorForASingleTask(t *testing.T) {
	a, _, _ := taskApp(t)
	a.width, a.height = 50, 28
	a.taskUpdate(update(1, "Ship the port", session.TaskRunning, session.TaskNotice{}))

	door := stripText(a)
	if !strings.Contains(door, "▸ 1 task ") && !strings.HasSuffix(strings.TrimSpace(door), "▸ 1 task") {
		// The tail may add the state; the count word is what must be singular.
		if !strings.Contains(door, "▸ 1 task") {
			t.Fatalf("one task did not draw a singular door:\n%q", door)
		}
	}
	if strings.Contains(door, "1 tasks") {
		t.Fatalf("the door pluralised a single task:\n%q", door)
	}
}

// THE ROSTER'S PHONE ROWS ARE TWO-LINE CARDS: the name and its state on top, and
// what the work came to with how long ago under it, indented as a card's tail.
func TestThePhoneRosterRowsAreTwoLineCards(t *testing.T) {
	a, _, _ := taskApp(t)
	a.width, a.height = 50, 28
	railRun(a)
	a.comp.tasks = []session.TaskIndexEntry{
		pastTask("4", "sweep-the-call-sites", "Sweep the call sites", 3*time.Hour),
	}
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the page refused to open with work to show")
	}

	lines := taskSheetLines(a)
	name, tail := -1, -1
	for i, line := range lines {
		if strings.Contains(line, "Sweep the call sites") {
			name = i
		}
		if strings.Contains(line, "it came home clean") {
			tail = i
		}
	}
	if name < 0 || tail < 0 {
		t.Fatalf("the earlier row is not a two-line card:\n%s", strings.Join(lines, "\n"))
	}
	if tail != name+1 {
		t.Fatalf("the card's tail is not the line under its name (name=%d tail=%d)", name, tail)
	}
	// The tail hangs indented under the name, not flush with it.
	if !strings.HasPrefix(lines[tail], strings.Repeat(" ", 2+taskSheetPhoneIndent)) {
		t.Fatalf("the card's tail is not indented under the label:\n%q", lines[tail])
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w > a.width {
			t.Fatalf("roster row %d is %d cells wide on a %d-column frame:\n%q", i, w, a.width, line)
		}
	}
}

// THE LIST SCROLLS SO A CARD PAST THE FOLD IS REACHABLE. On a short phone frame
// the window follows the cursor by lines, not by rows, so a two-line card the
// cursor walks to is drawn whole rather than clipped at the fold.
func TestThePhoneRosterScrollsToACardPastTheFold(t *testing.T) {
	a, _, _ := taskApp(t)
	a.width, a.height = 40, 14
	railRun(a)
	a.comp.tasks = []session.TaskIndexEntry{
		pastTask("41", "sweep-the-call-sites", "Sweep the call sites", 3*time.Hour),
		pastTask("42", "port-the-parser", "Port the parser", 40*time.Hour),
		pastTask("43", "wire-the-seam", "Wire the seam", 2*time.Hour),
		pastTask("44", "cut-the-goldens", "Cut the goldens", 5*time.Hour),
		pastTask("45", "read-the-law", "Read the law twice", 6*time.Hour),
	}
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the page refused to open")
	}

	// Walk the cursor to the bottom of the list, and find out what is down there:
	// the fixture is grouped by what you do next, so which card is last is the
	// place's business rather than the test's.
	for i := 0; i < 40; i++ {
		a.taskSheetMove(1)
	}
	last, ok := a.taskSheetCurrent()
	if !ok {
		t.Fatal("the walk left the cursor on nothing")
	}
	lines := taskSheetLines(a)
	joined := strings.Join(lines, "\n")
	head := -1
	for i, line := range lines {
		if strings.Contains(line, last.entry.Title) {
			head = i
		}
	}
	if head < 0 {
		t.Fatalf("scrolling did not bring the cursor's card into view:\n%s", joined)
	}
	// AND IT IS DRAWN WHOLE. A card is two lines, so a window that counted rows
	// rather than lines would leave the tail of the one a thumb scrolled to
	// clipped at the fold.
	if head+1 >= len(lines) || !strings.Contains(lines[head+1], "it came home clean") {
		t.Fatalf("the cursor's card is clipped at the fold:\n%s", joined)
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w > a.width {
			t.Fatalf("roster row %d is %d cells wide on a %d-column frame:\n%q", i, w, a.width, line)
		}
	}

	// A wheel walks the cursor the same way a key does, so it reaches the fold too.
	a.taskSheetScroll(-40)
	if strings.Contains(strings.Join(taskSheetLines(a), "\n"), last.entry.Title) {
		t.Fatal("the wheel did not scroll the list back up")
	}
}

// THE LIST'S FOOT IS A BACK BAR, NOT A KEY LEGEND. A person leaves by tapping
// `‹ back`, which drops them to the conversation.
func TestThePhoneRosterFootIsABackBarToTheConversation(t *testing.T) {
	a, _, _ := taskApp(t)
	a.width, a.height = 50, 28
	railRun(a)
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the page refused to open")
	}

	lines := taskSheetLines(a)
	foot := lines[len(lines)-1]
	if !strings.Contains(foot, plain(homeSheetBackWord)) {
		t.Fatalf("the phone foot is not a back bar:\n%q", foot)
	}
	// And the key legend a keyboard reads is gone from it.
	if strings.Contains(foot, "enter opens its room") {
		t.Fatalf("the phone foot still draws the key legend:\n%q", foot)
	}

	y, ok := taskSheetHitY(a, taskSheetHitBar)
	if !ok {
		t.Fatal("the foot bar answers to no press")
	}
	drive(t, a, tea.MouseClickMsg{X: 2, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: 2, Y: y, Button: tea.MouseLeft})
	if a.at(pageTasks) {
		t.Fatal("tapping `‹ back` did not return to the conversation")
	}
}

// TAPPING A CARD OPENS THE RECORD, AND ITS OWN `‹ back` RETURNS TO THE LIST.
// That is the second of the two backs, both bands: the record card backs out to
// the list, and the list's bar backs out to the conversation.
func TestTappingAPhoneCardOpensTheRecordAndBacksToTheList(t *testing.T) {
	a, _, _ := taskApp(t)
	a.width, a.height = 50, 28
	a.comp.tasks = []session.TaskIndexEntry{
		pastTask("4", "sweep-the-call-sites", "Sweep the call sites", 3*time.Hour),
	}
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the page refused to open")
	}

	// Tap the earlier card — a row of a conversation that is closed opens the
	// record, not a room.
	y, ok := taskSheetHitY(a, taskSheetHitRow)
	if !ok {
		t.Fatal("the earlier card answers to no press")
	}
	drive(t, a, tea.MouseClickMsg{X: 2, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: 2, Y: y, Button: tea.MouseLeft})
	if !a.at(pageTasks) || !a.taskSheet.detailOn {
		t.Fatalf("the card did not open the record: open=%v detail=%v", a.at(pageTasks), a.taskSheet.detailOn)
	}

	// The record card's foot is its own two-band bar, and its `‹ back` backs out
	// to the list rather than closing the page.
	yBack, ok := taskCardHitY(a, taskCardHitMention)
	if !ok {
		t.Fatal("the record card has no bar to press")
	}
	drive(t, a, tea.MouseClickMsg{X: 2, Y: yBack, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: 2, Y: yBack, Button: tea.MouseLeft})
	if !a.at(pageTasks) || a.taskSheet.detailOn {
		t.Fatalf("`‹ back` on the record did not return to the list: open=%v detail=%v", a.at(pageTasks), a.taskSheet.detailOn)
	}
}

// taskCardHitY is the first screen row of the record card that answers to a kind
// of hit, and whether there is one.
func taskCardHitY(a *app, want taskCardHit) (int, bool) {
	width, height := a.size()
	_, hits, _, _ := a.taskCardFrame(width, height)
	for y, hit := range hits {
		if hit == want {
			return y, true
		}
	}
	return 0, false
}

// NONE OF THIS CHANGES AT 80 COLUMNS. The strip is chips, the roster rows are one
// line, and the foot is the key legend a keyboard reads.
func TestThePhoneTaskFlowChangesNothingAtEightyColumns(t *testing.T) {
	a, _, _ := taskApp(t)
	a.width, a.height = 80, 28
	railRun(a)
	a.comp.tasks = []session.TaskIndexEntry{
		pastTask("4", "sweep-the-call-sites", "Sweep the call sites", 3*time.Hour),
	}

	// The strip is still a row of chips with names on it, not a door.
	door := stripText(a)
	if strings.Contains(door, "▸ ") && strings.Contains(door, "tasks") {
		t.Fatalf("the strip became a phone door at 80 columns:\n%q", door)
	}
	if !strings.Contains(door, "Ship the port") {
		t.Fatalf("the wide strip lost its chips:\n%q", door)
	}

	if !openTaskPlaceWithRows(a) {
		t.Fatal("the page refused to open")
	}
	lines := taskSheetLines(a)
	// The earlier row is one line: its name and its age share it.
	for i, line := range lines {
		if strings.Contains(line, "Sweep the call sites") {
			if strings.Contains(line, "3h") == false {
				t.Fatalf("the wide earlier row is not one line with its age on it:\n%q", line)
			}
			if i+1 < len(lines) && strings.Contains(lines[i+1], "it came home clean") {
				t.Fatalf("the wide earlier row spilled onto a second line:\n%q", lines[i+1])
			}
		}
	}
	// The foot is the key legend, not a back bar.
	foot := lines[len(lines)-1]
	if !strings.Contains(foot, "enter") {
		t.Fatalf("the wide foot is not the key legend:\n%q", foot)
	}
	if _, ok := taskSheetHitY(a, taskSheetHitBar); ok {
		t.Fatal("the wide foot drew a phone back bar")
	}
}
