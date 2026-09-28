package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// ── THE FOOT OF THE FRAME: TWO ROWS, EACH WITH ONE JOB ──────────────────────
//
//	─ glm-5.3-flash (deepinfra):high · ◇ asks ── $0.27 · 58% cached   66.8k/1.3M · 5%   ⠹ working · 12s   project: ~/src/parser ─
//	 › your sentence
//	 alt+e effort · alt+a approvals · alt+k chats · / commands · space space home
//
// THE SEAM IS WHAT ANSWERS AND HOW MUCH. The rule above the box carries the
// model answering the conversation on the left, and the numbers on the right
// (footswap.go says why they moved up on 2026-09-17). It is the line a
// person's eye crosses on the way into the box, which is why the facts they
// most often want to change — which model, how hard, what it may run — are
// written on it and pressable there. The machine answering for the model is
// ALWAYS on it once one has answered, whoever served, and IT IS WRITTEN INTO
// THE MODEL'S OWN CELL — `glm-5.3-flash (deepinfra)` — so the two halves of
// "who is answering" read as one word (render.go's [app.modelRiderAt]).
//
// THE CONVERSATION'S NAME IS NOT ON IT. It was, from 2026-09-09 until
// 2026-09-17, and the owner ruled it off: a title takes up the room the
// numbers need, and it is already on the tab strip and the breadcrumb bar
// (chattabs.go, title.go). The machine on a `--host` session still leads the
// line — it is the half nobody can reconstruct from anything else on screen.
//
// THE NUMBERS ARE GROUPED BY THE QUESTION EACH GROUP ANSWERS, three cells of
// air between groups and a dot only inside one: the bill (what it cost, and
// what the cache gave back), the meter (what it is carrying), what is alive
// elsewhere (background jobs), and the posture (YOLO, drawn only when the gate
// is open AND the seam is not carrying the approvals chip — approvalchip.go).
// The right end is the one segment true of the whole line — the state word and
// its clock — with the live rate beside it while a turn writes.
//
// THE LAST ROW IS THE KEYS that work right now, and nothing else
// ([app.statusRows]).
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
// it and nothing else. Until 2026-09-17 the numbers were the last row, the
// keys were the seam's right, and the name led the seam.

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
	// groupPosture is the gate, drawn only when it is open and only on the
	// frames whose seam has no approvals chip (approvalchip.go's
	// [app.approvalSegment]).
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
//
// Jobs and watches have no page of their own and are not a door; the rate, the
// link and the state word are readings, not controls. `YOLO` was a third door,
// onto /permissions, until the posture moved to the seam as a control of its
// own (approvalchip.go's [app.legendApprovalPress]); on the frames it is still
// drawn here it is a reading.
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
	case segCost, segCache, segCtx, segETA:
		return true
	}
	return false
}

// markDoors walks one run as it was drawn and records every door on it. base is
// the column the run starts at and row which row it is on: the seam's
// ([legendDoorRow]) on every tier but the phone's, else the deck's own.
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
	}
	return nil, false
}

// statusDoorPress resolves a click on the numbers to the door under it, and
// reports whether it took the click. The numbers are on the seam
// (footswap.go), so the row that answers is the legend's.
//
// THE ROW IS RESOLVED BEFORE THE COLUMN. [app.chromeAt] lays the chrome out to
// answer, and laying it out is what writes the doors — read the other way
// round, this would be testing a column from the frame before this one.
func (a *app) statusDoorPress(x, y int) (tea.Cmd, bool) {
	if a.copy.on || a.at(pageSettings) || a.pick.open {
		return nil, false
	}
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeLegend {
		return nil, false
	}
	door, ok := a.doorAt(x, legendDoorRow)
	if !ok {
		return nil, false
	}
	return a.doorPress(door)
}

// hoveringMeter is the pointer over the meter's door, for the paint.
func (a *app) hoveringMeter() bool { return a.hot.kind == hoverMeter }

// runStatusNote prints /status into the conversation, which is what pressing
// the meter does: the meter is the one figure on the line whose whole story —
// the window, the threshold, what compaction will do — is several lines long.
func (a *app) runStatusNote() tea.Cmd {
	return a.slash("/status")
}

// ── THE SEAM'S IDENTITY ─────────────────────────────────────────────────────

