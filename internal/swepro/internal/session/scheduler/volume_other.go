//go:build !(linux || darwin || freebsd || netbsd || openbsd || dragonfly)

// aforge-embed: D6 — the volume measurement behind the disk-floor cap.
package scheduler

// volumeTotalBytes cannot measure a volume here, so the ported floor stands
// exactly as resourceguard computed it. An unmeasurable volume is never an
// argument for lowering a safety floor.
func volumeTotalBytes(string) (float64, bool) { return 0, false }
