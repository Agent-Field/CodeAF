package update

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWindowsReplacementFailureLeavesTheOriginalAtItsTarget proves the
// Windows half of D9 without asking this host to run a Windows executable.
func TestWindowsReplacementFailureLeavesTheOriginalAtItsTarget(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "codeaf.exe")
	temporary := filepath.Join(directory, ".codeaf.tmp")
	if err := os.WriteFile(target, []byte("original"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(temporary, []byte("replacement"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := windowsUnableToMoveReplacement2
	err := replaceWindowsExecutable(temporary, target, func(gotTarget, gotReplacement, gotBackup string) error {
		if gotTarget != target || gotReplacement != temporary || gotBackup != target+".old" {
			t.Fatalf("replace(%q, %q, %q)", gotTarget, gotReplacement, gotBackup)
		}
		// Windows has already moved the original to the backup when it reports
		// ERROR_UNABLE_TO_MOVE_REPLACEMENT_2, while the replacement stays put.
		if moveErr := os.Rename(gotTarget, gotBackup); moveErr != nil {
			t.Fatal(moveErr)
		}
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("replacement error = %v, want %v", err, want)
	}
	raw, readErr := os.ReadFile(target)
	if readErr != nil || string(raw) != "original" {
		t.Fatalf("original target = %q, %v", raw, readErr)
	}
}

// TestTheWindowsBuildUsesTheReplacementRecoveryContract keeps the platform
// door on the operation that repairs ReplaceFileW's partial failure.
func TestTheWindowsBuildUsesTheReplacementRecoveryContract(t *testing.T) {
	raw, err := os.ReadFile("replace_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "return replaceWindowsExecutable(temporary, target, replaceFile)") {
		t.Fatal("the Windows replacement door bypasses the atomic replacement contract")
	}
}
