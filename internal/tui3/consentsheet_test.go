package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE APPROVAL QUESTION ON A PHONE.
//
// Under sixty columns the block becomes a bottom sheet: the same three answers,
// laid out as full-width bands a thumb can hit, over a command region that wraps
// instead of being cut. Every assertion here is about the phone tier or about
// the frames that must not have noticed it (questionsheet.go).

// phoneAsk raises one question on a forty-four column frame — a phone in a
// terminal, which is the width this wave is measured at.
func phoneAsk(t *testing.T) (*wiredAgent, *app) {
	t.Helper()
	agent, a := wired([]session.Event{
		toolBegin("bash", "bash rm -rf build"),
		consentEvent(7, "bash", "bash rm -rf build", `bash pattern "rm -rf *"`),
	})
	a.width, a.height = 44, 30
	typeLine(t, a, "clean the tree")
	settleAsk(a)
	if !a.asking() {
		t.Fatal("the question never came up")
	}
	return agent, a
}

// sheetRows is the block as a reader sees it, laid out at the frame's width.
func askRows(a *app) []string {
	out := make([]string, 0, 8)
	for _, line := range a.questionRows(a.width) {
		out = append(out, plain(line))
	}
	return out
}

// commandRegion is the sheet's middle: everything between the title rule and
// the plain rule the bands sit under.
func commandRegion(t *testing.T, rows []string) []string {
	t.Helper()
	for i, row := range rows[1:] {
		if strings.Trim(row, "─") == "" {
			return rows[1 : i+1]
		}
	}
	t.Fatalf("the sheet has no rule under its command:\n%s", strings.Join(rows, "\n"))
	return nil
}

// chromeRowY is the screen row one row of the block is drawn on, which is what a
// click carries. It is derived the way [app.chromeAt] derives it backwards, so
// the test and the surface cannot disagree about where a row is.
func chromeRowY(t *testing.T, a *app, block int) int {
	t.Helper()
	_, marks, _, _ := a.chrome(a.width)
	for at, mark := range marks {
		if mark.kind == chromeQuestion && mark.index == block {
			return at + a.height - len(marks)
		}
	}
	t.Fatalf("no row of the block is marked %d", block)
	return -1
}

// THE SHEET. A title with the tool's name in it, the command whole, the policy's
// sentence, and three bands — each of them the width of the frame, because a
// target a thumb misses is a target that is not there.
func TestThePhoneSheetLaysTheAnswersOutAsBands(t *testing.T) {
	_, a := phoneAsk(t)
	rows := askRows(a)
	joined := strings.Join(rows, "\n")

	for _, want := range []string{
		"? bash",                  // the title rule names what is asking
		"rm -rf build",            // the command, in the region of its own
		`bash pattern "rm -rf *"`, // the policy's own words for why
		"[1] allow once",
		"[3] deny",
		"[2] always",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("the sheet is missing %q:\n%s", want, joined)
		}
	}
	// The line of words the wider frames draw is NOT on it — that is the whole
	// of this wave: one offer sentence at forty-four columns is the shape that
	// did not fit.
	if strings.Contains(joined, "allow? [1] allow once") {
		t.Fatalf("the phone frame still drew the one-line offer:\n%s", joined)
	}
	// Nothing on it is wider than the frame.
	for i, row := range rows {
		if w := ansi.StringWidth(row); w > a.width {
			t.Fatalf("row %d is %d cells wide on a %d-column frame: %q", i, w, a.width, row)
		}
	}
	// AND THE GEOMETRY AGREES WITH THE DRAWING. The sheet's height is counted
	// rather than derived because its command region wraps; a block whose height
	// and whose rows disagreed would put the caret a row off the box.
	if got, want := a.questionHeight(), len(rows); got != want {
		t.Fatalf("the block claims %d rows and drew %d", got, want)
	}

	// THE TARGETS. Three of them, each its own row, each at least ten cells.
	if len(a.questionBands) != 3 {
		t.Fatalf("the sheet recorded %d targets, want three: %+v", len(a.questionBands), a.questionBands)
	}
	seen := map[int]bool{}
	for _, tap := range a.questionBands {
		if w := tap.span.to - tap.span.from; w < 10 {
			t.Fatalf("a band is %d cells wide, which is a target a thumb misses: %+v", w, tap)
		}
		if tap.span.from != 0 || tap.span.to != a.width {
			t.Fatalf("a band is not the width of the frame: %+v", tap)
		}
		if seen[tap.row] {
			t.Fatalf("two answers share row %d: %+v", tap.row, a.questionBands)
		}
		seen[tap.row] = true
	}

	// EVERY ROW OF THE SHEET ANSWERS TO THE POINTER, not just the ones with
	// answers on them: a press that fell through the gap between two bands would
	// expand a transcript row somebody was trying to deny.
	_, marks, _, _ := a.chrome(a.width)
	blocks := 0
	for _, mark := range marks {
		if mark.kind == chromeQuestion {
			blocks++
		}
	}
	if blocks != len(rows) {
		t.Fatalf("%d of the sheet's %d rows fall through to the body", len(rows)-blocks, len(rows))
	}
}

