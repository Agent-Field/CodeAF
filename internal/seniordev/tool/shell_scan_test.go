//go:build !windows

package tool

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestScanShellPermissions(t *testing.T) {
	workspace := filepath.Join(string(filepath.Separator), "work", "repo")
	scan := ScanShellPermissions(
		`git status && cp "inside.txt" /outside/dst; cd ../other; npm run test`,
		ShellScanOptions{
			CWD:       workspace,
			Workspace: workspace,
			Shell:     "/bin/bash",
			Home:      "/home/test",
			IsDir: func(path string) bool {
				return path == "/outside/dst"
			},
		},
	)
	if want := []string{"/outside/dst", "/work"}; !reflect.DeepEqual(scan.Dirs, want) {
		t.Errorf("Dirs = %#v, want %#v", scan.Dirs, want)
	}
	if want := []string{"git status", `cp "inside.txt" /outside/dst`, "npm run test"}; !reflect.DeepEqual(scan.Patterns, want) {
		t.Errorf("Patterns = %#v, want %#v", scan.Patterns, want)
	}
	if want := []string{"git status *", "cp *", "npm run test *"}; !reflect.DeepEqual(scan.Always, want) {
		t.Errorf("Always = %#v, want %#v", scan.Always, want)
	}
}

func TestShellPathScannerSkipsDynamicAndKeepsGlobPrefix(t *testing.T) {
	scan := ScanShellPermissions(
		`rm /external/logs/*.txt; cat "$HOME/secret"; touch ~/outside/new`,
		ShellScanOptions{
			CWD:       "/repo",
			Workspace: "/repo",
			Shell:     "/bin/bash",
			Home:      "/home/test",
		},
	)
	want := []string{"/external", "/home/test/outside"}
	if !reflect.DeepEqual(scan.Dirs, want) {
		t.Fatalf("Dirs = %#v, want %#v", scan.Dirs, want)
	}
}
