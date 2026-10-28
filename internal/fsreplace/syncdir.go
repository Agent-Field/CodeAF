package fsreplace

import (
	"errors"
	"fmt"
	"os"
	"runtime"
)

// SyncDir persists directory entries where the operating system supports it.
// Windows does not support FlushFileBuffers on the read-only directory handles
// returned by os.Open. Regular file contents still require their own Sync
// before Rename; a directory flush cannot substitute for that boundary.
func SyncDir(path string) (err error) {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, dir.Close()) }()
	info, err := dir.Stat()
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("sync directory %s: not a directory", path)
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	return dir.Sync()
}
