package store

import (
	"os"
	"strconv"
)

// fileStamp is a comparable stat-only identity, shared by readers of the store.
type fileStamp struct {
	size, modified int64
	identity
}

type identity struct {
	device, inode  uint64
	seconds, nanos int64
}

// FileStamp catches atomic replacement and in-place rewrites that preserve
// size and modification time, without opening the document.
func FileStamp(info os.FileInfo) fileStamp {
	return fileStamp{info.Size(), info.ModTime().UnixNano(), fileIdentity(info)}
}

func (s fileStamp) String() string {
	return strconv.FormatInt(s.size, 10) + " " + strconv.FormatInt(s.modified, 10) + " " + strconv.FormatUint(s.device, 10) + " " + strconv.FormatUint(s.inode, 10) + " " + strconv.FormatInt(s.seconds, 10) + " " + strconv.FormatInt(s.nanos, 10)
}
