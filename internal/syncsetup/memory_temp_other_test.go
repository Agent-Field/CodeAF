//go:build !linux

package syncsetup

// memoryTempBase answers nothing off Linux: macOS and Windows have no tmpfs at a
// fixed path, so the package keeps the ordinary temporary directory there.
func memoryTempBase() (string, bool) { return "", false }
