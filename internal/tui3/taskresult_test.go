package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── WHAT AN OPEN LANDED CARD SAYS, AND IN WHAT ORDER ────────────────────────
//
// THE DEFECT, FROM A REAL CARD (2026-09-07). A ten-chapter story landed, its
// branch would not merge, somebody took it as done, and the expansion drew ONE
// DIM WALL in composition order: the merge refusal, git's own error, the
// sentence recording the decision, the words "finished, but needs your look"
// from before that decision, the file count, the branch, the model, the price,
// the span — and then, at the bottom and still in its markdown source, the
// story. Everything at one weight, in one hue, with the thing the person
// delegated the work for last.
//
// The card read ONE FIELD. internal/session has kept the landing's own story and
// what the work produced apart since task_result.go — the report is rewritten by
// every accept, verdict and refused merge that happens to a node afterwards,
// while the result never moves — and this card had never read the second one.
//
// These are the rules the split lands under.

// doneResultApp is one landed node with an answer of its own, at a stated width.
func doneResultApp(t *testing.T, width int, state session.TaskState, notice session.TaskNotice) *app {
	t.Helper()
	a, _, advance := roomApp(t)
	a.width = width
	advance(14*time.Minute + 29*time.Second)
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(7, "Write a 10 chapter story", state, notice)})
	return a
}

// conflictedStoryNotice is the screenshot's own landing: an answer in markdown,
// a branch that would not merge, git's words about why, and the sentence
// recording that somebody took it as done anyway.
func conflictedStoryNotice() session.TaskNotice {
	return session.TaskNotice{
		Model: "anthropic/claude-opus-4", CostUSD: 0.42,
		Merge: mergeWordConflicted, Branch: "task/write-a-10-chapter-story-9c1a",
		Changed: []string{"story/chapters.md"},
		Report: "its branch task/write-a-10-chapter-story-9c1a did not merge cleanly and was kept: " +
			"story/chapters.md already holds work of your own\n" +
			"error: Your local changes to the following files would be overwritten by merge:\n" +
			"you took this as done",
		Result: "**The Salt Road**\n\n## Chapter one\n\nThe cart came over the ridge at dawn.\n",
	}
}

// THE ANSWER IS READ AS PROSE AND NOT AS ITS SOURCE. A task asked for writing
// answers in markdown whether or not anybody asked it to, and the card used to
// wrap that text as plain lines — so a person who delegated a story got
// `**The Salt Road**` back, asterisks and all, in the same grey as the file
// count.
func TestAnOpenCardReadsTheAnswerAsProseAndNotAsMarkdownSource(t *testing.T) {
	a := doneResultApp(t, 80, session.TaskDone, conflictedStoryNotice())
	clickHit(t, a, hitDone)

	text := taskText(a)
	for _, want := range []string{"The Salt Road", "Chapter one", "The cart came over the ridge at dawn."} {
		if !strings.Contains(text, want) {
			t.Fatalf("the open card does not say %q:\n%s", want, text)
		}
	}
	for _, never := range []string{"**The Salt Road**", "## Chapter one"} {
		if strings.Contains(text, never) {
			t.Fatalf("the open card drew the answer's markdown source (%q):\n%s", never, text)
		}
	}
}

// AND NOTHING WRAPS IT IN A SECOND FOREGROUND. prose paints its own rows, body
// ink included (markdown.go states the law), so the answer's rows must not be
// handed through [palette.dim] the way every furniture row on this card is —
// which is exactly what made the whole expansion one grey wall.
func TestTheAnswerIsNotPaintedInTheFurnitureHue(t *testing.T) {
	a := doneResultApp(t, 80, session.TaskDone, conflictedStoryNotice())
	card := a.doneCardAt(len(a.entries) - 1)
	if card == nil {
		t.Fatal("the landing drew no card")
	}
	card.open = true

	// The hue is read off the palette rather than spelled, and it is proved to be
	// a hue at all before anything is asserted about its absence: on a terminal
	// that paints nothing this test would otherwise pass by saying nothing.
	dim := huePrefix(a.pal.dim("x"))
	if dim == "" {
		t.Fatal("the test palette must paint a distinct furniture hue")
	}
	for _, line := range a.doneDetail(card, 80) {
		if !strings.Contains(plain(line), "The cart came over the ridge at dawn.") {
			continue
		}
		if strings.Contains(line, dim) {
			t.Fatalf("the answer is painted in the furniture hue:\n%q", line)
		}
		return
	}
	t.Fatalf("the open card never drew the answer:\n%s", plainOf(a.doneDetail(card, 80)))
}

