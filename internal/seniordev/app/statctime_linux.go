//go:build linux

// Linux spelling of the stat ctime field read by durable_sessions.go's projection mark.
package app

import (
	"syscall"
	"time"
)

func statChangedNanos(stat *syscall.Stat_t) int64 {
	return int64(stat.Ctim.Sec)*int64(time.Second) + int64(stat.Ctim.Nsec)
}
