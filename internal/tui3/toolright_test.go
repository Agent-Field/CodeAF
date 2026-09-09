package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE TOOL ROW'S RIGHT COLUMN (toolview.go).
//
// One claim, and every test here is a corner of it: the figures a person reads
// off a tool call — is it turning, how long has it been turning, what did it
// come to — are AT THE FRAME'S EDGE, whole, at every width, and the row that
// carries them measures exactly the width it was given.
//
// The defect these were written against: every tool row is drawn two columns in
// (workfold.go's INDENT LAW) and the line was laid out to the frame's WHOLE
// width, so it overhung by two and [app.railJoin] cut it back with an ellipsis.
// A running call's `0.4s ⠋` became `0…` — a spinner that never turned, a clock
// that said nothing, and an emptiness-law violation at the edge of the screen.

// runningFetch is a surface mid-turn with one web_fetch call in flight, at a
// stated width, aged so it has a clock to draw.
func runningFetch(t *testing.T, width int, url string, age time.Duration) *app {
	t.Helper()
	a := newTestApp(&fakeAgent{model: "m"})
	a.width = width
	a.pal = newPalette(tokens.ANSI256, false)
	typeLine(t, a, "read me these")
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{
		Kind: session.EventToolBegin, Tool: "web_fetch",
		Hint: "web_fetch " + url,
		Args: `{"url":"` + url + `"}`,
	}})
	// This fixture measures the detailed row, inside the opened work.
	showLiveWork(t, a)
	a.entries[firstTool(t, a)].began = a.now().Add(-age)
	a.touch()
	return a
}

// toolRowsOf is every tool line on the frame, plain.
func toolRowsOf(a *app, width int) []string {
	var out []string
	for _, r := range a.visible(width) {
		if r.hit == hitTool || r.hit == hitNone && r.entry >= 0 {
			line := strings.TrimLeft(plain(r.text), " ")
			if strings.HasPrefix(line, railMid) || strings.HasPrefix(line, railLast) {
				out = append(out, plain(r.text))
			}
		}
	}
	return out
}

const longFetchURL = "https://www.reuters.com/world/us/us-treasury-double-sizes-some-debt-buybacks-2026-08-20/"

// ── 1. the row measures what it was given ───────────────────────────────────

// NO ROW IS EVER WIDER THAN THE FRAME IT IS DRAWN IN. This is the whole defect,
// stated as arithmetic: two cells of overhang is what [app.railJoin] turns into
// an ellipsis over the right column.
func TestAToolRowNeverOverhangsItsFrame(t *testing.T) {
	for _, width := range []int{80, 120, 200} {
		a := runningFetch(t, width, longFetchURL, 4*time.Second)
		for _, r := range a.visible(width) {
			if got := ansi.StringWidth(plain(r.text)); got > width {
				t.Fatalf("at %d columns a row measured %d:\n%q", width, got, plain(r.text))
			}
		}
	}
}

// ── 2. what the column says while the call runs ─────────────────────────────

// A RUNNING CALL CARRIES ITS SPINNER AND ITS OWN AGE, at the frame's edge and
// whole. The spinner is the claim that something is turning and the clock is how
// long it has been turning; a row with only one of them is a row that cannot
// tell a two-second fetch from a two-minute one.
//
// The age is spelled the way a duration you are LIVING THROUGH is spelled —
// whole seconds ([countUpWord]) — and not the way a finished call's is.
func TestARunningRowEndsWithItsSpinnerAndItsAge(t *testing.T) {
	for _, width := range []int{80, 120, 200} {
		a := runningFetch(t, width, longFetchURL, 4*time.Second)
		rows := toolRowsOf(a, width)
		if len(rows) != 1 {
			t.Fatalf("at %d columns the turn drew %d tool rows, want 1", width, len(rows))
		}
		want := tokens.Spinner(a.paints/spinnerStep) + " 4s"
		if !strings.HasSuffix(rows[0], want) {
			t.Fatalf("at %d columns the row is\n\t%q\nwant it to end %q", width, rows[0], want)
		}
		// AND NOTHING OF THE OLD DEFECT SURVIVES. `0…` was the clock's first
		// character and an ellipsis where the rest of the column should have
		// been, and a bare `…` at the edge was the same cut one cell earlier.
		if strings.Contains(rows[0], "0…") || strings.HasSuffix(rows[0], " …") {
			t.Fatalf("at %d columns the right column was clipped: %q", width, rows[0])
		}
	}
}