// A DELIVERY THAT DID NOT GET HOME IS SAID ONCE, AT THE TOP, IN THE WARN HUE.
// The head already says the state; this is the sentence that says WHY, and it
// may never be filed under the answer or left in the same grey as the price — a
// card reading `done` over an unmerged branch is the one thing this expansion
// must not let happen quietly.
func TestAnUndeliveredLandingLeadsTheCardInTheWarnHue(t *testing.T) {
	a := doneResultApp(t, 80, session.TaskDone, conflictedStoryNotice())
	card := a.doneCardAt(len(a.entries) - 1)
	if card == nil {
		t.Fatal("the landing drew no card")
	}
	card.open = true

	rows := a.doneDetail(card, 80)
	if len(rows) == 0 {
		t.Fatal("the open card drew nothing")
	}
	first := rows[0]
	if !strings.Contains(plain(first), "did not merge cleanly") {
		t.Fatalf("the card does not lead with why the work is not in the tree:\n%s", plainOf(rows))
	}
	if !strings.Contains(plain(first), glyphHalted) {
		t.Fatalf("the delivery line carries no mark:\n%q", plain(first))
	}
	if warn := huePrefix(a.pal.warn("x")); warn != "" && !strings.Contains(first, warn) {
		t.Fatalf("the delivery line is not drawn in the warn hue:\n%q", first)
	}
	// ONCE. The sentence is the report's first line, and the report used to be
	// printed whole further down the same card as well.
	if n := strings.Count(plainOf(rows), "did not merge cleanly"); n != 1 {
		t.Fatalf("the delivery failure is stated %d times, want once:\n%s", n, plainOf(rows))
	}
}

// AND THE DIAGNOSTICS UNDER IT ARE KEPT. git's own words about a refusal are
// what somebody needs to fix it; what changed is that they are RANKED — quiet,
// under the sentence that says where the work is, rather than beside it.
func TestTheLandingsOwnDetailStaysOnTheCardUnderTheHeadline(t *testing.T) {
	a := doneResultApp(t, 80, session.TaskDone, conflictedStoryNotice())
	clickHit(t, a, hitDone)

	text := taskText(a)
	for _, want := range []string{
		"would be overwritten",
		"you took this as done",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the open card dropped %q:\n%s", want, text)
		}
	}
	headline := strings.Index(text, "did not merge cleanly")
	detail := strings.Index(text, "would be overwritten")
	answer := strings.Index(text, "The cart came over the ridge at dawn.")
	if headline < 0 || detail < headline || answer < detail {
		t.Fatalf("the card is not in the order why → how → what it produced:\n%s", text)
	}
}

// AN OPEN CARD DOES NOT REPEAT ITS OWN PREVIEW. The muted line under the head
// quotes the landing's first sentence, which is the sentence the expansion
// prints in full one row lower — so on an open card the quote stands down. It
// is still there the moment the card is closed again.
func TestAnOpenCardDropsTheQuotedPreviewItIsAboutToPrintInFull(t *testing.T) {
	a := doneResultApp(t, 160, session.TaskDone, conflictedStoryNotice())

	collapsed := taskText(a)
	if !strings.Contains(collapsed, `"its branch task/write-a-10-chapter-story-9c1a did not merge cleanly`) {
		t.Fatalf("the collapsed card lost its preview:\n%s", collapsed)
	}
	clickHit(t, a, hitDone)
	open := taskText(a)
	if strings.Contains(open, `"its branch`) {
		t.Fatalf("the open card still quotes the sentence it prints in full:\n%s", open)
	}
	// And closing it brings the preview back, unchanged.
	clickHit(t, a, hitDone)
	if again := taskText(a); again != collapsed {
		t.Fatalf("closing the card did not restore it:\nwas:\n%s\nnow:\n%s", collapsed, again)
	}
}

