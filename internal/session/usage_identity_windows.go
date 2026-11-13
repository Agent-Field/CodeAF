package session

import (
	"os"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

func lockUsageIdentityWriter(*os.File) error { return nil }
func unlockUsageIdentityWriter(*os.File)     {}

// Append-only handles lack the data access LockFileEx requires.
func lockUsageIdentityReader(reader *os.File) error { return filelock.Lock(reader, true, false) }
func unlockUsageIdentityReader(reader *os.File)     { _ = filelock.Unlock(reader) }
