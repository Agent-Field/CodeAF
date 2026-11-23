package session

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// appendArtifactIndex is the only index append funnel. prepare runs under both
// locks, so lookup-through-append transactions reuse the lock without taking it
// twice. The lock coordinates processes sharing one local index; separate hosts
// with separate index files are out of scope.
func appendArtifactIndex(path string, prepare func() ([]byte, error), durable bool) error {
	if strings.TrimSpace(path) == "" {
		return fs.ErrInvalid
	}
	artifactsMu.Lock()
	defer artifactsMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// Lock the index inode so aliases meet on the same gate. filelock uses
	// flock on Unix and LockFileEx over a reserved non-content byte on Windows.
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := filelock.Lock(file, true, false); err != nil {
		return err
	}
	defer filelock.Unlock(file)
	line, err := prepare()
	if err != nil {
		return err
	}
	ended, err := shareIndexEnded(file)
	if err != nil {
		return err
	}
	if !ended {
		line = append([]byte{'\n'}, line...)
	}
	written, err := file.Write(line)
	if err == nil && written != len(line) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return err
	}
	if durable {
		return file.Sync()
	}
	return nil
}
