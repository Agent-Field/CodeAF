//go:build !linux && !darwin

package session

import "os"

// fileIdentity: see mediaident_linux.go. A platform whose stat is not read
// here knows only the size and the modification time, and says zero for the
// rest — which makes two versions differ only by those two, never by a guess.
func fileIdentity(os.FileInfo) (ctimeNS int64, dev, ino uint64) { return 0, 0, 0 }
