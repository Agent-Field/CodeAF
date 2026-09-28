//go:build !linux

package processgroup

const RunMarkerEnv = "CODEAF_DELEGATE_RUN"

func EnableSubreaper() error { return nil }
func CleanupDescendants()    {}

func CleanupRun(string) {}
