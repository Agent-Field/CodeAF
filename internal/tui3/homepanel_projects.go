package tui3

// projectsPanel is `projects` (docs/design/home-mission-control/DESIGN.md §1). Its rows are
// read in the next change; until then the grid draws its heading and whisper.
type projectsPanel struct{ homePanelBase }

func (projectsPanel) rows(in *homeGridInput) homePanelRows { return homePanelRows{} }
