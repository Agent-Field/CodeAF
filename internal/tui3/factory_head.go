package tui3

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/tui2/reltime"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE HANDOVER ────────────────────────────────────────────────────────────
//
// The strip at the top of the factory page's pane column: what happened on the
// floor since the person last looked, what waits on them now, and what the
// floor is made of and has cost today. It is four rows and a blank, and the
// page stacks the pane under it ([app.factoryBody]).
//
//	◆ handover · since 23:12 · 7h 12m · $8.44 ──────────────────────────
//	✓ 2 shipped #1661 #1663 · 3 arrived · 1 question handled
//	? 2 waiting on you                          24h ▁▁▁▃▁▁█▁▁▁▁▂▁▁▁▁▁▁▁▁▁▁▁▁
//	  3 repos · github · chat · benches 2/6 · polled 4m ago   $11.31 / $60 today
//
// THE STRIP READS THE SNAPSHOT AND NOTHING ELSE, as the rest of the page does
// (the framedisk law): the shift, the counts, the sources and the day's money
// all arrive on [factory.Snapshot], copied off the loop.
//
// THE EMPTINESS LAW HOLDS CLAUSE BY CLAUSE. A count of zero draws no clause, a
// spend of zero draws no figure, and a rail with nothing spent against it draws
// only the rail. Two sentences are the deliberate exceptions, because each one
// answers the question its row exists for: a shift in which nothing happened
// says so in one dim sentence rather than leaving a hole a person would read as
// a page that failed to load, and the row that says what waits on the person
// says `nothing waits on you` rather than nothing at all, because "is anything
// mine?" is the one question a person opens this page to ask.

// factoryHeadSparkFloor is the narrowest pane that still draws the day's
// sparkline beside what waits on the person. Under it the sparkline goes and
// the words keep the row.
const factoryHeadSparkFloor = 60

// factoryHeadHours is how many hourly cells the sparkline draws: the whole of
// the day the shift's hours cover, ending at the current hour.
const factoryHeadHours = 24

// factoryMockBeat is how much real time the mock floor's clock takes to move
// by [factory.Snapshot.Speed], so a speed of thirty seconds a beat is a floor
// running at 150×. A real engine has no speed and the strip draws none.
const factoryMockBeat = 200 * time.Millisecond

// The words the strip says when there is nothing to count. They are named so
// the manual and the tests quote the code's own spelling.
const (
	factoryHeadWord        = "handover"
	factoryHeadQuietWords  = "quiet · nothing happened while you were away"
	factoryHeadNoWaitWords = "nothing waits on you"
	factoryHeadHoursWord   = "24h"
)

// factorySparkSteps and factorySparkASCII are the sparkline's two alphabets,
// lowest first. The ASCII one is what a screen reader or a terminal without
// block drawing gets, and it climbs by visual weight the way the blocks do.
var (
	factorySparkSteps = [...]string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}
	factorySparkASCII = [...]string{".", ":", "-", "=", "+", "*", "#", "%"}
)

// factoryHead is the handover strip: four rows and a blank, each exactly width
// cells. It answers nil for a width with no room in it.
func (a *app) factoryHead(width int) []string {
	if width <= 0 {
		return nil
	}
	// THE STRIP STANDS ONE CELL OFF THE RAIL'S SEPARATOR, as the pane under it
	// does ([app.factoryPane]), so the two read as one column.
	snap, inner := a.fp.snap, width-1
	rows := []string{
		a.factoryHeadTitle(snap, inner),
		a.factoryHeadShift(snap, inner),
		a.factoryHeadWaiting(snap, inner),
		a.factoryHeadFloor(snap, inner),
		"",
	}
	for i, row := range rows {
		if inner > 0 {
			row = factoryPad(row, inner)
		} else {
			row = ""
		}
		rows[i] = factoryPad(" "+row, width)
	}
	return rows
}

// factoryHeadTitle is the strip's heading: the manager's mark, the word, when
// the shift began and how long ago, and what it spent, muted, with a hairline
// out to the edge. A shift with no start says no time, and one that spent
// nothing says no money.
func (a *app) factoryHeadTitle(snap factory.Snapshot, width int) string {
	pal := a.pal
	words := []string{factoryHeadWord}
	if since := snap.Shift.Since; !since.IsZero() {
		words = append(words, "since "+since.In(factoryHeadZone(snap)).Format("15:04"))
		if !snap.Now.IsZero() {
			words = append(words, reltime.Elapsed(snap.Now.Sub(since)))
		}
	}
	if snap.Shift.Spent > 0 {
		words = append(words, dollars(snap.Shift.Spent))
	}
	head := fit(a.linearMark("◆", "*")+" "+strings.Join(words, rowSep), width)
	line := pal.muted(head)
	if rest := width - ansi.StringWidth(head) - 1; rest > 0 {
		line += " " + pal.dim(strings.Repeat(a.linearMark("─", "-"), rest))
	}
	return line
}

