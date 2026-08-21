package tui3

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// workLab is one conversation with `tasks` pieces of work behind it, the newest
// first, each with a name and an outcome — which is the shape the work band is
// about. The cursor is left on that conversation.
func workLab(t *testing.T, tasks int) *app {
	t.Helper()
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the working one", "/tmp/alpha", now)
	for i := 0; i < tasks; i++ {
		lab.task("-tmp-alpha", session.TaskIndexEntry{
			ID: strconv.Itoa(i), Name: "task-" + strconv.Itoa(i),
			Label: "Task " + strconv.Itoa(i), Title: "Task " + strconv.Itoa(i),
			Status: string(session.TaskDone), Outcome: "outcome " + strconv.Itoa(i),
			FilesChanged: 14, EndedAt: now.Add(-time.Duration(i+1) * time.Hour),
			SessionID: "aaaa000000000001",
		})
	}
	a := lab.app(mine)
	a.width, a.height = 100, 40
	a.openHome()
	a.home.point(mine)
	return a
}

// workCard is the right pane as plain rows.
func workCard(t *testing.T, a *app) []string {
	t.Helper()
	width, _ := a.size()
	_, right := homeColumns(width)
	if right <= 0 {
		t.Fatalf("a %d-column frame lent the card nothing", width)
	}
	var rows []string
	for _, line := range a.homeDetail(right, 30, a.pal) {
		rows = append(rows, strings.TrimRight(ansi.Strip(line), " "))
	}
	return rows
}

func workRowAt(t *testing.T, rows []string, want string) int {
	t.Helper()
	for at, row := range rows {
		if strings.Contains(row, want) {
			return at
		}
	}
	t.Fatalf("the card has no row holding %q:\n%s", want, strings.Join(rows, "\n"))
	return -1
}

// THE SHAPE ITSELF: the name on its own line, what it came to indented under it,
// and a blank before the next task.
func TestTheWorkBandIsTwoLinesAndABlank(t *testing.T) {
	a := workLab(t, 2)
	rows := workCard(t, a)
	first := workRowAt(t, rows, "Task 0")
	if !strings.HasPrefix(rows[first+1], strings.Repeat(" ", homeWorkIndent)+"outcome 0") {
		t.Fatalf("the outcome does not hang under the name:\n%s", strings.Join(rows, "\n"))
	}
	if !strings.Contains(rows[first+1], "14 files") {
		t.Fatalf("the outcome line lost the file count:\n%s", strings.Join(rows, "\n"))
	}
	if rows[first+2] != "" {
		t.Fatalf("there is no blank between two tasks:\n%s", strings.Join(rows, "\n"))
	}
	if !strings.Contains(rows[first+3], "Task 1") {
		t.Fatalf("the second task does not follow the blank:\n%s", strings.Join(rows, "\n"))
	}
	// AND NONE AFTER THE LAST. The band ends on the second task's own sentence,
	// and whatever comes next is another band with the card's own blank between.
	last := workRowAt(t, rows, "Task 1")
	if rows[last+1] == "" {
		t.Fatalf("the band drew a blank after its last task:\n%s", strings.Join(rows, "\n"))
	}
}

// DONE IS THE ABSENCE OF A MARK (D11). No tick, and no `done` either — the word
// was the loudest thing on every row and it never said anything.
func TestADoneTaskWearsNoTickAndNoWord(t *testing.T) {
	a := workLab(t, 2)
	card := strings.Join(workCard(t, a), "\n")
	for _, banned := range []string{glyphDone, doneWord + " Task", "done  "} {
		if strings.Contains(card, banned) {
			t.Fatalf("the card draws %q on a landed task:\n%s", banned, card)
		}
	}
}

// EVERY OTHER STATE LEADS THE SENTENCE, with home's own glyph in front of it.
func TestATaskThatIsNotDoneLeadsWithItsState(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the working one", "/tmp/alpha", now)
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "1", Name: "look", Label: "Look At This", Title: "Look At This",
		Status: string(session.TaskUnverified), Outcome: "nobody could judge it",
		EndedAt: now.Add(-time.Hour), SessionID: "aaaa000000000001",
	})
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "2", Name: "broke", Label: "Broke It", Title: "Broke It",
		Status: string(session.TaskFailed), Outcome: "the build would not run",
		EndedAt: now.Add(-2 * time.Hour), SessionID: "aaaa000000000001",
	})
	a := lab.app(mine)
	a.width, a.height = 100, 40
	a.openHome()
	a.home.point(mine)
	rows := workCard(t, a)
	card := strings.Join(rows, "\n")
	for _, want := range []string{
		homeAskGlyph + " " + taskUnverifiedWord + " · nobody could judge it",
		glyphBad + " " + doneFailWord + " · the build would not run",
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("the card does not say %q:\n%s", want, card)
		}
	}
	// The name is still the first line of each — the state is UNDER it.
	at := workRowAt(t, rows, "Look At This")
	if strings.Contains(rows[at], taskUnverifiedWord) {
		t.Fatalf("the state landed on the name's line:\n%s", card)
	}
}

