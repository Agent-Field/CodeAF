package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/tui2/reltime"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE HANDOVER ────────────────────────────────────────────────────────────
//
// The strip across the top of the factory floor: what happened on the floor
// since the person last looked, what waits on them now, and what the floor is
// made of and has cost today. It is four rows and a blank, it spans the whole
// width above the rows and the peek, and the page lays the floor under it
// ([app.factoryBody]).
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

// The words the strip says when there is nothing to count. They are named so
// the manual and the tests quote the code's own spelling.
const (
	factoryHeadWord = "handover"
	// factoryHeadNothingWords is the shift's row while nothing happened but a
	// read is in flight, asked for with no word yet, or failing: the floor is
	// not quiet, so it does not say quiet.
	factoryHeadNothingWords = "nothing happened while you were away"
	// factoryHeadQuietWords is said only on a floor where nothing is happening
	// AND nothing happened.
	factoryHeadQuietWords  = "quiet · " + factoryHeadNothingWords
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

// factoryHead is the handover: ONE LINE AND A BLANK by default, and the four
// rows and a blank of the full strip under `h` ([app.factoryHeadLine]), each
// exactly width cells. It answers nil for a width with no room in it.
//
// THE ONE LINE IS THE DEFAULT (owner ruling, 2026-10-08): the four rows said
// what a person reads once a morning, and the rows under them are what they
// read all day, so the rows get the height and `h` brings the strip back. The
// choice is remembered with the split (factory_split.go).
func (a *app) factoryHead(width int) []string {
	if width <= 0 {
		return nil
	}
	// THE STRIP STANDS [factoryMargin] IN FROM THE FRAME'S EDGE, the margin
	// every region of the floor keeps, so its mark sits in the column of the
	// rows' state marks.
	snap, inner := a.fp.snap, width-factoryMargin
	rows := []string{a.factoryHeadLine(snap, inner), ""}
	if a.fp.headFull {
		rows = []string{
			a.factoryHeadTitle(snap, inner),
			a.factoryHeadShift(snap, inner),
			a.factoryHeadWaiting(snap, inner),
			a.factoryHeadFloor(snap, inner),
			"",
		}
	}
	for i, row := range rows {
		if inner > 0 {
			row = factoryPad(row, inner)
		} else {
			row = ""
		}
		rows[i] = factoryPad(factoryMarginPad()+row, width)
	}
	return rows
}

// factoryHeadLine is the handover as one line: the manager's mark, what
// shipped and what arrived, what waits on the person in the question's hue,
// the day's money against its rail, and how fresh the floor is —
//
//	◆ 2 shipped · 3 arrived · ? 5 waiting · $8.44 / $60 · polled 14s ago
//
// A CLAUSE THAT COUNTS NOTHING IS NOT ON THE LINE (the emptiness law), and a
// line too long for the width drops whole clauses from its right. The last
// clause spins while the floor is being read ([app.factoryHeadFresh]).
func (a *app) factoryHeadLine(snap factory.Snapshot, width int) string {
	pal := a.pal
	var segs, plains []string
	add := func(plain, painted string) {
		segs, plains = append(segs, painted), append(plains, plain)
	}
	if n := snap.Shift.Shipped; n > 0 {
		w := itoa(n) + " shipped"
		add(w, pal.ink(w))
	}
	if n := snap.Shift.Arrived; n > 0 {
		w := itoa(n) + " arrived"
		add(w, pal.ink(w))
	}
	if n := factoryWaitingIn(snap); n > 0 {
		w := "? " + itoa(n) + " waiting"
		add(w, pal.ask(w))
	}
	switch {
	case snap.Daily > 0 && snap.Rail > 0:
		w := " / " + factoryRailWord(snap.Rail)
		add(dollars(snap.Daily)+w, placeMoneyInk(pal)(dollars(snap.Daily))+pal.muted(w))
	case snap.Daily > 0:
		add(dollars(snap.Daily), placeMoneyInk(pal)(dollars(snap.Daily)))
	case snap.Rail > 0:
		w := "/ " + factoryRailWord(snap.Rail)
		add(w, pal.muted(w))
	}
	if fresh, painted := a.factoryHeadFresh(snap); fresh != "" {
		add(fresh, painted)
	}
	mark := a.linearMark("◆", "*")
	if len(segs) == 0 {
		return pal.muted(mark) + " " + pal.dim(fit(factoryHeadQuietWords, max(width-factoryLeadW, 0)))
	}
	return pal.muted(mark) + " " + factoryJoinWhole(segs, plains, pal.dim(rowSep), max(width-factoryLeadW, 0))
}

// factoryHeadFresh is how fresh the floor is, as one clause, plain and
// painted: a whole-floor re-read in flight, `⠋ refreshing 8 items · 3 done`;
// else a source mid-read that says where it is, `⠋ reading
// Agent-Field/CodeAF · 1 of 3`, the repository muted and the rest dim; else a
// source mid-poll that does not, `github · ⠋ polling`; else the save's first
// read, `⠋ reading the repositories you watch`; else, past its wait with no
// word, `no word from the read yet`, no spinner; else when a source last
// answered, `polled 14s ago`, led by a source's trouble, `github · not
// reachable`; and "" when none of these is known.
//
// THE PROGRESS IS THIS CLAUSE, not a second one: the line spins once, here,
// and a read with nothing on the floor yet stands as the line on its own
// rather than under the quiet sentence.
func (a *app) factoryHeadFresh(snap factory.Snapshot) (string, string) {
	pal := a.pal
	if all := strings.TrimSpace(snap.BusyAll); all != "" {
		w := a.factorySpin() + " " + all
		return w, pal.dim(w)
	}
	if src, ok := factoryPollingSource(snap); ok {
		if repo, count := factorySourceProgress(src); repo != "" {
			lead := a.factorySpin() + " reading "
			plain, painted := lead+repo, pal.dim(lead)+pal.muted(repo)
			if count != "" {
				plain += rowSep + count
				painted += pal.dim(rowSep + count)
			}
			return plain, painted
		}
		w := src.Name + rowSep + a.factorySpin() + " polling"
		return w, pal.dim(w)
	}
	if a.factoryFirstReading() {
		w := a.factorySpin() + " " + factoryFirstReadWords
		return w, pal.dim(w)
	}
	// A SOURCE IN TROUBLE IS SAID ON THE ONE LINE, items or none: a failing
	// read is never `quiet`, and the four-row strip's facts row already says
	// it the same way, `github · not reachable`.
	trouble := factoryHeadTrouble(snap)
	if a.factoryNoWord() {
		w := factoryNoWordWords
		if trouble != "" {
			w += rowSep + trouble
		}
		return w, pal.dim(w)
	}
	var polled time.Time
	for _, src := range snap.Sources {
		if src.Polled.After(polled) {
			polled = src.Polled
		}
	}
	var w string
	switch ago := reltime.Short(polled, snap.Now); ago {
	case "":
	case "now":
		w = "polled just now"
	default:
		w = "polled " + ago + " ago"
	}
	if trouble != "" {
		w = strings.TrimSuffix(trouble+rowSep+w, rowSep)
	}
	return w, pal.dim(w)
}

// factoryHeadTrouble is the first named source whose last read failed, as the
// facts row says it, `github · not reachable`, and "" when none did.
func factoryHeadTrouble(snap factory.Snapshot) string {
	for _, src := range snap.Sources {
		if src.Name != "" && src.Name != string(factory.OriginChat) && strings.TrimSpace(src.Trouble) != "" {
			return src.Name + rowSep + strings.TrimSpace(src.Trouble)
		}
	}
	return ""
}

// factoryPollingSource is the first named source a read is in flight on.
func factoryPollingSource(snap factory.Snapshot) (factory.SourceInfo, bool) {
	for _, src := range snap.Sources {
		if src.Polling && src.Name != "" {
			return src, true
		}
	}
	return factory.SourceInfo{}, false
}

// factorySourceProgress is where a source's read is: the repository it is on
// now and `1 of 3 · 200 items so far`, or "" for either it does not know.
func factorySourceProgress(src factory.SourceInfo) (repo, count string) {
	repo = strings.TrimSpace(src.Reading)
	if repo == "" {
		return "", ""
	}
	if src.Of > 0 {
		count = itoa(min(max(src.Read, 0), src.Of)) + " of " + itoa(src.Of)
	}
	// THE ITEMS SO FAR ARE THE SAME CLAUSE: a busy repository lists for a
	// while before its rows stand, and the count is what says the read moves.
	if src.Items > 0 {
		items := itoa(src.Items) + " items so far"
		if count != "" {
			count += rowSep + items
		} else {
			count = items
		}
	}
	return repo, count
}

// factoryInFlight says whether the floor is being read right now: a
// whole-floor re-read, or a source mid-poll.
func factoryInFlight(snap factory.Snapshot) bool {
	_, polling := factoryPollingSource(snap)
	return polling || strings.TrimSpace(snap.BusyAll) != ""
}

// factoryToggleHead is `h`: the handover's one line or its four rows, the
// choice written beside the split.
func (a *app) factoryToggleHead() tea.Cmd {
	a.fp.headFull = !a.fp.headFull
	a.fp.splitRead = true
	a.touch()
	return a.factorySaveSplit()
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
	lead, mark := factorySpaces(factoryLeadW), ""
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
		words := factoryHeadQuietWords
		if factoryInFlight(snap) || !a.fp.readingSince.IsZero() || factoryHeadTrouble(snap) != "" {
			words = factoryHeadNothingWords
		}
		return factorySpaces(factoryLeadW) + pal.dim(fit(words, width-factoryLeadW))
	}
	leadW := factoryLeadW
	if mark != "" {
		leadW = ansi.StringWidth(mark) + 1
	}
	return lead + pal.ink(rowTail(fields, width-leadW))
}