// factoryHeadZone is the location the shift's start is read in: the
// snapshot's own clock's, so a fixture pinned to UTC reads as UTC and a live
// floor reads as the person's wall clock.
func factoryHeadZone(snap factory.Snapshot) *time.Location {
	if snap.Now.IsZero() {
		return time.Local
	}
	return snap.Now.Location()
}

// factoryHeadShift is what the shift did: what shipped, by name, what arrived
// and how many questions were handled, each clause drawn only when it counted
// something, and the quiet sentence when none did. A narrow pane loses the
// shipped items' names before it loses a clause, and the last clause before
// the first ([rowTail]).
func (a *app) factoryHeadShift(snap factory.Snapshot, width int) string {
	pal := a.pal
	sh := snap.Shift
	var fields []rowField
	// THE TICK LEADS ONLY WHAT SHIPPED. It is the settled mark, and a row
	// that only says what arrived has nothing settled on it to mark.
	lead, mark := "  ", ""
	if sh.Shipped > 0 {
		mark = a.icon(tokens.GSettled)
		lead = pal.add(mark) + " "
		count := itoa(sh.Shipped) + " shipped"
		if len(sh.Shipping) > 0 {
			fields = append(fields, rowSay(count+" "+strings.Join(sh.Shipping, " "), count))
		} else {
			fields = append(fields, rowSay(count))
		}
	}
	if sh.Arrived > 0 {
		fields = append(fields, rowSay(itoa(sh.Arrived)+" arrived"))
	}
	if sh.Handled > 0 {
		fields = append(fields, rowSay(itoa(sh.Handled)+" "+factoryPlural(sh.Handled, "question", "questions")+" handled"))
	}
	if len(fields) == 0 {
		return "  " + pal.dim(fit(factoryHeadQuietWords, width-2))
	}
	leadW := 2
	if mark != "" {
		leadW = ansi.StringWidth(mark) + 1
	}
	return lead + pal.ink(rowTail(fields, width-leadW))
}

// factoryHeadWaiting is how many items wait on the person, in the question's
// hue, or the dim sentence saying none do; and at the right, on a pane wide
// enough, the day's work as a sparkline of the shift's hours ending at the
// current hour. A day with no hours in it draws no sparkline.
func (a *app) factoryHeadWaiting(snap factory.Snapshot, width int) string {
	pal := a.pal
	left := "  " + pal.dim(factoryHeadNoWaitWords)
	leftW := 2 + ansi.StringWidth(factoryHeadNoWaitWords)
	if n := snap.Count(factory.StateNeedsYou); n > 0 {
		words := "? " + itoa(n) + " waiting on you"
		left, leftW = pal.ask(words), ansi.StringWidth(words)
	}
	if width < factoryHeadSparkFloor {
		return fit(left, width)
	}
	spark := a.factoryHeadSpark(snap)
	if spark == "" {
		return fit(left, width)
	}
	right := pal.dim(factoryHeadHoursWord) + " " + spark
	rightW := ansi.StringWidth(factoryHeadHoursWord) + 1 + factoryHeadHours
	if leftW+rowGutter+rightW > width {
		return fit(left, width)
	}
	return left + strings.Repeat(" ", width-leftW-rightW) + right
}

// factoryHeadSpark is the shift's hours as one cell each, oldest first and the
// current hour last, scaled to the busiest hour: muted, with the current hour
// in ink so the line says where now is. An hour with nothing in it draws the
// lowest step, so the line keeps its length; a day with nothing in any hour is
// no sparkline at all.
func (a *app) factoryHeadSpark(snap factory.Snapshot) string {
	hours := snap.Shift.Hours
	peak := 0
	for _, n := range hours {
		peak = max(peak, n)
	}
	if peak <= 0 {
		return ""
	}
	steps := factorySparkSteps
	if a.linear || a.pal.ascii {
		steps = factorySparkASCII
	}
	top := len(steps) - 1
	now := snap.Now.In(factoryHeadZone(snap)).Hour()
	cells := make([]string, 0, factoryHeadHours)
	for i := 1; i <= factoryHeadHours; i++ {
		n := hours[(now+i)%factoryHeadHours]
		cells = append(cells, steps[min(top, max(0, (n*top+peak/2)/peak))])
	}
	last := len(cells) - 1
	return a.pal.muted(strings.Join(cells[:last], "")) + a.pal.ink(cells[last])
}

