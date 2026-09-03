package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ── THE ROW AT EVERY WIDTH ──────────────────────────────────────────────────
//
// rowfit.go's four laws, held to real frames. The rows in these tests are
// PASTED: the fitter is pure, so the same catalog at the same width is the same
// line, and a law about what a narrow terminal shows is only a law if somebody
// can read the answer.

// rowfitCatalog is one model with every fact published — the row the hierarchy
// is actually about, since a row with two facts on it never has to rank them.
var rowfitCatalog = []Model{
	{ID: flash, ContextLength: 1_000_000, PromptPrice: 0.09e-6, CompletionPrice: 0.18e-6, ArenaElo: 1290},
}

// rowfitPicker is that model's picker, open, with the conversation held to
// CoreWeave — a PIN and not the chooser's answer, because the chooser samples
// afresh at every moment and a pasted row cannot be sampled.
func rowfitPicker(t *testing.T) *app {
	t.Helper()
	laneLab(t, threeLanes())
	a := pickerApp(t, &fakeAgent{model: flash}, rowfitCatalog)
	a.profileDir = t.TempDir()
	typeLine(t, a, "/model")
	a.pick.pin = "CoreWeave"
	return a
}

// rowfitRow is the model's own row as the list draws it at width, with the
// band's padding taken back off.
func rowfitRow(a *app, width int) string {
	out := []string{}
	for _, line := range a.pick.rows(width, overlayItemLines(width, "x"), a.pal, -1, a.reasoningFor) {
		out = append(out, strings.TrimRight(plain(line), " "))
	}
	return strings.Join(out, "\n")
}

// THE NAME IS WHOLE AT EVERY WIDTH AND THE FACTS ARE SPENT IN RANK ORDER. Six
// frames, one catalog row, and the whole design readable down the column: the
// machine that will answer outlives the price, the price outlives the window,
// and the id is never touched.
func TestThePickerRowKeepsItsNameAndSpendsFactsInRankOrder(t *testing.T) {
	a := rowfitPicker(t)
	for _, c := range []struct {
		width int
		want  string
	}{
		// UNDER SIXTY THE TAIL TAKES A LINE OF ITS OWN (the two-line law at
		// tierPhone, palette.go) — so the name is whole with the whole frame to
		// itself, and the fitter spends the line under it instead of the
		// remainder of this one.
		{40, "› deepseek/deepseek-v4-flash\n    via coreweave · ▲0.4s · $0.18/M · 1M"},
		// AT SIXTY THE PRICE STEPS ONE RUNG DOWN ITS OWN LADDER — `$0.18/M` to
		// `$0.18` — because [rowGutter] is two cells now and the row is one cell
		// tighter than it was. That is law 2 working: a fact takes the longest
		// spelling that fits, and the spelling it fell back to is one the row
		// authored.
		{60, "› deepseek/deepseek-v4-flash   via coreweave · ▲0.4s · $0.18"},
		{80, "› deepseek/deepseek-v4-flash      via coreweave · ▲0.4s · $0.09/$0.18 per M · 1M"},
		{100, "› deepseek/deepseek-v4-flash       via coreweave · ▲0.4s · $0.09/$0.18 per M · 1M · 24t/s · elo 1290"},
		// AND FROM HERE UP THE ROW STOPS GROWING. Every fact is already said, so
		// the only thing a wider frame could add is blank cells between the name
		// and them — which is what [overlayMeasure] is for. 120 and 160 draw the
		// same hundred-and-two cells and leave the rest of the frame empty.
		{120, "› deepseek/deepseek-v4-flash         via coreweave · ▲0.4s · $0.09/$0.18 per M · 1M · 24t/s · elo 1290"},
		{160, "› deepseek/deepseek-v4-flash         via coreweave · ▲0.4s · $0.09/$0.18 per M · 1M · 24t/s · elo 1290"},
	} {
		if got := rowfitRow(a, c.width); got != c.want {
			t.Fatalf("at %d columns the row is\n got %q\nwant %q", c.width, got, c.want)
		}
		for _, line := range strings.Split(c.want, "\n") {
			if width := ansi.StringWidth(line); width > c.width {
				t.Fatalf("at %d columns a line drew %d cells: %q", c.width, width, line)
			}
		}
	}
}

