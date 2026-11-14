//go:build linux

package syncsetup

import "syscall"

// tmpfsMagic is the filesystem type statfs reports for tmpfs (linux/magic.h).
const tmpfsMagic = 0x01021994

// memoryTempMinFree is the room /dev/shm must have before the package uses it.
// The package's stores need tens of megabytes; a container's default 64 MiB
// /dev/shm is too close to that to be worth the risk of running out mid-test.
const memoryTempMinFree = 1 << 30

// memoryTempBase answers /dev/shm when it is tmpfs with room for the package.
func memoryTempBase() (string, bool) {
	var fs syscall.Statfs_t
	if syscall.Statfs("/dev/shm", &fs) != nil || int64(fs.Type) != tmpfsMagic {
		return "", false
	}
	if uint64(fs.Bavail)*uint64(fs.Bsize) < memoryTempMinFree {
		return "", false
	}
	return "/dev/shm", true
}
