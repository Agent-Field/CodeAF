//go:build windows

package run

import (
	"os"

	"github.com/Agent-Field/codeaf/internal/processgroup"
)

// C10 uses the native process probe when compiling Windows check tests.
func killZero(pid int) error {
	if processgroup.ProcessAlive(pid) {
		return nil
	}
	return os.ErrProcessDone
}
