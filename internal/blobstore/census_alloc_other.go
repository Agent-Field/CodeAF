//go:build !linux && !darwin

package blobstore

import "io/fs"

func censusAllocated(fs.FileInfo) censusAllocation { return censusAllocation{} }
