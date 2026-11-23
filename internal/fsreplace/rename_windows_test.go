package fsreplace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// Int-fix contract A3: a persistent editor lock preserves both files and the
// native error through the public rename operation.
func TestIntFixContractAWindowsRenameKeepsLockedFilesAndErrors(t *testing.T) {
	dir := t.TempDir()
	from, to := filepath.Join(dir, "next"), filepath.Join(dir, "state")
	if err := os.WriteFile(from, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	name, err := windows.UTF16PtrFromString(to)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	err = Rename(from, to)
	if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) && !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatalf("persistent lock error = %v", err)
	}
	var linkErr *os.LinkError
	if !errors.As(err, &linkErr) || linkErr.Op != "rename" || linkErr.Old != from || linkErr.New != to {
		t.Fatalf("lost rename context: %v", err)
	}
	for path, want := range map[string]string{from: "new", to: "old"} {
		if got, err := os.ReadFile(path); err != nil || string(got) != want {
			t.Fatalf("failed rename changed %s: %q, %v", path, got, err)
		}
	}
}

// TestK3WindowsEditorLockRetries proves K3 against an actual NTFS handle that
// denies deletion. The third failed attempt releases it deterministically.
// Int-fix contract A3: retry the same Windows leaf that Rename uses.
func TestK3WindowsEditorLockRetries(t *testing.T) {
	dir := t.TempDir()
	from, to := filepath.Join(dir, "from"), filepath.Join(dir, "to")
	for _, p := range []string{from, to} {
		if err := os.WriteFile(p, []byte(p), 0600); err != nil {
			t.Fatal(err)
		}
	}
	name, err := windows.UTF16PtrFromString(to)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	attempts := 0
	err = Retry(func() error {
		attempts++
		err := rename(from, to)
		if attempts == 3 {
			if err == nil {
				t.Fatal("replacement ignored editor handle")
			}
			windows.CloseHandle(handle)
		}
		return err
	})
	if err != nil || attempts != 4 {
		t.Fatalf("replacement: attempts=%d err=%v", attempts, err)
	}
}
