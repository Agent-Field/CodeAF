package cellstore

import (
	"crypto/sha256"
	"fmt"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
)

// socketFor puts the daemon under the user's local profile. Hashing the full
// binary path isolates both engine upgrades and explicit development engines,
// while keeping the AF_UNIX address below its 108-byte ceiling (K2).
func socketFor(bin string) string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base, _ = os.UserCacheDir()
	}
	if short, err := windows.UTF16PtrFromString(base); err == nil {
		buf := make([]uint16, 32768)
		if n, err := windows.GetShortPathName(short, &buf[0], uint32(len(buf))); err == nil && n < uint32(len(buf)) {
			base = windows.UTF16ToString(buf[:n])
		}
	}
	sum := sha256.Sum256([]byte(bin))
	return filepath.Join(base, "codeaf", fmt.Sprintf("e-%x.sock", sum[:8]))
}
