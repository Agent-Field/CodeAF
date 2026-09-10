package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// ── THE FOOT OF THE FRAME: TWO ROWS, EACH WITH ONE JOB ──────────────────────
//
//	─ porting the parser · glm-5.3-flash · via deepinfra ──── space space home · / commands ─
//	 › your sentence
//
//	$0.27 · ⟲ saved $0.0038 · 58% cached   66.8k/1.3M · 5%   2 jobs   YOLO        38 tok/s · ⠹ working · 12s
//
// THE SEAM IS WHO AND WHERE. The rule above the box carries the conversation's
// name and the model answering it on the left, and the keys that work right now
// on the right. It is the line a person's eye crosses on the way into the box,
// which is why the two facts they most often want to change — which
// conversation, which model — are written on it and pressable there. The `via
// <machine>` rider is ALWAYS on it while a sighting is fresh, whoever served:
// the model is spelled there as its basename, so the vendor half of the id is
// not on the screen for the rider to repeat.
//
// THE STATUS ROW IS NUMBERS AND ALIVENESS. The ledger on the left is grouped by
// the question each group answers, three cells of air between groups and a dot
// only inside one: the bill (what it cost, and what the cache gave back), the
// meter (what it is carrying), what is alive elsewhere (background jobs), and
// the posture (YOLO, drawn only when the gate is open). The right edge is the
// one segment true of the whole line — the state word and its clock — with the
// live rate beside it while a turn writes.
//
// THE RATE IS THE STREAM'S OWN AND NEVER AN AVERAGE. `38 tok/s` is what the
// wire is producing at this moment ([PhaseNews.Rate], measured on the live
// stream), drawn only while it is being measured; the per-turn burn — output
// over the turn's whole wall time, waits included — is on `/status` and the
// phone sheet and is not a claim about now.
//
// Until 2026-09-09 the name and model were on the status row's left and every
// figure sat in one dotted run beside them, so a long title pushed the numbers
// off the frame and nothing on the row read first. The seam had the branch on
// it and nothing else.

// hudGroup is which question a segment answers, and therefore which run of the
// ledger it is drawn in. Segments in one group are joined by ` · `; groups are
// separated by [groupGap] cells of nothing, because space is what the eye reads
// as "a different subject" and a glyph there would be furniture claiming to be
// structure.
type hudGroup uint8

const (
	// groupBill is the money: the bill and what the cache gave back from it.
	groupBill hudGroup = iota
	// groupMeter is what the conversation is carrying, and the forecast about it.
	groupMeter
	// groupElse is what is alive somewhere other than this conversation. It is
	// background jobs alone now: the tab strip above says how many conversations
	// are open, and the standing count is a line at the foot of the task column
	// (task.go's [app.railFootRows]).
	groupElse
	// groupPosture is the gate, drawn only when it is open.
	groupPosture
	// groupAlive is the right edge: how fast it is writing, how the machine it
	// runs on answers, whose move it is, and what it is doing.
	groupAlive
	// groupOff is the facts that are NOT on the line at all any more — the
	// session delta, the crew word, the per-turn burn, the open count and the
	// standing count — kept in the telemetry list so the phone sheet and /status
	// still say them.
	groupOff
)

// groupGapRun is the air between two groups of the ledger, and groupGap is how
// many cells that is. The run is the constant and the count is derived from it,
// because this separator is written on every frame and building it with
// [strings.Repeat] there is an allocation the scroll's own law counts
// (inputsmooth_test.go's [TestOneScreenScrollOfFourThousandLinesStaysInsideTheAllocationLaw]).
const groupGapRun = "   "

const groupGap = len(groupGapRun)

// segGroup is the one table that says where every segment is drawn.
func segGroup(kind hudSeg) hudGroup {
	switch kind {
	case segCost, segCache:
		return groupBill
	case segCtx, segETA:
		return groupMeter
	case segAmbient:
		return groupElse
	case segYolo:
		return groupPosture
	case segRate, segLink, segQuestions, segState:
		return groupAlive
	}
	return groupOff
}