// THE SPINNER TURNS. It is the one animation on a tool line, and a still frame
// on a row that claims work is happening is the defect this column was cut out
// of in the first place.
func TestTheSpinnerOnAToolRowAdvancesBetweenPaints(t *testing.T) {
	a := runningFetch(t, 200, longFetchURL, 4*time.Second)

	a.paints = 0
	a.touch()
	first := toolRowsOf(a, 200)[0]

	a.paints = spinnerStep
	a.touch()
	second := toolRowsOf(a, 200)[0]

	if first == second {
		t.Fatalf("the spinner did not turn across two paints: %q at both", first)
	}
	if !strings.HasSuffix(first, " 4s") || !strings.HasSuffix(second, " 4s") {
		t.Fatalf("the clock moved with the spinner:\n\t%q\n\t%q", first, second)
	}
}

// ── 3. what the column says once the call is done ───────────────────────────

// A FINISHED CALL CARRIES WHAT IT CAME TO AND HOW LONG IT TOOK, in that order,
// dotted, at the frame's edge — and no success glyph, ever.
func TestAFinishedRowEndsWithItsSizeAndItsDuration(t *testing.T) {
	// 12698 bytes is "12.4 KB" in [byteWord]'s own spelling, which is the
	// spelling the row watched the same page arrive in.
	page := strings.Repeat("a", 12698)
	a := toolAppAt(t, 200, call("web_fetch", `{"url":"`+longFetchURL+`"}`, page))
	for i := range a.entries {
		if a.entries[i].kind == entryTool {
			a.entries[i].ran = 800 * time.Millisecond
		}
	}
	a.touch()

	rows := toolRowsOf(a, 200)
	if len(rows) != 1 {
		t.Fatalf("the turn drew %d tool rows, want 1", len(rows))
	}
	if !strings.HasSuffix(rows[0], "12.4 KB · 0.8s") {
		t.Fatalf("the finished row is\n\t%q\nwant it to end \"12.4 KB · 0.8s\"", rows[0])
	}
	if strings.ContainsAny(rows[0], "✓✔") {
		t.Fatalf("a finished row drew a success glyph: %q", rows[0])
	}
}

// A CALL WHOSE PAGE WAS EMPTY DRAWS NO SIZE. The emptiness law: unknown or zero
// renders as nothing, never "0 B".
func TestAFetchThatCameBackEmptyDrawsNoSize(t *testing.T) {
	a := toolAppAt(t, 200, call("web_fetch", `{"url":"`+longFetchURL+`"}`, ""))
	for i := range a.entries {
		if a.entries[i].kind == entryTool {
			a.entries[i].ran = 800 * time.Millisecond
		}
	}
	a.touch()

	rows := toolRowsOf(a, 200)
	if strings.Contains(rows[0], "0 B") {
		t.Fatalf("an empty page drew a size: %q", rows[0])
	}
	if !strings.HasSuffix(rows[0], "0.8s") {
		t.Fatalf("the duration went with the size it was beside: %q", rows[0])
	}
}

// ── 4. the target ───────────────────────────────────────────────────────────

// A URL IS CUT IN THE MIDDLE, and the two ends a person reads it by survive: the
// host says whose page this is and the tail says which page. Three fetches of
// one news site cut at the END are three identical rows.
func TestALongURLKeepsItsHostAndItsTail(t *testing.T) {
	a := runningFetch(t, 80, longFetchURL, 4*time.Second)
	row := toolRowsOf(a, 80)[0]

	if !strings.Contains(row, "https://www.reuters.com") {
		t.Fatalf("the cut took the host: %q", row)
	}
	if !strings.Contains(row, "2026-08-20/") {
		t.Fatalf("the cut took the tail: %q", row)
	}
	if !strings.Contains(row, glyphMore) {
		t.Fatalf("a URL too long for the row was not cut at all: %q", row)
	}
}

