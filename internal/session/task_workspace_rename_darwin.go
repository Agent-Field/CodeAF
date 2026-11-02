//go:build darwin

package session

import "golang.org/x/sys/unix"

func workspaceApplicationRenameAtExclusive(from int, name string, to int, target string) error {
	return unix.RenameatxNp(from, name, to, target, unix.RENAME_EXCL)
}
