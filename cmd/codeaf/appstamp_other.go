//go:build !linux && !darwin

package main

import "os"

// Unknown identity or change time cannot certify a cached reading.
func appFileIdentity(os.FileInfo) (uint64, uint64, int64, bool) { return 0, 0, 0, false }

func openAppRestFile(path string) (*os.File, error) { return os.Open(path) }
