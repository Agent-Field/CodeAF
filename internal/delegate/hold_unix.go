//go:build !windows

package delegate

import (
	"strconv"
	"syscall"

	"github.com/Agent-Field/codeaf/internal/env"
)

// keepHold keeps the hold on its folder that the program's host handed it
// ([HoldEnv]) for as long as this process lives, and for no process it starts.
//
// IT IS NEVER CLOSED, and never made an *os.File, whose finalizer would close
// it: the kernel lets it go when this process exits, which is the point. And
// it is closed on exec, because a descriptor handed down is not, and a dev
// server the program's shell left running would otherwise hold the folder for
// as long as it ran.
func keepHold() {
	fd, err := strconv.Atoi(env.Get(HoldEnv))
	if err != nil || fd < 3 {
		return
	}
	syscall.CloseOnExec(fd)
}