// factoryWaitingIn is how many items on a floor wait on the person: every item
// asking a question, and every landed item, whose proof sheet waits for the
// sign-off.
//
// A LANDED ITEM WAITS ON YOU AS MUCH AS A QUESTION DOES. On the 2026-10-08
// hand run issue #1 landed, its sheet waited for the sign-off, and the
// handover said `nothing waits on you`; the handover, its full form and the
// tab bar's `?` all count through here, so they cannot disagree.
func factoryWaitingIn(snap factory.Snapshot) int {
	return snap.Count(factory.StateNeedsYou) + snap.Count(factory.StateLanded)
}

// factoryHeadWaiting is how many items wait on the person, in the question's
// hue, or the dim sentence saying none do; and at the right, on a pane wide
// enough, the day's work as a sparkline of the shift's hours ending at the
// current hour. A day with no hours in it draws no sparkline.
func (a *app) factoryHeadWaiting(snap factory.Snapshot, width int) string {
	pal := a.pal
	left := factorySpaces(factoryLeadW) + pal.dim(factoryHeadNoWaitWords)
	leftW := factoryLeadW + ansi.StringWidth(factoryHeadNoWaitWords)
	if n := factoryWaitingIn(snap); n > 0 {
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
	return left + factorySpaces(width-leftW-rightW) + right
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
// muted. The money keeps its cells and
// the facts give way, the last first ([rowTail]).
func (a *app) factoryHeadFloor(snap factory.Snapshot, width int) string {
	pal := a.pal
	right, rightW := a.factoryHeadMoney(snap)
	if rightW > 0 && factoryLeadW+rightW > width {
		right, rightW = "", 0
	}
	room := width - factoryLeadW
	if rightW > 0 {
		room -= rightW + rowGutter
	}
	facts := rowTail(factoryHeadFactsSpun(snap, a.factorySpin()), room)
	if facts == "" && right == "" {
		return ""
	}
	factsW := ansi.StringWidth(facts)
	line := factorySpaces(factoryLeadW) + pal.dim(facts)
	if right == "" {
		return line
	}
	return line + factorySpaces(width-factoryLeadW-factsW-rightW) + right
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
			// A source whose last read failed says so beside its name, `github ·
			// not reachable`, and keeps its polled time: the floor is as stale as
			// its last good read.
			name := src.Name
			if src.Trouble != "" {
				name += rowSep + src.Trouble
			}
			fields = append(fields, rowSay(name))
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

// factoryHeadFactsSpun is [factoryHeadFacts] with work in flight said in
// place of the freshness: a whole-floor re-read as `spin refreshing 8 items`,
// a source mid-poll as `github · spin polling`, or `github · spin reading
// Agent-Field/CodeAF · 1 of 3` when the read says where it is, with no polled
// clause after it. The progress is the source's own clause, so the row spins
// once.
func factoryHeadFactsSpun(snap factory.Snapshot, spin string) []rowField {
	fields := factoryHeadFacts(snap)
	busy := strings.TrimSpace(snap.BusyAll)
	var polling, doing string
	for _, src := range snap.Sources {
		if src.Polling && src.Name != "" && src.Name != string(factory.OriginChat) {
			polling, doing = src.Name, "polling"
			if repo, count := factorySourceProgress(src); repo != "" {
				doing = "reading " + repo
				if count != "" {
					doing += rowSep + count
				}
			}
			break
		}
	}
	if busy == "" && polling == "" {
		return fields
	}
	out := fields[:0:0]
	for _, f := range fields {
		switch {
		case strings.HasPrefix(f.full, "polled "):
			continue
		case polling != "" && (f.full == polling || strings.HasPrefix(f.full, polling+rowSep)):
			f = rowSay(f.full + rowSep + spin + " " + doing)
		}
		out = append(out, f)
	}
	if busy != "" {
		out = append(out, rowSay(spin+" "+busy))
	}
	return out
}

// factoryHeadMoney is the day's money and what it measures: `$11.31 / $60
// today`, the spent figure in money's ink and the rest muted. NOTHING SPENT IS
// NO FIGURE, so a day that has spent nothing draws only `/ $60 today`, and a
// floor with no rail draws only the spend.
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

// factoryPlural is one or many.
func factoryPlural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
