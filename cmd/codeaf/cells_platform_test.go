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

// TestC26OffCommandsDoNothing proves C26 before even a --name flag can write a
// local identity or a relay request can be prepared.
func TestC26OffCommandsDoNothing(t *testing.T) {
	want := cell.OffReason
	if runtime.GOOS == "windows" {
		want = cell.WindowsReason
	}
	for _, value := range []string{"", "0", "true"} {
		t.Run("env="+value, func(t *testing.T) {
			t.Setenv(cell.EnvVar, value)
			assertCellCommandsRefuse(t, want)
		})
	}
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
		t.Skip("C11 covers the native Windows refusal")
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
	cmd.Env = append(os.Environ(), "CODEAF_TEST_OLD_FURROW=1", "CODEAF_CELLS=1", "CODEAF_FURROW="+program)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("old-engine commands: %v\n%s", err, out)
	}
}
