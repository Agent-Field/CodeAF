//go:build linux || darwin

package blobstore

import (
	"io/fs"
	"syscall"
)

func censusAllocated(info fs.FileInfo) censusAllocation {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return censusAllocation{}
	}
	return censusAllocation{identity: censusInode{device: uint64(stat.Dev), inode: uint64(stat.Ino)}, bytes: stat.Blocks * 512, known: true}
}
