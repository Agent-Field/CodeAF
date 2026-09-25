package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ── THE FOOT'S TWO ROWS SWAP THEIR RIGHT-HAND JOBS ──────────────────────────
//
// Until 2026-09-17 the seam over the box carried the keys that work right now
// on its right, and the row under the box carried the numbers:
//
//	─ porting the parser · glm-5.3-flash:high · ◇ asks ──── space space home · / commands ─
//	 › your sentence
//	 $0.27 · ⟲ saved $0.0038 · 58% cached   66.8k/1.3M · 5%             38 tok/s · ⠹ working · 12s
//
// Home's foot was the other way round — its chords on the seam, its hint on the
// last row — and the two boxes a person moves between most looked like two
// different programs. The owner ruled that THE LOWEST LINE IS FOR KEYS on both,
// and that the telemetry goes up onto the seam where the chords were:
//
//	─ glm-5.3-flash (deepinfra):high · ◇ asks ── $0.27 · 58% cached   66.8k/1.3M · 5%   ⠹ working · 12s ─
//	 › your sentence
//	 alt+e effort · alt+a approvals · alt+k chats · / commands · space space home
//
//	─ glm-5.3-flash:auto · ◇ asks ───────────────── project: ~/codeaf ─
//	 › type to search or start something new
//	 alt+p project · alt+e effort · alt+a approvals · / commands
//
// THE SEAM IS WHAT ANSWERS AND HOW MUCH. Everything on it is a fact about the
// conversation — its model and the machine serving it, its rung, its gate,
// and now its bill, its meter and what it is doing (the name came off it the
// same day: foot.go) — and three of its cells are still doors: the
// model, the rung and the gate on the left (foot.go), the bill and the meter on
// the right (the same two doors they were on the row below, recorded on the
// seam's own row under [legendDoorRow]). The telemetry keeps every law it had:
// the age fade, the money door's warning hue, the narrow ladder ([dropOrder])
// — only the row it is drawn on has changed.
//
// THE LAST ROW IS THE KEYS AND NOTHING ELSE. What used to be the seam's right
// slot — the state's own keys, the chord diagnosis, the earned tip, the four
// idle doors ([app.footHint]) — is the whole of the row now, left-aligned one
// cell in, exactly as a place's foot draws its hint (pages.go's [placeFrame]).
// The home door is pressable there ([app.homeDoorPress]).
//
// TWO FRAMES KEEP THE OLD SHAPE, on purpose. At the PHONE tier the row is a
// two-row deck and the seam has no room for a number (statusdeck.go), so the
// keys stay on the seam and the deck stays at the foot. And on a frame WITH NO
// SEAM — the greeting, or a window too short for the rule ([app.seamShowing])
// — the last row carries the keys on the left and the right edge's aliveness
// on the right, because the state word is the one fact a frame may never lose.

// legendDoorRow is the row a door on the SEAM is recorded under ([statusDoor.row]),
// beside the status row's own indices, so [app.doorAt] can tell the two apart.
const legendDoorRow = -1

// seamShowing reports whether this frame draws the seam at all: the rule needs
// a row of clearance, and the greeting stands where the rule would be
// (view.go's [app.chrome]).
func (a *app) seamShowing() bool {
	return a.footClearance() > 0 && a.welcomeHeight() == 0
}

// seamCarriesTelemetry reports whether the numbers are on the seam this frame
// — every tier but the phone's.
func (a *app) seamCarriesTelemetry() bool {
	width, _ := a.size()
	return layoutTier(width) != tierPhone
}

// hintRowKind is the chrome row the keys are drawn on, for the doors that
// resolve a press against it: the last row, or the seam at phone width.
func (a *app) hintRowKind() chromeKind {
	if a.seamCarriesTelemetry() {
		return chromeStatus
	}
	return chromeLegend
}

// seamRung is one rung of the seam's right-hand ladder: the label as measured,
// and how many steps down the ladder built it, so the runs it was built from
// can be had again for the one rung that is painted ([app.seamRungParts]).
//
// THE RUNG CARRIES NO PAINT AND NO COPY OF THE RUNS. The ladder is climbed on
// every frame, and painting six rungs to draw one — two builders and a copy
// of each run per rung — was fifty allocations on a line rebuilt while the
// screen scrolls, which the scroll's own law counts (inputsmooth_test.go's
// [TestOneScreenScrollOfFourThousandLinesStaysInsideTheAllocationLaw]).
type seamRung struct {
	// plain is the label of a rung that is a HINT (the phone tier's keys);
	// a telemetry rung keeps only its width and is spelled when drawn.
	plain string
	width int
	steps int
	// cheap reports that the rung has spent only the cheap end of [dropOrder]
	// — the jobs, the forecast, the cache's cash half, the rate — and still
	// carries every dear segment the full set had: the cache, the bill and
	// the meter. THE RIDER OUTRANKS THE CHEAP NUMBERS AND NOTHING DEARER:
	// [app.legend]'s rider tier stops at the last cheap rung.
	cheap bool
}

