package cellstore

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// SavingStuckError means a storage process owns the lock but cannot be reached.
type SavingStuckError struct{ Socket string }

func (e *SavingStuckError) Error() string {
	return fmt.Sprintf("saving is stuck; your turns stay on this computer, and saving resumes on its own within %s minutes", idleMinutes)
}

func daemonServesSocket(uid, ownUID string, argv []string, socket string) bool {
	if uid != ownUID || len(argv) < 4 || argv[1] != "serve" {
		return false
	}
	name := filepath.Base(argv[0])
	if !storageProgramName.MatchString(name) {
		return false
	}
	found := false
	for i := 2; i < len(argv); i++ {
		if argv[i] == "--socket" {
			if found || i+1 >= len(argv) || argv[i+1] != socket {
				return false
			}
			found = true
			i++
		} else if strings.HasPrefix(argv[i], "--socket=") {
			return false
		}
	}
	return found
}

var storageProgramName = regexp.MustCompile(`^furrow(?:-[0-9]+\.[0-9]+\.[0-9]+-[0-9a-f]+)?$`)
