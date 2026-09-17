//go:build windows

package update

import (
	"os"
	"syscall"
	"unsafe"
)

var replaceFileW = syscall.NewLazyDLL("kernel32.dll").NewProc("ReplaceFileW")

func replaceExecutable(temporary, target string) error {
	return replaceWindowsExecutable(temporary, target, replaceFile)
}

func replaceFile(target, replacement, backup string) error {
	targetPointer, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	replacementPointer, err := syscall.UTF16PtrFromString(replacement)
	if err != nil {
		return err
	}
	backupPointer, err := syscall.UTF16PtrFromString(backup)
	if err != nil {
		return err
	}
	result, _, callErr := replaceFileW.Call(
		uintptr(unsafe.Pointer(targetPointer)),
		uintptr(unsafe.Pointer(replacementPointer)),
		uintptr(unsafe.Pointer(backupPointer)),
		0,
		0,
		0,
	)
	if result != 0 {
		return nil
	}
	if callErr != syscall.Errno(0) {
		return callErr
	}
	return syscall.EINVAL
}

// CleanupOld removes the previous Windows executable when the next process can.
func CleanupOld(target string) { _ = os.Remove(target + ".old") }