// THE FOLD CUTS AT A TASK BOUNDARY AND NEVER THROUGH ONE. Three tasks is three
// names with three sentences under them, and the line that says so.
func TestTheWorkBandFoldsAtThreeTasks(t *testing.T) {
	a := workLab(t, 6)
	rows := workCard(t, a)
	card := strings.Join(rows, "\n")
	for i := 0; i < homeWorkTasks; i++ {
		if !strings.Contains(card, "Task "+strconv.Itoa(i)) {
			t.Fatalf("the band dropped task %d:\n%s", i, card)
		}
	}
	if strings.Contains(card, "Task "+strconv.Itoa(homeWorkTasks)) {
		t.Fatalf("the band drew a fourth task:\n%s", card)
	}
	fold := workRowAt(t, rows, "…3 more tasks")
	if !strings.HasPrefix(strings.TrimSpace(rows[fold]), bandFoldGlyph) {
		t.Fatalf("the fold line wears no fold mark:\n%s", card)
	}
	// THE CUT IS AT THE BOUNDARY: the row above the fold line is the third
	// task's own sentence, not a name left hanging with nothing under it.
	if !strings.Contains(rows[fold-1], "outcome "+strconv.Itoa(homeWorkTasks-1)) {
		t.Fatalf("the fold cut through a task:\n%s", card)
	}
}

// A CLICK ON THE FOLD LINE OPENS THAT BAND, and a second folds it again. It is
// the one gesture that acts on ONE band — `m` acts on the whole card.
func TestClickingTheWorkFoldLineTogglesIt(t *testing.T) {
	a := workLab(t, 6)
	width, height := a.size()
	lines, _, _, _ := a.homeFrame(width, height)
	left, _ := homeColumns(width)
	row := -1
	for y, line := range lines {
		if strings.Contains(ansi.Strip(line), "…3 more tasks") {
			row = y
		}
	}
	if row < 0 {
		t.Fatalf("the fold line is not on the frame:\n%s", homeText(a))
	}
	a.homePress(left+homeGutter+2, row)
	if strings.Contains(homeText(a), "…3 more tasks") {
		t.Fatalf("the click did not open the band:\n%s", homeText(a))
	}
	if !strings.Contains(homeText(a), "Task 5") {
		t.Fatalf("the opened band does not draw what it was hiding:\n%s", homeText(a))
	}
	lines, _, _, _ = a.homeFrame(width, height)
	for y, line := range lines {
		if strings.Contains(ansi.Strip(line), "…3 fewer") {
			row = y
		}
	}
	a.homePress(left+homeGutter+2, row)
	if !strings.Contains(homeText(a), "…3 more tasks") {
		t.Fatalf("the second click did not fold it back:\n%s", homeText(a))
	}
	// AND THE CURSOR NEVER MOVED. A press in the right pane acts on the card,
	// not on the list across the gutter.
	if line, ok := a.home.focusedLine(); !ok || line.kind != homeSession {
		t.Fatalf("a click on the card moved the list's cursor (kind %v)", line.kind)
	}
}

// `m` IS THE KEYBOARD'S WAY IN, and it acts on the whole card because the
// column has no cursor of its own.
func TestMOpensEveryFoldOnTheCard(t *testing.T) {
	a := workLab(t, 6)
	a.homeKey(key("m"))
	if strings.Contains(homeText(a), "…3 more tasks") {
		t.Fatalf("m did not open the work band:\n%s", homeText(a))
	}
	a.homeKey(key("m"))
	if !strings.Contains(homeText(a), "…3 more tasks") {
		t.Fatalf("m again did not fold it back:\n%s", homeText(a))
	}
	// AND ONLY WITH NOTHING TYPED. In the box an m is an m.
	a.homeKey(key("x"))
	a.homeKey(key("m"))
	if !strings.Contains(a.home.box.String(), "m") {
		t.Fatalf("m was eaten as a key while something was typed: box is %q", a.home.box.String())
	}
}