// seamIdentity is the legend's left cluster out of a room, built to a budget,
// and the columns its three doors occupy within it.
//
//	devbox · glm-5.3-flash (deepinfra):high · ◇ asks
//
// THE LADDER IS [seamLadder], read top to bottom, and it never clips.
//
// THREE CELLS ON THIS CLUSTER ARE CONTROLS AND ALL THREE RETURN THEIR COLUMNS:
// the model opens the picker ([app.legendModelPress]), the rung walks one step
// ([app.legendEffortPress]) and the approvals chip walks the gate's wheel one
// stop ([app.legendApprovalPress]). The rider stands between the model and
// the rung — the owner's ruling of 2026-09-17 that the model and its machine
// read as one thing — and is outside the model's span, so a press on `via`
// opens nothing; the rung and the chip are measured past it (seamSpans). A
// room comes through [app.roomSeamIdentity] with the way out where the name
// slot is.
func (a *app) seamIdentity(width, room int, tier seamTier) (string, hudSpan, hudSpan, hudSpan) {
	pieces := a.seamPieces(width)
	return seamLay(&pieces, room, tier)
}

// seamLay walks the ladder over one set of pieces and returns the first rung
// that fits the room on the given tier, or nothing.
func seamLay(pieces *seamPieces, room int, tier seamTier) (string, hudSpan, hudSpan, hudSpan) {
	for _, try := range seamLadder {
		if !try.on(tier) {
			continue
		}
		if cluster, named, dial, gate, ok := pieces.lay(try, room); ok {
			return cluster, named, dial, gate
		}
	}
	return "", hudSpan{}, hudSpan{}, hudSpan{}
}

// seamTier is how much of the ladder the seam's left may walk on one pass of
// [app.legend]'s search, which tries the numbers at every width against the
// costliest tier first (footswap.go): a name cut to twelve cells, or a
// machine gone from the seam, is a worse trade than a forecast gone from it,
// and a ladder that cut the name before it dropped a number would make that
// trade on every narrow frame.
type seamTier uint8

const (
	// seamTierRider keeps the name and the model entire AND the rider whole:
	// which machine is answering is the one fact on this line about NOW.
	seamTierRider seamTier = iota
	// seamTierWhole keeps the name and the model entire; the rider may be
	// said shorter or not at all.
	seamTierWhole
	// seamTierAny is the whole ladder, cuts included.
	seamTierAny
)

// on reports whether a rung is allowed on one tier.
func (t seamTry) on(tier seamTier) bool {
	switch tier {
	case seamTierRider:
		return t.name && t.model && !t.cut && t.rider == riderWhole
	case seamTierWhole:
		return t.name && t.model && !t.cut
	}
	return true
}

// seamPieces is everything the identity cluster can be made of, gathered once
// per frame so the ladder below is only ever choosing between them.
type seamPieces struct {
	host, name, model, rung, gate, project string
	// rider is the WHOLE rider — ` (relace)`, ` (parasail · rescued)`, or a
	// rescue's own ` · slow · trying coreweave…` — and fit is the door that
	// answers it for a given room, or says nothing ([app.modelRiderAt]). The
	// two are kept apart because the ladder needs to know whether a rung
	// seated the rider entire.
	rider string
	fit   func(room int) string
}

func (a *app) seamPieces(width int) seamPieces {
	// THE NAME SLOT IS EMPTY ON A CONVERSATION. The title stood here from
	// 2026-09-09 (with the folder standing in until the session had named
	// itself) until the owner ruled it off on 2026-09-17: it took the room the
	// numbers need, and the tab strip and the breadcrumb bar already say it.
	// The slot is still a slot because a room writes its way out into it
	// (roomseam.go).
	//
	// THE MODEL KEEPS ITS FULL ADDRESS, AND HOW HARD IT IS ASKED TO THINK IS THE
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
	//
	// AND A PINNED LANE RIDES THE ID AS `@lane` ([app.modelWord]), which is the
	// one place the pin is written on the chrome: the status row and the phone
	// deck take the same word from the same function.
	pieces := seamPieces{host: a.host, model: a.modelWord(),
		project: a.seamProjectWord()}
	// AND IN A MANAGED TEAM THE SLOT SAYS WHERE THE WORDS GO, once there are
	// words: the empty box says it as its placeholder (teamrailpointer.go's
	// [app.trafficHint]), and the first keystroke took the placeholder away with
	// the one fact a person typing to a team needs. `to ◆ manager`, or
	// `to @web` with a member in front.
	if !a.input.empty() {
		pieces.name = a.trafficHint()
	}
	if pieces.model != "" {
		// A rung with no model beside it has nothing to be about, and the ladder
		// it belongs to is reached by name (`/effort`) rather than from a cell
		// floating on its own. The same goes for the rider.
		pieces.rung = a.effortChipText()
		// AND WHAT RUNS WITHOUT ASKING, after the rung: it is a fact about
		// what the model may do, so it stands with the model (approvalchip.go).
		pieces.gate = a.approvalChipText()
		pieces.rider = a.modelRiderAt(-1)
		pieces.fit = a.modelRiderAt
	}
	return pieces
}

