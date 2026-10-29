//go:build darwin

package taskcopy

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unicode"

	"golang.org/x/sys/unix"
)

const portableDarwinProvenance = "com.apple.provenance"

func portableExtendedAttributes(path string) error {
	return portableDarwinExtendedAttributes(path, unix.Llistxattr, unix.Lgetxattr)
}

// macOS attaches com.apple.provenance itself to what a tracked process writes,
// and a tracked process cannot change or remove it. Apply writes new inodes that
// receive their own provenance, so the old value is not carried, whoever wrote
// it: an untracked process can store any 11 bytes there, and those bytes are
// outside the portable contract like the attribute itself. Quarantine and all
// other attributes stay refused because replacing a file would silently drop
// security or user state.
func portableDarwinExtendedAttributes(path string, list func(string, []byte) (int, error), get func(string, string, []byte) (int, error)) error {
	var names [len(portableDarwinProvenance) + 1]byte
	n, err := list(path, names[:])
	if errors.Is(err, unix.ENOTSUP) {
		return nil
	}
	if errors.Is(err, unix.ERANGE) {
		return errors.New(portableExtendedMetadataRefusal)
	}
	if err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	if n != len(names) || string(names[:]) != portableDarwinProvenance+"\x00" {
		return errors.New(portableExtendedMetadataRefusal)
	}
	var value [12]byte
	n, err = get(path, portableDarwinProvenance, value[:])
	if errors.Is(err, unix.ERANGE) || errors.Is(err, unix.ENOATTR) {
		return errors.New(portableExtendedMetadataRefusal)
	}
	if err != nil {
		return err
	}
	// The bytes stay opaque: the size is a compatibility constraint, not proof
	// of system origin, and it keeps any larger payload under this name refused.
	if n != 11 {
		return errors.New(portableExtendedMetadataRefusal)
	}
	clear(names[:])
	n, err = list(path, names[:])
	if errors.Is(err, unix.ERANGE) || errors.Is(err, unix.ENOTSUP) {
		return errors.New(portableExtendedMetadataRefusal)
	}
	if err != nil {
		return err
	}
	if n != len(names) || string(names[:]) != portableDarwinProvenance+"\x00" {
		return errors.New(portableExtendedMetadataRefusal)
	}
	return nil
}

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
	return portableDarwinACLListing(output.String(), len(paths))
}

func portableDarwinACLListing(output string, paths int) error {
	// Preserve leading whitespace because ls indents ACL entries even when the
	// mode field shows '@' for an attribute instead of '+' for the ACL.
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) != paths {
		return errors.New("portable apply cannot preserve native access-control metadata")
	}
	for _, line := range lines {
		if strings.TrimLeftFunc(line, unicode.IsSpace) != line {
			return errors.New("portable apply cannot preserve native access-control metadata")
		}
		i := 0
		for i < len(line) && line[i] >= '0' && line[i] <= '9' {
			i++
		}
		if i > 0 && strings.HasPrefix(line[i:], ": ") {
			return errors.New("portable apply cannot preserve native access-control metadata")
		}
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
