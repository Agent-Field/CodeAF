//go:build unix

package diagprofile

import (
	"os"
	"syscall"
)

// openNew is O_CREATE|O_EXCL|O_NOFOLLOW: it fails if anything, including a
// dangling symlink, already holds the name.
func openNew(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
}

// preparePrivateDir makes the directory if its parent exists, then insists it
// is a real directory (not a symlink), owned by this user, and closed to group
// and others. An existing directory is never chmod'ed: loosened permissions
// are the developer's to fix, and silently repairing them would hide that.
func preparePrivateDir(dir string) error {
	if err := os.Mkdir(dir, 0o700); err != nil && !os.IsExist(err) {
		return refuse("the directory could not be created")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return refuse("the path is not a real directory")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return refuse("the directory must not be accessible to group or others (mode 0700)")
	}
	if st, ok := info.Sys().(*syscall.Stat_t); !ok || int(st.Uid) != os.Geteuid() {
		return refuse("the directory must be owned by the current user")
	}
	return nil
}
