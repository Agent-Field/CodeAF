//go:build !windows

package main

import "os"

// appSyncRoot asks the disk to keep root's names.
func appSyncRoot(root *os.Root) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	err = dir.Sync()
	if closeErr := dir.Close(); err == nil {
		err = closeErr
	}
	return err
}
