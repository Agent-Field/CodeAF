package bare

import (
	"errors"
	"io/fs"
	"strings"
	"syscall"
	"testing"

	"github.com/Agent-Field/codeaf/internal/shell"
)

func TestStartFailedTextWindowsReasonComesFirst(t *testing.T) {
	for _, tc := range []struct {
		name string
		code syscall.Errno
		gist string // what the failed step's one-line summary must still say
	}{
		{"infected", 225, "Microsoft Defender"},
		{"deleted", 226, "Microsoft Defender"},
		{"access denied", 5, "Microsoft Defender"},
		{"command line too long", 206, "32,767"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := &fs.PathError{Op: "fork/exec", Path: `C:\Program Files\Git\bin\bash.exe`, Err: tc.code}
			reason, advice := shell.StartFailure("windows", err)
			got := startFailedText("windows", err)
			lines := strings.SplitN(got, "\n", 3)
			if len(lines) != 3 {
				t.Fatalf("want reason, advice, and original error on separate lines: %q", got)
			}
			if want := "Failed to start command: " + reason; reason == "" || lines[0] != want {
				t.Errorf("first line = %q, want %q", lines[0], want)
			}
			// The session summarises a failed step by its first line, cut at 80
			// bytes, and the summary is what the desktop shows beside the step.
			if len(lines[0]) > 80 || !strings.Contains(lines[0], tc.gist) {
				t.Errorf("first line %q (%d bytes) must fit 80 bytes and say %q", lines[0], len(lines[0]), tc.gist)
			}
			if lines[1] != advice {
				t.Errorf("second line = %q, want advice %q", lines[1], advice)
			}
			if want := "Windows said: " + err.Error(); lines[2] != want {
				t.Errorf("last line = %q, want original error %q", lines[2], want)
			}
		})
	}
}

func TestStartFailedTextPreservesOtherFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		goos string
		err  error
	}{
		{"Windows missing file", "windows", syscall.Errno(2)},
		{"Windows other permission error", "windows", syscall.Errno(13)},
		{"Windows no errno", "windows", errors.New("boom\noriginal detail")},
		{"Linux I/O error", "linux", syscall.Errno(5)},
		{"Linux permission denied", "linux", syscall.Errno(13)},
		{"Linux length code", "linux", syscall.Errno(206)},
		{"Linux virus code", "linux", syscall.Errno(225)},
		{"macOS virus code", "darwin", syscall.Errno(226)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := startFailedText(tc.goos, tc.err)
			if want := "Failed to start command: " + tc.err.Error(); got != want {
				t.Errorf("text = %q, want unchanged text %q", got, want)
			}
		})
	}
}