// AND THE NAME IS NEVER CUT WHILE A FACT IS STILL STANDING. This is law 1 said
// as the thing a person would notice: there is no width at which the row would
// rather show `deepseek/deepseek-v4-fl…` than drop a number.
func TestNoWidthTruncatesTheNameWhileTelemetryRemains(t *testing.T) {
	a := rowfitPicker(t)
	// FROM SIXTY UP, where the name and the facts share one line and law 1 has
	// something to decide. Under it the tail has a line of its own and takes
	// nothing from the name, so a phone that cannot hold a twenty-six-cell id
	// cuts it with nothing gained back.
	for width := 60; width <= 160; width++ {
		row := rowfitRow(a, width)
		if !strings.Contains(row, glyphMore) {
			continue
		}
		if fact := strings.TrimSpace(strings.SplitN(strings.TrimPrefix(row, "  "), "  ", 2)[0]); !strings.Contains(fact, glyphMore) {
			continue
		}
		// The name was cut. Then nothing else may be on the line.
		rest := strings.TrimSpace(row)
		if strings.Contains(rest, rowSep) || strings.Contains(rest, "coreweave") {
			t.Fatalf("at %d columns a cut name still carries facts: %q", width, row)
		}
	}
}

// ── THE NAME, WHEN THE NAME ITSELF WILL NOT FIT ─────────────────────────────

// THE AUTHOR GOES FIRST AND ONLY WHEN IT MUST. At forty-eight cells the whole
// id fits and is drawn whole — the author is not spent to buy a number, which
// is what the old arithmetic did — and it is only under the id's own width that
// `nvidia/` comes off and leaves the name a person was actually looking for.
func TestALongSlugDropsItsAuthorBeforeItIsEverCut(t *testing.T) {
	plan := rowPlan{
		primary: "nvidia/nemotron-3.5-lightning",
		author:  true,
		fields:  []rowField{rowSay("via coreweave", "coreweave"), rowSay("▲0.4s", "0.4s"), rowSay("1M")},
	}
	for _, c := range []struct {
		room        int
		label, note string
	}{
		{46, "nvidia/nemotron-3.5-lightning", "via coreweave"},
		{40, "nvidia/nemotron-3.5-lightning", "coreweave"},
		{28, "nemotron-3.5-lightning", ""},
		{16, "nemotr…lightning", ""},
	} {
		label, note := plan.fit(c.room)
		if label != c.label || note != c.note {
			t.Fatalf("in %d cells the name is %q with %q, want %q with %q",
				c.room, label, note, c.label, c.note)
		}
	}
}

// AND TWO ROWS SHARING A SLUG KEEP THEIR AUTHORS, because there the author is
// the only thing telling them apart — so the cut moves into the middle and the
// head carries the company after all.
func TestTwoModelsSharingASlugKeepTheirAuthors(t *testing.T) {
	shared := sharedSlugs([]Model{{ID: "moonshotai/kimi-k3"}, {ID: "openrouter/kimi-k3"}, {ID: "openai/gpt-4.1-mini"}})
	if !shared["kimi-k3"] || shared["gpt-4.1-mini"] {
		t.Fatalf("the shared slugs are %v", shared)
	}
	for _, id := range []string{"moonshotai/kimi-k3", "openrouter/kimi-k3"} {
		plan := rowPlan{primary: id, author: !shared[rowSlug(id)]}
		label, _ := plan.fit(14)
		if !strings.HasPrefix(label, id[:5]) || !strings.HasSuffix(label, "kimi-k3") {
			t.Fatalf("%q shortened to %q — the author it is told apart by is gone", id, label)
		}
	}
	// The unshared one is free to shed its author, in the same list.
	plan := rowPlan{primary: "openai/gpt-4.1-mini", author: !shared["gpt-4.1-mini"]}
	if label, _ := plan.fit(14); label != "gpt-4.1-mini" {
		t.Fatalf("an unshared slug shortened to %q", label)
	}
}

