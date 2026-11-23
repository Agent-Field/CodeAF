//go:build !linux && !darwin && !windows

package cell

// platformProcessStatus has only signal 0 on the other Unixes. It cannot give a
// start time, so a recycled pid is told apart from the owner by the stall bound.
func platformProcessStatus(pid int) (startToken string, alive bool) {
	return "", signalAlive(pid)
}
