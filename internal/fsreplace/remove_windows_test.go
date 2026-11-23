package fsreplace

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// Contract 2: a transient outside reader can close during the bounded retry,
// whereas a persistent sharing violation is returned with the state retained.
func TestPublishContract2WindowsRemoveRetriesAndKeepsErrors(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "tree"}[all], func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "state")
			if err := os.WriteFile(path, []byte("state"), 0600); err != nil {
				t.Fatal(err)
			}
			name, err := windows.UTF16PtrFromString(path)
			if err != nil {
				t.Fatal(err)
			}
			handle, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			release := sync.OnceFunc(func() { windows.CloseHandle(handle) })
			defer release()
			remove := func() error { return Remove(path) }
			if all {
				remove = func() error { return RemoveAll(dir) }
			}
			if err := remove(); !errors.Is(err, windows.ERROR_SHARING_VIOLATION) && !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
				t.Fatalf("persistent lock error = %v", err)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("failed removal discarded state: %v", err)
			}
			closed := make(chan struct{})
			go func() {
				time.Sleep(30 * time.Millisecond)
				release()
				close(closed)
			}()
			err = remove()
			<-closed
			if err != nil {
				t.Fatalf("transient lock did not clear: %v", err)
			}
		})
	}
}
