//go:build linux

package taskcopy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPortableMutationRefusesNativeFileFlags(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "result.txt")
	write(t, path, "result\n")
	if err := os.Chown(path, -1, os.Getgid()); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	flags, err := unix.IoctlGetInt(int(file.Fd()), unix.FS_IOC_GETFLAGS)
	if err != nil {
		t.Skipf("filesystem flags unavailable: %v", err)
	}
	if err := CheckPortableMutationMetadata(root, []string{"result.txt"}); err != nil {
		t.Fatalf("unadorned fixture must be supported: %v", err)
	}
	const nodump = 0x40
	if err = unix.IoctlSetPointerInt(int(file.Fd()), unix.FS_IOC_SETFLAGS, flags|nodump); err != nil {
		t.Skipf("fixture cannot set nodump: %v", err)
	}
	defer unix.IoctlSetPointerInt(int(file.Fd()), unix.FS_IOC_SETFLAGS, flags)
	if err = CheckPortableMutationMetadata(root, []string{"result.txt"}); err == nil {
		t.Fatal("native file flags would be dropped by replacement")
	} else if !strings.Contains(err.Error(), "native filesystem flags") {
		t.Fatalf("native flags refused for wrong reason: %v", err)
	}
}
