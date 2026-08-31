package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE FORMING BLOCK WHILE THE BRIEF IS BEING WRITTEN.
//
// One claim, and every test here is a corner of it: the seconds between a task
// being asked for and a task existing are seconds a person can SEE INTO — the
// newest line of the brief under the phase row, a few more of them behind the
// fold every block on this surface has, and one block rather than a stack when
// several commands are in flight at once.

// shapingApp is a surface with `/task` shaping a brief and nothing else going
// on: the sizing call is off, so the command goes straight to the wait this file
// is about.
func shapingApp(t *testing.T, briefs ...string) *app {
	t.Helper()
	a := taskStartApp(t, &taskCommandFake{Agent: &fakeAgent{model: "m"}}, config.TaskStartSingle)
	base := time.Now()
	a.clock = func() time.Time { return base.Add(13 * time.Second) }
	for _, brief := range briefs {
		_ = a.slash("/task " + brief)
	}
	if len(a.waits) != len(briefs) {
		t.Fatalf("%d commands raised %d forming blocks", len(briefs), len(a.waits))
	}
	return a
}

// shaping feeds one fragment of the shaper's answer to the wait at `at`, exactly
// as the lane delivers it: the raw accumulated text of a JSON object nobody has
// finished writing.
func shaping(a *app, at int, brief string) {
	a.shapingTail(a.waits[at].seq, `{"title":"a name","brief":"`+brief)
}

// ── 1. the tail ─────────────────────────────────────────────────────────────

// THE DEFECT THIS CLOSES, checked where it was visible: `shaping the brief…`
// and a number going up, for as long as a careful model takes to write three
// paragraphs. The words exist the whole time; now they are on screen.
//
// AND THE ROW IS ONE ROW, WHATEVER ARRIVES. That is the tail's whole promise —
// a preview that grew a line every second would push the conversation up the
// screen for the length of the call.
func TestTheFormingTailShowsTheBriefBeingWrittenAndNeverAddsARow(t *testing.T) {
	a := shapingApp(t, "write the release notes")

	// BEFORE THE FIRST FRAGMENT THERE IS NO TAIL. Nothing has been written, and
	// the emptiness law's answer to an unknown is to draw no row at all.
	quiet := a.preflightRows(72)
	if len(quiet) != 3 {
		t.Fatalf("a wait with nothing written yet drew %d rows, want 3:\n%s", len(quiet), plainRowsText(quiet))
	}
	if !strings.Contains(plainRowsText(quiet), taskShapingNote) {
		t.Fatalf("the block does not say what it is waiting on:\n%s", plainRowsText(quiet))
	}

	shaping(a, 0, "Write the release notes for v2.4.")
	first := a.preflightRows(72)
	if len(first) != 4 {
		t.Fatalf("the tail drew %d rows, want the three-row block and one tail:\n%s",
			len(first), plainRowsText(first))
	}
	if body := plainRowsText(first); !strings.Contains(body, "release notes for v2.4") {
		t.Fatalf("the tail does not carry the brief:\n%s", body)
	}

	// IT MOVES. More of the brief arrives and the row says the newer thing —
	// which is the whole difference between a preview and a still photograph.
	shaping(a, 0, "Write the release notes for v2.4. The audience is somebody who was not in the room, so name what changed for the person using it.")
	moved := a.preflightRows(72)
	if len(moved) != 4 {
		t.Fatalf("A SECOND FRAGMENT GREW THE BLOCK: %d rows\n%s", len(moved), plainRowsText(moved))
	}
	if plainRowsText(moved) == plainRowsText(first) {
		t.Fatalf("the tail did not move as the brief was written:\n%s", plainRowsText(moved))
	}
	if body := plainRowsText(moved); !strings.Contains(body, "person using it.") {
		t.Fatalf("the tail is not showing the NEWEST part of the brief:\n%s", body)
	}

	// AND A LONG BRIEF IS STILL ONE ROW. Six hundred words of it changes the
	// words on the row and nothing about the height of the block.
	shaping(a, 0, strings.Repeat("the brief goes on and on and on. ", 60))
	if long := a.preflightRows(72); len(long) != 4 {
		t.Fatalf("A LONG BRIEF GREW THE BLOCK: %d rows\n%s", len(long), plainRowsText(long))
	}
}

