//go:build !linux

package processgroup

const RunMarkerEnv = "CODEAF_DELEGATE_RUN"

func EnableSubreaper() {}

func CleanupRun(string) {}
