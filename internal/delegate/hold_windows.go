//go:build windows

package delegate

// keepHold has nothing to keep on a machine whose launches hand no hold
// ([Launch.Hold]).
func keepHold() {}