// factoryHeadFloor is the floor's facts in one dim line: how many repos, which
// sources, how many benches are busy, and when a source was last polled; and
// at the right the day's money, the spent figure in money's ink and the rail
// muted, with the mock clock's speed after it. The money keeps its cells and
// the facts give way, the last first ([rowTail]).
func (a *app) factoryHeadFloor(snap factory.Snapshot, width int) string {
	pal := a.pal
	right, rightW := a.factoryHeadMoney(snap)
	if rightW > 0 && 2+rightW > width {
		right, rightW = "", 0
	}
	room := width - 2
	if rightW > 0 {
		room -= rightW + rowGutter
	}
	facts := rowTail(factoryHeadFacts(snap), room)
	if facts == "" && right == "" {
		return ""
	}
	factsW := ansi.StringWidth(facts)
	line := "  " + pal.dim(facts)
	if right == "" {
		return line
	}
	return line + strings.Repeat(" ", width-2-factsW-rightW) + right
}

// factoryHeadFacts is the floor's facts, most telling first. A fact with
// nothing behind it is no field at all, so it costs no cells and ends nothing.
func factoryHeadFacts(snap factory.Snapshot) []rowField {
	var fields []rowField
	if n := len(snap.Repos); n > 0 {
		fields = append(fields, rowSay(itoa(n)+" "+factoryPlural(n, "repo", "repos")))
	}
	// THE CHAT IS SAID LAST. It is the source every floor has, so it is the one
	// that tells a person least about this floor.
	var chat bool
	var polled time.Time
	for _, src := range snap.Sources {
		if src.Polled.After(polled) {
			polled = src.Polled
		}
		if src.Name == string(factory.OriginChat) {
			chat = true
			continue
		}
		if src.Name != "" {
			fields = append(fields, rowSay(src.Name))
		}
	}
	if chat {
		fields = append(fields, rowSay(string(factory.OriginChat)))
	}
	if snap.Benches > 0 {
		busy := snap.Count(factory.StateRunning)
		fields = append(fields, rowSay("benches "+itoa(busy)+"/"+itoa(snap.Benches)))
	}
	if ago := reltime.Short(polled, snap.Now); ago != "" {
		if ago == "now" {
			fields = append(fields, rowSay("polled just now"))
		} else {
			fields = append(fields, rowSay("polled "+ago+" ago"))
		}
	}
	return fields
}

// factoryHeadMoney is the day's money and what it measures: `$11.31 / $60
// today`, the spent figure in money's ink and the rest muted. NOTHING SPENT IS
// NO FIGURE, so a day that has spent nothing draws only `/ $60 today`, and a
// floor with no rail draws only the spend. The mock clock's speed follows,
// dim.
func (a *app) factoryHeadMoney(snap factory.Snapshot) (string, int) {
	pal := a.pal
	var out strings.Builder
	w := 0
	put := func(s string, ink func(string) string) {
		out.WriteString(ink(s))
		w += ansi.StringWidth(s)
	}
	switch {
	case snap.Daily > 0 && snap.Rail > 0:
		put(dollars(snap.Daily), placeMoneyInk(pal))
		put(" / "+factoryRailWord(snap.Rail)+" today", pal.muted)
	case snap.Daily > 0:
		put(dollars(snap.Daily), placeMoneyInk(pal))
		put(" today", pal.muted)
	case snap.Rail > 0:
		put("/ "+factoryRailWord(snap.Rail)+" today", pal.muted)
	}
	if x := factorySpeedWord(snap.Speed, a.linearMark("×", "x")); x != "" {
		if w > 0 {
			put(rowSep, pal.dim)
		}
		put(x, pal.dim)
	}
	return out.String(), w
}

// factoryRailWord is the day's rail as it is written: whole dollars without
// cents, because a rail is a round figure a person chose.
func factoryRailWord(usd float64) string {
	if usd == float64(int(usd)) {
		return "$" + groupDigits(itoa(int(usd)))
	}
	return dollars(usd)
}

// factorySpeedWord is the mock clock's speed as a multiple of real time, and
// "" for a real engine, whose speed is zero.
func factorySpeedWord(speed time.Duration, times string) string {
	if speed <= 0 {
		return ""
	}
	x := int((speed + factoryMockBeat/2) / factoryMockBeat)
	return itoa(max(x, 1)) + times
}

// factoryPlural is one or many.
func factoryPlural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
