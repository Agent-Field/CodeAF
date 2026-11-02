package cellstore

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/executor"
	"golang.org/x/sys/windows"
)

// TestR9WindowsUnavailableSocketsKeepToolsWorking proves F6 through the seat:
// denied or preclaimed short bases disable history without starting capture.
func TestR9WindowsUnavailableSocketsKeepToolsWorking(t *testing.T) {
	t.Cleanup(cell.UseAvailabilityInTests(true))
	base := t.TempDir()
	for range 20 {
		base = filepath.Join(base, "subdir")
	}
	if err := os.MkdirAll(base, 0700); err != nil {
		t.Fatal(err)
	}
	// The user's own temp is the only fallback; a short one here makes it a
	// candidate that each case then preclaims or denies.
	short := shortUserTemp(t)
	t.Setenv("LOCALAPPDATA", base)
	t.Setenv("TMP", short)
	t.Setenv("TEMP", short)
	for _, kind := range []string{"preclaimed", "denied"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("CODEAF_HOME", t.TempDir())
			bin := `C:\engine\furrow-r9-fixture.exe`
			exercised := 0
			for attempt := 0; ; attempt++ {
				socket := socketFor(bin)
				if socket == "" {
					break
				}
				exercised++
				if attempt == 3 {
					t.Fatal("F6: selector did not exhaust the bounded candidates")
				}
				dir := filepath.Dir(socket)
				if kind == "preclaimed" {
					if err := os.Remove(dir); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(dir, []byte("preclaimed"), 0600); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = os.Remove(dir) })
				} else {
					user, err := windows.GetCurrentProcessToken().GetTokenUser()
					if err != nil {
						t.Fatal(err)
					}
					// Owner Rights denies the normally implicit owner READ_CONTROL.
					sd, err := windows.SecurityDescriptorFromString("D:P(D;;RC;;;OW)(A;OICI;FA;;;" + user.User.Sid.String() + ")")
					if err != nil {
						t.Fatal(err)
					}
					dacl, _, err := sd.DACL()
					if err != nil {
						t.Fatal(err)
					}
					if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() {
						// The fixture denied READ_CONTROL, so its DACL cannot be put back; the
						// parent temp folder's delete-child right removes the empty directory.
						if err := os.Remove(dir); err != nil {
							t.Error(err)
						}
					})
				}
				if err := privateSocketDir(dir); err == nil {
					t.Fatalf("F6: %s candidate remained usable", kind)
				}
			}
			if exercised == 0 {
				t.Fatalf("F6: no usable %s candidate to take away; the fixture tested nothing", kind)
			}
			c := newCell(t)
			project := t.TempDir()
			var notices []string
			reports := make(chan error, 8)
			store := blockingCapture{make(chan struct{}, 1), make(chan struct{}), make(chan TurnInfo, 1)}
			defer close(store.release)
			for range 2 {
				var err error
				c, err = cell.OpenAt(c.Root, c.ID)
				if err != nil {
					t.Fatal(err)
				}
				opened := false
				seat, err := SeatOver(executor.HostBound, c, project, nil, func(err error) { reports <- err }, func(Engine) Store {
					opened = true
					return store
				}, func(e *Engine) { e.Binary = bin }, WithWarmCapture(), WithPipeline(), WithNotices(func(line string) { notices = append(notices, line) }))
				if err != nil || opened {
					t.Fatalf("F6: unavailable endpoint opened capture: opened=%v err=%v", opened, err)
				}
				for _, tool := range []string{"ls", "read", "ls"} {
					ran := false
					err := seat.Around(t.Context(), executor.Call{Tool: tool}, func() ([]byte, bool) { ran = true; return []byte("ok"), false })
					if err != nil || !ran {
						t.Fatalf("F6: %s blocked: ran=%v err=%v", tool, ran, err)
					}
				}
				executor.Settle(t.Context(), seat)
				if err := executor.Drain(t.Context(), seat); err != nil {
					t.Fatal(err)
				}
			}
			want := "rewind is off because this chat cannot save file history securely; restart codeaf to try again."
			if len(notices) != 1 || notices[0] != want {
				t.Fatalf("F6: notices across reopen = %q, want one plain sentence", notices)
			}
			select {
			case err := <-reports:
				t.Fatalf("F6: history off reported a saving outcome: %v", err)
			default:
			}
		})
	}
}

