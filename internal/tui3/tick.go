package tui3

import tea "charm.land/bubbletea/v2"

// surfaceTick is the one door through which the surface schedules a delayed
// message. Production keeps Bubble Tea's real clock; the test harness replaces
// the door with its controllable clock so a pure callback costs no wall time.
var surfaceTick = tea.Tick
