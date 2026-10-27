package main

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/redact"
)

// appFollowReader retains byte progress and an unfinished line across reads.
// The public output door keeps its separate line cursor.
type appFollowReader struct {
	file    *os.File
	reader  *bufio.Reader
	info    os.FileInfo
	offset  int64
	pending string
}

// start skips the complete lines already present, retaining a partial suffix.
func (r *appFollowReader) start(path string) error {
	if err := r.open(path); err != nil {
		return err
	}
	for {
		line, err := r.reader.ReadString('\n')
		r.offset += int64(len(line))
		if err != nil {
			r.pending = line
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func (r *appFollowReader) open(path string) error {
	r.close()
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return err
	}
	r.file, r.info = file, info
	r.reader = bufio.NewReaderSize(file, 64<<10)
	return nil
}

func (r *appFollowReader) close() {
	if r.file != nil {
		_ = r.file.Close()
	}
	*r = appFollowReader{}
}

func (r *appFollowReader) read(path string, limit int) ([]string, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		r.close()
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// A size-preserving rewrite can leave the cursor at EOF on the same inode.
	rewritten := r.info != nil && info.Size() == r.info.Size() && !info.ModTime().Equal(r.info.ModTime())
	if r.file == nil || !os.SameFile(r.info, info) || rewritten || info.Size() < r.info.Size() || info.Size() < r.offset+int64(r.reader.Buffered()) {
		if err := r.open(path); err != nil {
			return nil, err
		}
		info = r.info
	}
	r.info = info
	if info.Size() <= r.offset {
		return nil, nil
	}
	var lines []string
	for len(lines) < limit {
		line, err := r.reader.ReadString('\n')
		r.offset += int64(len(line))
		r.pending += line
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		lines = append(lines, redact.Secrets(strings.TrimRight(r.pending, "\r\n")))
		r.pending = ""
	}
	return lines, nil
}

// follow tails one spool until it is unfollowed or the backend closes. At a
// drained EOF a tick costs one stat; bytes are read only once the file grows,
// shrinks or is replaced. No watcher: on macOS a directory watch holds a
// descriptor for every spool beside this one.
func (b *appBackend) follow(id, path string, reader *appFollowReader, f *appFollow) {
	defer close(f.done)
	defer reader.close()
	ticker := time.NewTicker(appFollowEvery)
	defer ticker.Stop()
	for {
		select {
		case <-f.stop:
			return
		case <-ticker.C:
		}
		lines, err := reader.read(path, appOutputLines)
		if err != nil || len(lines) == 0 {
			continue
		}
		select {
		case <-f.stop:
			return
		default:
		}
		b.emit.Emit(appwire.EventProcessOutput, appwire.ProcessOutput{ID: id, Lines: lines})
	}
}