// TestR8WindowsDeepLocalAppDataSocketBudget proves F2 beyond short-name conversion.
// Each nested component already fits 8.3, so a short alias cannot bound depth.
func TestR8WindowsDeepLocalAppDataSocketBudget(t *testing.T) {
	base := t.TempDir()
	for range 20 {
		base = filepath.Join(base, "subdir")
	}
	if err := os.MkdirAll(base, 0700); err != nil {
		t.Fatal(err)
	}
	homes := []string{t.TempDir(), t.TempDir()}
	short := shortUserTemp(t)
	for _, profile := range []string{base, filepath.Join(base, strings.Repeat("é", 40), "missing")} {
		t.Run(filepath.Base(profile), func(t *testing.T) {
			t.Setenv("LOCALAPPDATA", profile)
			t.Setenv("TMP", short)
			t.Setenv("TEMP", short)
			t.Setenv("CODEAF_HOME", homes[0])
			address := func(bin string) string {
				socket := socketFor(bin)
				if socket != "" {
					// These home/build identities belong only to this fixture.
					t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(socket)) })
				}
				return socket
			}
			socket := address(`C:\engine\furrow.exe`)
			if !filepath.IsAbs(socket) || len(socket) >= 108 {
				t.Fatalf("F2: invalid AF_UNIX socket %q (%d bytes), want absolute and <108", socket, len(socket))
			}
			if socket != address(`C:\engine\furrow.exe`) || socket == address(`C:\engine\furrow-next.exe`) {
				t.Fatal("F2: short fallback lost stable build identity")
			}
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatalf("F2: shortened address cannot bind: %v", err)
			}
			defer listener.Close()
			conn, err := net.Dial("unix", socket)
			if err != nil {
				t.Fatalf("F2: shortened address cannot dial: %v", err)
			}
			conn.Close()
			t.Setenv("CODEAF_HOME", homes[1])
			if socket == address(`C:\engine\furrow.exe`) {
				t.Fatal("F2: short fallback lost home identity")
			}
		})
	}
}

// C1/C3: Common Windows spellings of the same home retain guard/socket identity.
func TestR8WindowsHomeCaseTrailingSeparators(t *testing.T) {
	login := t.TempDir()
	t.Setenv("USERPROFILE", login)
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("CODEAF_HOME", login)
	a := socketFor(`C:\engine\furrow.exe`)
	for _, p := range []string{strings.ToUpper(login) + `\`, strings.ToLower(login) + `\`} {
		if !holdsHome(p) {
			t.Errorf("C1: home spelling accepted: %s", p)
		}
		t.Setenv("CODEAF_HOME", p)
		if socketFor(`C:\engine\furrow.exe`) != a {
			t.Errorf("C3: home spelling changed socket: %s", p)
		}
	}
}

// shortUserTemp is a short directory in the user's own temp folder: the one
// fallback base a deep LOCALAPPDATA may use.
func shortUserTemp(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(os.TempDir(), "r9")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// C4: A configured drive root is never the user's private temp folder.
func TestW3WindowsFallbackRequiresUserTemp(t *testing.T) {
	base := filepath.Join(t.TempDir(), strings.Repeat("subdir\\", 20))
	if err := os.MkdirAll(base, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCALAPPDATA", base)
	t.Setenv("CODEAF_HOME", t.TempDir())
	root := filepath.VolumeName(base) + string(filepath.Separator)
	t.Setenv("TMP", root)
	t.Setenv("TEMP", root)
	if got := socketFor(`C:\engine\furrow-root-temp.exe`); got != "" {
		t.Fatalf("C4: drive-root temp selected an endpoint: %q", got)
	}
}
