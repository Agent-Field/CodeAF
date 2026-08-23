package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// phoneHome opens home on a phone-sized frame. Fifty by thirty is the size the
// e2e harness drives a pocket terminal at, and it is comfortably under
// [tierPhone]'s sixty columns.
func phoneHome(t *testing.T, lab *homeLab, standing string) *app {
	t.Helper()
	a := lab.app(standing)
	a.width, a.height = 50, 30
	a.openHome()
	return a
}

// phoneText is the whole phone frame with the paint stripped.
func phoneText(a *app) string {
	width, height := a.size()
	lines, _, _, _ := a.homeFrame(width, height)
	return ansi.Strip(strings.Join(lines, "\n"))
}

// phoneRows is the same frame as its rows, for the assertions that are about
// WHERE something landed rather than about whether it is there.
func phoneRows(a *app) []string {
	width, height := a.size()
	lines, _, _, _ := a.homeFrame(width, height)
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = ansi.Strip(line)
	}
	return out
}

// phoneCursorLine is the kind of line the cursor is on, for the assertions
// about a cursor that must not have moved.
func phoneCursorLine(a *app) (homeLine, bool) { return a.home.focusedLine() }

// rowAt answers the first screen row whose text contains a word, and -1.
func rowAt(rows []string, word string) int {
	for i, row := range rows {
		if strings.Contains(row, word) {
			return i
		}
	}
	return -1
}

// ── the inbox ───────────────────────────────────────────────────────────────

// THE THREE SECTIONS, IN THE ORDER A PERSON TRIAGES IN. What is stopped, what
// is moving, and what happened while they were away — and the projects under
// all three.
func TestOnAPhoneHomeIsAnInboxOfWaitingRunningAndSinceYouLeft(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "port the picker", "/tmp/alpha", now.Add(-2*time.Minute))
	lab.presence("-tmp-alpha", "aaaa000000000001", session.PresenceWaiting, "bash wants to run git push", now)
	lab.session("-tmp-beta", "bbbb000000000001", "pricing research", "/tmp/beta", now.Add(-3*time.Hour))
	lab.task("-tmp-beta", session.TaskIndexEntry{
		ID: "7", Title: "crunch", Label: "crunch",
		Status: string(session.TaskRunning), SessionID: "bbbb000000000001",
	})
	lab.presence("-tmp-beta", "bbbb000000000001", session.PresenceWorking, "", now,
		session.PresenceTask{ID: "7", Title: "crunch", State: "running", StartedAt: now.Add(-time.Minute)})

	a := phoneHome(t, lab, mine)
	rows := phoneRows(a)
	waiting := rowAt(rows, homePhoneWaitingWord)
	running := rowAt(rows, homePhoneRunningWord)
	if waiting < 0 {
		t.Fatalf("no %q section:\n%s", homePhoneWaitingWord, strings.Join(rows, "\n"))
	}
	if running < 0 {
		t.Fatalf("no %q section:\n%s", homePhoneRunningWord, strings.Join(rows, "\n"))
	}
	if waiting > running {
		t.Fatalf("%q sorted under %q:\n%s", homePhoneWaitingWord, homePhoneRunningWord, strings.Join(rows, "\n"))
	}
	if rowAt(rows, "Port the Picker") < waiting {
		t.Fatalf("the waiting conversation is not in its section:\n%s", strings.Join(rows, "\n"))
	}
	if rowAt(rows, "Pricing Research") < running {
		t.Fatalf("the running conversation is not in its section:\n%s", strings.Join(rows, "\n"))
	}
}

// A SECTION WITH NOTHING IN IT IS NOT DRAWN, which is the emptiness law applied
// to a whole heading.
func TestAPhoneSectionWithNothingInItIsNotDrawn(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "quiet", "/tmp/alpha", time.Now())
	a := phoneHome(t, lab, mine)
	text := phoneText(a)
	if strings.Contains(text, homePhoneWaitingWord) {
		t.Fatalf("a machine with nothing waiting drew the heading anyway:\n%s", text)
	}
	if !strings.Contains(text, "Quiet") {
		t.Fatalf("the project's own conversation is missing:\n%s", text)
	}
}