// THE COMMAND WRAPS RATHER THAN BEING CUT. One line fitted to forty-four
// columns is an approval prompt with the interesting half of the command
// missing — and the tail of a command is where the reason to say no usually is.
func TestTheSheetWrapsTheCommandInsteadOfCuttingIt(t *testing.T) {
	long := "bash git commit -m \"the wave\" --amend --no-verify && " +
		"git push --force-with-lease origin HEAD:refs/heads/main"
	_, a := wired([]session.Event{
		toolBegin("bash", long),
		consentEvent(7, "bash", long, ""),
	})
	a.width, a.height = 44, 30
	typeLine(t, a, "ship it")
	settleAsk(a)

	region := commandRegion(t, askRows(a))
	if len(region) < 2 {
		t.Fatalf("the command took one row on a forty-four column frame:\n%s",
			strings.Join(region, "\n"))
	}
	for _, want := range []string{"git commit", "refs/heads/main"} {
		if !strings.Contains(strings.Join(region, "\n"), want) {
			t.Fatalf("the command region lost %q:\n%s", want, strings.Join(region, "\n"))
		}
	}

	// AND IT IS CAPPED, because one pathological argument must not push the
	// answers off a short screen. What was dropped is said with an ellipsis
	// rather than dropped silently.
	huge := "bash " + strings.Repeat("git commit -m \"one more wave of words\" && ", 12)
	_, b := wired([]session.Event{toolBegin("bash", huge), consentEvent(8, "bash", huge, "")})
	b.width, b.height = 44, 30
	typeLine(t, b, "ship it")
	settleAsk(b)

	capped := askRows(b)
	region2 := commandRegion(t, capped)
	// The region's last row is the REASON, which is not the command: the gate
	// says why it is asking whether or not the policy gave words for it
	// ([session.ConsentFallbackReason]).
	region2 = region2[:len(region2)-1]
	if len(region2) != questionNarrowLines {
		t.Fatalf("the command region is %d rows, want the cap of %d:\n%s",
			len(region2), questionNarrowLines, strings.Join(region2, "\n"))
	}
	if !strings.HasSuffix(region2[len(region2)-1], glyphMore) {
		t.Fatalf("the capped command does not say it was cut: %q", region2[len(region2)-1])
	}
	if got, want := b.questionHeight(), len(capped); got != want {
		t.Fatalf("the block claims %d rows and drew %d", got, want)
	}
}