// seamTry is one rung of the ladder: which of the pieces are on the line, and
// whether the name may be cut to seat them.
type seamTry struct {
	rung, gate, model, name bool
	// rider is how the rider is treated: [riderWhole] means the step fails
	// unless the rider fits entire, [riderFit] takes whatever spelling the room
	// allows (which may be none), [riderNone] leaves it off.
	rider riderWant
	// cut allows the name to be shortened with one ellipsis, never under
	// [legendNameFloor] cells, to make the rest fit. A rung that cuts is
	// skipped outright where there is no name to cut — a conversation's seam —
	// since it could only ever fit where the rung above it already did.
	cut bool
}

type riderWant uint8

const (
	riderNone riderWant = iota
	riderFit
	riderWhole
)

// seamLadder is THE LADDER, top to bottom: the first rung that fits is drawn.
// It gives up the cheapest true thing first and never clips a word. The name
// slot is empty on a conversation since 2026-09-17, so the rungs that cut or
// keep a name are a room's (its way out stands there) and a conversation
// walks past them:
//
//	1  everything, the rider entire
//	2  the name is cut to seat the rider entire
//	3  the rider is shortened to fit
//	4  the thinking rung goes, whole
//	5  the approvals chip goes, whole
//	6  the name is cut for the model alone
//	7  the model goes, leaving a room's way out
//	8  the name goes, leaving only a remote machine's identity
var seamLadder = []seamTry{
	{name: true, model: true, rung: true, gate: true, rider: riderWhole},
	{name: true, model: true, rung: true, gate: true, rider: riderWhole, cut: true},
	{name: true, model: true, rung: true, gate: true, rider: riderFit},
	{name: true, model: true, gate: true, rider: riderFit},
	{name: true, model: true, rider: riderFit},
	{name: true, model: true, cut: true},
	{name: true, cut: true},
	{},
}

// lay draws one rung of the ladder into the room, and reports false when what
// that rung asks for does not fit. The three spans are where the model, the
// rung and the approvals chip fell, measured off the head that was actually
// drawn, so a door is only ever recorded where its cell is.
//
// THE RIDER IS WRITTEN ONTO THE MODEL — `glm-5.3-flash (deepinfra)` — and the
// rung and the gate come after the pair, so the two halves of "who is
// answering" are never separated by a cell about something else (the owner's
// ruling of 2026-09-17).
func (p seamPieces) lay(try seamTry, room int) (string, hudSpan, hudSpan, hudSpan, bool) {
	none := hudSpan{}
	if try.cut && p.name == "" {
		return "", none, none, none, false
	}
	model, rung, gate, rider := "", "", "", ""
	if try.model {
		model = p.model
	}
	if try.rung && model != "" {
		rung = p.rung
	}
	if try.gate && model != "" {
		gate = p.gate
	}
	// A rung that wants the rider entire fails outright when there is none to
	// seat, so the ladder moves on to the rungs that do not ask for it.
	if try.rider == riderWhole && (model == "" || p.rider == "") {
		return "", none, none, none, false
	}
	if try.rider == riderWhole {
		rider = p.rider
	}
	// The name is the one piece with a floor rather than a fixed width: what
	// it has is the room less everything else on the line.
	name := ""
	if try.name {
		name = p.name
		if try.cut {
			rest := dotted(p.host, dotted(seamModelEffort(model+rider, rung), gate))
			left := room - ansi.StringWidth(rest)
			if rest != "" {
				left -= ansi.StringWidth(legendJoin)
			}
			if left < legendNameFloor {
				return "", none, none, none, false
			}
			name = fit(name, left)
		}
	}
	head := dotted(p.host, name)
	if try.rider == riderFit && model != "" {
		bare := dotted(head, dotted(seamModelEffort(model, rung), gate))
		if ansi.StringWidth(bare) > room {
			return "", none, none, none, false
		}
		rider = p.fit(room - ansi.StringWidth(bare))
	}
	cluster := dotted(head, dotted(seamModelEffort(model+rider, rung), gate))
	if ansi.StringWidth(cluster) > room {
		return "", none, none, none, false
	}
	named, dial, chip := seamSpans(head, model, rider, rung, gate)
	return cluster, named, dial, chip, true
}

// seamEffortJoin binds effort to the model without spending a separate badge.
const seamEffortJoin = ":"

func seamModelEffort(model, rung string) string {
	if model == "" || rung == "" {
		return model
	}
	return model + seamEffortJoin + rung
}

