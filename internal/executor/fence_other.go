//go:build !linux && !darwin

package executor

import "os/exec"

// confineFence leaves the command as it is: this system has no jail, which
// [FenceLimitLine] says once.
func confineFence(*exec.Cmd, *Fence) error { return nil }

func fenceLimit() string { return FenceDegradedLine }
