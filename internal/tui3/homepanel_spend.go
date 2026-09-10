package tui3

// spendPanel is `spend` (docs/design/home-mission-control/DESIGN.md §1). Its rows are
// read in the next change; until then the grid draws its heading and whisper.
type spendPanel struct{ homePanelBase }

func (spendPanel) rows(in *homeGridInput) homePanelRows { return homePanelRows{} }
