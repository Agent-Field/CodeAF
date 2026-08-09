//go:build darwin

// Darwin names the stat ctime field Ctimespec, not Ctim; same value, same units.
package main

import (
	"syscall"
	"time"
)

func statChangedNanos(stat *syscall.Stat_t) int64 {
	return int64(stat.Ctimespec.Sec)*int64(time.Second) + int64(stat.Ctimespec.Nsec)
}