// dearKinds are the segments the rider may not be kept at the cost of.
var dearKinds = [...]hudSeg{segCache, segCost, segCtx}

// carriesAll reports whether a run still holds every one of the named kinds.
func carriesAll(parts []hudPart, kinds []hudSeg) bool {
	for _, kind := range kinds {
		found := false
		for _, part := range parts {
			if part.kind == kind {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// seamParts is the telemetry as the seam takes it: assembled once, the quiet
// frame's bill and meter taken out, and the change clocks stamped from the
// full set before any width pressure is applied ([app.freshen]'s own law),
// so a number that moved has moved whether or not this frame had room to say
// so.
func (a *app) seamParts(width int) []hudPart {
	parts := a.telemetry(width)
	if _, up := a.questionDialog(width); up {
		// The visible decision already says what is waiting and who must answer.
		dropKind(&parts, segQuestions)
		dropKind(&parts, segState)
	}
	if a.statusQuiet() {
		parts = quietParts(parts)
	}
	a.freshen(parts)
	return parts
}

// seamTelemetryRungs is the numbers said at every width the seam might have,
// fullest first, each rung one step down [app.seamShrink] from the one before
// it. The parts they were built from are returned with them, for the paint.
func (a *app) seamTelemetryRungs(width int) ([]seamRung, []hudPart) {
	parts := a.seamParts(width)
	ledger, alive := lineParts(parts)
	var dear [len(dearKinds)]hudSeg
	n := 0
	for _, kind := range dearKinds {
		if carriesAll(ledger, []hudSeg{kind}) {
			dear[n] = kind
			n++
		}
	}
	rungs := make([]seamRung, 0, len(dropOrder)+4)
	for steps := 0; ; steps++ {
		cells := seamLabelWidth(ledger, alive)
		if cells == 0 {
			break
		}
		rungs = append(rungs, seamRung{width: cells, steps: steps, cheap: carriesAll(ledger, dear[:n])})
		if !a.seamShrink(&ledger, &alive) {
			break
		}
	}
	return rungs, parts
}

// seamShrink takes one rung off the two runs and reports whether it found
// one: the status row's own ladder first ([dropOrder]), and below it THE
// SEGMENTS THE STATUS ROW NEVER DROPPED — the link, the posture and the
// question chip — one at a time, because the row had a second row to wrap
// onto and the seam has none: the state word must be the last thing standing
// on the right.
func (a *app) seamShrink(ledger, alive *[]hudPart) bool {
	if a.shrink(ledger, alive, len(dropOrder)) {
		return true
	}
	return dropKind(alive, segLink) || dropKind(ledger, segYolo) || dropKind(alive, segQuestions)
}

// seamRungParts is one rung's two runs, built again from the parts the ladder
// was climbed on — for the one rung the frame paints and records doors along.
func (a *app) seamRungParts(parts []hudPart, steps int) ([]hudPart, []hudPart) {
	ledger, alive := lineParts(parts)
	for i := 0; i < steps; i++ {
		a.seamShrink(&ledger, &alive)
	}
	return ledger, alive
}

// seamLabelWidth is how many cells one rung's label takes, which is all a
// width decision needs: the ledger, the gap the status row kept between its
// clusters, and the right edge. No string is built for a rung not drawn.
func seamLabelWidth(ledger, alive []hudPart) int {
	cells := hudWidth(ledger) + hudWidth(alive)
	if len(ledger) > 0 && len(alive) > 0 {
		cells += hudGap
	}
	return cells
}

// seamTelemetryLabel is one rung's label painted (and plain beside it): the
// ledger, the gap, and the right edge, each run through the status row's own
// painter so the age fade and the doors' lifts are the ones they always were.
func (a *app) seamTelemetryLabel(ledger, alive []hudPart) (string, string) {
	ledgerPainted, ledgerPlain := a.paintParts(ledger)
	alivePainted, alivePlain := a.paintParts(alive)
	switch {
	case ledgerPlain == "":
		return alivePainted, alivePlain
	case alivePlain == "":
		return ledgerPainted, ledgerPlain
	}
	return ledgerPainted + groupGapRun + alivePainted, ledgerPlain + groupGapRun + alivePlain
}

// hintRow is the last row of a conversation's frame: the keys, one cell in,
// in the payload grammar every hint on this surface is painted in. On a frame
// with no seam the right edge's aliveness rides the same row's right, and the
// keys give up their clauses before the state word gives up anything.
//
// AND THE PROJECT IS AT ITS RIGHT END, since 2026-09-22, exactly as it is on
// home's keys row (hometip.go's [app.homeFootLine]): right-justified in what
// the keys leave, cut on the right where they leave it too little, gone
// where they leave it less than a word. It came down off the seam so the
// two feet a person moves between most read the same way, and it is still
// a door, onto the folder chooser ([app.seamProjectPress]), so its columns
// are recorded here, as the row is laid out ([app.seamProjectSpan]).
//
// THE DOCK IS LAID OUT LAST, in what the keys and the right end left
// (walldock.go): it narrows to fewer cells, then goes, before the keys lose a
// clause or the project a cell. While the pointer rests on it, the keys' own
// cells say what is under the pointer instead, and the dock does not move,
// because it was fitted against the keys and not against the words standing
// in for them.
func (a *app) hintRow(width int) string {
	a.homeDoor = hudSpan{}
	a.seamProjectSpan = hudSpan{}
	a.dockClear()
	hint := a.footHint(width)
	right, rightPlain := "", ""
	if !a.seamShowing() {
		right, rightPlain = a.seamAliveLabel(width)
	}
	warning := a.chatCreditWarning
	if layoutTier(width) == tierPhone || ansi.StringWidth(warning)+ansi.StringWidth(rightPlain)+3 > width {
		warning = ""
	}
	room := width - 1
	if rightPlain != "" {
		room -= ansi.StringWidth(rightPlain) + hudGap
	}
	if warning != "" {
		room -= ansi.StringWidth(warning) + 1
	}
	for hint != "" && ansi.StringWidth(hint) > room {
		hint = a.hintShorter(hint)
	}
	if room > 0 && hint != "" {
		hint = fit(hint, room)
	} else {
		hint = ""
	}
	keys := ansi.StringWidth(hint)
	if keys > 0 {
		keys++
	}
	// THE LOW-CREDIT LINE STANDS JUST LEFT OF WHATEVER HOLDS THE RIGHT EDGE
	// (credits.go, #1439): the aliveness on a frame with no seam, the project on
	// every other. It is drawn whole or not at all, so the project is fitted to
	// what the keys AND the line leave, and gives way before the line does.
	// The right end is settled first, as one run starting at column from, so
	// the dock can be fitted into what is left between it and the keys.
	warned := ""
	if warning != "" {
		warned = a.pal.warn(warning)
	}
	tail, from := "", width
	switch {
	case rightPlain != "":
		tail, from = right, width-ansi.StringWidth(rightPlain)
		if warned != "" {
			tail = warned + strings.Repeat(" ", hudGap) + tail
			from -= ansi.StringWidth(warning) + hudGap
		}
	default:
		before := keys
		if warned != "" {
			before += 1 + ansi.StringWidth(warning)
		}
		// THE PROJECT, in what the keys leave, never inside a room, whose page
		// carries the node's own identity (roomseam.go).
		if project := a.seamProjectWord(); project != "" && !a.roomOpen() {
			if text, span, ok := projectAtRight(project, before, width); ok {
				a.seamProjectSpan = span
				tail = a.paintSeamProject(text,
					hudSpan{from: ansi.StringWidth(targetProjectLead), to: ansi.StringWidth(text)},
					a.hot.kind == hoverSeamProject) + " "
				from = width - 1 - ansi.StringWidth(text)
				if warned != "" {
					tail = warned + strings.Repeat(" ", hudGap) + tail
					from -= ansi.StringWidth(warning) + hudGap
				}
				break
			}
		}
		if warned != "" {
			tail, from = warned+" ", width-1-ansi.StringWidth(warning)
		}
	}
	// The dock finishes one cell short of the frame's edge, the keys' inset
	// mirrored, or a gap short of whatever holds the right end.
	end := width - 1
	if tail != "" {
		end = from - hudGap
	}
	dock, dockW := a.dockRow(width, end, ansi.StringWidth(hint))
	paint := func(s string) string { return paintHint(s, a.pal, a.pal.dim) }
	// THE WORDS UNDER THE POINTER ARE THE HINT LINE'S OWN DIM, with the key
	// they name stepped up as every key on this line is: they stand in for the
	// keys, so they are painted as the keys are rather than louder than them.
	if words := a.dockHoverWords(); words != "" {
		hint = fit(words, max(0, end-1-dockW-hudGap))
	} else if offset := strings.Index(hint, homeDoorWord); offset >= 0 {
		at := 1 + ansi.StringWidth(hint[:offset])
		a.homeDoor = hudSpan{from: at, to: at + ansi.StringWidth(homeDoorWord)}
	}
	// THE ROW FILLS THE FRAME, as every foot row does: a row shorter than the
	// frame would leave the cells behind it to whatever the last frame drew.
	line := ""
	if hint != "" {
		line = " " + paint(hint)
	}
	used := ansi.StringWidth(hint)
	if used > 0 {
		used++
	}
	if dockW > 0 {
		line += strings.Repeat(" ", max(0, end-dockW-used)) + dock
		used = end
	}
	if tail != "" {
		return line + strings.Repeat(" ", max(1, from-used)) + tail
	}
	return line + strings.Repeat(" ", max(0, width-used))
}

// seamAliveLabel is the right edge alone — the rate and the state word — for
// the frames that have no seam to put the whole ledger on. The rate goes
// before the state word does, as it does on every other ladder.
func (a *app) seamAliveLabel(width int) (string, string) {
	_, alive := lineParts(a.seamParts(width))
	painted, plain := a.paintParts(alive)
	for ansi.StringWidth(plain) > width-1 && dropKind(&alive, segRate) {
		painted, plain = a.paintParts(alive)
	}
	return painted, plain
}
