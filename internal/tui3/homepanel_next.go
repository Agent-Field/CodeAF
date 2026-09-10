package tui3

// nextPanel is `next up` (docs/design/home-mission-control/DESIGN.md §1). Its rows are
// read in the next change; until then the grid draws its heading and whisper.
type nextPanel struct{ homePanelBase }

func (nextPanel) rows(in *homeGridInput) homePanelRows { return homePanelRows{} }
