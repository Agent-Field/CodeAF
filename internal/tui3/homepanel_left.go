package tui3

// leftPanel is `since you left` (docs/design/home-mission-control/DESIGN.md §1). Its rows are
// read in the next change; until then the grid draws its heading and whisper.
type leftPanel struct{ homePanelBase }

func (leftPanel) rows(in *homeGridInput) homePanelRows { return homePanelRows{} }
