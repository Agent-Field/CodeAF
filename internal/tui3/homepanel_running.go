package tui3

// runningPanel is `running` (docs/design/home-mission-control/DESIGN.md §1). Its rows are
// read in the next change; until then the grid draws its heading and whisper.
type runningPanel struct{ homePanelBase }

func (runningPanel) rows(in *homeGridInput) homePanelRows { return homePanelRows{} }