// lineParts is the telemetry as the status row draws it: the facts that are
// off the line removed, the rest split into the ledger and the right edge.
//
// THE TWO RUNS COME OUT OF ONE ARRAY, counted first and then filled, because
// this is laid out several times a frame — the frame's height asks for it as
// well as the frame's row — and two slices grown a segment at a time is half a
// dozen allocations on a scrolling screen (inputsmooth_test.go's
// [TestOneScreenScrollOfFourThousandLinesStaysInsideTheAllocationLaw]). Each
// run is capped at exactly what it holds, so the narrow ladder's [dropKind] —
// which only ever shortens them — can never grow one into the other's cells.
func lineParts(parts []hudPart) (ledger, alive []hudPart) {
	ledgers, alives := 0, 0
	for _, part := range parts {
		switch segGroup(part.kind) {
		case groupOff:
		case groupAlive:
			alives++
		default:
			ledgers++
		}
	}
	buf := make([]hudPart, 0, ledgers+alives)
	ledger = buf[0:0:ledgers]
	alive = buf[ledgers : ledgers : ledgers+alives]
	for _, part := range parts {
		switch segGroup(part.kind) {
		case groupOff:
			continue
		case groupAlive:
			alive = append(alive, part)
		default:
			ledger = append(ledger, part)
		}
	}
	return ledger, alive
}

// partSep is the separator drawn BEFORE part i of a run: nothing before the
// first, a dot inside a group, air between groups.
func partSep(parts []hudPart, i int) string {
	if i == 0 {
		return ""
	}
	if segGroup(parts[i-1].kind) == segGroup(parts[i].kind) {
		return partDot
	}
	return groupGapRun
}

// partDot is the separator inside one group. It is a constant for the same
// reason [groupGapRun] is: these two strings are built on every frame.
const partDot = " · "

// hudWidth is what a run of segments measures, joined, unpainted.
func hudWidth(parts []hudPart) int {
	width := 0
	for i, part := range parts {
		width += ansi.StringWidth(partSep(parts, i)) + ansi.StringWidth(part.text)
	}
	return width
}

// paintParts joins one run, painted and plain. The plain string is what every
// width decision is made from: measuring a painted string is measuring escape
// sequences.
//
// TWO BUILDERS, because this runs on every frame and the row it builds is the
// only thing on a scrolling screen rebuilt from nothing each time
// ([TestOneScreenScrollOfFourThousandLinesStaysInsideTheAllocationLaw]).
func (a *app) paintParts(parts []hudPart) (string, string) {
	if len(parts) == 0 {
		return "", ""
	}
	var painted, plain strings.Builder
	room := hudWidth(parts)
	plain.Grow(room)
	painted.Grow(room * 4)
	for i, part := range parts {
		if sep := partSep(parts, i); sep != "" {
			painted.WriteString(a.pal.dim(sep))
			plain.WriteString(sep)
		}
		painted.WriteString(a.paintPart(part))
		plain.WriteString(part.text)
	}
	return painted.String(), plain.String()
}

// ── THE NARROW LADDER ───────────────────────────────────────────────────────

// hudRung is one step the row can take when it does not fit: a segment said
// shorter, or a segment given up.
type hudRung struct {
	kind    hudSeg
	shorten bool
}

// dropOrder is what the line gives up, first to last, and it is ordered by how
// actionable each thing is:
//
//	ambient   a job holding a port is a thing a person acts on, but rarely now
//	eta       a forecast, and the meter beside it is already painted the warning
//	cache ↓   the cash half of the cache goes and the hit rate stays
//	rate      the live rate; the clock on the state word already says it is alive
//	cache     the hit rate, an accounting nicety beside the bill
//	cost      the bill
//	ctx       what the conversation is carrying, which is the decision it forces
//
// The state word, the posture and the link are not on it at all: one is why a
// person is looking at the line, one is why they should be, and the third is
// the reason nothing else on the line is moving.
//
// The open count and the standing count were the first two rungs until
// 2026-09-09 and are off the line entirely now, so there is nothing left for
// the ladder to spend before it reaches the jobs.
var dropOrder = []hudRung{
	{segAmbient, false}, {segETA, false},
	{segCache, true}, {segRate, false}, {segCache, false}, {segCost, false}, {segCtx, false},
}

