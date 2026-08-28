//go:build windows

package enginehost

import "os/exec"

// detach is nothing on Windows, and saying nothing is the honest version.
// There is no setsid and no process group teardown of the unix kind here; a
// child started this way already outlives its parent. The door exists so the
// caller does not have to know which platform it is on.
func detach(command *exec.Cmd) {}
