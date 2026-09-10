package tui3

// leftPanel is `since you left`: what watches, tasks and memory did while the
// terminal was shut, newest first — the switcher's own ledger lines
// ([switcherReading.addLedger]), each a door into the place that owns it.
//
// OWED: lane P — a line per landed task with its outcome and cost, and a line
// per file made (DESIGN §3 P3).
type leftPanel struct{ homePanelBase }

func (leftPanel) rows(in *homeGridInput) homePanelRows {
	var lines []homeLine
	for _, row := range in.ledger {
		lines = append(lines, homeLine{kind: homeLedger, project: row.place, dir: row.title,
			view: row.item, item: row.item.Item, cell: &homeCell{panel: panelLeft, title: row.title}})
	}
	out := homePanelRows{lines: lines}
	if len(lines) > 0 {
		out.said = sinceAt(in.seen, in.now)
	}
	return out
}