// riderRung is where the identity's own shorter spelling stands on the ladder,
// while a room is open and the row's left is the room chip: everything above it
// on [dropOrder] goes before the chip is asked for a shorter spelling, and
// everything from it down survives until the chip has given one.
//
// IT IS THE RATE'S RUNG, FOUND RATHER THAN COUNTED. A literal here is a second
// place the ladder's order is written down, and the two drifted the moment two
// segments came off the line.
var riderRung = func() int {
	for i, rung := range dropOrder {
		if rung.kind == segRate {
			return i
		}
	}
	return len(dropOrder)
}()

// shrink takes one rung off the two runs, and reports whether it found one.
// A rung marked shorten replaces the segment with its shorter true spelling
// once, and is skipped when the segment is already that short.
func (a *app) shrink(ledger, alive *[]hudPart, upTo int) bool {
	for i, rung := range dropOrder {
		if i >= upTo {
			return false
		}
		if rung.shorten {
			if a.shortenKind(ledger, rung.kind) {
				return true
			}
			continue
		}
		if dropKind(ledger, rung.kind) || dropKind(alive, rung.kind) {
			return true
		}
	}
	return false
}

// shortenKind swaps a segment for its shorter spelling, if it has one and is
// not already wearing it.
func (a *app) shortenKind(parts *[]hudPart, kind hudSeg) bool {
	for i, part := range *parts {
		if part.kind != kind {
			continue
		}
		var shorter string
		switch kind {
		case segCache:
			shorter = a.warmSegmentShort()
		}
		if shorter == "" || shorter == part.text {
			return false
		}
		(*parts)[i].text = shorter
		return true
	}
	return false
}

// dropKind removes one named segment if the run is carrying it.
func dropKind(parts *[]hudPart, kind hudSeg) bool {
	for i, part := range *parts {
		if part.kind == kind {
			*parts = append((*parts)[:i], (*parts)[i+1:]...)
			return true
		}
	}
	return false
}

// ── THE DOORS ───────────────────────────────────────────────────────────────
//
// EVERY SEGMENT THAT CAN OPEN SOMETHING IS A DOOR, and a door is recorded where
// it was drawn — as the row is laid out — so the press that follows resolves
// against this frame and not the one before it. The set that LIGHTS under the
// pointer is exactly the set a press acts on (hover.go's own law).
//
//	$0.27 · ⟲ saved …   the Spending tab of /settings   (moneydoor.go)
//	66.8k/1.3M · 5%     /status, one fact per line
//	YOLO                /permissions                     (permissions.go)
//
// Jobs and watches have no page of their own and are not a door; the rate, the
// link and the state word are readings, not controls.
//
// TWO DOORS LEFT THIS ROW ON 2026-09-09 AND ONE OF THEM IS STILL A DOOR. The
// open count is gone because the tab strip already names every conversation;
// the standing count moved to the foot of the task column, where it is drawn
// dim, brightens under the pointer and opens /standing exactly as it did here
// (task.go's [app.railFootRows], standdoor.go).

// statusDoor is one pressable segment on the status row.
type statusDoor struct {
	kind hudSeg
	span hudSpan
	row  int
}

// doorKinds is which segments are doors at all.
func doorKind(kind hudSeg) bool {
	switch kind {
	case segCost, segCache, segCtx, segETA, segYolo:
		return true
	}
	return false
}

// markDoors walks one run as it was drawn and records every door on it. base is
// the column the run starts at and row which of the status row's rows it is on.
//
// The legacy span ([app.moneySpan]) is written beside the table because the
// paint and the tests of that door read it by name.
func (a *app) markDoors(parts []hudPart, base, row int) {
	at := base
	for i, part := range parts {
		at += ansi.StringWidth(partSep(parts, i))
		width := ansi.StringWidth(part.text)
		if doorKind(part.kind) {
			a.doors = append(a.doors, statusDoor{kind: part.kind, span: hudSpan{from: at, to: at + width}, row: row})
		}
		if part.kind == segCost {
			// The reservation in front of the figure is space, not a door.
			room, _ := splitReserve(part.text)
			a.moneySpan, a.moneyRow = hudSpan{from: at + len(room), to: at + width}, row
		}
		at += width
	}
}

