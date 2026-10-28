//go:build !windows

package cellstore

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/Agent-Field/codeaf/internal/home"
)

// socketFor keys the daemon to its absolute home and engine build.
func socketFor(bin string) string {
	root := canonicalTree(home.Dir())
	sum := sha256.Sum256([]byte(root + "\x00" + bin))
	name := fmt.Sprintf("codeaf-e-%d-%x", os.Getuid(), sum[:12])
	if runtime := os.Getenv("XDG_RUNTIME_DIR"); filepath.IsAbs(runtime) && ownedSocketDir(runtime) == nil {
		socket := filepath.Join(runtime, name, "e.sock")
		if len(socket) < maxSocketBytes && privateSocketDir(filepath.Dir(socket)) == nil && validateSocket(socket) == nil {
			return socket
		}
	}
	socket := filepath.Join(root, "v3", "engine-"+strings.TrimPrefix(filepath.Base(bin), "furrow-")+".sock")
	if len(socket) < maxSocketBytes {
		return socket
	}
	// Long project/test homes still share one managed daemon. Unix kernels count
	// the address bytes rather than the resolved path, so a short temp alias is
	// required even when the home directory itself is valid (U1/U4).
	for _, base := range []string{os.TempDir(), "/tmp"} {
		for candidate := range shortSocketCandidates {
			dir := name
			if candidate > 0 {
				dir += fmt.Sprintf("-%d", candidate)
			}
			socket := filepath.Join(base, dir, "e.sock")
			if filepath.IsAbs(socket) && len(socket) < maxSocketBytes && privateSocketDir(filepath.Dir(socket)) == nil && validateSocket(socket) == nil {
				return socket
			}
		}
	}
	return ""
}

const shortSocketCandidates = 4

// privateSocketDir never repairs a preclaimed directory into acceptance.
func privateSocketDir(dir string) error {
	if err := os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
		return err
	}
	return ownedSocketDir(dir)
}

func ownedSocketDir(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode().Perm() != 0700 || !ok || stat.Uid != uint32(os.Getuid()) {
		return fmt.Errorf("saving-engine socket directory is not private")
	}
	return nil
}

// validateSocket rechecks short endpoints before dialing or pruning them.
func validateSocket(socket string) error {
	dir := filepath.Dir(socket)
	if !strings.HasPrefix(filepath.Base(dir), "codeaf-e-") {
		return nil
	}
	if err := ownedSocketDir(dir); err != nil {
		return err
	}
	info, err := os.Lstat(socket)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if info.Mode()&os.ModeSymlink != 0 || !ok || stat.Uid != uint32(os.Getuid()) {
		return fmt.Errorf("saving-engine socket is not private")
	}
	return nil
}

// maxSocketBytes leaves room for the terminator on both Linux and macOS.
const maxSocketBytes = 100
