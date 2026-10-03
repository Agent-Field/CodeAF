package cell

import (
	"path/filepath"
	"strings"
)

// TaskDropping is the one directory under a task's working copy that is the
// harness's and never the person's work: job logs, saved pictures a session
// keeps for itself, anything this program leaves behind while the task works.
// It lives here, below every package that sorts a copy's files into the
// person's and ours, because each of them has to agree about it and a
// disagreement would carry a log to another computer as a change the person
// never made.
const TaskDropping = ".codeaf"

// legacyTaskDropping is the folder an older build kept the same machinery in.
const legacyTaskDropping = ".aforge-v3" // legacy-name

// TaskDroppings is the one list of repository-local task machinery a reader,
// cleaner, carrier or index builder must recognise. Writes keep using
// [TaskDropping] because live worktree registrations cannot be moved.
func TaskDroppings() []string { return []string{TaskDropping, legacyTaskDropping} }

// IsTaskDropping reports whether path, relative to a task's working copy, is a
// dropping folder or anything inside one.
func IsTaskDropping(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
	for _, name := range TaskDroppings() {
		if clean == name || strings.HasPrefix(clean, name+"/") {
			return true
		}
	}
	return false
}
