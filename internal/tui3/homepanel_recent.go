package tui3

// recentPanel is `where you were` (docs/design/home-mission-control/DESIGN.md §1). Its rows are
// read in the next change; until then the grid draws its heading and whisper.
type recentPanel struct{ homePanelBase }

func (recentPanel) rows(in *homeGridInput) homePanelRows { return homePanelRows{} }