// doorAt is the door under a column of one status row, if any.
func (a *app) doorAt(x, row int) (statusDoor, bool) {
	for _, door := range a.doors {
		if door.row == row && door.span.holds(x) {
			return door, true
		}
	}
	return statusDoor{}, false
}

// doorHover is what the pointer over a door is called (hover.go).
func doorHover(kind hudSeg) hoverKind {
	switch kind {
	case segCost, segCache:
		return hoverMoney
	case segCtx, segETA:
		return hoverMeter
	case segYolo:
		return hoverPosture
	}
	return hoverNothing
}

// doorPress is a click on one status door: what it opens.
func (a *app) doorPress(door statusDoor) (tea.Cmd, bool) {
	switch door.kind {
	case segCost, segCache:
		return a.openSpending(spendTodayKey), true
	case segCtx, segETA:
		return a.runStatusNote(), true
	case segYolo:
		a.openPermissions()
		return nil, true
	}
	return nil, false
}

// statusDoorPress resolves a click on the status row to the door under it, and
// reports whether it took the click.
//
// THE ROW IS RESOLVED BEFORE THE COLUMN. [app.chromeAt] lays the chrome out to
// answer, and laying it out is what writes the doors — read the other way
// round, this would be testing a column from the frame before this one.
func (a *app) statusDoorPress(x, y int) (tea.Cmd, bool) {
	if a.copy.on || a.at(pageSettings) || a.pick.open {
		return nil, false
	}
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeStatus {
		return nil, false
	}
	door, ok := a.doorAt(x, mark.index)
	if !ok {
		return nil, false
	}
	return a.doorPress(door)
}

// hoveringMeter and hoveringPosture are the pointer over those two doors, for
// the paint.
func (a *app) hoveringMeter() bool   { return a.hot.kind == hoverMeter }
func (a *app) hoveringPosture() bool { return a.hot.kind == hoverPosture }

// runStatusNote prints /status into the conversation, which is what pressing
// the meter does: the meter is the one figure on the line whose whole story —
// the window, the threshold, what compaction will do — is several lines long.
func (a *app) runStatusNote() tea.Cmd {
	return a.slash("/status")
}

// ── THE SEAM'S IDENTITY ─────────────────────────────────────────────────────

