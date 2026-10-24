package shell

import (
	"errors"
	"testing"
)

// N1 follows a shim's real Git installation, preserves bin-before-usr/bin and
// WSL rejection, bounds the ancestor walk, and falls through on probe failure.
func TestWindowsGitShimResolution(t *testing.T) {
	const shim = `D:\tools\shims\git.exe`
	const root = `E:\nonstandard Git`
	const bin = root + `\bin\bash.exe`
	const usr = root + `\usr\bin\bash.exe`
	const fallback = `F:\msys64\usr\bin\bash.exe`
	for _, tc := range []struct {
		name, execPath, want string
		files                []string
		err                  error
	}{
		{"shim root", "E:/nonstandard Git/mingw64/libexec/git-core\r\n", bin, []string{bin, usr}, nil},
		{"usr fallback", root + `\mingw64\libexec\git-core`, usr, []string{usr}, nil},
		{"first ancestor wins", root + `\mingw64\libexec\git-core`, root + `\mingw64\bin\bash.exe`, []string{root + `\mingw64\bin\bash.exe`, bin}, nil},
		{"four parents", root + `\a\b\c\d`, bin, []string{bin}, nil},
		{"five parents is too far", root + `\a\b\c\d\e`, fallback, []string{bin, fallback}, nil},
		{"nowhere useful", `E:\missing\libexec\git-core`, fallback, []string{fallback}, nil},
		{"probe failed or timed out", "", fallback, []string{bin, fallback}, errors.New("probe failed")},
		{"empty probe", " \r\n", fallback, []string{fallback}, nil},
		{"WSL alias root rejected", `C:\WindowsApps\Git\mingw64\libexec\git-core`, fallback, []string{`C:\WindowsApps\Git\bin\bash.exe`, fallback}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lookup := fakeLookup(nil, map[string]string{"git.exe": shim, "bash.exe": fallback}, tc.files...)
			calls := 0
			lookup.GitExecPath = func(git string) (string, error) {
				calls++
				if git != shim {
					t.Fatalf("probed %q, want the discovered shim %q", git, shim)
				}
				return tc.execPath, tc.err
			}
			if got, err := ResolveWindows(lookup); err != nil || got != tc.want || calls != 1 {
				t.Fatalf("got %q, %v, calls %d; want %q once", got, err, calls, tc.want)
			}
		})
	}
	// A failed shim probe still gives the ordered install roots their turn.
	lookup := fakeLookup(map[string]string{"ProgramFiles": `E:`}, map[string]string{"git.exe": shim}, `E:\Git\bin\bash.exe`)
	lookup.GitExecPath = func(string) (string, error) { return "", errors.New("timeout") }
	if got, err := ResolveWindows(lookup); err != nil || got != `E:\Git\bin\bash.exe` {
		t.Fatalf("standard root after failure: %q, %v", got, err)
	}
}

// N1 keeps known Git layouts free of process probes, even when their bash is
// absent and the resolver must continue through the ordinary fallback rules.
func TestWindowsKnownGitLayoutsNeverProbe(t *testing.T) {
	for _, suffix := range []string{`cmd\git.exe`, `bin\git.exe`, `mingw64\bin\git.exe`} {
		for _, installed := range []bool{false, true} {
			const bash = `E:\Git\bin\bash.exe`
			const fallback = `F:\msys64\usr\bin\bash.exe`
			files, want := []string{fallback}, fallback
			if installed {
				files, want = append(files, bash), bash
			}
			lookup := fakeLookup(nil, map[string]string{"git.exe": `E:\Git\` + suffix, "bash.exe": fallback}, files...)
			lookup.GitExecPath = func(string) (string, error) {
				t.Fatal("known layout spawned a Git probe")
				return "", nil
			}
			if got, err := ResolveWindows(lookup); err != nil || got != want {
				t.Fatalf("%s installed=%v: %q, %v; want %q", suffix, installed, got, err, want)
			}
		}
	}
}