// A TAP ON A BAND IS THAT ANSWER. Three bands, three answers, and the scope each
// one carries is the scope its key carries.
func TestATapOnABandAnswersTheQuestion(t *testing.T) {
	for _, tc := range []struct {
		word string
		want answered
	}{
		{"allow once", answered{id: 7, allow: true, scope: session.ConsentOnce}},
		{"deny", answered{id: 7, allow: false, scope: session.ConsentOnce}},
		{"always", answered{id: 7, allow: true, scope: session.ConsentToolSession}},
	} {
		t.Run(tc.word, func(t *testing.T) {
			agent, a := phoneAsk(t)
			rows := askRows(a)
			at := -1
			for i, row := range rows {
				if strings.Contains(row, tc.word) && strings.Contains(row, "[") {
					at = i
				}
			}
			if at < 0 {
				t.Fatalf("no band says %q:\n%s", tc.word, strings.Join(rows, "\n"))
			}
			// The middle of the band, which is where a thumb lands and where the
			// word itself is not — a target that only answered under its own
			// letters would be the three-cell target this sheet exists to replace.
			// The spot is measured ONCE: the press answers and takes the sheet
			// down, so a second measure for the release would be of a block that
			// is no longer there.
			x, y := a.width/2, chromeRowY(t, a, at)
			drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			drive(t, a, tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
			if len(agent.answers) != 1 || agent.answers[0] != tc.want {
				t.Fatalf("a tap on %q resolved %+v, want %+v", tc.word, agent.answers, tc.want)
			}
			if a.asking() {
				t.Fatal("the question stayed up after it was tapped")
			}
		})
	}
}

// A PRESS THAT MISSES ANSWERS NOTHING AND REACHES NOTHING. The sheet covers the
// bottom of a small screen; a title row that let a click through would be the
// transcript answering for the person.
func TestAPressOnTheSheetNeverFallsThrough(t *testing.T) {
	agent, a := phoneAsk(t)
	rows := askRows(a)
	title := chromeRowY(t, a, 0)
	if !strings.Contains(rows[0], "? bash") {
		t.Fatalf("row zero is not the title: %q", rows[0])
	}
	opened := 0
	for i := range a.entries {
		if a.entries[i].open {
			opened++
		}
	}
	drive(t, a, tea.MouseClickMsg{X: 2, Y: title, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: 2, Y: title, Button: tea.MouseLeft})
	if len(agent.answers) != 0 {
		t.Fatalf("a press on the title answered the question: %+v", agent.answers)
	}
	if !a.asking() {
		t.Fatal("a press on the title took the question down")
	}
	for i := range a.entries {
		if a.entries[i].open {
			opened--
		}
	}
	if opened != 0 {
		t.Fatal("a press on the sheet reached a transcript row underneath it")
	}
}

// THE CLOCK IS BOTTOM-RIGHT AND OFF THE BANDS, and a pointer stops it exactly as
// a key does: the countdown is there so an unattended session cannot park a call
// forever, and a tap inside the question is a person at the machine.
func TestTheSheetsClockSitsBottomRightAndPausesOnKey(t *testing.T) {
	_, a := phoneAsk(t)
	a.askWait = 9 * time.Second
	startAskClock(a, a.now(), false)

	rows := askRows(a)
	foot := rows[len(rows)-1]
	if !strings.Contains(foot, "9s") {
		t.Fatalf("the sheet's last row does not carry the countdown: %q", foot)
	}
	if !strings.HasSuffix(strings.TrimRight(foot, " "), "9s") {
		t.Fatalf("the countdown is not at the right end of the row: %q", foot)
	}
	// It is not inside a target: the last band is above it, and a clock a person
	// cannot touch without answering is a clock that answers by accident.
	for _, tap := range a.questionBands {
		if tap.row == len(rows)-1 {
			t.Fatalf("the countdown shares a row with an answer: %+v", tap)
		}
	}

	// The hold, unchanged: every key the block reads stops the clock, the
	// answers included.
	drive(t, a, key("x"))
	if !askHeld(a) {
		t.Fatal("a key did not stop the reading clock")
	}
	foot = askRows(a)[len(rows)-1]
	if !strings.Contains(foot, "paused") {
		t.Fatalf("the paused clock does not say so: %q", foot)
	}
	if a.questionAnimating() {
		t.Fatal("the held countdown is still asking for frames")
	}
}

// THE QUEUE COUNT KEEPS ITS CORNER. Two questions raised together are two
// decisions, and the person answering the first must be told the second is
// coming — on the phone as everywhere else.
func TestTheSheetSaysHowManyQuestionsAreBehindIt(t *testing.T) {
	_, a := wired([]session.Event{
		toolBegin("bash", "bash make"),
		toolBegin("bash", "bash git push"),
		consentEvent(1, "bash", "bash make", "default"),
		consentEvent(2, "bash", "bash git push", "default"),
	})
	a.width, a.height = 44, 30
	typeLine(t, a, "build it")
	settleAsk(a)
	if askCount(a) != 2 {
		t.Fatalf("the surface holds %d questions, want two", askCount(a))
	}
	if got := strings.Join(askRows(a), "\n"); !strings.Contains(got, "1 more") {
		t.Fatalf("the sheet does not count what is behind it:\n%s", got)
	}
}

// A QUESTION THAT CANNOT REMEMBER A YES DOES NOT OFFER ONE — the sheet's own
// half of the rule the offer line keeps (consent.go's [ask.memo]).
func TestTheSheetLeavesTheAlwaysBandOffAQuestionThatCannotRememberIt(t *testing.T) {
	ask := consentEvent(5, "bash", "bash make test", "the turn is repeating itself")
	ask.Memo = false
	_, a := wired([]session.Event{toolBegin("bash", "bash make test"), ask})
	a.width, a.height = 44, 30
	typeLine(t, a, "run the tests")
	settleAsk(a)

	rows := askRows(a)
	if got := strings.Join(rows, "\n"); strings.Contains(got, "] always") {
		t.Fatalf("the widening yes is on a question that would drop it:\n%s", got)
	}
	if len(a.questionBands) != 2 {
		t.Fatalf("the sheet recorded %d targets, want two: %+v", len(a.questionBands), a.questionBands)
	}
	if got, want := a.questionHeight(), len(rows); got != want {
		t.Fatalf("the block claims %d rows and drew %d", got, want)
	}
}

// THE WIDER FRAMES DID NOT NOTICE. The sheet is the phone tier's shape and only
// the phone tier's: at sixty columns and above the block is the same three rows
// — the call, the answers, the rule — in the same order.
func TestTheWideBlockIsUnchangedByThePhoneSheet(t *testing.T) {
	// Sixty columns is the promotion's own width and is
	// [TestANarrowFrameGivesEveryAnswerARowRatherThanCuttingOne]'s: the answers
	// row does not fit there and the card is what it becomes.
	for _, width := range []int{80, 120, 200} {
		_, a := phoneAsk(t)
		a.width = width
		rows := askRows(a)
		if len(rows) != 3 {
			t.Fatalf("at %d columns the block is %d rows:\n%s", width, len(rows),
				strings.Join(rows, "\n"))
		}
		if !strings.Contains(rows[0], "rm -rf build") {
			t.Fatalf("at %d columns the block does not open with the call: %q", width, rows[0])
		}
		offer, at := askOffer(t, a)
		if at != 1 {
			t.Fatalf("at %d columns the answers are on row %d, want the row under the call", width, at)
		}
		if !strings.Contains(plain(offer), "allow? [1] allow once") {
			t.Fatalf("at %d columns the answers row is gone: %q", width, plain(offer))
		}
		if !strings.Contains(rows[2], `bash pattern "rm -rf *"`) {
			t.Fatalf("at %d columns the rule is gone: %q", width, rows[2])
		}
		if len(a.questionBands) != 0 {
			t.Fatalf("a band reached a %d-column frame: %+v", width, a.questionBands)
		}
		if got, want := a.questionHeight(), len(rows); got != want {
			t.Fatalf("at %d columns the block claims %d rows and drew %d", width, got, want)
		}
	}
}

// AND THE ANSWERS ROW IS A TARGET TOO. Every answer on this surface is a key and
// a tap at every width — the phone sheet is the shape that changes, not the
// bargain.
func TestTheOfferLineAnswersToThePointerAtTheWiderWidths(t *testing.T) {
	agent, a := phoneAsk(t)
	a.width = 120
	row, block := askOffer(t, a)
	line := plain(row)
	at := strings.Index(line, "[2]")
	if at < 0 || len(a.questionSpans) != 3 {
		t.Fatalf("the answers row recorded %d targets on %q", len(a.questionSpans), line)
	}
	// The WORD is part of the target and not decoration beside it: three cells is
	// a target a person aims at, and "[2] always, this tool" is one they hit.
	for _, span := range a.questionSpans {
		if w := span.to - span.from; w < 4 {
			t.Fatalf("an answer's target is %d cells wide: %+v", w, span)
		}
	}
	// Measured once: the press answers, and the answers row is gone by the
	// release.
	x, y := at+4, chromeRowY(t, a, block)
	drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	want := answered{id: 7, allow: true, scope: session.ConsentToolSession}
	if len(agent.answers) != 1 || agent.answers[0] != want {
		t.Fatalf("a press on the widening yes resolved %+v, want %+v", agent.answers, want)
	}
}
