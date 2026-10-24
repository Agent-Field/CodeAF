package fsreplace

import (
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"testing"
)

// TestK3WindowsEditorLockRetries proves K3 against an actual NTFS handle that
// denies deletion. The third failed attempt releases it deterministically.
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
		err := os.Rename(from, to)
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