// THREE, THEN A DOOR.
func TestAPhoneSectionShowsThreeAndFoldsTheRest(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	var mine string
	for i, id := range []string{"aaaa000000000001", "aaaa000000000002", "aaaa000000000003", "aaaa000000000004", "aaaa000000000005"} {
		transcript := lab.session("-tmp-alpha", id, "waiting "+itoa(i), "/tmp/alpha", now.Add(-time.Duration(i)*time.Minute))
		lab.presence("-tmp-alpha", id, session.PresenceWaiting, "a question", now)
		if i == 0 {
			mine = transcript
		}
	}
	a := phoneHome(t, lab, mine)
	text := phoneText(a)
	if !strings.Contains(text, "…2 more") {
		t.Fatalf("five waiting conversations drew no fold:\n%s", text)
	}
	// AND THE DOOR OPENS IN PLACE. It is the same gesture the quiet tail has.
	a.home.foldSection(homePhoneWaitingKey)
	if text := phoneText(a); !strings.Contains(text, "…2 fewer") {
		t.Fatalf("the fold did not open:\n%s", text)
	}
}

// A ROW IS TWO LINES AT THIS TIER: the label, and the dim tail under it.
func TestAPhoneRowIsTwoLines(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "port the picker", "/tmp/alpha", now.Add(-time.Hour))
	a := phoneHome(t, lab, mine)
	rows := phoneRows(a)
	at := rowAt(rows, "Port the Picker")
	if at < 0 || at+1 >= len(rows) {
		t.Fatalf("no conversation row:\n%s", strings.Join(rows, "\n"))
	}
	if strings.Contains(rows[at], "1h") {
		t.Fatalf("the tail shared the label's line:\n%s", rows[at])
	}
	if !strings.Contains(rows[at+1], "1h") {
		t.Fatalf("the tail is not on the line under the label:\n%s", strings.Join(rows[at:at+2], "\n"))
	}
}

// AND A ROW APPEARS ONCE. What the sections lifted is not said again under its
// project.
func TestAPhoneInboxDrawsAWaitingRowOnlyOnce(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "port the picker", "/tmp/alpha", now)
	lab.presence("-tmp-alpha", "aaaa000000000001", session.PresenceWaiting, "a question", now)
	a := phoneHome(t, lab, mine)
	if n := strings.Count(phoneText(a), "Port the Picker"); n != 1 {
		t.Fatalf("the row was drawn %d times, want once:\n%s", n, phoneText(a))
	}
}

// SEARCH IS UNTOUCHED: no sections, no tiers, and the two action rows against
// the box exactly as at every other width.
func TestTypingOnAPhoneSearchesWithNoSections(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "port the picker", "/tmp/alpha", now)
	lab.presence("-tmp-alpha", "aaaa000000000001", session.PresenceWaiting, "a question", now)
	a := phoneHome(t, lab, mine)
	for _, r := range "port" {
		a.homeKey(key(string(r)))
	}
	text := phoneText(a)
	if strings.Contains(text, homePhoneWaitingWord+"\n") && strings.Contains(text, homePhoneNewsWord) {
		t.Fatalf("a search kept the sections:\n%s", text)
	}
	if !strings.Contains(text, homeStartWord) || !strings.Contains(text, homeAskHereWord) {
		t.Fatalf("the action rows are not at the foot:\n%s", text)
	}
}

// ── the sheet ───────────────────────────────────────────────────────────────

