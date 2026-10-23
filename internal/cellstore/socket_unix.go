//go:build !windows

package cellstore

import (
	"crypto/sha256"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/home"
	"os"
	"path/filepath"
	"strings"
)

// socketFor keys the daemon to its engine build, preserving Unix socket names.
func socketFor(bin string) string {
	socket := home.Join("v3", "engine-"+strings.TrimPrefix(filepath.Base(bin), "furrow-")+".sock")
	if len(socket) < maxSocketBytes {
		return socket
	}
	// Long project/test homes still share one managed daemon. Unix kernels count
	// the address bytes rather than the resolved path, so a short temp alias is
	// required even when the home directory itself is valid (U1/U4).
	sum := sha256.Sum256([]byte(home.Dir() + "\x00" + bin))
	name := fmt.Sprintf("codeaf-e-%d-%x", os.Getuid(), sum[:12])
	base := os.TempDir()
	if len(filepath.Join(base, name, "e.sock")) >= maxSocketBytes {
		base = "/tmp"
	}
	return filepath.Join(base, name, "e.sock")
}

// maxSocketBytes leaves room for the terminator on both Linux and macOS.
const maxSocketBytes = 100
