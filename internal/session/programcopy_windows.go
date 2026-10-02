//go:build windows

package session

import (
	"fmt"
	"os"
	"path/filepath"
)

// ownedByThisAccount is true on a machine whose folders carry no owner codeaf
// reads: the cache folder a copy is made under is the account's own there.
func ownedByThisAccount(os.FileInfo) bool { return true }

// openProgramMessage refuses links and non-files before the platform's open,
// then checks the opened descriptor against the original regular file before
// reading, so a replacement cannot supply another file's bytes.
func openProgramMessage(dir, notes string) (*os.File, error) {
	path := filepath.Join(dir, notes, programCommitMessageFile)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("commit message is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		_ = file.Close()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("commit message changed while opening")
	}
	return file, nil
}