// seamIdentity is the legend's left cluster out of a room, built to a budget,
// and the columns its two doors occupy within it.
//
//	devbox · porting the parser · glm-5.3-flash · ⠿ high · via deepinfra · main*
//
// THE LADDER GIVES UP THE CHEAPEST TRUE THING FIRST, and it never clips:
//
//	1  the branch goes — the shell prompt behind this pane still says it
//	2  the rider is said shorter, then not at all — it is the one part with a
//	   shorter true spelling ([app.modelRiderAt])
//	3  the thinking rung goes, WHOLE — half a rung word is a word somebody reads
//	   as another rung, and the ladder it belongs to is one command away
//	4  the name is cut, one ellipsis, never under [legendNameFloor] cells
//	5  the model goes — the name is what tells two panes apart
//	6  the name goes — and the machine, on a --host session, is the last thing
//	   standing, because it is the half nobody can reconstruct from elsewhere
//
// TWO CELLS ON THIS CLUSTER ARE CONTROLS AND BOTH RETURN THEIR COLUMNS: the
// model opens the picker ([app.legendModelPress]) and the rung walks one step
// ([app.legendEffortPress]). The rider is no longer inside the model's own span
// — the rung stands between them now — which is the price of anchoring the rung
// to the name it is about rather than to whatever the line happens to end with
// (effortchip.go). A room does not come through here: its legend says the way
// out, and its status row carries the room chip ([app.identityParts]).
func (a *app) seamIdentity(width, room int) (string, hudSpan, hudSpan) {
	name := a.sessionName()
	if name == "" {
		// THE FOLDER STANDS IN UNTIL THE SESSION HAS NAMED ITSELF, so this slot
		// is never empty — and it stands in ALONE. [app.place] is written with
		// the machine in front of it (`devbox:app`) for the status row, which
		// had no host segment of its own; this line has one, and `devbox ·
		// devbox:app` would name the machine twice (host.go).
		name = strings.TrimPrefix(a.place, a.host+":")
	}
	// THE MODEL IS ITS BASENAME, AND HOW HARD IT IS BEING ASKED TO THINK IS THE
	// CELL AFTER IT. The level used to be spelled onto the id with a colon —
	// `glm-5.3-flash:high` — which said the picker-dialled level and nothing
	// else, so one ladder had two spellings on one frame: this suffix, and the
	// chip on the tray saying the RESOLVED rung. There is one cell now, it says
	// the resolved rung whichever scope decided it, and the resolver already
	// folds a level dialled onto the model into that answer (internal/effort).
	//
	// The id is BUILT here rather than lent through [app.model] the way the phone
	// deck's row is (view.go's [app.statusRow]), because a lent id no longer
	// matches the endpoint sighting the `via` rider is looked up by.
	model := modelBase(a.model)
	thinking := a.effortChipText()
	if model == "" {
		// A rung with no model beside it has nothing to be about, and the ladder
		// it belongs to is reached by name (`/effort`) rather than from a cell
		// floating on its own.
		thinking = ""
	}
	branch := a.branchWord()
	if width < hudTight {
		branch = ""
	}
	// The pieces before the model, joined, and the pieces after it.
	head := dotted(a.host, name)
	// spans is where the model and the rung fell inside a cluster built from
	// these pieces. Both are measured off [head], which is the only thing drawn
	// before them, and a cluster too short to hold one of them yields no span at
	// all — a door is recorded only where it was actually drawn.
	spans := func(cluster, rung string) (hudSpan, hudSpan) {
		if model == "" {
			return hudSpan{}, hudSpan{}
		}
		from := 0
		if head != "" {
			from = ansi.StringWidth(head + legendJoin)
		}
		to := from + ansi.StringWidth(model)
		if ansi.StringWidth(cluster) < to {
			return hudSpan{}, hudSpan{}
		}
		named := hudSpan{from: from, to: to}
		if rung == "" {
			return named, hudSpan{}
		}
		at := to + ansi.StringWidth(legendJoin)
		end := at + ansi.StringWidth(rung)
		if ansi.StringWidth(cluster) < end {
			return named, hudSpan{}
		}
		return named, hudSpan{from: at, to: end}
	}
	// A step: a cluster with the rung and the rider it can afford, or false when
	// what is fixed about it does not fit at all. With `keep` the rider is part
	// of what is fixed: a step that cannot seat the whole rider fails, so the
	// ladder can give up something ABOVE the rider before the rider itself.
	full := ""
	if model != "" {
		full = a.modelRiderAt(-1)
	}
	step := func(rung, tail string, keep bool) (string, hudSpan, hudSpan, bool) {
		bare := dotted(head, dotted(model, rung), tail)
		if ansi.StringWidth(bare) > room {
			return "", hudSpan{}, hudSpan{}, false
		}
		rider := ""
		if model != "" {
			rider = a.modelRiderAt(room - ansi.StringWidth(bare))
		}
		if keep && rider != full {
			return "", hudSpan{}, hudSpan{}, false
		}
		cluster := dotted(head, dotted(model, rung)+rider, tail)
		named, dial := spans(cluster, rung)
		return cluster, named, dial, true
	}
	// THE RIDER OUTRANKS THE BRANCH AND THE NAME'S TAIL. `via relace` is the one
	// fact on this line that says what is happening NOW — which machine is
	// answering — and the owner's ruling is that it is always on the seam. The
	// branch is on the folder's own line and the name's tail is in the tab
	// strip; so a rider that does not fit beside them takes the branch's cells
	// first, then the name's, and drops only when the name is at its floor. It
	// used to be the filler after everything else, and a conversation with a
	// long title never showed who served it.
	if full != "" {
		if cluster, named, dial, ok := step(thinking, branch, true); ok {
			return cluster, named, dial
		}
		if branch != "" {
			if cluster, named, dial, ok := step(thinking, "", true); ok {
				return cluster, named, dial
			}
		}
		fixed := dotted(a.host, dotted(model, thinking)+full)
		left := room - ansi.StringWidth(fixed)
		if fixed != "" {
			left -= ansi.StringWidth(legendJoin)
		}
		if model != "" && name != "" && left >= legendNameFloor {
			cut := dotted(a.host, fit(name, left))
			cluster := dotted(cut, dotted(model, thinking)+full)
			// The spans are measured off the cut head, which is what was drawn.
			from := ansi.StringWidth(cut + legendJoin)
			named := hudSpan{from: from, to: from + ansi.StringWidth(model)}
			dial := hudSpan{}
			if thinking != "" {
				at := named.to + ansi.StringWidth(legendJoin)
				dial = hudSpan{from: at, to: at + ansi.StringWidth(thinking)}
			}
			return cluster, named, dial
		}
	}
	if cluster, named, dial, ok := step(thinking, branch, false); ok {
		return cluster, named, dial
	}
	if branch != "" {
		if cluster, named, dial, ok := step(thinking, "", false); ok {
			return cluster, named, dial
		}
	}
	// THE RUNG GOES WHOLE OR NOT AT ALL, which is the same law the chip kept on
	// the tray and pickrow.go's about an answer: `hig` is a word somebody reads
	// as another rung.
	if thinking != "" {
		if cluster, named, dial, ok := step("", "", false); ok {
			return cluster, named, dial
		}
	}
	// The name is cut. What it has is the room less the machine and the model.
	fixed := dotted(a.host, model)
	left := room - ansi.StringWidth(fixed)
	if fixed != "" {
		left -= ansi.StringWidth(legendJoin)
	}
	if model != "" && left >= legendNameFloor {
		head = dotted(a.host, fit(name, left))
		cluster := dotted(head, model)
		named, _ := spans(cluster, "")
		return cluster, named, hudSpan{}
	}
	// The model goes.
	left = room - ansi.StringWidth(a.host)
	if a.host != "" {
		left -= ansi.StringWidth(legendJoin)
	}
	if left >= legendNameFloor {
		return dotted(a.host, fit(name, left)), hudSpan{}, hudSpan{}
	}
	if ansi.StringWidth(a.host) <= room {
		return a.host, hudSpan{}, hudSpan{}
	}
	return "", hudSpan{}, hudSpan{}
}

