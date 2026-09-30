//go:build !windows

package session

import (
	"os"
	"syscall"
)

// ownedByThisAccount says the folder info describes belongs to the account
// codeaf runs as ([privateCopyRoot]).
func ownedByThisAccount(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Getuid()
}
