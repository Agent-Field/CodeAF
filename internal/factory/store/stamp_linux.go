package store

import (
	"os"
	"syscall"
)

func fileIdentity(info os.FileInfo) identity {
	st := info.Sys().(*syscall.Stat_t)
	return identity{uint64(st.Dev), st.Ino, st.Ctim.Sec, st.Ctim.Nsec}
}
