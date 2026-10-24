package cellstore

import "os"

// syncPath uses a writable file handle because FlushFileBuffers refuses a
// read-only handle. Windows has no supported directory flush; like the engine,
// we flush each regular file and retain the filesystem's directory ordering.
func syncPath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return nil
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
