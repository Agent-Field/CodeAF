//go:build windows

package cell

import (
	"errors"
	"strconv"

	"golang.org/x/sys/windows"
)

// platformProcessStatus opens the process, which on Windows is the only honest
// probe: signal 0 is not supported there, so the old check read EVERY owner as
// dead and let another process run tools on files that were still arriving.
//
// A pid nobody holds a handle to any more cannot be opened (ERROR_INVALID_PARAMETER),
// and a process that has exited while someone still holds a handle to it is
// signalled, so both are proof of a death. An open that is refused, or any
// answer this does not understand, cannot tell, so the owner is taken as alive
// and the stall bound decides. The creation time is the start token, so a pid
// handed out again is not mistaken for the owner.
func platformProcessStatus(pid int) (startToken string, alive bool) {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return "", false
		}
		return "", true
	}
	defer windows.CloseHandle(handle)
	if event, waitErr := windows.WaitForSingleObject(handle, 0); waitErr == nil && event == windows.WAIT_OBJECT_0 {
		return "", false
	}
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &created, &exited, &kernel, &user); err != nil {
		return "", true
	}
	return strconv.FormatInt(created.Nanoseconds(), 10), true
}
