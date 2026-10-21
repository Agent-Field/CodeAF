package handoff

import "time"

// setSymlinkTime declines this optional seed on Windows. Chat movement is
// absent there, and touching a link's target would change the original tree.
func setSymlinkTime(string, time.Time) bool { return false }