// THE WHOLE URL IS ON A WIDE FRAME. It comes off the call's own arguments and
// not off session's hint, which is clipped to eighty bytes for a log column —
// the reason a two-hundred column frame used to draw a URL cut at seventy cells
// with a hundred and thirty cells of blank after it (toolstat.go's targetField).
func TestAWideFrameDrawsTheWholeURL(t *testing.T) {
	a := runningFetch(t, 200, longFetchURL, 4*time.Second)
	if row := toolRowsOf(a, 200)[0]; !strings.Contains(row, longFetchURL) {
		t.Fatalf("the whole URL is not on a two-hundred column row: %q", row)
	}
}

// A COMMAND IS CUT AT THE END. It is read left to right and its first words are
// what it does; a middle cut would hide the verb and keep the argument.
func TestALongCommandIsCutAtItsEnd(t *testing.T) {
	const command = "go test ./internal/tui3/ ./internal/session/ ./internal/manual/ -run TestSomethingWithAVeryLongName"
	a := toolAppAt(t, 80, call("bash", `{"command":"`+command+`"}`, "ok"))
	row := toolRowsOf(a, 80)[0]

	if !strings.Contains(row, "bash go test ./internal/tui3/") {
		t.Fatalf("the command lost its head: %q", row)
	}
	if !strings.HasSuffix(strings.TrimRight(row, " "), glyphMore) {
		t.Fatalf("the command was not cut at its end: %q", row)
	}
}

// ── 5. the drop order ───────────────────────────────────────────────────────

// THE COLUMN GIVES UP WHOLE SEGMENTS, IN ORDER: the size first, then the
// duration, and the mark is never given up while there is room for it. Half a
// figure is worse than no figure — "12.4 K" is a number a person has to
// distrust — and below the room for the mark alone the column is absent
// entirely rather than drawn as a stub.
//
// It is asserted against the budget rather than against a frame because the
// order is the LAW and a width is only one sample of it.
func TestTheRightColumnDropsWholeSegmentsInOrder(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.pal = newPalette(tokens.ANSI256, false)

	done := &entry{
		kind: entryTool, tool: "read", status: toolOK, ran: 800 * time.Millisecond,
		detail: toolDetail{Args: `{"path":"a.go"}`, Output: "one\ntwo\nthree"},
	}
	for _, tc := range []struct {
		budget int
		want   string
	}{
		{budget: 40, want: "3 lines · 0.8s"},
		{budget: 14, want: "3 lines · 0.8s"},
		{budget: 13, want: "0.8s"}, // the size goes first, whole
		{budget: 4, want: "0.8s"},
		{budget: 3, want: ""}, // and then the duration, whole
		{budget: 0, want: ""},
	} {
		text, cells := a.toolTail(done, tc.budget)
		if plain(text) != tc.want || cells != ansi.StringWidth(tc.want) {
			t.Fatalf("a finished call at a budget of %d drew %q (%d cells), want %q",
				tc.budget, plain(text), cells, tc.want)
		}
	}

	// AND THE MARK OUTLIVES BOTH. A running row keeps its spinner down to the
	// last cell, because a spinner is the one thing on the row that cannot be
	// inferred from anything else on it.
	a.state = stateWorking
	// A call with NO bound, so the clock is an age and nothing else: a bounded
	// one states what is left of the bound beside it, which is a sentence about
	// the countdown rather than about this order (see [app.countClock]).
	live := &entry{
		kind: entryTool, tool: "read", status: toolRunning,
		began:  a.now().Add(-4 * time.Second),
		detail: toolDetail{Args: `{"path":"a.go"}`},
	}
	spinner := tokens.Spinner(a.paints / spinnerStep)
	for _, tc := range []struct {
		budget int
		want   string
	}{
		{budget: 40, want: spinner + " 4s"},
		{budget: 4, want: spinner + " 4s"},
		{budget: 3, want: spinner},
		{budget: 1, want: spinner},
		{budget: 0, want: ""},
	} {
		text, cells := a.toolTail(live, tc.budget)
		if plain(text) != tc.want || cells != ansi.StringWidth(tc.want) {
			t.Fatalf("a running call at a budget of %d drew %q (%d cells), want %q",
				tc.budget, plain(text), cells, tc.want)
		}
	}
}

