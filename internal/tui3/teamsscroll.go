package tui3

// Wheel events move terminal rows rather than hopping between controls. The
// pointer chooses the viewport; keyboard focus and team selection stay put.
func (a *app) teamsScrollPane(delta int) {
	a.tp.paneWheel = true
	a.tp.paneOffset = min(max(a.tp.paneOffset+delta, 0), max(a.tp.paneRows-a.tp.paneRoom, 0))
	a.tp.hot = teamsRef{}
	a.touch()
}

func (a *app) teamsScrollRail(delta int) {
	a.tp.railWheel = true
	a.tp.railOffset = min(max(a.tp.railOffset+delta, 0), max(a.tp.railRows-a.tp.railRoom, 0))
	a.tp.hot = teamsRef{}
	a.touch()
}

// A different selection starts at its overview's top. Revisiting the same
// selection keeps the reading position, including one reached with the wheel.
func (a *app) teamsScrollSelection(id string) {
	if id != a.tp.sel {
		a.tp.paneOffset = 0
		a.tp.paneWheel = false
	}
}
