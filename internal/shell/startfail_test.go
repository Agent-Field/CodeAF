package shell

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"syscall"
	"testing"
)

func TestStartFailureWindowsSecurityBlock(t *testing.T) {
	for _, code := range []syscall.Errno{225, 226, 5} {
		t.Run(fmt.Sprintf("%d", code), func(t *testing.T) {
			err := &fs.PathError{Op: "fork/exec", Path: `C:\Program Files\Git\bin\bash.exe`, Err: code}
			reason, advice := StartFailure("windows", err)
			if !strings.Contains(reason, "Microsoft Defender") {
				t.Fatalf("reason does not name the Windows security block: %q", reason)
			}
			if code == 5 {
				if !strings.Contains(reason, "denied") || !strings.Contains(reason, "likely") {
					t.Errorf("access denied must hedge the cause: %q", reason)
				}
			} else if !strings.Contains(reason, "antivirus") || !strings.Contains(reason, "blocked") {
				t.Errorf("virus error must name the security block: %q", reason)
			}
			for _, want := range []string{"Protection history", "Windows Security", "Tell the person what you were trying to run", "rather than rewording the command to get past the security block"} {
				if !strings.Contains(advice, want) {
					t.Errorf("advice %q does not contain %q", advice, want)
				}
			}
			if strings.Contains(advice, "retry") || strings.Contains(advice, "rephrase") {
				t.Errorf("advice must not suggest another attempt: %q", advice)
			}
			if strings.ContainsAny(reason, "\r\n") || strings.ContainsAny(advice, "\r\n") {
				t.Error("reason and advice must each fit on one line")
			}
		})
	}
}

func TestStartFailureWindowsCommandLineLimit(t *testing.T) {
	err := &fs.PathError{Op: "fork/exec", Path: `C:\Program Files\Git\bin\bash.exe`, Err: syscall.Errno(206)}
	reason, advice := StartFailure("windows", err)
	for _, want := range []string{"32,767", "command line"} {
		if !strings.Contains(reason, want) {
			t.Errorf("reason %q does not contain %q", reason, want)
		}
	}
	if !strings.Contains(advice, "file in the working folder") || !strings.Contains(advice, "run that file") {
		t.Errorf("advice does not explain how to run a script file: %q", advice)
	}
	if strings.Contains(reason+advice, "Defender") || strings.Contains(reason+advice, "antivirus") || strings.Contains(advice, "Protection history") {
		t.Errorf("length limit must not be treated as a security block: %q %q", reason, advice)
	}
}

func TestStartFailureLeavesOtherErrorsAlone(t *testing.T) {
	for _, tc := range []struct {
		name string
		goos string
		err  error
	}{
		{"Windows missing file", "windows", syscall.Errno(2)},
		{"Windows other permission error", "windows", syscall.Errno(13)},
		{"Linux I/O error", "linux", syscall.Errno(5)},
		{"Linux permission denied", "linux", syscall.Errno(13)},
		{"Linux length code", "linux", syscall.Errno(206)},
		{"Linux virus code", "linux", syscall.Errno(226)},
		{"macOS virus code", "darwin", syscall.Errno(225)},
		{"Windows no errno", "windows", errors.New("boom")},
		{"Windows text alone", "windows", errors.New("Access is denied. errno 225")},
		{"nil", "windows", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reason, advice := StartFailure(tc.goos, tc.err)
			if reason != "" || advice != "" {
				t.Errorf("unrecognised start failure got reason %q, advice %q", reason, advice)
			}
		})
	}
}