// A FRAGMENT THAT SAYS LESS THAN WHAT IS ON SCREEN IS NOT AN ERASURE. The field
// being followed opens before it has any content, so the first fragments after
// the brief's opening quote honestly answer with nothing — and a tail that
// blanked itself every time the model paused would flicker at the person reading
// it.
func TestTheFormingTailIsNeverUnsaidByAnEmptyFragment(t *testing.T) {
	a := shapingApp(t, "write the release notes")
	shaping(a, 0, "Write the release notes for v2.4.")
	a.shapingTail(a.waits[0].seq, `{"title":"a name","brief":"`)
	if body := plainRowsText(a.preflightRows(72)); !strings.Contains(body, "release notes for v2.4") {
		t.Fatalf("an empty fragment wiped the tail:\n%s", body)
	}
}

// THE PROPOSAL ROAD HAS NO STREAM BEHIND IT, and it draws no tail. That brief
// was written before the person was ever asked, so a row promising a preview
// would be machinery pretending to be telemetry.
func TestAnApprovedProposalsBlockDrawsNoTail(t *testing.T) {
	a := newTestApp(&taskCommandFake{Agent: &fakeAgent{model: "m"}})
	a.task = &taskCard{id: 41, title: "index the adapters", name: "adapter index"}
	a.entries = append(a.entries, entry{kind: entryTask, turn: a.turn, card: a.task})
	a.answerTask(true, "")
	rows := a.preflightRows(72)
	if len(rows) != 3 {
		t.Fatalf("the proposal's block drew %d rows, want 3:\n%s", len(rows), plainRowsText(rows))
	}
	for _, r := range rows {
		if r.hit == hitForming {
			t.Fatalf("a block with nothing to show offered a door: %q", plain(r.text))
		}
	}
}

// ── 2. the window ───────────────────────────────────────────────────────────

// THE FOLD EVERY BLOCK HAS, SPENT ON THE ONE THING STILL BEING WRITTEN. `→` on
// an empty box opens the tail into a few lines, `←` shuts it, and the mark on
// the row says which of the two it is at rest.
func TestTheFormingTailOpensAWindowAndShutsItAgain(t *testing.T) {
	a := shapingApp(t, "write the release notes")
	long := "Write the release notes for v2.4. " + strings.Repeat("Say what changed for the person using it, and which release it lands in. ", 12)
	shaping(a, 0, long)

	closed := a.preflightRows(72)
	if len(closed) != 4 {
		t.Fatalf("the closed block is %d rows, want 4:\n%s", len(closed), plainRowsText(closed))
	}
	if !strings.Contains(plain(closed[3].text), bandFoldGlyph) {
		t.Fatalf("the closed tail wears no fold mark: %q", plain(closed[3].text))
	}

	if !a.openForming() {
		t.Fatal("→ found nothing to open on a block with a brief arriving")
	}
	open := a.preflightRows(72)
	if want := 3 + formingWindowLines; len(open) != want {
		t.Fatalf("the window is %d rows, want %d:\n%s", len(open), want, plainRowsText(open))
	}
	if !strings.Contains(plain(open[3].text), bandFoldOpenGlyph) {
		t.Fatalf("the opened window wears no open mark: %q", plain(open[3].text))
	}
	// THE WINDOW ENDS WHERE THE TAIL WAS: it is the last few lines, so its last
	// row carries the words the closed block was showing. Only the mark differs —
	// it rides the first row of whatever is drawn.
	tailWords := strings.TrimSpace(strings.TrimPrefix(plain(closed[len(closed)-1].text), formingRail+bandFoldGlyph))
	lastWords := strings.TrimSpace(strings.TrimPrefix(plain(open[len(open)-1].text), formingRail))
	if tailWords != lastWords {
		t.Fatalf("the window does not end on the tail:\n closed %q\n open   %q", tailWords, lastWords)
	}

	if !a.closeForming() {
		t.Fatal("← found nothing to shut on an open window")
	}
	if shut := a.preflightRows(72); len(shut) != 4 {
		t.Fatalf("the window did not collapse back: %d rows\n%s", len(shut), plainRowsText(shut))
	}

	// AND THE KEYS KEEP EVERY MEANING THEY HAVE. A block with nothing written,
	// and a window already shut, both answer false so the arrows go on navigating.
	if a.closeForming() {
		t.Fatal("← took the key from a block with nothing open")
	}
	b := shapingApp(t, "write the release notes")
	if b.openForming() {
		t.Fatal("→ took the key from a block with nothing written yet")
	}
}

// THE PRESS IS THE SAME FOLD REACHED WITH A HAND, and the whole of a wait's rows
// answer it — the row and the preview under it are one thing.
func TestPressingTheFormingBlockOpensAndShutsIt(t *testing.T) {
	a := shapingApp(t, "write the release notes")
	shaping(a, 0, "Write the release notes for v2.4.")
	rows := a.preflightRows(72)
	tail := rows[len(rows)-1]
	if tail.hit != hitForming {
		t.Fatalf("the tail answers no press: hit %v", tail.hit)
	}
	a.formingPress(tail.turn)
	if !a.waits[0].open {
		t.Fatal("a press on the tail opened nothing")
	}
	a.formingPress(tail.turn)
	if a.waits[0].open {
		t.Fatal("a second press did not shut it again")
	}
}

