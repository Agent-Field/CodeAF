package tui3

// needsPanel is `needs you` (docs/design/home-mission-control/DESIGN.md §1). Its rows are
// read in the next change; until then the grid draws its heading and whisper.
type needsPanel struct{ homePanelBase }

func (needsPanel) rows(in *homeGridInput) homePanelRows { return homePanelRows{} }