// ── THE TAIL ────────────────────────────────────────────────────────────────

// LAW 3: THE TAIL IS A PREFIX OF ITSELF. A fact that will not fit ends the
// tail; a cheaper one behind it is not promoted into the gap, because a row
// that skipped the price to show the elo says the price is unpublished.
func TestTheTailIsAPrefixAndNeverSkipsForward(t *testing.T) {
	fields := []rowField{rowSay("via coreweave", "coreweave"), rowSay("$0.09/$0.18 per M", "$0.18/M"), rowSay("1M")}
	for _, c := range []struct {
		room int
		want string
	}{
		{60, "via coreweave · $0.09/$0.18 per M · 1M"},
		{28, "via coreweave · $0.18/M · 1M"},
		{27, "via coreweave · $0.18/M"},
		{22, "via coreweave"},
		{12, "coreweave"},
		{4, ""},
	} {
		if got := rowTail(fields, c.room); got != c.want {
			t.Fatalf("in %d cells the tail is %q, want %q", c.room, got, c.want)
		}
	}
}

// LAW 4 — THE EMPTINESS LAW. A fact nobody published draws nothing, spends
// nothing, and does not end the tail: the window behind an unpublished price
// still gets drawn, and a model nothing is known about draws no separators at
// all rather than a row of gaps.
func TestAnUnknownFactDrawsNothingAndFreesItsSpace(t *testing.T) {
	fields := []rowField{rowSay("coreweave"), rowSay(""), rowSay("1M"), rowField{}, rowSay("elo 1290")}
	if got := rowTail(fields, 60); got != "coreweave · 1M · elo 1290" {
		t.Fatalf("the unknown fields left a hole: %q", got)
	}
	if got := rowAll(modelFields(Model{ID: "vendor/quiet"}, "")); got != "" {
		t.Fatalf("a model nobody published anything about says %q", got)
	}
	if got := rowAll([]rowField{rowField{}, rowField{}}); got != "" {
		t.Fatalf("two unknown fields drew %q", got)
	}
}

// ── THE FOLD'S OWN ROWS ─────────────────────────────────────────────────────

// A LANE ROW AT SIXTY KEEPS THE NAME, THE WAIT AND THE RATE — the three facts
// the fold exists to compare — and gives up the sparkline first.
func TestTheLaneRowsKeepNameFirstTokenAndThroughputAtSixty(t *testing.T) {
	laneLab(t, threeLanes())
	view, ok := laneFor(laneViews(flash, timeNow()), "Cloudflare")
	if !ok {
		t.Fatal("the ledger this test installed believes nothing")
	}
	for _, c := range []struct {
		room int
		want string
	}{
		{54, "0.8s · 58 t/s · $1.3/M · no tools · 100%"},
		{34, "0.8s · 58 t/s · $1.3/M"},
		{30, "0.8s · 58 t/s"},
		{20, "0.8s"},
		{12, ""},
	} {
		label, note := laneRowText(view, c.room)
		if label != "cloudflare" || note != c.want {
			t.Fatalf("in %d cells the lane row is %q / %q, want %q", c.room, label, note, c.want)
		}
	}
}

// AND THE WHOLE FOLD FITS INSIDE A SIXTY-CELL FRAME, drawn by the list itself.
func TestTheOpenFoldDrawsInsideASixtyCellFrame(t *testing.T) {
	a := rowfitPicker(t)
	drive(t, a, key("right"))
	if a.pick.unfold == "" {
		t.Fatal("→ did not open the fold")
	}
	for _, line := range a.pick.rows(60, 10, a.pal, -1, a.reasoningFor) {
		if width := ansi.StringWidth(plain(line)); width > 60 {
			t.Fatalf("a fold row drew %d cells: %q", width, plain(line))
		}
	}
	// The `auto` row's sentence gives way to the name of the machine it would
	// send you to, which is the fact somebody opened the fold to read.
	label, note := a.pick.entryText(1, 40, nil)
	if !strings.Contains(label, "auto") || strings.Contains(note, "picks the fastest") {
		t.Fatalf("at forty cells the auto row reads %q / %q", label, note)
	}
	if !strings.Contains(note, "now") {
		t.Fatalf("the auto row lost the machine it would use: %q", note)
	}
}

