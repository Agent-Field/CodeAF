package shell

import (
	"os"
	"path/filepath"
	"testing"
)

// R1 pins the existing Unix open flags independently of the host's platform.
func TestUnixLogFlags(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "freebsd"} {
		if got, want := logFlags(goos), os.O_CREATE|os.O_WRONLY|os.O_APPEND; got != want {
			t.Fatalf("%s flags = %d, want %d", goos, got, want)
		}
	}
	if got, want := logFlags("windows"), os.O_CREATE|os.O_WRONLY; got != want {
		t.Fatalf("Windows flags = %d, want %d", got, want)
	}
}

// R1 keeps earlier output when a command reopens a log on either platform.
func TestOpenLogKeepsExistingOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "command.log")
	if err := os.WriteFile(path, []byte("earlier\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := OpenLog(path, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("later\n"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "earlier\nlater\n" {
		t.Fatalf("log = %q, %v", got, err)
	}
}
