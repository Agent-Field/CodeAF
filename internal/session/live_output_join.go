package session

// WantLiveToolOutput is a window that joined this conversation asking for a
// shell call's output as it arrives, the way the desktop app shows it. It
// holds until release is called, which the window's leaving does. Every
// window sees the output while one wants it: the events go to the
// conversation's one hub, and a terminal draws them as the command's row
// filling in.
func (a *Agent) WantLiveToolOutput() (release func()) {
	a.liveOutputWanted.Add(1)
	var once bool
	return func() {
		if once {
			return
		}
		once = true
		a.liveOutputWanted.Add(-1)
	}
}