// WHOSE HANDS, WHAT THEY CAME TO AND WHEN ARE ONE ROW. They were three stacked
// rows of four words each, which is what a table looks like when it should have
// been a sentence. Nobody opens a landed card to read the price.
func TestTheSecondaryFactsAreOneRow(t *testing.T) {
	a := doneResultApp(t, 160, session.TaskDone, conflictedStoryNotice())
	card := a.doneCardAt(len(a.entries) - 1)
	if card == nil {
		t.Fatal("the landing drew no card")
	}
	card.open = true

	want := doneModelLabel + "anthropic/claude-opus-4" + railSep + dollars(0.42) + railSep + doneSpanLabel
	if got := plainOf(a.doneDetail(card, 158)); !strings.Contains(got, want) {
		t.Fatalf("the facts are not one row (%q):\n%s", want, got)
	}
}

// AND THE ROW WRAPS BY WHOLE FACTS AT EVERY WIDTH. A span broken across two
// lines is a fact a person has to reassemble, so the row starts a new line
// rather than splitting one — and every fact survives at sixty columns.
func TestTheSecondaryFactsRowNeverSplitsAFactAtAnyWidth(t *testing.T) {
	for _, width := range []int{60, 80, 160} {
		a := doneResultApp(t, width, session.TaskDone, conflictedStoryNotice())
		card := a.doneCardAt(len(a.entries) - 1)
		if card == nil {
			t.Fatalf("width %d: the landing drew no card", width)
		}
		card.open = true
		rows := a.doneDetail(card, width-2)
		got := plainOf(rows)
		for _, fact := range []string{
			doneModelLabel + "anthropic/claude-opus-4",
			dollars(0.42),
			doneSpanLabel + "09:00 → 09:14",
		} {
			if !strings.Contains(got, fact) {
				t.Fatalf("width %d: the facts row lost or split %q:\n%s", width, fact, got)
			}
		}
		// AND NOTHING OVERFLOWS THE COLUMN, which is the other half of a row that
		// wraps rather than truncates.
		for _, line := range rows {
			if w := ansi.StringWidth(line); w > width-2 {
				t.Fatalf("width %d: a row is %d cells wide:\n%q", width, w, plain(line))
			}
		}
	}
}

// A CUT ANSWER SAYS WHERE THE WHOLE OF IT IS. The engine hands a reader only the
// beginning of a long answer (session's taskResultCarry), and a block of text
// that stops mid-sentence with nothing under it is this card claiming the work
// stopped there.
func TestACutAnswerNamesWhereTheWholeOfItIs(t *testing.T) {
	a := doneResultApp(t, 80, session.TaskDone, session.TaskNotice{
		Merge: mergeWordMerged, Report: "the survey is written",
		Result: "Sixteen machines answer for this model.", ResultCut: true,
		ResultWhole: "/tmp/journal/node-7-result.txt",
	})
	clickHit(t, a, hitDone)

	text := taskText(a)
	if !strings.Contains(text, doneMoreAt+"/tmp/journal/node-7-result.txt") {
		t.Fatalf("the card does not say where the rest of the answer is:\n%s", text)
	}
}

// AND A CUT ANSWER NOBODY KEPT SAYS THAT INSTEAD. The emptiness law is about
// figures; this is its sibling about pointers — a card must not draw a path it
// does not have, and "the rest of it was not kept" is a whole sentence.
func TestACutAnswerWithNowhereToPointSaysSo(t *testing.T) {
	a := doneResultApp(t, 80, session.TaskDone, session.TaskNotice{
		Merge: mergeWordMerged, Report: "the survey is written",
		Result: "Sixteen machines answer for this model.", ResultCut: true,
	})
	clickHit(t, a, hitDone)

	text := taskText(a)
	if !strings.Contains(text, doneMoreGone) {
		t.Fatalf("the card is silent about an answer it only has the start of:\n%s", text)
	}
	if strings.Contains(text, doneMoreAt) {
		t.Fatalf("the card points somewhere nothing named:\n%s", text)
	}
}