// ENTER OPENS THE CARD AS A SHEET: the title, the place, and the bands.
func TestEnterOnAPhoneRowOpensItsCardAsASheet(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "port the picker", "/tmp/alpha", now)
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "1", SessionID: "aaaa000000000001", Title: "port the roster",
		Label: "port the roster", Outcome: "the roster resumes cleanly",
		Status: string(session.TaskDone), EndedAt: now.Add(-time.Hour), FilesChanged: 4,
	})
	a := phoneHome(t, lab, mine)
	a.home.point(mine)
	a.homeKey(key("enter"))
	if !a.homeSheetShowing() {
		t.Fatal("enter did not open the sheet")
	}
	text := phoneText(a)
	for _, want := range []string{homeSheetBackWord, "Port the Picker", "port the roster"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the sheet does not carry %q:\n%s", want, text)
		}
	}
	// AND esc COMES BACK WITH THE CURSOR WHERE IT WAS.
	held, _ := phoneCursorLine(a)
	a.homeKey(key("esc"))
	if a.homeSheetShowing() {
		t.Fatal("esc did not close the sheet")
	}
	back, ok := phoneCursorLine(a)
	if !ok || back.row.Transcript != held.row.Transcript {
		t.Fatalf("the cursor moved: %q became %q", held.row.Transcript, back.row.Transcript)
	}
}

// A TAP DOES THE SAME, in one gesture and not two — there is no second column
// to preview into.
func TestATapOnAPhoneRowOpensTheSheetInOneGesture(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "port the picker", "/tmp/alpha", now)
	a := phoneHome(t, lab, mine)
	rows := phoneRows(a)
	at := rowAt(rows, "Port the Picker")
	if at < 0 {
		t.Fatalf("no row to press:\n%s", strings.Join(rows, "\n"))
	}
	a.homePress(2, at)
	if !a.homeSheetShowing() {
		t.Fatalf("one press did not open the sheet")
	}
}

// A TAP ON THE SHEET'S TOP ROW IS THE WAY BACK.
func TestATapOnTheSheetsBackRowReturnsToTheInbox(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "port the picker", "/tmp/alpha", time.Now())
	a := phoneHome(t, lab, mine)
	a.home.point(mine)
	a.openHomeSheet()
	rows := phoneRows(a)
	if at := rowAt(rows, homeSheetBackWord); at != 0 {
		t.Fatalf("%q is on row %d, want the top:\n%s", homeSheetBackWord, at, strings.Join(rows, "\n"))
	}
	a.homePress(1, 0)
	if a.homeSheetShowing() {
		t.Fatal("a tap on the back row did not close the sheet")
	}
}

// THE BOOKKEEPING IS BEHIND ONE FOLD, and `m` opens it.
func TestThePhoneSheetHoldsRepoKeysAndSpendBehindMore(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "port the picker", lab.workspace("alpha"), now)
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "1", SessionID: "aaaa000000000001", Title: "port", Label: "port",
		Status: string(session.TaskDone), EndedAt: now.Add(-time.Hour), Cost: 1.25,
	})
	a := phoneHome(t, lab, mine)
	a.home.point(mine)
	a.openHomeSheet()
	if !strings.Contains(phoneText(a), homeSheetMoreWord) {
		t.Fatalf("the sheet drew no fold:\n%s", phoneText(a))
	}
	if strings.Contains(phoneText(a), "ctrl+t new chat here") {
		t.Fatalf("the keys band is not behind the fold:\n%s", phoneText(a))
	}
	a.homeKey(key("m"))
	if !strings.Contains(phoneText(a), "ctrl+t new chat here") {
		t.Fatalf("m did not open the fold:\n%s", phoneText(a))
	}
}

// ── the answer bands ────────────────────────────────────────────────────────

// A QUESTION IN ANOTHER WINDOW, ANSWERED FROM A PHONE: one band per answer, the
// whole row a target, and the key on the engine's doorstep.
func TestThePhoneSheetDrawsAnswersAsFullWidthBands(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "mine", "/tmp/alpha", now)
	row := lab.session("-tmp-beta", "bbbb000000000001", "port the picker", "/tmp/beta", now)
	lab.asking("-tmp-beta", "bbbb000000000001", consentQuestion(7, "needs your ok to run bash"), now)

	a := phoneHome(t, lab, mine)
	var left []string
	a.leaveAnswer = func(dir string, kind session.QuestionKind, id uint64, key string) error {
		left = append(left, key)
		return nil
	}
	a.home.point(row)
	a.openHomeSheet()
	rows := phoneRows(a)
	at := rowAt(rows, "allow once")
	if at < 0 {
		t.Fatalf("no answer band:\n%s", strings.Join(rows, "\n"))
	}
	// ONE ANSWER PER ROW: the second answer is not on the first's line.
	if strings.Contains(rows[at], "deny") {
		t.Fatalf("two answers shared a row:\n%s", rows[at])
	}
	// AND THE WHOLE ROW IS THE TARGET — a press at the far right of it answers.
	a.homePress(46, at)
	if len(left) != 1 || left[0] != "1" {
		t.Fatalf("the engine seam received %v, want [1]", left)
	}
	if !strings.Contains(phoneText(a), answerWaitingWord) {
		t.Fatalf("the band did not settle:\n%s", phoneText(a))
	}
}

