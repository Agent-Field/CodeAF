package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunPatchReplacesTheSingleMatch(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "thing.txt")
	os.WriteFile(name, []byte("alpha\nbeta\ngamma\n"), 0o644)

	err := runPatch([]string{"--old", "beta", "--new", "BETA", name})
	if err != nil {
		t.Fatalf("patch refused a unique match: %v", err)
	}
	after, _ := os.ReadFile(name)
	if string(after) != "alpha\nBETA\ngamma\n" {
		t.Errorf("the file now reads %q, want the one region replaced", after)
	}
}

func TestRunPatchRefusesZeroAndTwoMatchesWithExitOne(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "thing.txt")
	os.WriteFile(name, []byte("same\nsame\n"), 0o644)

	err := runPatch([]string{"--old", "absent", "--new", "x", name})
	if err == nil {
		t.Fatal("patch accepted an old text that matches nothing")
	}
	if exitCodeOf(err) != 1 {
		t.Errorf("a refusal left with %d, want 1", exitCodeOf(err))
	}
	after, _ := os.ReadFile(name)
	if string(after) != "same\nsame\n" {
		t.Error("a refused patch changed the file")
	}

	err = runPatch([]string{"--old", "same", "--new", "x", name})
	if err == nil || !strings.Contains(err.Error(), "2") {
		t.Errorf("a two-match refusal says %v, which never names the count", err)
	}
	after, _ = os.ReadFile(name)
	if string(after) != "same\nsame\n" {
		t.Error("a refused patch changed the file")
	}
}

func TestRunPatchReadsBothTextsFromFiles(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "thing.txt")
	os.WriteFile(name, []byte("one two three\n"), 0o644)
	oldFile := filepath.Join(dir, "old.txt")
	newFile := filepath.Join(dir, "new.txt")
	os.WriteFile(oldFile, []byte("one two"), 0o644)
	os.WriteFile(newFile, []byte("ONE TWO"), 0o644)

	err := runPatch([]string{"--old-file", oldFile, "--new-file", newFile, name})
	if err != nil {
		t.Fatalf("patch refused file-sourced texts: %v", err)
	}
	after, _ := os.ReadFile(name)
	if string(after) != "ONE TWO three\n" {
		t.Errorf("the file now reads %q", after)
	}
}

func TestRunPatchRefusesAmbiguousTextArguments(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "thing.txt")
	os.WriteFile(name, []byte("body\n"), 0o644)

	err := runPatch([]string{"--old", "a", "--old-file", name, "--new", "b", name})
	if err == nil || !strings.Contains(err.Error(), "--old") {
		t.Errorf("both --old sources says %v, which never names the flag", err)
	}
	err = runPatch([]string{"--old", "a", name})
	if err == nil || !strings.Contains(err.Error(), "--new") {
		t.Errorf("no replacement says %v, which never names the flag", err)
	}
}
