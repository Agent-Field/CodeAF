//go:build !unix

package diagprofile

import "os"

// openNew is O_CREATE|O_EXCL; this platform has no O_NOFOLLOW, and O_EXCL
// alone already refuses an existing name, symlink included.
func openNew(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
}

// preparePrivateDir cannot check Unix ownership or modes here; it still
// requires a real directory.
func preparePrivateDir(dir string) error {
	if err := os.Mkdir(dir, 0o700); err != nil && !os.IsExist(err) {
		return refuse("the directory could not be created")
	}
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
		return refuse("the path is not a real directory")
	}
	return nil
}
