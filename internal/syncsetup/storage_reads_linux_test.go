package syncsetup

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/identity"
	"golang.org/x/sys/unix"
)

// File opens observe the complete production sample, including reads inside
// dependencies; seam counters alone cannot detect duplicated quiet checks.
func watchStorageReads(t *testing.T, home string) func() map[string]int {
	t.Helper()
	fd, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unix.Close(fd) })
	// Close events keep consecutive opens of the same file from coalescing.
	if _, err := unix.InotifyAddWatch(fd, home, unix.IN_OPEN|unix.IN_CLOSE_NOWRITE); err != nil {
		t.Fatal(err)
	}
	return func() map[string]int {
		t.Helper()
		counts := map[string]int{}
		buf := make([]byte, 64<<10)
		for {
			n, err := unix.Read(fd, buf)
			if err == unix.EAGAIN {
				return counts
			}
			if err != nil {
				t.Fatal(err)
			}
			for at := 0; at < n; {
				mask := binary.NativeEndian.Uint32(buf[at+4 : at+8])
				size := int(binary.NativeEndian.Uint32(buf[at+12 : at+16]))
				name := strings.TrimRight(string(buf[at+unix.SizeofInotifyEvent:at+unix.SizeofInotifyEvent+size]), "\x00")
				if mask&unix.IN_Q_OVERFLOW != 0 {
					t.Fatal("file-read measurement overflowed")
				}
				if mask&unix.IN_OPEN != 0 {
					counts[name]++
				}
				at += unix.SizeofInotifyEvent + size
			}
		}
	}
}

func TestStorageOptInSharedWaiterWork(t *testing.T) {
	for _, override := range []bool{false, true} {
		for _, n := range []int{1, 10, 50} {
			for _, mode := range []string{"on", "off", "self-hosted"} {
				t.Run(fmt.Sprintf("env=%v/%s/%d", override, mode, n), func(t *testing.T) {
					home := t.TempDir()
					relay := ProductionRelayURL
					if mode == "self-hosted" {
						relay = "http://own.test"
					}
					t.Setenv(URLVar, "")
					if override {
						t.Setenv(URLVar, relay)
					} else {
						if err := os.Unsetenv(URLVar); err != nil {
							t.Fatal(err)
						}
						if err := SaveRelayURL(home, relay); err != nil {
							t.Fatal(err)
						}
					}
					id, err := identity.Ensure(home)
					if err != nil {
						t.Fatal(err)
					}
					state := mode
					if state == "self-hosted" {
						state = "on"
					}
					if err := SaveStorage(home, relay, id.ID(), state); err != nil {
						t.Fatal(err)
					}
					timers, requests := 0, 0
					reads := watchStorageReads(t, home)
					var next func()
					p := storageProcessFor(home)
					p.fetch = func(context.Context) error { requests++; return nil }
					p.after = func(wait time.Duration, fn func()) func() bool {
						if wait > StoragePollEvery {
							t.Fatalf("wait %v", wait)
						}
						timers++
						next = fn
						return func() bool { next = nil; return true }
					}
					woke := 0
					if mode == "off" {
						for i := 0; i < n; i++ {
							p.waiting[uint64(i)] = func() { woke++ }
						}
					}
					p.check(false)
					// Discard setup reads, then observe every production file open during
					// one minute of steady-state waiting, including nested quiet checks.
					reads()
					timers, requests = 0, 0
					if next != nil {
						fn := next
						next = nil
						fn()
					}
					wantReads, wantTimers := map[string]int{}, 0
					if mode == "off" {
						wantReads[identity.File], wantReads[StorageFile] = 1, 1
						if !override {
							wantReads[SavedURLFile] = 1
						}
						wantTimers = 1
					}
					actual := reads()
					if !reflect.DeepEqual(actual, wantReads) || timers != wantTimers || requests != 0 {
						t.Fatalf("reads=%v want=%v timers=%d requests=%d", actual, wantReads, timers, requests)
					}
					t.Logf("env=%v chats=%d state=%s file-reads/min=%v timer-arms/min=%d relay-requests=%d", override, n, mode, actual, timers, requests)
					if mode == "off" {
						// A raw file change models another process sharing this home.
						raw := fmt.Sprintf(`{"relay":%q,"identity":%q,"storage":"on"}`, relay, id.ID())
						if err := os.WriteFile(filepath.Join(home, StorageFile), []byte(raw), 0600); err != nil {
							t.Fatal(err)
						}
						fn := next
						fn()
						if woke != n || next != nil {
							t.Fatalf("wake=%d timer=%v", woke, next != nil)
						}
					}
				})
			}
		}
	}

}