// legendModelPress is a click on the model's name in the seam, and reports
// whether it took the click. Out of a room the picker it opens moves the
// conversation's model; a room's own door is on its status row
// ([app.statusPress]).
func (a *app) legendModelPress(x, y int) bool {
	if a.copy.on || a.at(pageSettings) || a.pick.open || a.roomOpen() {
		return false
	}
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeLegend || !a.seamModelSpan.holds(x) {
		return false
	}
	a.openPicker()
	return true
}

// legendEffortPress is a click on the thinking rung beside it, and it WALKS THE
// LADDER ONE STEP rather than opening a list — the gesture the room panel's own
// thinking row already makes on a task (roompanel.go), so one press means one
// step wherever a person meets a rung. The five rows with their sentences are
// `/effort` (effortchip.go's [app.runEffort]).
//
// It answers a command as well as whether it took the press, because the step
// may cross a wire: over `--host` the rung is set on the far engine and the
// resolved word read back, so the work goes to the loop rather than being run
// under the pointer.
func (a *app) legendEffortPress(x, y int) (tea.Cmd, bool) {
	if a.copy.on || a.at(pageSettings) || a.pick.open || a.roomOpen() {
		return nil, false
	}
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeLegend || !a.seamEffortSpan.holds(x) {
		return nil, false
	}
	return a.cycleEffort(), true
}

// paintSpan paints one cluster, LIFTING the span while lift is wanted. The
// pieces are painted separately rather than nested, because these hues are raw
// SGR with an explicit reset (styles.go's [palette.paint]): a colour inside a
// colour would end the outer one at the inner one's reset.
func paintSpan(text string, span hudSpan, paint, lift func(string) string, lifted bool) string {
	if !lifted || !span.pressable() || span.to > ansi.StringWidth(text) {
		return paint(text)
	}
	head := ansi.Cut(text, 0, span.from)
	segment := ansi.Cut(text, span.from, span.to)
	tail := ansi.Cut(text, span.to, ansi.StringWidth(text))
	return paint(head) + lift(segment) + paint(tail)
}