// AND NO WIDTH BETWEEN THE TIERS CLIPS A FIGURE. The sweep is the frame's own
// answer to the law above: every narrow row still adds up, and none of them ends
// on half a number.
func TestNoNarrowRowEverClipsAFigure(t *testing.T) {
	const path = "internal/session/transport/streaming/loop.go"
	for width := 120; width >= 62; width -= 2 {
		a := toolAppAt(t, width, call("read", `{"path":"`+path+`"}`, "one\ntwo\nthree"))
		for i := range a.entries {
			if a.entries[i].kind == entryTool {
				a.entries[i].ran = 800 * time.Millisecond
			}
		}
		a.touch()
		row := toolRowsOf(a, width)[0]

		if !strings.HasSuffix(row, "0.8s") {
			t.Fatalf("at %d columns the column shed the duration before the size: %q", width, row)
		}
		if strings.Contains(row, "0…") || strings.HasSuffix(row, " …") {
			t.Fatalf("at %d columns a figure was clipped: %q", width, row)
		}
		if got := ansi.StringWidth(row); got > width {
			t.Fatalf("at %d columns the row measured %d: %q", width, got, row)
		}
	}
}

// ── 6. the width on screen is the width laid out ────────────────────────────

// THE ROW IS BUILT AT THE WIDTH IT IS DRAWN AT, and the closed task column
// changes that width. A layout that used the terminal's width instead would put
// the right column under the rail's edge — which is [app.railJoin]'s cue to cut
// it off, and the defect in one sentence.
func TestTheRowFollowsTheWidthTheFrameActuallyHas(t *testing.T) {
	const full = 200
	open := runningFetch(t, full, longFetchURL, 4*time.Second)

	stowed := runningFetch(t, full, longFetchURL, 4*time.Second)
	stowed.railAway = true
	stowed.touch()
	if !stowed.railStowed() {
		t.Fatal("the column did not stow")
	}
	if stowed.bodyWidth() != full-railGripCols {
		t.Fatalf("the closed edge charges %d columns, want %d", full-stowed.bodyWidth(), railGripCols)
	}
	if open.bodyWidth() == stowed.bodyWidth() {
		t.Fatalf("closing the column gave the conversation nothing: both bodies are %d", open.bodyWidth())
	}

	// Both frames end the tool row with the whole column, and neither ends it
	// with the cut the closed edge used to make.
	for name, a := range map[string]*app{"open": open, "stowed": stowed} {
		row := toolRowsOf(a, a.bodyWidth())[0]
		// The row is the body's whole width — its two cells of indent and the
		// line laid out inside them — and not a cell more.
		if got := ansi.StringWidth(row); got != a.bodyWidth() {
			t.Fatalf("%s: the row measured %d in a body of %d", name, got, a.bodyWidth())
		}
		if lead := workIndentCols(a.bodyWidth()); !strings.HasPrefix(row, strings.Repeat(" ", lead)) {
			t.Fatalf("%s: the row lost the indent it is laid out for: %q", name, row)
		}
		if !strings.HasSuffix(row, " 4s") {
			t.Fatalf("%s: the row lost its clock: %q", name, row)
		}
		if strings.Contains(plain(frame(a)), "0…") {
			t.Fatalf("%s: the frame still carries the clipped column", name)
		}
	}
}