// AN ANSWER THE CHECK DID NOT ACCEPT IS NAMED AND NOT DRAWN. Nothing was handed
// over, so there is no answer on this card to promote — and the whole report is
// the landing's own account, every line of it.
func TestAHeldAnswerIsNamedAndNeverDrawnAsTheAnswer(t *testing.T) {
	a := doneResultApp(t, 80, session.TaskFailed, session.TaskNotice{
		Merge: mergeWordAborted, Branch: "task/parser", Ending: session.TaskEndingRefused,
		Report: "incomplete — the key table is ported and the escape table is not\n" +
			"its branch task/parser was kept",
		ResultHeld: true, ResultWhole: "/tmp/journal/node-7.jsonl",
	})
	card := a.doneCardAt(len(a.entries) - 1)
	if card == nil {
		t.Fatal("the landing drew no card")
	}
	card.open = true

	answer, account := card.answerAndAccount()
	if answer != "" {
		t.Fatalf("a held answer was promoted to the card's answer:\n%s", answer)
	}
	if !strings.Contains(account, "its branch task/parser was kept") {
		t.Fatalf("the landing's account lost a line to the answer block:\n%s", account)
	}
	// AND THE POINTER IS WHERE THE ANSWER IS AND NOTHING ELSE. The sentence this
	// block used to lead with — `what it produced was not taken as done` — was a
	// second spelling of a state the card's own reason row now says in the
	// engine's words (docs/design/task-states/DESIGN.md).
	got := plainOf(a.doneDetail(card, 78))
	if !strings.Contains(got, doneMoreAt+"/tmp/journal/node-7.jsonl") {
		t.Fatalf("the card does not say where the answer is:\n%s", got)
	}
	if strings.Contains(got, "was not taken as done") {
		t.Fatalf("the card still spells the state a second time:\n%s", got)
	}
}

// A LANDING THAT CARRIED NO RESULT AT ALL DRAWS WHAT IT ALWAYS DREW. The engine
// omits the result when the report already carries the answer exactly — every
// task that finished in two or three short lines — and a checkpoint written
// before results existed has none. Both fall back to the report, whole, with no
// second account line above it.
func TestALandingWithNoResultFallsBackToItsReport(t *testing.T) {
	a := doneResultApp(t, 80, session.TaskDone, session.TaskNotice{
		Model: "openai/gpt-5", Merge: mergeWordMerged, Branch: "task/story",
		Report: "the guard is in and the regression test passes",
	})
	card := a.doneCardAt(len(a.entries) - 1)
	if card == nil {
		t.Fatal("the landing drew no card")
	}
	card.open = true

	answer, account := card.answerAndAccount()
	if answer != "the guard is in and the regression test passes" {
		t.Fatalf("the report is not the answer of a card with no result:\n%q", answer)
	}
	if account != "" {
		t.Fatalf("a delivered landing invented an account line:\n%q", account)
	}
	if n := strings.Count(plainOf(a.doneDetail(card, 78)), "the guard is in"); n != 1 {
		t.Fatalf("the report is drawn %d times, want once:\n%s", n, plainOf(a.doneDetail(card, 78)))
	}
}

// AND AN UNDELIVERED LANDING WITH NO RESULT SPLITS ITS REPORT RATHER THAN
// DRAWING IT TWICE. The engine composes such a report with the landing sentence
// FIRST, which is the sentence the head's preview already quotes, so that line
// is the account and the rest of the report is what the work said.
func TestAnUndeliveredLandingWithNoResultSplitsItsReportOnce(t *testing.T) {
	a := doneResultApp(t, 80, session.TaskUnverified, session.TaskNotice{
		Merge: mergeWordAborted, Branch: "task/story",
		Report: "finished, but needs your look — nobody could say whether it holds\n" +
			"ten chapters are written and the last one is short",
	})
	card := a.doneCardAt(len(a.entries) - 1)
	if card == nil {
		t.Fatal("the landing drew no card")
	}
	card.open = true

	answer, account := card.answerAndAccount()
	if account != "finished, but needs your look — nobody could say whether it holds" {
		t.Fatalf("the landing sentence is not the account:\n%q", account)
	}
	if answer != "ten chapters are written and the last one is short" {
		t.Fatalf("what the work said is not the answer:\n%q", answer)
	}
	got := plainOf(a.doneDetail(card, 78))
	if n := strings.Count(got, "nobody could say whether it holds"); n != 1 {
		t.Fatalf("the landing sentence is drawn %d times, want once:\n%s", n, got)
	}
}

