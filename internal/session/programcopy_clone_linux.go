//go:build linux

package session

import (
	"os"
	"os/exec"
)

// cloneTree clones the folder source to target copy-on-write, on a filesystem
// that reflinks (btrfs, XFS, bcachefs), and fails on one that cannot — ext4
// among them — leaving nothing at target. `--reflink=always` refuses rather
// than falling back to a full copy, which for a node_modules is minutes.
func cloneTree(source, target string) error {
	if err := exec.Command("cp", "-a", "--reflink=always", source, target).Run(); err != nil {
		_ = os.RemoveAll(target)
		return err
	}
	return nil
}
