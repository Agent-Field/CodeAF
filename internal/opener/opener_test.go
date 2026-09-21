package opener

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A WSL box without xdg-open reaches the Windows desktop through wslview; one
// that has xdg-open keeps it, because that is what every chat door has always
// called and what a person's browser choice is wired through.
func TestWSLFallsToTheDesktopBridgeOnlyWithoutXdgOpen(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("WSL is a Linux environment")
	}
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
	if name, arguments := Command(); name != "wslview" || len(arguments) != 0 {
		t.Fatalf("WSL opener without xdg-open = %q %v, want wslview", name, arguments)
	}
	if err := os.WriteFile(filepath.Join(bin, "xdg-open"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if name, _ := Command(); name != "xdg-open" {
		t.Fatalf("WSL opener with xdg-open on PATH = %q, want xdg-open", name)
	}
	t.Setenv("WSL_DISTRO_NAME", "")
	if name, _ := Command(); name != "xdg-open" {
		t.Fatalf("plain Linux opener = %q, want xdg-open", name)
	}
}

func TestStartReportsWhenTheBrowserProcessCannotStart(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the test controls the Linux opener through PATH")
	}
	t.Setenv("PATH", t.TempDir())
	t.Setenv("WSL_DISTRO_NAME", "")
	err := Start("https://example.test/sign-in")
	if err == nil || err.Error() != "the browser did not open" {
		t.Fatalf("Start error = %v, want a synchronous browser start failure", err)
	}
}
