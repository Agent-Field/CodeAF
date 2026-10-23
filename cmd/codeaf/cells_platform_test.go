package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/furrowbin"
)

// TestC27UnavailableCommandsDoNothing proves C27.3 before even a --name
// flag can write an identity or prepare a relay request.
func TestC27UnavailableCommandsDoNothing(t *testing.T) {
	t.Cleanup(cell.UseAvailabilityInTests(false))
	want := furrowbin.CellsUnavailable
	assertCellCommandsRefuse(t, want)
}

func assertCellCommandsRefuse(t *testing.T, want string) {
	t.Helper()
	for name, run := range map[string]func() error{
		"pair":     func() error { return (pairDoor{}).run(context.Background(), []string{"--name", "changed"}) },
		"identity": func() error { return runIdentity([]string{"show"}) },
		"rotate":   func() error { return runIdentity([]string{"rotate", "--yes"}) },
		"cell":     func() error { return runCell([]string{"log"}) },
		"rename":   func() error { return runDevicesRename([]string{"changed"}) },
	} {
		if err := run(); err == nil || err.Error() != want {
			t.Errorf("%s: %v, want %q", name, err, want)
		}
	}
}

// TestC12OldEngineCLI proves C12 through the real command doors in a fresh
// process, so a previous test's compatible-engine cache cannot mask failure.
func TestC12OldEngineCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("this fake engine is a Unix shell script; K1 covers the native engine")
	}
	if os.Getenv("CODEAF_TEST_OLD_FURROW") == "1" {
		assertCellCommandsRefuse(t, furrowbin.CellsUnavailable)
		return
	}
	program := filepath.Join(t.TempDir(), "old-furrow")
	if err := os.WriteFile(program, []byte("#!/bin/sh\nexit 2\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestC12OldEngineCLI$", "-test.v") //codeaf:plumbing runs the real command doors with a fresh engine capability cache
	cmd.Env = append(os.Environ(), "CODEAF_TEST_OLD_FURROW=1", "CODEAF_FURROW="+program)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("old-engine commands: %v\n%s", err, out)
	}
}