// ── 3. the brief that landed ────────────────────────────────────────────────

// WHEN THE BRIEF LANDS, THE PREVIEW GOES WITH THE BLOCK. The tail is telemetry
// about a wait; the moment there is a task to point at, the wait is over and the
// task's own row is the thing that says so.
func TestALandedBriefTakesTheTailAndTheWindowWithIt(t *testing.T) {
	f := &taskCommandFake{Agent: &fakeAgent{model: "m"}}
	a := taskStartApp(t, f, config.TaskStartSingle)
	cmd := a.slash("/task write the release notes")
	shaping(a, 0, "Write the release notes for v2.4.")
	a.waits[0].open = true
	if len(a.preflightRows(72)) < 4 {
		t.Fatal("the block never grew a preview to lose")
	}
	_, _ = a.Update(taskMsg(cmd))
	if a.waiting() {
		t.Fatal("the wait outlived the task it was waiting for")
	}
	if got := plainRowsText(a.preflightRows(72)); got != "" {
		t.Fatalf("the preview survived the landing:\n%s", got)
	}
}

// ── 4. several at once ──────────────────────────────────────────────────────

// THREE COMMANDS IN FLIGHT ARE ONE BLOCK, and only the row a person is pointed
// at shows anything under it. Three four-row blocks stacked at the transcript
// tail is a wall; this is a head, three rows and a preview.
func TestSeveralTasksFormingShareOneBlockAndOnePreview(t *testing.T) {
	a := shapingApp(t, "write the release notes", "fix the nil-map crash", "write the docs")
	shaping(a, 0, "Write the release notes for v2.4.")
	shaping(a, 1, "Reproduce the crash from the stack trace in issue #94.")
	shaping(a, 2, "Document the /task command and its two forms.")

	a.waitAt = 1
	rows := a.preflightRows(72)
	body := plainRowsText(rows)
	if !strings.Contains(body, "tasks · 3 forming") {
		t.Fatalf("three waits drew no head that counts them:\n%s", body)
	}
	if len(rows) != 1+3+1 {
		t.Fatalf("three waits drew %d rows, want a head, three rows and one preview:\n%s", len(rows), body)
	}
	for _, want := range []string{"write the release notes", "fix the nil-map crash", "write the docs"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the block lost %q:\n%s", want, body)
		}
	}
	// ONLY THE POINTED ROW'S PREVIEW. The other two are shaping too and neither
	// of them is drawing a line.
	if !strings.Contains(body, "stack trace in issue #94") {
		t.Fatalf("the pointed row shows no preview:\n%s", body)
	}
	if strings.Contains(body, "release notes for v2.4") || strings.Contains(body, "two forms") {
		t.Fatalf("A ROW NOBODY IS POINTED AT DREW ITS PREVIEW:\n%s", body)
	}

	// AND THE ROWS WALK WITH THE KEYS THE TRANSCRIPT ALREADY WALKS WITH, moving
	// the preview and nothing else.
	if !a.walkForming(-1) || a.waitAt != 0 {
		t.Fatalf("↑ did not walk the block: at %d", a.waitAt)
	}
	if body := plainRowsText(a.preflightRows(72)); !strings.Contains(body, "release notes for v2.4") {
		t.Fatalf("the preview did not follow the walk:\n%s", body)
	}
	if a.walkForming(-1) {
		t.Fatal("the walk ran off the top instead of falling through")
	}
	if !a.walkForming(1) || !a.walkForming(1) || a.waitAt != 2 {
		t.Fatalf("↓ did not reach the last row: at %d", a.waitAt)
	}
	if a.walkForming(1) {
		t.Fatal("the walk ran off the bottom instead of falling through")
	}

	// A PRESS ON A ROW NOBODY IS POINTED AT TAKES THE POINT AND OPENS IT, because
	// the point is only visible as the preview it draws.
	a.formingPress(0)
	if a.waitAt != 0 || !a.waits[0].open {
		t.Fatalf("a press on another row did not take the point: at %d open %v", a.waitAt, a.waits[0].open)
	}
}