// ── THE HINT LINE ───────────────────────────────────────────────────────────

// THE HINT IS ONE STRING SAID TWICE AND THE TEST IS WHAT KEEPS THEM ONE. Below
// sixty the keys go from the right, whole — never a key spelled `es…`.
func TestThePickerHintIsRankedAndNeverHalfAKey(t *testing.T) {
	if got := rowAll(pickerHintFields); got != pickerHint {
		t.Fatalf("the hint's fields join to %q, want %q", got, pickerHint)
	}
	if got := pickerHintAt(58); got != pickerHint {
		t.Fatalf("at sixty columns the box says %q — the whole line was the law", got)
	}
	for _, room := range []int{58, 42, 30, 20, 8} {
		got := pickerHintAt(room)
		if ansi.StringWidth(got) > room {
			t.Fatalf("in %d cells the hint drew %d: %q", room, ansi.StringWidth(got), got)
		}
		if strings.Contains(got, glyphMore) {
			t.Fatalf("in %d cells the hint cut a key: %q", room, got)
		}
		if got != "" && !strings.HasPrefix(pickerHint, got) {
			t.Fatalf("in %d cells the hint is %q, which is not the head of the line", room, got)
		}
	}
}

// ── THE FLOOR UNDER EVERY OTHER LIST ────────────────────────────────────────

// A NOTE MAY TAKE THE ROW'S SECOND HALF AND NO MORE. Lists that hand the row a
// tail they did not budget — settings' `tool exceptions`, ten tools in one
// value — used to squeeze the label to nothing, and a column of full-width
// values with no names in front of them answers "which setting is this" with
// silence.
func TestALongNoteNoLongerPushesTheLabelOffTheRow(t *testing.T) {
	long := "• propose_task:allow, tasks:allow, read:allow, ls:allow, " +
		"services:allow, track:allow, edit:allow, write:allow"
	pal := newPalette(tokens.ANSI256, false)
	for _, width := range []int{24, 44, 60, 80, 120} {
		line := plain(overlayRowTinted("tool exceptions", long, nil, false, markNone, false, width, pal))
		if ansi.StringWidth(line) > width {
			t.Fatalf("at %d the row drew %d cells: %q", width, ansi.StringWidth(line), line)
		}
		if !strings.HasPrefix(strings.TrimSpace(line), "tool exce") {
			t.Fatalf("at %d columns the label was pushed off: %q", width, line)
		}
	}
}

// ── WHAT THE STATUS LINE IS HANDED ──────────────────────────────────────────

// THE SERVED SEGMENT IS THE SAME MECHANISM WITH NO NAME IN FRONT OF IT: the
// machine outranks the clock, and the last cell of the segment belongs to the
// machine's name.
func TestTheServedSegmentDegradesToTheLaneAlone(t *testing.T) {
	fields := servedFields("CoreWeave", "thinking 4s", "0.6s", "61 t/s")
	for _, c := range []struct {
		room int
		want string
	}{
		{60, "via coreweave · thinking 4s · 0.6s · 61 t/s"},
		{34, "via coreweave · thinking 4s · 0.6s"},
		{28, "via coreweave · thinking 4s"},
		{20, "via coreweave"},
		{12, "coreweave"},
		{6, ""},
	} {
		if got := rowTail(fields, c.room); got != c.want {
			t.Fatalf("in %d cells the served segment is %q, want %q", c.room, got, c.want)
		}
	}
	// And with nothing measured yet it is the lane alone, not a row of gaps.
	if got := rowAll(servedFields("CoreWeave", "", "", "")); got != "via coreweave" {
		t.Fatalf("an unmeasured turn says %q", got)
	}
}
