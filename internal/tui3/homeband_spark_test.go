package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// handsLab is a machine with `working` conversations on it, and the app looking
// at it with its reading already taken.
func handsLab(t *testing.T, working int) (*app, time.Time) {
	t.Helper()
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the newest chat", "/tmp/alpha", now)
	for i := 0; i < working; i++ {
		id := "bbbb00000000000" + itoa(i+1)
		lab.session("-tmp-beta", id, "runner "+itoa(i+1), "/tmp/beta", now.Add(-time.Duration(i)*time.Minute))
		lab.presence("-tmp-beta", id, session.PresenceWorking, "", now)
	}
	a := lab.app(mine)
	a.clock = func() time.Time { return now }
	a.openHome()
	return a, now
}

// THE PULSE COUNTS THE MACHINE'S HANDS, AND SAYS NOTHING AT ZERO.
//
// `N working` is the one place the FIGURE is said — the chart under it is the
// shape and prints no number — and it obeys the emptiness law at the bottom and
// nowhere else: one hand out is worth knowing from across a room, so the segment
// draws at one and vanishes at nothing.
func TestThePulseSaysHowManyHandsAreWorkingAndIsAbsentAtZero(t *testing.T) {
	a, now := handsLab(t, 3)
	line := plain(a.pulseLine(90, a.pal))
	if !strings.Contains(line, "3"+pulseWorkingWord) {
		t.Fatalf("the pulse does not count the machine's hands: %q", line)
	}
	if facts := a.machineFactsAt(now); facts.hands != 3 {
		t.Fatalf("the reading says %d hands, want 3", facts.hands)
	}
	// THE FIGURE AND THE ZONE ARE ONE ACCOUNTING. The moving strip on the left of
	// the same screen gathers the same things; a top line that counted them a
	// second way would be the screen arguing with itself.
	if names := zoneNames(a, attentionMovingWord); len(names) != 3 {
		t.Fatalf("the moving zone reads %v, and the pulse says 3 working", names)
	}

	// AND ONE HAND STILL SPEAKS.
	one, _ := handsLab(t, 1)
	if line := plain(one.pulseLine(90, one.pal)); !strings.Contains(line, "1"+pulseWorkingWord) {
		t.Fatalf("a machine with one hand out says nothing about it: %q", line)
	}

	// A MACHINE WITH NOTHING OUT SAYS NOTHING AT ALL — never `0 working`.
	quiet, still := handsLab(t, 0)
	for _, segment := range quiet.pulseSegments(still, quiet.pal) {
		if strings.Contains(plain(segment), strings.TrimSpace(pulseWorkingWord)) {
			t.Fatalf("a quiet machine's pulse still counts its hands: %q", plain(segment))
		}
	}
}

// THE PAYLOAD RULE, ON THE ONE LINE THAT IS ALL PAYLOAD: the count steps up into
// the datum hue and the word beside it stays dim. Never the accent — the budget
// is one lit element per screen and a top line is a glance rather than the live
// or chosen thing.
func TestTheHandsCountIsTheDatumAndTheWordIsDim(t *testing.T) {
	a, now := handsLab(t, 2)
	segments := a.pulseSegments(now, a.pal)
	found := ""
	for _, segment := range segments {
		if strings.Contains(plain(segment), pulseWorkingWord) {
			found = segment
		}
	}
	if found == "" {
		t.Fatalf("the pulse has no hands segment on it: %q", segments)
	}
	if !strings.HasPrefix(found, a.pal.data("2")) {
		t.Fatalf("the count is not the datum hue: %q", plain(found))
	}
	if !strings.Contains(found, a.pal.dim(pulseWorkingWord)) {
		t.Fatalf("the word beside the count is not dim: %q", plain(found))
	}
	if accent := paintPrefix(a.pal.accent("x")); strings.Contains(found, accent) {
		t.Fatalf("the hands segment spends the accent: %q", plain(found))
	}
}

// AN EMPTY CHART IS FURNITURE. A window that has been zero from end to end draws
// no band at all — not a heading over a flat line along the floor — and neither
// does a machine whose ring has not been filled in yet.
func TestTheHandsSparkIsAbsentUntilSomethingHasRun(t *testing.T) {
	a, now := handsLab(t, 0)
	ctx := machineBandContext(a, now, 36)
	if rows := drawHandsBand(a, ctx); len(rows) != 0 {
		t.Fatalf("a machine that has done nothing drew a chart: %q", rows)
	}
	a.handsRing = make([]int, handsRingSize)
	if rows := drawHandsBand(a, ctx); len(rows) != 0 {
		t.Fatalf("a window of nothing at all drew a chart: %q", rows)
	}
	// AND IT IS NOT ON THE CARD EITHER, which is the claim a person can see.
	if card := machineCardText(a, 40); strings.Contains(card, machineHandsWord) {
		t.Fatalf("the machine's card carries an empty chart:\n%s", card)
	}
}

