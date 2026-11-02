package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// The folder check answers, before anything is cloned, whether the folder a
// person chose can be written and how much room its disk has, measured at the
// nearest folder that already exists. It needs no started factory and changes
// nothing on disk.
func TestLocalFactoryFolderCheckReportsRoomWithoutMakingTheFolder(t *testing.T) {
	s := newLocalSetup(t)
	check := func(folder string) appwire.FactorySetupFolder {
		t.Helper()
		var out appwire.FactorySetupFolder
		s.must("factory.setup_folder_check", map[string]any{"caller": "w1", "folder": folder}, &out)
		return out
	}
	got := check("~/code/new/api")
	if got.Folder != "~/code/new/api" || got.Exists || got.Checked != "~" || !got.Writable || got.FreeBytes <= 0 {
		t.Fatalf("a folder still to be made: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(s.userHm, "code")); !os.IsNotExist(err) {
		t.Fatalf("the check made a folder: %v", err)
	}
	there := filepath.Join(s.userHm, "api")
	if err := os.Mkdir(there, 0o700); err != nil {
		t.Fatal(err)
	}
	if got = check(there); got.Folder != "~/api" || !got.Exists || got.Checked != "~/api" || !got.Writable {
		t.Fatalf("an existing folder: %+v", got)
	}
	if permissionsHold() {
		locked := filepath.Join(s.userHm, "locked")
		if err := os.Mkdir(locked, 0o500); err != nil {
			t.Fatal(err)
		}
		if got = check(filepath.Join(locked, "api")); got.Writable || got.Checked != "~/locked" {
			t.Fatalf("a folder this account cannot write: %+v", got)
		}
	}
	s.mustFail("factory.setup_folder_check", map[string]any{"caller": "w1", "folder": filepath.Join(s.home, "x")}, appwire.CodeForbidden)
	if s.backend.local.running {
		t.Fatal("the folder check started the factory")
	}
}

// permissionsHold says folder modes bind this account: a unix account that
// is not root. Windows has no such modes and root passes every check.
func permissionsHold() bool { return runtime.GOOS != "windows" && os.Geteuid() != 0 }

// A folder that cannot hold a checkout says why instead of borrowing the room
// of a folder above it: a file where a folder should be, a file on the way to
// it, or a folder this account may not look into.
func TestLocalFactoryFolderCheckNamesWhatIsInTheWay(t *testing.T) {
	s := newLocalSetup(t)
	check := func(folder string) appwire.FactorySetupFolder {
		t.Helper()
		var out appwire.FactorySetupFolder
		s.must("factory.setup_folder_check", map[string]any{"caller": "w1", "folder": folder}, &out)
		return out
	}
	file := filepath.Join(s.userHm, "notes.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, folder := range []string{"~/notes.txt", "~/notes.txt/api"} {
		got := check(folder)
		if got.Problem != "~/notes.txt is a file, not a folder" || got.Exists || got.Writable || got.FreeBytes != 0 || got.Checked != "~/notes.txt" {
			t.Fatalf("%s: %+v", folder, got)
		}
	}
	if !permissionsHold() {
		return
	}
	closed := filepath.Join(s.userHm, "closed")
	if err := os.Mkdir(closed, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(closed, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(closed, 0o700) })
	if got := check("~/closed/api"); got.Problem != "codeaf cannot look inside ~/closed" || got.Writable || got.Exists || got.FreeBytes != 0 {
		t.Fatalf("a folder this account may not look into: %+v", got)
	}
	// Writing into a folder also needs the right to enter it.
	noEntry := filepath.Join(s.userHm, "noentry")
	if err := os.Mkdir(noEntry, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(noEntry, 0o700) })
	if got := check("~/noentry"); got.Writable || !got.Exists {
		t.Fatalf("a folder that can be written but not entered: %+v", got)
	}
}

// Continue reaches the factory as its own step: once reading has finished it
// is the factory's refusal, not an unknown step.
func TestLocalFactorySetupContinueReachesTheFactory(t *testing.T) {
	s := newLocalSetup(t)
	s.start()
	s.setup("factory.setup_connect", map[string]any{"source": "token", "token": setupSecret})
	s.setup("factory.setup_select", map[string]any{"repos": []string{"acme/api"}})
	s.until("checkouts", func(v appwire.FactorySetupView) bool { return v.State == "checkouts" })
	s.mustFail("factory.setup_continue", map[string]any{"caller": "w1"}, appwire.CodeEngine)
	s.neverSaid(setupSecret)
}
