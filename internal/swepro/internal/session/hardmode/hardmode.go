// Package hardmode ports src/session/hard-mode.ts lines 1-36 from swe-pro
// commit 3b25a1a. It is the single live-read and single write path for the
// user's exact-string hard-mode opt-in.
package hardmode

import "os"

const (
	hardEnv             = "CODEAF_HARD"
	escalationReasonEnv = "CODEAF_HARD_ESCALATION_REASON"

	// DefaultAuditFixCycles is the normal audit-fix cycle cap.
	DefaultAuditFixCycles = 2
	// HardAuditFixCycles is the hard-mode audit-fix cycle cap.
	HardAuditFixCycles = 4
)

// IsHardMode reads the environment live. Only the exact, case-sensitive
// one-character string "1" enables hard mode.
func IsHardMode() bool {
	return os.Getenv(hardEnv) == "1"
}

// EnableHardMode flips the process into hard mode and records the first
// triggering reason. Once CODEAF_HARD is exactly "1", later calls are no-ops
// and preserve the existing reason.
func EnableHardMode(reason string) {
	if os.Getenv(hardEnv) == "1" {
		return
	}
	_ = os.Setenv(hardEnv, "1")
	_ = os.Setenv(escalationReasonEnv, reason)
}
