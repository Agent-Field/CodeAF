package shell

import "testing"

// N2 and N3 ask Windows to bind relative names before converting them, preserving
// drive-relative, root-relative and ordinary relative path semantics.
func TestWindowsListPathBindsRelativeNames(t *testing.T) {
	for _, tc := range []struct{ native, absolute, want string }{
		{`C:`, `C:\drive cwd`, `/c/drive cwd`},
		{`C:rel`, `C:\drive cwd\rel`, `/c/drive cwd/rel`},
		{`\x`, `D:\x`, `/d/x`},
		{`rel\x`, `D:\current\rel\x`, `/d/current/rel/x`},
	} {
		calls := 0
		abs := func(name string) (string, error) {
			calls++
			if name != tc.native {
				t.Fatalf("absolutiser saw %q, want original %q", name, tc.native)
			}
			return tc.absolute, nil
		}
		if got := listPath("windows", tc.native, abs); got != tc.want || calls != 1 {
			t.Fatalf("%q => %q, calls %d; want %q once", tc.native, got, calls, tc.want)
		}
	}
}

// N2 and N3 leave absolute Windows, UNC, POSIX, empty and every Unix path independent
// of the current directory, so they never invoke the injected absolutiser.
func TestListPathOnlyBindsRelativeWindowsNames(t *testing.T) {
	abs := func(name string) (string, error) {
		t.Fatalf("unexpected absolutiser call for %q", name)
		return "", nil
	}
	for _, tc := range []struct{ native, want string }{
		{`C:\tools\bin`, `/c/tools/bin`}, {`D:/tools/bin`, `/d/tools/bin`},
		{`\\server\share\bin`, `//server/share/bin`}, {`//server/share/bin`, `//server/share/bin`},
		{`/tools/bin`, `/tools/bin`}, {"", ""},
	} {
		if got := listPath("windows", tc.native, abs); got != tc.want {
			t.Fatalf("%q => %q, want %q", tc.native, got, tc.want)
		}
	}
	for _, goos := range []string{"linux", "darwin"} {
		for _, name := range []string{`C:`, `C:rel`, `\x`, `rel\x`, `C:\tools\bin`, `/usr/bin`, ""} {
			if got := listPath(goos, name, abs); got != name {
				t.Fatalf("%s changed %q to %q", goos, name, got)
			}
		}
	}
}
