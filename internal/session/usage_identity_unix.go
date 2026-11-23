//go:build !windows

package session

import (
	"os"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

func lockUsageIdentityWriter(writer *os.File) error { return filelock.Lock(writer, true, false) }
func unlockUsageIdentityWriter(writer *os.File)     { _ = filelock.Unlock(writer) }

func lockUsageIdentityReader(*os.File) error { return nil }
func unlockUsageIdentityReader(*os.File)     {}