// AND A DIGIT DOES THE SAME THING FROM THE KEYBOARD, because a phone with a
// hardware keyboard is a laptop.
func TestADigitOnThePhoneSheetAnswers(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "mine", "/tmp/alpha", now)
	row := lab.session("-tmp-beta", "bbbb000000000001", "port the picker", "/tmp/beta", now)
	lab.asking("-tmp-beta", "bbbb000000000001", consentQuestion(7, "needs your ok to run bash"), now)
	a := phoneHome(t, lab, mine)
	var left []string
	a.leaveAnswer = func(dir string, kind session.QuestionKind, id uint64, key string) error {
		left = append(left, key)
		return nil
	}
	a.home.point(row)
	a.openHomeSheet()
	a.homeKey(key("3"))
	if len(left) != 1 || left[0] != "3" {
		t.Fatalf("the engine seam received %v, want [3]", left)
	}
}

// ── tasks by touch ──────────────────────────────────────────────────────────

// A TAP ON A TASK ROW OPENS THAT TASK'S RECORD — the same card enter opens on
// the task page.
func TestATapOnATaskRowOfThePhoneSheetOpensItsRecord(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "port the picker", "/tmp/alpha", now)
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "1", SessionID: "aaaa000000000001", Title: "port the roster",
		Label: "port the roster", Outcome: "the roster resumes cleanly",
		Status: string(session.TaskDone), EndedAt: now.Add(-time.Hour),
	})
	a := phoneHome(t, lab, mine)
	a.home.point(mine)
	a.openHomeSheet()
	rows := phoneRows(a)
	at := rowAt(rows, "port the roster")
	if at < 0 {
		t.Fatalf("no task row on the sheet:\n%s", strings.Join(rows, "\n"))
	}
	a.homePress(3, at)
	if !a.taskSheet.detailOn {
		t.Fatal("a tap on the task row opened no record")
	}
	if a.taskSheet.detail.Label != "port the roster" {
		t.Fatalf("it opened %q", a.taskSheet.detail.Label)
	}
}

// AND THE RECORD'S VERBS ARE BANDS AT THIS TIER, not letters in a legend.
func TestTheTaskRecordsFootIsBandsOnAPhone(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 50, 24
	a.taskSheet = taskSheet{open: true, detailOn: true, detail: session.TaskIndexEntry{
		ID: "1", Title: "port the roster", Label: "port the roster",
		Status: string(session.TaskDone),
	}}
	width, height := a.size()
	lines, hits, _, _ := a.taskCardFrame(width, height)
	foot := ansi.Strip(lines[len(lines)-1])
	if !strings.Contains(foot, homeSheetBackWord) || !strings.Contains(foot, taskPhoneMentionWord) {
		t.Fatalf("the foot is not two bands: %q", foot)
	}
	if hits[len(hits)-1] != taskCardHitMention {
		t.Fatalf("the foot row answers %v", hits[len(hits)-1])
	}
	// AND A WIDE FRAME IS UNTOUCHED.
	a.width = 100
	width, height = a.size()
	lines, _, _, _ = a.taskCardFrame(width, height)
	if !strings.Contains(ansi.Strip(lines[len(lines)-1]), taskCardKeys) {
		t.Fatalf("a wide record lost its keys line: %q", ansi.Strip(lines[len(lines)-1]))
	}
}

// ── the action bar ──────────────────────────────────────────────────────────

