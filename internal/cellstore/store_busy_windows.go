//go:build windows

package cellstore

import (
	"context"
	"golang.org/x/sys/windows"
)

const windowsStillActive = 259

func storeProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	return code == windowsStillActive
}

func pidServesSocket(context.Context, int, string) bool { return false }

func describeProcess(context.Context, int) string { return "" }

func lockHolderFromOS(context.Context, string) StoreHolder { return StoreHolder{} }
