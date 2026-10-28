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
	"github.com/Agent-Field/codeaf/internal/session"
)

const (
	appFollowQuietFirst = time.Second
	appFollowQuietNext  = 2 * time.Second
	appFollowQuietMax   = 5 * time.Second
)

func (f *appFollow) processState(state session.JobState) {
	f.ended.Store(state == session.JobDone || state == session.JobFailed || state == session.JobStopped)
}

// appFollowReader retains byte progress and an unfinished line across reads.
// The public output door keeps its separate line cursor.
type appFollowReader struct {
	info     os.FileInfo
	offset   int64
	pending  string
	active   bool
	openFile func(string) (appFollowFile, error)
	stat     func(string) (os.FileInfo, error)
}

type appFollowFile interface {
	io.ReadSeekCloser
	Stat() (os.FileInfo, error)
}

// start skips the complete lines already present, retaining a partial suffix.
func (r *appFollowReader) start(path string) error {
	file, err := r.open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, 64<<10)
	for {
		line, err := reader.ReadString('\n')
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

func (r *appFollowReader) open(path string) (appFollowFile, error) {
	var file appFollowFile
	var err error
	if r.openFile != nil {
		file, err = r.openFile(path)
	} else {
		file, err = os.Open(path)
	}
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	// Handle-derived identity survives closing the file, including on Windows.
	if r.changed(info) {
		r.close()
	}
	r.info = info
	return file, nil
}

func (r *appFollowReader) close() {
	r.info, r.offset, r.pending = nil, 0, ""
}

func (r *appFollowReader) changed(info os.FileInfo) bool {
	return r.info == nil || !os.SameFile(r.info, info) || info.Size() < r.info.Size() || info.Size() < r.offset ||
		(info.Size() == r.info.Size() && !info.ModTime().Equal(r.info.ModTime()))
}

func (r *appFollowReader) read(path string, limit int) ([]string, error) {
	r.active = false
	stat := r.stat
	if stat == nil {
		stat = os.Stat
	}
	info, err := stat(path)
	if errors.Is(err, os.ErrNotExist) {
		r.close()
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if r.changed(info) {
		r.close()
	} else if info.Size() <= r.offset {
		return nil, nil
	}
	r.active = true
	file, err := r.open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if _, err := file.Seek(r.offset, io.SeekStart); err != nil {
		return nil, err
	}
	reader := bufio.NewReaderSize(file, 64<<10)
	var lines []string
	for len(lines) < limit {
		line, err := reader.ReadString('\n')
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

// follow tails one spool until it is unfollowed or the backend closes. Only a
// known ended, drained spool backs off; running output keeps its fast schedule.
func (b *appBackend) follow(id, path string, reader *appFollowReader, f *appFollow) {
	defer close(f.done)
	defer reader.close()
	delay := appFollowEvery
	timer := time.NewTimer(delay)
	defer timer.Stop()
	for {
		select {
		case <-f.stop:
			return
		case <-timer.C:
		}
		lines, err := reader.read(path, appOutputLines)
		if err == nil && f.ended.Load() && !reader.active && reader.info != nil && reader.offset >= reader.info.Size() && reader.pending == "" {
			switch delay {
			case appFollowEvery:
				delay = appFollowQuietFirst
			case appFollowQuietFirst:
				delay = appFollowQuietNext
			default:
				delay = appFollowQuietMax
			}
		} else {
			delay = appFollowEvery
		}
		timer.Reset(delay)
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
