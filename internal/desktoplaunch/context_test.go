package desktoplaunch

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGUIHomeDoesNotReplaceExplicitOrCLIWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
	got, err := Workspace("", true)
	if err != nil || got != home {
		t.Fatalf("GUI workspace %q %v", got, err)
	}
	explicit := filepath.Join(t.TempDir(), "project")
	got, err = Workspace(explicit, true)
	if err != nil || got != explicit {
		t.Fatalf("explicit workspace %q %v", got, err)
	}
	cwd, _ := os.Getwd()
	got, err = Workspace("", false)
	if err != nil || got != cwd {
		t.Fatalf("CLI workspace %q %v", got, err)
	}
}

func TestMacGUIRecoversLoginToolsWithoutInventingPrefixes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX login PATH semantics")
	}
	bin := t.TempDir()
	tool := filepath.Join(bin, "gh")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	calls := 0
	path := loginPath(true, "darwin", "/bin/zsh", "/usr/bin:/bin", func(ctx context.Context, shell, script string) ([]byte, error) {
		calls++
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("unbounded shell")
		}
		if shell != "/bin/zsh" || !strings.Contains(script, `/usr/bin/printenv PATH`) {
			t.Fatal("fixed PATH query missing")
		}
		return []byte("profile startup noise" + markerStart + bin + ":.:/usr/bin:" + bin + markerEnd + "ignored tail"), nil
	})
	t.Setenv("PATH", path)
	found, err := exec.LookPath("gh")
	if err != nil || found != tool || calls != 1 {
		t.Fatalf("tool lookup %q %v; path %q", found, err, path)
	}
	if strings.Contains(path, ":.:") || path != bin+":/usr/bin:/bin" {
		t.Fatalf("unsafe or duplicated path %q", path)
	}
}

func TestCLIAndOtherPlatformsNeverExecuteLoginShell(t *testing.T) {
	for _, tc := range []struct {
		gui      bool
		platform string
	}{{false, "darwin"}, {true, "linux"}, {true, "windows"}} {
		got := loginPath(tc.gui, tc.platform, "/bin/zsh", "original", func(context.Context, string, string) ([]byte, error) {
			t.Fatal("unexpected shell execution")
			return nil, nil
		})
		if got != "original" {
			t.Fatal(got)
		}
	}
}

func TestBadLoginOutputOrShellFailureRetainsInheritedPath(t *testing.T) {
	for _, data := range []string{"noise", markerStart + "relative" + markerEnd, markerStart + "/bin\nother" + markerEnd, strings.Repeat("x", maxOutput+1)} {
		got := loginPath(true, "darwin", "/bin/zsh", "/usr/bin", func(context.Context, string, string) ([]byte, error) { return []byte(data), nil })
		if got != "/usr/bin" {
			t.Fatalf("bad output changed PATH to %q", got)
		}
	}
	got := loginPath(true, "darwin", "/bin/zsh", "/usr/bin", func(context.Context, string, string) ([]byte, error) { return nil, errors.New("startup failed") })
	if got != "/usr/bin" {
		t.Fatal(got)
	}
	got = loginPath(true, "darwin", "relative-shell", "/usr/bin", func(context.Context, string, string) ([]byte, error) {
		t.Fatal("relative shell executed")
		return nil, nil
	})
	if got != "/usr/bin" {
		t.Fatal(got)
	}
}

func TestActualShellFramingAndOutputBound(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX login shell")
	}
	output, err := runShell(context.Background(), "/bin/sh", `/usr/bin/printf '\nDESKTOP_LOGIN_PATH_BEGIN\n'; /usr/bin/printenv PATH; /usr/bin/printf 'DESKTOP_LOGIN_PATH_END\n'`)
	if err != nil || !strings.Contains(string(output), markerStart) || !strings.Contains(string(output), markerEnd) {
		t.Fatalf("framing failed %v", err)
	}
	var bounded boundedOutput
	if _, err := bounded.Write(make([]byte, maxOutput)); err != nil {
		t.Fatal(err)
	}
	if _, err := bounded.Write([]byte("x")); err == nil {
		t.Fatal("unbounded output")
	}
}
