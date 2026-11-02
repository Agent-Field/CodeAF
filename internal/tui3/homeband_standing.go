package tui3

// ── THE STANDING STRIP: THE BANDS THAT BELONG TO HOME, NOT TO A CARD ────────
//
// The rescue prompt, the devices row and the add-another-device card are about
// the person's fleet, not about the row under the cursor. They stand at the
// foot of the resting home, drawn whatever the cursor is on and whether or not
// home has any row at all (a remote chat's row has no card; an empty home has
// no rows). Each declares itself `standing` and is in no card.

import "strings"

// standingStrip is a blank row and the standing bands, indented as the panels
// are. The strip is absent where it would take more than half of the room, and
// a band that does not fit is dropped whole from the bottom, so the one nearest
// the grid is the most decision-relevant.
func (a *app) standingStrip(width, room int) []placeRow {
	inner := width - homeGridMargin
	if inner < 1 {
		return nil
	}
	ctx := bandContext{width: inner, pal: a.pal, now: a.now()}
	if a.pair.open {
		// The modal card must remain visible even when other standing bands fill
		// the small terminal. Its keys belong to the same visible surface.
		lines := a.pair.draw(inner, max(1, room-3), a.now(), a.pal)
		lines = append(lines, a.pal.dim(a.pair.hint()))
		return standingRows(lines)
	}
	var lines []string
	var targets []addMachineTarget
	for _, band := range standingBands() {
		card := band.draw(a, ctx)
		if len(card) == 0 {
			continue
		}
		limit := room / 2
		// An opened form needs its input on screen even in a short terminal.
		if band.name == "addmachine" && (a.addMachine.open || a.pair.open) {
			limit = room
		}
		if len(lines)+len(card)+2 > limit {
			break
		}
		for i := range card {
			target := addMachineNone
			if band.name == "addmachine" && !a.pair.open {
				target = addMachineToggle
				if a.addMachine.open && i == len(card)-2 {
					target = addMachineInput
				}
			}
			targets = append(targets, target)
		}
		lines = append(lines, card...)
	}
	rows := standingRows(lines)
	for i, target := range targets {
		mark := rows[i+1].hit.(homeMark)
		mark.addMachine = target
		rows[i+1].hit = mark
	}
	return rows
}

func standingRows(lines []string) []placeRow {
	if len(lines) == 0 {
		return nil
	}
	mark := homeMark{line: -1, pane: -1}
	rows := []placeRow{{hit: mark}}
	for _, text := range lines {
		rows = append(rows, placeRow{text: strings.Repeat(" ", homeGridMargin) + text, hit: mark})
	}
	return rows
}
