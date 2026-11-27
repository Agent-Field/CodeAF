//go:build unix

package shell

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func setLoginPathTestEnv(t *testing.T, path string, fallback ...string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", path)
	t.Setenv("SHELL", "")
	old := loginPathFallbackDirs
	loginPathFallbackDirs = fallback
	t.Cleanup(func() { loginPathFallbackDirs = old })
}

func stubLoginShell(t *testing.T, script string, mode os.FileMode) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "login-shell")
	if err := os.WriteFile(file, []byte("#!/bin/sh\n"+script+"\n"), mode); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", file)
}

func requireProcessPath(t *testing.T, want string) {
	t.Helper()
	if got := os.Getenv("PATH"); got != want {
		t.Fatalf("process PATH = %q, want %q", got, want)
	}
}

// Contract L1: The probe uses the login flags and appends only missing paths in their reported order.
func TestInheritLoginPathAppendsTheProbe(t *testing.T) {
	setLoginPathTestEnv(t, "/usr/bin:/bin")
	stubLoginShell(t, `
[ "$#" -eq 3 ] && [ "$1" = -l ] && [ "$2" = -c ] || exit 1
printf 'welcome to the shell\n'
export PATH=/stub/a:/stub/b:/usr/bin
eval "$3"
printf '\nmore banner text\n'
`, 0o755)

	InheritLoginPath()
	requireProcessPath(t, "/usr/bin:/bin:/stub/a:/stub/b")
}

// Contract L2: The existing PATH stays byte for byte, including duplicates and empty entries.
func TestInheritLoginPathPreservesExistingEntries(t *testing.T) {
	const original = "/z:/a:/m:/a::"
	setLoginPathTestEnv(t, original)
	stubLoginShell(t, `printf '__CODEAF_PATH__/m:/a:/z:/stub/a\n'`, 0o755)

	InheritLoginPath()
	requireProcessPath(t, original+":/stub/a")
}

// Contract L3: A slow probe with a child holding stdout returns promptly and uses the fixed directories.
func TestInheritLoginPathTimesOut(t *testing.T) {
	fallback := t.TempDir()
	setLoginPathTestEnv(t, "/usr/bin:/bin", fallback)
	old := loginPathTimeout
	loginPathTimeout = 200 * time.Millisecond
	t.Cleanup(func() { loginPathTimeout = old })
	stubLoginShell(t, `
printf '__CODEAF_PATH__/stub/partial\n'
sleep 5 &
wait
`, 0o755)

	start := time.Now()
	InheritLoginPath()
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("timed-out probe took %v, want at most %v", elapsed, time.Second)
	}
	requireProcessPath(t, "/usr/bin:/bin:"+fallback)
}

// Contract L4: Every unusable probe falls back to the same installed directories.
func TestInheritLoginPathFallsBack(t *testing.T) {
	for _, tc := range []struct {
		name   string
		script string
		mode   os.FileMode
	}{
		{"nonzero exit", `printf '__CODEAF_PATH__/stub/a\n'; exit 7`, 0o755},
		{"no marker", `printf '/stub/a\n'`, 0o755},
		{"missing SHELL", "", 0},
		{"not executable", `printf '__CODEAF_PATH__/stub/a\n'`, 0o644},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fallback := t.TempDir()
			setLoginPathTestEnv(t, "/usr/bin:/bin", fallback)
			if tc.mode != 0 {
				stubLoginShell(t, tc.script, tc.mode)
			} else if err := os.Unsetenv("SHELL"); err != nil {
				t.Fatal(err)
			}

			InheritLoginPath()
			requireProcessPath(t, "/usr/bin:/bin:"+fallback)
		})
	}
}

// Contract L5: Installed fallback directories follow the login paths, and the local bin comes from HOME.
func TestInheritLoginPathChecksFallbackDirectories(t *testing.T) {
	alreadyPresent, installed := t.TempDir(), t.TempDir()
	missing, file := filepath.Join(installed, "missing"), filepath.Join(installed, "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	setLoginPathTestEnv(t, alreadyPresent, alreadyPresent, installed, missing, file, "$HOME/.local/bin")
	localBin := filepath.Join(os.Getenv("HOME"), ".local", "bin")
	if err := os.MkdirAll(localBin, 0o755); err != nil {
		t.Fatal(err)
	}
	stubLoginShell(t, `printf '__CODEAF_PATH__/stub/a\n'`, 0o755)

	InheritLoginPath()
	requireProcessPath(t, alreadyPresent+":/stub/a:"+installed+":"+localBin)
}

// Contract L6: Calling again adds no duplicate login or fallback entries.
func TestInheritLoginPathIsIdempotent(t *testing.T) {
	fallback := t.TempDir()
	setLoginPathTestEnv(t, "/usr/bin", fallback)
	stubLoginShell(t, `printf '__CODEAF_PATH__/stub/a:/stub/a\n'`, 0o755)
	want := "/usr/bin:/stub/a:" + fallback

	InheritLoginPath()
	requireProcessPath(t, want)
	InheritLoginPath()
	requireProcessPath(t, want)
}

// Contract L7: Empty and relative entries from the login shell stay off PATH.
func TestInheritLoginPathIgnoresRelativeAndEmptyProbeEntries(t *testing.T) {
	setLoginPathTestEnv(t, "/usr/bin:/bin")
	stubLoginShell(t, `printf '__CODEAF_PATH__::.:bin:/stub/a::\n'`, 0o755)

	InheritLoginPath()
	requireProcessPath(t, "/usr/bin:/bin:/stub/a")
}

// Contract L8: A marker embedded in banner text starts the PATH and the first newline ends it.
func TestInheritLoginPathFindsMarkerWithinALine(t *testing.T) {
	setLoginPathTestEnv(t, "/usr/bin")
	stubLoginShell(t, `printf 'welcome __CODEAF_PATH__/stub/a:/stub/b\n:/another/banner\n'`, 0o755)

	InheritLoginPath()
	requireProcessPath(t, "/usr/bin:/stub/a:/stub/b")
}

// Contract L9: A child started with the default environment sees the appended PATH.
func TestInheritLoginPathReachesChildProcesses(t *testing.T) {
	setLoginPathTestEnv(t, "/usr/bin:/bin")
	stubLoginShell(t, `printf '__CODEAF_PATH__/stub/a'`, 0o755)

	InheritLoginPath()
	out, err := exec.Command("/bin/sh", "-c", `printf %s "$PATH"`).Output()
	if err != nil {
		t.Fatal(err)
	}
	if want := "/usr/bin:/bin:/stub/a"; string(out) != want {
		t.Fatalf("child PATH = %q, want %q", out, want)
	}
}
