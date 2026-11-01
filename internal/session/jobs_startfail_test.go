package session

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"syscall"
	"testing"

	"github.com/Agent-Field/codeaf/internal/shell"
)

func TestJobStartErrorWindowsReasonComesFirstAndWrapsOriginal(t *testing.T) {
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
			orig := &fs.PathError{Op: "fork/exec", Path: `C:\Program Files\Git\bin\bash.exe`, Err: tc.code}
			reason, advice := shell.StartFailure("windows", orig)
			got := jobStartError("windows", orig)
			if !errors.Is(got, orig) {
				t.Errorf("start error does not wrap the original: %v", got)
			}
			var code syscall.Errno
			if !errors.As(got, &code) || code != tc.code {
				t.Errorf("start error lost errno %d: %v", tc.code, got)
			}
			lines := strings.SplitN(got.Error(), "\n", 3)
			if len(lines) != 3 {
				t.Fatalf("want reason, advice, and original error on separate lines: %q", got)
			}
			if reason == "" || lines[0] != reason {
				t.Errorf("first line = %q, want reason %q", lines[0], reason)
			}
			if lines[1] != advice {
				t.Errorf("second line = %q, want advice %q", lines[1], advice)
			}
			if want := "Windows said: " + orig.Error(); lines[2] != want {
				t.Errorf("last line = %q, want original error %q", lines[2], want)
			}
			// The background-job tool's result, summarised the way a failed step is.
			summary := clip(firstLine("Could not start the background job: "+got.Error()), hintLimit)
			if !strings.Contains(summary, tc.gist) {
				t.Errorf("step summary %q must say %q", summary, tc.gist)
			}
		})
	}
}

func TestJobStartErrorPreservesOtherFailures(t *testing.T) {
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
			orig := fmt.Errorf("executor: %w", tc.err)
			got := jobStartError(tc.goos, orig)
			if want := "could not start the command: " + orig.Error(); got.Error() != want {
				t.Errorf("text = %q, want unchanged text %q", got, want)
			}
			if !errors.Is(got, orig) || !errors.Is(got, tc.err) {
				t.Errorf("start error does not wrap the original: %v", got)
			}
			var want, code syscall.Errno
			wantErrno := errors.As(orig, &want)
			if gotErrno := errors.As(got, &code); gotErrno != wantErrno || code != want {
				t.Errorf("errno = %d, present = %v; want %d, present = %v", code, gotErrno, want, wantErrno)
			}
		})
	}
}
