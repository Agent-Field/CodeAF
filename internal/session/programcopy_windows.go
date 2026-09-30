//go:build windows

package session

import "os"

// ownedByThisAccount is true on a machine whose folders carry no owner codeaf
// reads: the cache folder a copy is made under is the account's own there.
func ownedByThisAccount(os.FileInfo) bool { return true }
