package main

import (
	"errors"
	"testing"
)

func TestWorkspaceServeRequiresNameAndHome(t *testing.T) {
	done, err := serveWorkspace([]string{"--workspace", "--listen", "127.0.0.1:0"})
	var status exitStatus
	if !done || !errors.As(err, &status) || status != exitCannotRun {
		t.Fatalf("done=%v err=%v", done, err)
	}
}

func TestPersonalServeDoesNotTakeTheWorkspaceBranch(t *testing.T) {
	done, err := serveWorkspace([]string{"--home", t.TempDir()})
	if done || err != nil {
		t.Fatalf("done=%v err=%v", done, err)
	}
}
