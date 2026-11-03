//go:build !unix

package main

import "os"

// Verify the final component and its opened identity before any content is read.
func workstreamAgentOpenSourcePath(path string) (*os.File, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if before.Mode()&os.ModeSymlink != 0 {
		return nil, errWorkstreamSourceLink
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if !os.SameFile(before, after) {
		file.Close()
		return nil, os.ErrInvalid
	}
	return file, nil
}