// EVERY ROW OF AN OPEN CARD IS STILL THE CARD. The whole block folds back on a
// click anywhere in it — the answer's rows included — which is the mechanic the
// expansion has always been behind, and the one a new block of rows is easiest
// to drop out of.
func TestEveryRowOfAnOpenCardFoldsItBackOnAClick(t *testing.T) {
	a := doneResultApp(t, 160, session.TaskDone, conflictedStoryNotice())
	clickHit(t, a, hitDone)
	card := a.doneCardAt(len(a.entries) - 1)
	if card == nil || !card.open {
		t.Fatal("the click did not open the card")
	}

	// The answer's own row answers to the card, and pressing it closes it.
	body, _ := a.window(a.width, a.viewHeight())
	found := false
	for i, r := range body {
		if !strings.Contains(plain(r.text), "The cart came over the ridge at dawn.") {
			continue
		}
		found = true
		if r.hit != hitDone {
			t.Fatalf("the answer's row is not part of the card it is inside:\n%q", plain(r.text))
		}
		drive(t, a, tea.MouseClickMsg{Y: a.bodyTop() + i, Button: tea.MouseLeft})
		drive(t, a, tea.MouseReleaseMsg{Y: a.bodyTop() + i, Button: tea.MouseLeft})
		break
	}
	if !found {
		t.Fatalf("the open card drew no answer to press:\n%s", taskText(a))
	}
	if card.open {
		t.Fatal("a click on the answer did not fold the card back")
	}
}

// AND IT SURVIVES A NAME NO COLUMN CAN HOLD. A path with no spaces in it and a
// run of wide characters are the two shapes that break a fitter, and the card
// draws both at sixty columns without overflowing.
func TestAnOpenCardHoldsItsColumnAgainstLongPathsAndWideCharacters(t *testing.T) {
	for _, width := range []int{60, 80, 160} {
		a := doneResultApp(t, width, session.TaskDone, session.TaskNotice{
			Model: "openai/gpt-5", Merge: mergeWordMerged,
			Branch:  "task/" + strings.Repeat("a-very-long-branch-segment-", 4),
			Changed: []string{"内部/非常に長い名前のファイル/" + strings.Repeat("段落", 30) + ".md"},
			Report:  "章が十個そろいました",
			Result:  "第一章\n\n" + strings.Repeat("荷車は夜明けに尾根を越えてきた。", 12),
			Brief:   strings.Repeat("salt caravan ", 40),
		})
		card := a.doneCardAt(len(a.entries) - 1)
		if card == nil {
			t.Fatalf("width %d: the landing drew no card", width)
		}
		card.open = true
		rows := a.doneRows(card, width, false)
		if len(rows) < 2 {
			t.Fatalf("width %d: the open card drew %d rows", width, len(rows))
		}
		for _, line := range rows {
			if w := ansi.StringWidth(line); w > width {
				t.Fatalf("width %d: a row is %d cells wide:\n%q", width, w, plain(line))
			}
		}
	}
}

// huePrefix is the escape a palette opens one hue with, read off a painted
// sample rather than spelled: the ramp is the palette's to choose, and a test
// that quoted an escape sequence would be a second copy of it.
func huePrefix(painted string) string {
	if i := strings.Index(painted, "x"); i > 0 {
		return painted[:i]
	}
	return ""
}

func TestExpandedAnswerDoesNotRepeatTheComposedReportPreview(t *testing.T) {
	notice := conflictedStoryNotice()
	notice.Report += "\n" + strings.TrimSpace(notice.Result)
	a := doneResultApp(t, 80, session.TaskDone, notice)
	clickHit(t, a, hitDone)
	text := taskText(a)
	if strings.Count(text, "The Salt Road") != 1 || !strings.Contains(text, "did not merge cleanly") {
		t.Fatalf("answer must appear once with its delivery diagnostic:\n%s", text)
	}
}
