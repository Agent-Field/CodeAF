package stream

import (
	"os"
	"path/filepath"
)

func paths(glob string) ([]string, error) { return filepath.Glob(glob) }

func read(t interface{ Fatal(...any) }, name string) string {
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