// WITH EXACTLY ONE TASK FORMING, NONE OF THE PLURAL MACHINERY DRAWS. No head
// that counts, no row that walks — the block is what it always was, plus the
// tail. It is the emptiness law read at the shape of a block rather than at a
// number.
func TestOneTaskFormingDrawsNoneOfThePluralMachinery(t *testing.T) {
	a := shapingApp(t, "write the release notes")
	shaping(a, 0, "Write the release notes for v2.4.")
	body := plainRowsText(a.preflightRows(72))
	if strings.Contains(body, formingHeadWord) || strings.Contains(body, formingStateWord) {
		t.Fatalf("a single wait drew the head that counts several:\n%s", body)
	}
	if !strings.HasPrefix(body, "▏ task\n") {
		t.Fatalf("the single block is not the block it has always been:\n%s", body)
	}
	if a.walkForming(1) || a.walkForming(-1) {
		t.Fatal("a single wait took the arrows the transcript walks with")
	}
	for _, r := range a.preflightRows(72) {
		if a.formingHot(r) {
			t.Fatalf("a single wait lit a row to say which one it is: %q", plain(r.text))
		}
	}
}

// AND A SETTLE FINDS ITS OWN BLOCK. Two commands are in flight and the door
// answers for one of them; the other one is still shaping and must still be on
// screen, saying so.
func TestASettleCollapsesItsOwnWaitAndNoOther(t *testing.T) {
	a := shapingApp(t, "write the release notes", "fix the nil-map crash")
	first := a.waits[0].seq
	a.settleShaping(first)
	if len(a.waits) != 1 {
		t.Fatalf("one settle left %d waits, want 1", len(a.waits))
	}
	if a.waits[0].brief != "fix the nil-map crash" {
		t.Fatalf("the settle collapsed the wrong wait: %q", a.waits[0].brief)
	}
	// AND THE BLOCK IS THE SINGULAR ONE AGAIN, because there is one of them.
	if body := plainRowsText(a.preflightRows(72)); strings.Contains(body, formingHeadWord) {
		t.Fatalf("a block of one still counts:\n%s", body)
	}
}

// ── 5. the phone, and every width ───────────────────────────────────────────

// NOTHING OVERFLOWS AND NOTHING WRAPS ONTO A ROW OF ITS OWN, at any width the
// surface is drawn at — which is the tail's promise stated in cells.
func TestTheFormingPreviewIsCutToEveryWidth(t *testing.T) {
	for _, width := range []int{phoneWidth, 30, 40, 60, 100} {
		a := shapingApp(t, "write the release notes", "fix the nil-map crash")
		shaping(a, 1, strings.Repeat("a brief with no line breaks in it at all, going on and on. ", 20))
		for _, open := range []bool{false, true} {
			a.waits[1].open = open
			rows := a.preflightRows(width)
			want := 1 + 2 + 1
			if open {
				want = 1 + 2 + formingWindowLines
			}
			if len(rows) != want {
				t.Fatalf("width %d open=%v drew %d rows, want %d:\n%s",
					width, open, len(rows), want, plainRowsText(rows))
			}
			for i, r := range rows {
				line := plain(r.text)
				if got := ansi.StringWidth(line); got > width {
					t.Fatalf("width %d open=%v row %d is %d cells: %q", width, open, i, got, line)
				}
				if !strings.HasPrefix(line, formingRail) {
					t.Fatalf("width %d row %d lost the hairline: %q", width, i, line)
				}
			}
		}
	}
}

// ── 6. one scanner, for both streams ────────────────────────────────────────

// THE PREVIEW GOES THROUGH THE SCANNER A FORMING TOOL CALL'S ARGUMENTS GO
// THROUGH, and that is the whole reason there is no second parser here to drift
// from the first: both are prefixes of a JSON object nothing may unmarshal.
func TestTheShapingPreviewReadsThePartialAnswerThroughTheOneScanner(t *testing.T) {
	partial := `{"title":"release notes","brief":"Write the notes for v2.4.\nSay what changed`
	got := shapingPreview(partial)
	want, _ := session.PartialString(partial, shapingPreviewField)
	if got != want {
		t.Fatalf("the surface reads the stream its own way: %q vs %q", got, want)
	}
	if !strings.Contains(got, "Say what changed") {
		t.Fatalf("the preview lost the newest words: %q", got)
	}
	// AND NOTHING BEFORE THE FIELD IS SHOWN. A title that has landed is not the
	// brief, and the raw JSON around it is not anybody's reading.
	if strings.Contains(got, "title") || strings.Contains(got, "{") {
		t.Fatalf("the raw answer reached the row: %q", got)
	}
	// A FIELD THAT HAS NOT OPENED YET IS NOTHING AT ALL, rather than a guess.
	if early := shapingPreview(`{"title":"release`); early != "" {
		t.Fatalf("the preview invented a brief: %q", early)
	}
}
