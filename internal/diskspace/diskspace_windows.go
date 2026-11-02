//go:build windows

package diskspace

import "golang.org/x/sys/windows"

func of(path string) (Usage, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return Usage{}, err
	}
	var free, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(name, &free, &total, &totalFree); err != nil {
		return Usage{}, err
	}
	return Usage{FreeBytes: free, TotalBytes: total}, nil
}
