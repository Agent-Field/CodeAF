//go:build !linux && !darwin

package executor

import "os/exec"

// confineFence leaves the command as it is: this system has no jail, which
// [FenceLimitLine] says once. A required fence ([Fence.Require]) has nothing
// to be applied with here, so its command is refused instead.
func confineFence(_ *exec.Cmd, f *Fence) error {
	if f.Require {
		return ErrFenceRequired
	}
	return nil
}

func fenceLimit() string { return FenceDegradedLine }