// ONE THING RUNNING BRINGS THE BAND ONTO THE CARD, heading and two rows of
// braille, inside its width at every width a card is drawn at.
func TestTheHandsSparkAppearsWhenTheMachineHasHandsOut(t *testing.T) {
	a, now := handsLab(t, 2)
	for _, width := range []int{machineHandsFloor, 36, 60} {
		rows := drawHandsBand(a, machineBandContext(a, now, width))
		if len(rows) != machineHandsRows+1 {
			t.Fatalf("at %d cells the band drew %d rows, want %d: %q",
				width, len(rows), machineHandsRows+1, rows)
		}
		if got := plain(rows[0]); !strings.HasPrefix(got, machineHandsWord) {
			t.Fatalf("the band did not lead with its heading: %q", got)
		}
		for _, row := range rows {
			if ansi.StringWidth(row) > width {
				t.Fatalf("a chart row is %d cells at %d: %q", ansi.StringWidth(row), width, plain(row))
			}
		}
		// THE CHART PRINTS NO NUMBER. The figure is the pulse's and is said once.
		for _, row := range rows[1:] {
			if strings.ContainsAny(plain(row), "0123456789") {
				t.Fatalf("the chart labelled itself: %q", plain(row))
			}
		}
	}
	if card := machineCardText(a, 40); !strings.Contains(card, machineHandsWord) {
		t.Fatalf("the band is not on the machine's card:\n%s", card)
	}
}

// A QUIET MACHINE'S LINE LIES FLAT ALONG THE FLOOR AND DOES NOT MOVE. Nothing in
// this band animates on its own account: the same samples drawn twice are the
// same cells twice, byte for byte.
func TestTheHandsSparkIsFlatAndStillWhileTheMachineIsQuiet(t *testing.T) {
	a, now := handsLab(t, 0)
	ctx := machineBandContext(a, now, 30)
	// PRIME THE READING FIRST, so the draw below answers from the cache and the
	// ring this test authored is the ring that gets drawn.
	a.machineFactsAt(now)
	a.handsRing = append([]int{4, 3, 2, 1}, make([]int, 20)...)

	rows := drawHandsBand(a, ctx)
	if len(rows) != machineHandsRows+1 {
		t.Fatalf("the band drew %q", rows)
	}
	floor := plain(rows[len(rows)-1])
	// THE LAST STRETCH IS THE ZEROS, AND THEY ARE ALL ON THE BOTTOM DOT ROW.
	if !strings.HasSuffix(floor, strings.Repeat("⣀", 8)) {
		t.Fatalf("a quiet stretch does not lie flat along the floor: %q", floor)
	}
	if again := drawHandsBand(a, ctx); strings.Join(again, "\n") != strings.Join(rows, "\n") {
		t.Fatalf("the chart moved with the samples standing still:\n%q\n%q", rows, again)
	}
}

// AND IT MOVES BECAUSE THE DATA MOVES. A different window is a different shape;
// there is no clock in the band and nothing to wind.
func TestTheHandsSparkChangesWhenTheSamplesChange(t *testing.T) {
	a, now := handsLab(t, 0)
	ctx := machineBandContext(a, now, 30)
	a.machineFactsAt(now)

	a.handsRing = []int{1, 2, 3, 4, 5, 6, 7, 8}
	up := strings.Join(drawHandsBand(a, ctx), "\n")
	a.handsRing = []int{8, 7, 6, 5, 4, 3, 2, 1}
	down := strings.Join(drawHandsBand(a, ctx), "\n")
	if up == down {
		t.Fatalf("a climb and a fall drew the same chart:\n%s", plain(up))
	}
	// NEWEST AT THE RIGHT: a climb ends at its peak, so the last cell of its TOP
	// row carries dots and the last cell of a fall's top row is empty while its
	// bottom one is not. (Row nought of each band is the heading.)
	climb, fall := strings.Split(up, "\n"), strings.Split(down, "\n")
	if lastCell(plain(climb[1])) == brailleBase {
		t.Fatalf("the newest sample of a climb is not at the top right: %q", plain(climb[1]))
	}
	if lastCell(plain(fall[1])) != brailleBase {
		t.Fatalf("a fall ends high: %q", plain(fall[1]))
	}
	if lastCell(plain(fall[2])) == brailleBase {
		t.Fatalf("the newest sample of a fall is not at the bottom right: %q", plain(fall[2]))
	}
}