func TestThePhoneActionBarIsThreeTargets(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "port the picker", "/tmp/alpha", time.Now())
	a := phoneHome(t, lab, mine)
	rows := phoneRows(a)
	bar := rows[len(rows)-1]
	for _, want := range []string{"open", "new", homeAskHereWord} {
		if !strings.Contains(bar, want) {
			t.Fatalf("the bar does not offer %q: %q", want, bar)
		}
	}
	if len(a.home.bar) != 3 {
		t.Fatalf("the bar recorded %d targets, want 3", len(a.home.bar))
	}
	// THE SHEET'S BAR IS ITS OWN THREE.
	a.home.point(mine)
	a.openHomeSheet()
	rows = phoneRows(a)
	bar = rows[len(rows)-1]
	for _, want := range []string{homeSheetBackWord, homeSheetOpenWord, homeSheetMoreWord} {
		if !strings.Contains(bar, want) {
			t.Fatalf("the sheet's bar does not offer %q: %q", want, bar)
		}
	}
	// AND A TAP ON `‹ back` IS THE WAY OUT.
	a.homePress(1, len(rows)-1)
	if a.homeSheetShowing() {
		t.Fatal("the bar's first target did not go back")
	}
}

// UNDER [homePhoneBarFloor] THE PLAIN HINT IS DRAWN INSTEAD.
func TestAVeryNarrowPhoneDrawsTheHintRatherThanTheBar(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "one", "/tmp/alpha", time.Now())
	a := phoneHome(t, lab, mine)
	a.width = 20
	rows := phoneRows(a)
	if len(a.home.bar) != 0 {
		t.Fatalf("a twenty-cell frame drew %d targets", len(a.home.bar))
	}
	if strings.TrimSpace(rows[len(rows)-1]) == "" {
		t.Fatal("it drew nothing at all instead of the hint")
	}
}

// ── touch facts ─────────────────────────────────────────────────────────────

// A TAP ON A SECTION HEADING FOLDS IT.
func TestATapOnAPhoneSectionHeadingFoldsIt(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "port the picker", "/tmp/alpha", now)
	lab.presence("-tmp-alpha", "aaaa000000000001", session.PresenceWaiting, "a question", now)
	a := phoneHome(t, lab, mine)
	rows := phoneRows(a)
	at := rowAt(rows, homePhoneWaitingWord)
	if at < 0 {
		t.Fatalf("no heading to press:\n%s", strings.Join(rows, "\n"))
	}
	a.homePress(1, at)
	if !a.home.sections[homePhoneWaitingKey] {
		t.Fatal("a tap on the heading did not fold the section")
	}
}

// MOUSE MOTION IS IGNORED AT THIS TIER: there is no hover on glass.
func TestAPhoneHomeHasNoHover(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "one", "/tmp/alpha", time.Now())
	a := phoneHome(t, lab, mine)
	a.homeHover(2, 4)
	if a.home.hover != -1 {
		t.Fatalf("the pointer took a row: %d", a.home.hover)
	}
}

// ── resize ──────────────────────────────────────────────────────────────────

// ROTATING A PHONE CROSSES SIXTY COLUMNS. The cursor, the sheet, the search text
// and the errands all survive the trip, in both directions.
func TestRotatingAPhoneKeepsTheCursorAndTheOpenSheet(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "port the picker", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "pricing research", "/tmp/alpha", now.Add(-time.Hour))
	a := phoneHome(t, lab, mine)
	a.home.point(mine)
	a.openHomeSheet()
	phoneText(a)

	a.width = 90
	wide := phoneText(a)
	if a.homeSheetShowing() {
		t.Fatal("the sheet stayed up on a wide frame")
	}
	if !strings.Contains(wide, "Port the Picker") {
		t.Fatalf("the row went missing on the wide frame:\n%s", wide)
	}
	line, ok := phoneCursorLine(a)
	if !ok || line.row.Transcript != mine {
		t.Fatalf("the cursor moved on the way out")
	}

	a.width = 50
	phoneText(a)
	if !a.homeSheetShowing() {
		t.Fatal("the sheet did not come back")
	}
	line, ok = phoneCursorLine(a)
	if !ok || line.row.Transcript != mine {
		t.Fatalf("the cursor moved on the way back")
	}
}

