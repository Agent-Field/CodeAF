//go:build !windows

package processgroup

import (
	"errors"
	"os/exec"
	"syscall"
)

func Configure(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func ConfigureDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func Configured(cmd *exec.Cmd) bool {
	return cmd.SysProcAttr != nil && (cmd.SysProcAttr.Setpgid || cmd.SysProcAttr.Setsid)
}

func Terminate(pid int) error {
	return groupSignal(pid, syscall.SIGTERM)
}

func Kill(pid int) error {
	return groupSignal(pid, syscall.SIGKILL)
}

func groupSignal(pid int, signal syscall.Signal) error {
	groupErr := syscall.Kill(-pid, signal)
	if groupErr == nil || errors.Is(groupErr, syscall.ESRCH) {
		return nil
	}
	leaderErr := syscall.Kill(pid, signal)
	if leaderErr == nil || errors.Is(leaderErr, syscall.ESRCH) {
		return nil
	}
	return leaderErr
}

func Alive(pid int) bool {
	err := syscall.Kill(-pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func ProcessAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func ReapExitedChild(pid int) bool {
	var status syscall.WaitStatus
	waited, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
	return err == nil && waited == pid
}

func SetPriority(pid, priority int) {
	_ = syscall.Setpriority(syscall.PRIO_PGRP, pid, priority)
}
