//go:build darwin

package taskcopy

import (
	"bytes"
	"context"
	"errors"
	"golang.org/x/sys/unix"
	"io/fs"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

func portableFilesystemFlags(path string, info fs.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Flags != 0 {
		return errors.New("portable apply cannot preserve native filesystem flags")
	}
	return nil
}

func portablePlatformMetadataBatch(ctx context.Context, paths []string) error {
	for len(paths) > 0 {
		n, size := 0, 0
		for n < len(paths) && n < 128 && size+len(paths[n]) < 32<<10 {
			size += len(paths[n]) + 1
			n++
		}
		if n == 0 {
			return errors.New("native metadata path exceeds its bound")
		}
		if err := portableDarwinACLBatch(ctx, paths[:n]); err != nil {
			return err
		}
		paths = paths[n:]
	}
	return ctx.Err()
}

func portableDarwinACLBatch(ctx context.Context, paths []string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	args := append([]string{"-ldben"}, paths...)
	cmd := exec.CommandContext(ctx, "/bin/ls", args...)
	output := &portableACLListing{}
	cmd.Stdout = output
	if err := cmd.Run(); err != nil {
		return errors.New("portable apply cannot verify native access-control metadata")
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != len(paths) {
		return errors.New("portable apply cannot preserve native access-control metadata")
	}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasSuffix(fields[0], "+") {
			return errors.New("portable apply cannot preserve native access-control metadata")
		}
	}
	return nil
}

type portableACLListing struct{ bytes.Buffer }

func (b *portableACLListing) Write(raw []byte) (int, error) {
	if b.Len()+len(raw) > 1<<20 {
		return 0, errors.New("native ACL listing exceeds its bound")
	}
	return b.Buffer.Write(raw)
}

// SetPortableSymlinkModeAt sets only the link inode through an owned parent descriptor.
func SetPortableSymlinkModeAt(parentFD int, name string, mode fs.FileMode) error {
	if mode != mode.Perm() {
		return errors.New("symlink permission bits are unsupported")
	}
	return unix.Fchmodat(parentFD, name, uint32(mode.Perm()), unix.AT_SYMLINK_NOFOLLOW)
}

// OSManagedAttribute reports whether an extended attribute is one macOS
// stamps on its own and that portable apply may leave behind. macOS 15 adds
// com.apple.provenance to every file a process tree started from certain apps
// creates, and it cannot be removed by its owner; the replacements apply
// writes are created by the same process tree and are stamped the same way,
// so nothing a person set is lost. Every other attribute, quarantine included,
// is still refused.
func OSManagedAttribute(name string) bool { return name == "com.apple.provenance" }