// seamSpans is where the model, the rung and the approvals chip stand in a
// cluster that begins with head, or empty spans for a cluster with no model on
// it. Each cell is measured from the one before it, so a cell that was given up
// leaves the next one's columns where they were drawn. The rider rides the
// model and is no door: the rung is measured past it.
func seamSpans(head, model, rider, rung, gate string) (hudSpan, hudSpan, hudSpan) {
	if model == "" {
		return hudSpan{}, hudSpan{}, hudSpan{}
	}
	from := 0
	if head != "" {
		from = ansi.StringWidth(head + legendJoin)
	}
	named := hudSpan{from: from, to: from + ansi.StringWidth(model)}
	at, dial := named.to+ansi.StringWidth(rider), hudSpan{}
	if rung != "" {
		dial = hudSpan{from: at + ansi.StringWidth(seamEffortJoin), to: at + ansi.StringWidth(seamEffortJoin) + ansi.StringWidth(rung)}
		at = dial.to
	}
	chip := hudSpan{}
	if gate != "" {
		chip = hudSpan{from: at + ansi.StringWidth(legendJoin), to: at + ansi.StringWidth(legendJoin) + ansi.StringWidth(gate)}
	}
	return named, dial, chip
}

// legendModelPress is a click on the model's name in the seam, and reports
// whether it took the click. Out of a room the picker it opens moves the
// conversation's model; a room's own door is on its status row
// ([app.statusPress]).
func (a *app) legendModelPress(x, y int) bool {
	if a.copy.on || a.at(pageSettings) || a.pick.open {
		return false
	}
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeLegend || !a.seamModelSpan.holds(x) {
		return false
	}
	// IN A ROOM THE NAME IS THE NODE'S, so the picker it opens moves the node
	// and touches neither the conversation nor any other task (room.go's
	// [app.retargetTask]); the span is recorded only while the node can still
	// be moved ([app.roomSeamDoors]).
	if a.roomOpen() {
		a.openTaskPicker(a.room.id)
		return true
	}
	a.openPickerFromChip()
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
	if a.copy.on || a.at(pageSettings) || a.pick.open {
		return nil, false
	}
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeLegend || !a.seamEffortSpan.holds(x) {
		return nil, false
	}
	// IN A ROOM THE RUNG IS THE NODE'S, and the press is the room's own
	// `alt+e` (taskeffort.go's [app.cycleTaskEffort]).
	if a.roomOpen() {
		a.cycleTaskEffort()
		return nil, true
	}
	return a.cycleEffort(), true
}

// legendApprovalPress is a click on the approvals chip after the rung, and it
// WALKS THE GATE'S WHEEL ONE STOP on the rung's own terms (approvalchip.go):
// one press, one step, with a note describing the resulting posture.
func (a *app) legendApprovalPress(x, y int) (tea.Cmd, bool) {
	if a.copy.on || a.at(pageSettings) || a.pick.open || a.roomOpen() {
		return nil, false
	}
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeLegend || !a.seamApprovalSpan.holds(x) {
		return nil, false
	}
	return a.cycleApproval(), true
}

// paintSpan paints one cluster, LIFTING the span while lift is wanted. The
// pieces are painted separately rather than nested, because these hues are raw
// SGR with an explicit reset (styles.go's [palette.paint]): a colour inside a
// colour would end the outer one at the inner one's reset.
func paintSpan(text string, span hudSpan, paint, lift func(string) string, lifted bool) string {
	return paintSpans(text, paint, spanLift{span: span, lift: lift, on: lifted})
}

// spanLift is one cell of a cluster and the paint it wants while it is lit.
type spanLift struct {
	span hudSpan
	lift func(string) string
	on   bool
}

// paintSpans is [paintSpan] for several cells on one line: the lifted spans are
// painted in the order they stand and the plain runs between them in the
// cluster's own tier, so no hue is ever opened inside another. The spans must
// be disjoint and given left to right, which is how the seam records them
// ([seamSpans]); a span that is off, empty or past the end of the text is
// painted as part of the run around it.
func paintSpans(text string, paint func(string) string, lifts ...spanLift) string {
	width := ansi.StringWidth(text)
	var out strings.Builder
	at := 0
	for _, l := range lifts {
		if !l.on || !l.span.pressable() || l.span.from < at || l.span.to > width {
			continue
		}
		if l.span.from > at {
			out.WriteString(paint(ansi.Cut(text, at, l.span.from)))
		}
		out.WriteString(l.lift(ansi.Cut(text, l.span.from, l.span.to)))
		at = l.span.to
	}
	if at == 0 {
		return paint(text)
	}
	if at < width {
		out.WriteString(paint(ansi.Cut(text, at, width)))
	}
	return out.String()
}
