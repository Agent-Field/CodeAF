package tui3

// runningPanel is `running`: every conversation with work out and every watch
// that is firing, the busiest first — title, how long it has been going, and
// what it is doing now on the line under it.
//
// THE FIRST ROW WEARS THE ONE MOVING CELL (law 8, homespinner.go's
// [homeView.spinAt]) and no other row wears a mark at all.
//
// OWED: lane P — the activity line, `done of total` and background jobs arrive
// with the engine's presence fields (DESIGN §3 P2, E1).
type runningPanel struct{ homePanelBase }

func (runningPanel) rows(in *homeGridInput) homePanelRows {
	var lines []homeLine
	for _, row := range in.rows {
		if !row.moving {
			continue
		}
		cell := &homeCell{panel: panelRunning, title: row.title, right: sinceAt(row.at, in.now), sub: row.note}
		if cell.sub == "" && row.kind == switcherConversation {
			cell.sub = tabSignalWord(tabWorking)
		}
		if len(lines) == 0 {
			cell.mark = cellMarkSpin
		}
		lines = append(lines, switcherRowLine(row, cell))
	}
	return homePanelRows{lines: lines, said: countWord(len(lines))}
}