// A CHART NEEDS ITS WIDTH. At the list tier home is the list and nothing else,
// and the fact a person keeps is the pulse's `N working`.
func TestTheHandsSparkNeverDrawsAtTheListTier(t *testing.T) {
	a, now := handsLab(t, 3)
	a.machineFactsAt(now)
	a.handsRing = []int{1, 2, 3, 2, 1, 2, 3}

	a.home.tier = homeTierList
	if rows := drawHandsBand(a, machineBandContext(a, now, 36)); len(rows) != 0 {
		t.Fatalf("the chart drew at the list tier: %q", rows)
	}
	// AND THE COUNT STILL SHOWS, which is what the narrow frame keeps.
	if line := plain(a.pulseLine(70, a.pal)); !strings.Contains(line, "3"+pulseWorkingWord) {
		t.Fatalf("a narrow home lost the hands count too: %q", line)
	}
	a.home.tier = homeTierCard
	if rows := drawHandsBand(a, machineBandContext(a, now, machineHandsFloor-1)); len(rows) != 0 {
		t.Fatalf("the chart drew under its own floor: %q", rows)
	}
	// AND A SHAPE IS NOT DRAWN WHERE A SHAPE CANNOT BE READ.
	a.linear = true
	if rows := drawHandsBand(a, machineBandContext(a, now, 36)); len(rows) != 0 {
		t.Fatalf("a surface being read aloud drew braille: %q", rows)
	}
	a.linear = false
	a.pal.ascii = true
	if rows := drawHandsBand(a, machineBandContext(a, now, 36)); len(rows) != 0 {
		t.Fatalf("a terminal without box drawing drew braille: %q", rows)
	}
}

// THE CHART IS A MOVING THING AND WEARS THE HUE THIS SURFACE PAINTS MOVING
// THINGS IN — and the card at rest still spends no accent with the chart on it.
func TestTheHandsSparkWearsTheMutedHueAndNoOther(t *testing.T) {
	a, now := handsLab(t, 2)
	a.machineFactsAt(now)
	a.handsRing = []int{1, 2, 3, 2, 1}
	rows := drawHandsBand(a, machineBandContext(a, now, 36))
	if len(rows) != machineHandsRows+1 {
		t.Fatalf("the band drew %q", rows)
	}
	for _, row := range rows {
		if !strings.HasPrefix(row, paintPrefix(a.pal.muted("x"))) {
			t.Fatalf("a band row is not in the muted tier: %q", row)
		}
	}
	card := strings.Join(a.machineCard(40, 40, a.pal), "\n")
	for word, paint := range map[string]func(string) string{
		"accent": a.pal.accent, "the question hue": a.pal.ask,
		"the failure hue": a.pal.bad, "the landed hue": a.pal.add,
	} {
		if prefix := paintPrefix(paint("x")); prefix != "" && strings.Contains(card, prefix) {
			t.Fatalf("the machine's card with a chart on it wears %s:\n%s", word, plain(card))
		}
	}
}

// THE GEOGRAPHY IS STABLE, AND THE CHART DID NOT TAKE `today`'S PLACE:
// keeping an eye on · hands · since you left · today.
func TestTheMachineCardKeepsItsBandOrderWithTheChartOnIt(t *testing.T) {
	want := []string{"watchlist", "hands", "sinceleft", "today"}
	var got []string
	for _, band := range homeBandsFor(bandKindMachine) {
		got = append(got, band.name)
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("the machine card reads %v, want %v", got, want)
	}
}

