//go:build windows

package processgroup

import (
	"fmt"
	"os/exec"
	"strconv"
	"syscall"

	"golang.org/x/sys/windows"
)

func Configure(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

func ConfigureDetached(cmd *exec.Cmd) {
	Configure(cmd)
}

func Configured(cmd *exec.Cmd) bool {
	return cmd.SysProcAttr != nil && cmd.SysProcAttr.CreationFlags&windows.CREATE_NEW_PROCESS_GROUP != 0
}

func Terminate(pid int) error {
	return taskkill(pid, false)
}

func Kill(pid int) error {
	return taskkill(pid, true)
}

func taskkill(pid int, force bool) error {
	if !ProcessAlive(pid) {
		return nil
	}
	args := []string{"/PID", strconv.Itoa(pid), "/T"}
	if force {
		args = append(args, "/F")
	}
	if output, err := exec.Command("taskkill.exe", args...).CombinedOutput(); err != nil {
		if !ProcessAlive(pid) {
			return nil
		}
		return fmt.Errorf("taskkill process group %d: %w: %s", pid, err, output)
	}
	return nil
}

func Alive(pid int) bool {
	return ProcessAlive(pid)
}

func ProcessAlive(pid int) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return err == windows.ERROR_ACCESS_DENIED
	}
	defer windows.CloseHandle(handle)
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	return code == 259 // STILL_ACTIVE
}

func ReapExitedChild(pid int) bool {
	return !ProcessAlive(pid)
}

func SetPriority(pid, _ int) {
	handle, err := windows.OpenProcess(windows.PROCESS_SET_INFORMATION, false, uint32(pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(handle)
	_ = windows.SetPriorityClass(handle, windows.BELOW_NORMAL_PRIORITY_CLASS)
}