// AND A SEARCH SURVIVES THE ROTATION TOO.
func TestRotatingAPhoneKeepsWhatWasTyped(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "port the picker", "/tmp/alpha", time.Now())
	a := phoneHome(t, lab, mine)
	for _, r := range "port" {
		a.homeKey(key(string(r)))
	}
	a.width = 90
	if text := phoneText(a); !strings.Contains(text, "port") {
		t.Fatalf("the query went missing:\n%s", text)
	}
	a.width = 50
	if a.home.box.String() != "port" {
		t.Fatalf("the box holds %q", a.home.box.String())
	}
}

// NOTHING CHANGES AT EIGHTY COLUMNS AND UP. The wide frame is the one it always
// was: two columns, a card, and no sections.
func TestNothingAboutHomeChangesAtEightyColumns(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "port the picker", "/tmp/alpha", now)
	lab.presence("-tmp-alpha", "aaaa000000000001", session.PresenceWaiting, "a question", now)
	a := lab.app(mine)
	a.width, a.height = 100, 30
	a.openHome()
	text := phoneText(a)
	if strings.Contains(text, homePhoneNewsWord) || strings.Contains(text, homeSheetBackWord) {
		t.Fatalf("the phone's shapes reached a wide frame:\n%s", text)
	}
	if a.home.phone {
		t.Fatal("a hundred-column frame thinks it is a phone")
	}
}

// ── since you left ──────────────────────────────────────────────────────────

// NEWS IS WHAT LANDED WHILE YOU WERE NOT IN THE ROOM: a note in an inbox, and
// a task that finished after the last thing you said.
func TestThePhoneInboxCarriesSinceYouLeft(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "port the picker", "/tmp/alpha", now.Add(-2*time.Hour))
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "1", SessionID: "aaaa000000000001", Title: "the deploy went green",
		Label: "the deploy went green", Status: string(session.TaskDone),
		EndedAt: now.Add(-time.Minute),
	})
	dir := filepath.Dir(mine)
	note := `{"at":"` + now.Add(-30*time.Minute).Format(time.RFC3339Nano) + `","words":"keep main green","text":"it is green"}`
	if err := os.WriteFile(standing.InboxPath(dir), []byte(note+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := phoneHome(t, lab, mine)
	text := phoneText(a)
	if !strings.Contains(text, homePhoneNewsWord) {
		t.Fatalf("no %q section:\n%s", homePhoneNewsWord, text)
	}
	for _, want := range []string{"the deploy went green", "keep main green"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the section does not carry %q:\n%s", want, text)
		}
	}
}

// ── the errand's sheet ──────────────────────────────────────────────────────

// AN ERRAND IS ONE KIND OF SHEET. `ask here` already stacked on a narrow frame;
// at this tier that stack IS the sheet, with the same way out and the same bar.
func TestAnErrandOnAPhoneIsTheSameSheet(t *testing.T) {
	lab := newErrandLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", now)
	a := lab.app(mine, []session.Event{
		text(session.EventTextDelta, "I will remind you at 6."),
		{Kind: session.EventTurnDone},
	})
	a.width, a.height = 50, 30
	a.openHome()
	typeHome(a, "remind me at 6")
	drive(t, a, key("up"), key("enter"))

	ex := theExchange(a)
	if ex == nil {
		t.Fatal("ask here made no errand")
	}
	a.home.sheet = homeSheet{open: true}
	if !a.homeSheetShowing() {
		t.Fatal("the errand is not showing as a sheet")
	}
	rows := phoneRows(a)
	if at := rowAt(rows, homeSheetBackWord); at != 0 {
		t.Fatalf("the errand's sheet has no %q row:\n%s", homeSheetBackWord, strings.Join(rows, "\n"))
	}
	if !strings.Contains(rows[len(rows)-1], homeSheetSendWord) {
		t.Fatalf("the errand's bar does not offer %q: %q", homeSheetSendWord, rows[len(rows)-1])
	}
	// AND tab HANDS THE KEYBOARD BACK, which is this sheet's way out.
	drive(t, a, key("tab"))
	if a.homeSheetShowing() {
		t.Fatal("tab did not put the list back")
	}
}