// THE RING IS SAMPLED ON HOME'S OWN BEAT — one reading per reading of the
// machine, and never one per paint.
func TestTheHandsRingSamplesOnceForEachReadingOfTheMachine(t *testing.T) {
	a, now := handsLab(t, 2)
	if len(a.handsRing) != 0 {
		t.Fatalf("the ring was filled before anything read the machine: %v", a.handsRing)
	}
	for i := 0; i < 5; i++ {
		a.machineFactsAt(now)
		a.pulseLine(90, a.pal)
		a.machineCard(40, 40, a.pal)
	}
	if len(a.handsRing) != 1 {
		t.Fatalf("one reading of the machine left %d samples: %v", len(a.handsRing), a.handsRing)
	}
	if a.handsRing[0] != 2 {
		t.Fatalf("the sample says %d hands, want 2", a.handsRing[0])
	}
	// A RESCAN IS THE NEXT BEAT, AND THE NEXT SAMPLE.
	a.refreshHome()
	a.machineFactsAt(a.home.world.Read)
	if len(a.handsRing) != 2 {
		t.Fatalf("a rescan did not take a sample: %v", a.handsRing)
	}
	// AND THE RING SURVIVES NOTHING BUT THIS PROCESS: it is not written anywhere,
	// so a ring longer than the window it keeps simply loses its oldest end.
	a.handsRing = make([]int, handsRingSize)
	a.sampleHands(9)
	if len(a.handsRing) != handsRingSize || a.handsRing[handsRingSize-1] != 9 {
		t.Fatalf("the ring is %d long and ends %d", len(a.handsRing), a.handsRing[len(a.handsRing)-1])
	}
}

// ── the machinery underneath ────────────────────────────────────────────────

// ONE QUANTIZER, TWO ALPHABETS. The bar spark and the braille chart round the
// same way, because two roundings would be two answers about one reading.
func TestTheSparkQuantizerFloorsAtNothingAndCapsAtTheTop(t *testing.T) {
	for _, c := range []struct{ reading, ceiling, steps, want int }{
		{0, 8, 8, 0}, {1, 8, 8, 1}, {7, 8, 8, 7}, {8, 8, 8, 7}, {80, 8, 8, 7},
		{-4, 8, 8, 0}, {4, 0, 8, 0}, {4, 8, 0, 0},
	} {
		if got := sparkLevel(c.reading, c.ceiling, c.steps); got != c.want {
			t.Fatalf("sparkLevel(%d, %d, %d) = %d, want %d",
				c.reading, c.ceiling, c.steps, got, c.want)
		}
	}
}

// TWO SAMPLES TO A CELL, NEWEST AT THE RIGHT, AND A WINDOW LONGER THAN THE ROOM
// KEEPS ITS TAIL.
func TestTheBrailleSparkPacksTwoSamplesToACell(t *testing.T) {
	if rows := brailleSpark([]int{0, 0, 0, 0}, 2, 10); rows != nil {
		t.Fatalf("a window with no height in it drew %q", rows)
	}
	if rows := brailleSpark(nil, 2, 10); rows != nil {
		t.Fatalf("no readings at all drew %q", rows)
	}
	rows := brailleSpark([]int{1, 2, 3, 4, 5, 6}, 2, 10)
	if len(rows) != 2 {
		t.Fatalf("a two-row chart drew %d rows", len(rows))
	}
	for _, row := range rows {
		if ansi.StringWidth(row) != 3 {
			t.Fatalf("six samples drew %d cells: %q", ansi.StringWidth(row), row)
		}
	}
	// A LONGER WINDOW THAN THE ROOM IS CLIPPED FROM THE OLD END.
	long := make([]int, 100)
	for i := range long {
		long[i] = i + 1
	}
	wide := brailleSpark(long, 2, 10)
	for _, row := range wide {
		if ansi.StringWidth(row) != 10 {
			t.Fatalf("a hundred samples in ten cells drew %d: %q", ansi.StringWidth(row), row)
		}
	}
	// AND AN ODD COUNT STILL ENDS AT THE RIGHT EDGE: the blank dot column goes
	// at the front, so the newest reading is the last dot drawn.
	odd := brailleSpark([]int{1, 9, 9}, 1, 10)
	if len(odd) != 1 || ansi.StringWidth(odd[0]) != 2 {
		t.Fatalf("three samples drew %q", odd)
	}
	// `⢀` is the LEFT cell holding one dot in its RIGHT column, which is the
	// blank dot column standing at the front; `⠉` is the newest two samples both
	// at the top of the last cell. Drawn without the lead the first cell would
	// read `⡀` and the newest sample would sit a dot short of the edge.
	if odd[0] != "⢀⠉" {
		t.Fatalf("an odd count does not end at the right edge: %q", odd[0])
	}
}

// lastCell is the final braille cell of a row.
func lastCell(row string) rune {
	cells := []rune(row)
	if len(cells) == 0 {
		return 0
	}
	return cells[len(cells)-1]
}
