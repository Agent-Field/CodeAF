//go:build darwin

package executor

import "os/exec"

// confineFence wraps cmd in sandbox-exec with the fence's profile, passed
// inline so nothing is written to disk. Where sandbox-exec cannot apply a
// profile the command runs as it is, which [FenceLimitLine] says once, unless
// the fence is required ([Fence.Require]), when it is refused instead.
func confineFence(cmd *exec.Cmd, f *Fence) error {
	if cmd.Err != nil {
		return cmd.Err
	}
	if !seatbeltWorks() {
		if f.Require {
			return ErrFenceRequired
		}
		return nil
	}
	cmd.Args = append([]string{sandboxExec, "-p", fenceProfile(f), cmd.Path}, cmd.Args[1:]...)
	cmd.Path = sandboxExec
	return nil
}

func fenceLimit() string {
	if seatbeltWorks() {
		return ""
	}
	return FenceDegradedLine
}
