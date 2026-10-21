package shell

import (
	"io"
	"os"
	"runtime"
)

// OpenLog opens a shell child's output at the end of its existing log. On the
// native Windows host on 2026-10-19, Git Bash exited 1 without writing a byte
// when stdout or stderr was Go's append-only Windows handle. A writable handle
// sought to the end worked; Unix keeps its original append flags and permissions.
//
// ONE CHILD OWNS THIS OFFSET ON WINDOWS. A separate appender cannot advance
// this handle's position, so callers must write their headers before Start and
// their trailers after Wait, or carry all writes through one synchronized sink.
func OpenLog(path string, mode os.FileMode) (*os.File, error) {
	file, err := os.OpenFile(path, logFlags(runtime.GOOS), mode)
	if err != nil {
		return nil, err
	}
	if runtime.GOOS == "windows" {
		if _, err := file.Seek(0, io.SeekEnd); err != nil {
			_ = file.Close()
			return nil, err
		}
	}
	return file, nil
}

func logFlags(goos string) int {
	if goos == "windows" {
		return os.O_CREATE | os.O_WRONLY
	}
	return os.O_CREATE | os.O_WRONLY | os.O_APPEND
}
