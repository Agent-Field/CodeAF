package main

import (
	"os"
	"path/filepath"
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
	if os.Geteuid() != 0 {
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
