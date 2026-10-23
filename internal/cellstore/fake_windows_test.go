package cellstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// K1/K3: a native executable fixture proves the same read-only help probes.
func init() {
	executable, _ := os.Executable()
	if strings.HasPrefix(filepath.Base(executable), "c3-fake-") {
		body, err := os.ReadFile(executable + ".help")
		if err != nil {
			os.Exit(2)
		}
		os.Stdout.Write(body)
		os.Exit(0)
	}
}

func fakeNativeEngine(t *testing.T, help string) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "c3-fake-engine.exe")
	body, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".help", []byte(help), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
