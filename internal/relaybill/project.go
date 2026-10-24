package relaybill

import (
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/session"
)

// smallRepo is what the moved chat works on: a few source files and one binary
// of a few kilobytes, the size of a person's first project.
func smallRepo(dir string) (string, error) {
	blob := make([]byte, 9000)
	for i := range blob {
		blob[i] = byte(i * 31)
	}
	files := map[string][]byte{
		"README.md":     []byte("# project\n"),
		"src/main.go":   []byte("package main\n\nfunc main() {}\n"),
		"src/util.go":   []byte("package main\n\nfunc util() int { return 1 }\n"),
		"data/blob.bin": blob,
	}
	for path, body := range files {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(full, body, 0o644); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// newChat makes the chat cell the first machine starts with, carrying the
// session record and transcript header every real chat has so that a machine
// that takes it finds the project.
func newChat(root, work string) (cell.Cell, error) {
	c, err := cell.CreateIn(root, cell.Options{Class: cell.HostBound})
	if err != nil {
		return c, err
	}
	meta := session.Meta{ID: c.ID, Title: "heavy user", Workspace: work, Created: time.Now()}
	if err := session.SaveMeta(c.Root, meta); err != nil {
		return c, err
	}
	header := `{"type":"session","version":3,"id":"` + c.ID + `","cwd":"` + work + `","timestamp":"2026-09-29T10:00:00Z"}` + "\n"
	return c, appendTo(session.Place{Dir: c.Root}.Transcript(), header)
}
