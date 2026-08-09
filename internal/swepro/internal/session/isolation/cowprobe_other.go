//go:build !linux

// Conservative non-Linux fallback for src/session/isolation.ts:138-159.
package isolation

import (
	"errors"
	"os"
)

func cloneFile(_, _ *os.File) error {
	return errors.New("FICLONE unavailable")
}
