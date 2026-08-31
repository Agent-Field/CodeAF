package config

// The foreground-command handoff is a session setting rather than a tool
// timeout: it decides when the conversation moves on while the same process
// keeps running. Zero deliberately leaves the older timeout-only posture.
const (
	KeyBashBackgroundAfter     = "bash.background_after_seconds"
	DefaultBashBackgroundAfter = 30
	BashBackgroundAfterHint    = "seconds a foreground command runs before it is kept running as a background job and the chat moves on. 0 waits for the command's own timeout. A change lands on the next session."
)

// BashBackgroundAfterAt resolves the foreground handoff clock in seconds.
// A persisted zero is the person's answer, not an absent value.
func BashBackgroundAfterAt(profileDir string) int {
	if value, ok := persistedInt(profileDir, KeyBashBackgroundAfter); ok && value >= 0 {
		return value
	}
	return DefaultBashBackgroundAfter
}
