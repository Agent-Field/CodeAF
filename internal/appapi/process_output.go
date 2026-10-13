package appapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/config"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var processURL = regexp.MustCompile(`(?:https?://)?(?:localhost|127\.0\.0\.1):[0-9]{1,5}(?:/[^\s\x00-\x20]*)?`)

const processOutputReadBytes = 64 << 10

func processTailLimit(query string) (int, error) {
	if query == "" {
		return processOutputReadBytes, nil
	}
	n, err := strconv.Atoi(query)
	if err != nil || n < 0 {
		return 0, errors.New("tail must be a non-negative number of bytes")
	}
	if n > MaxProcessOutputBytes {
		n = MaxProcessOutputBytes
	}
	return n, nil
}
func readProcessOutput(row processRow, limit int, now time.Time) (ProcessOutputResponse, error) {
	age := ProcessOutputRetention
	if config.AppMode() {
		_, age = config.AppOutputPolicyAt(config.ProfileDir())
	}
	return readProcessOutputWithAge(row, limit, now, age)
}
func readProcessOutputWithAge(row processRow, limit int, now time.Time, age time.Duration) (ProcessOutputResponse, error) {
	file, err := openProcessLog(row.File, row.Notice.LogPath)
	if err != nil {
		return ProcessOutputResponse{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return ProcessOutputResponse{}, err
	}
	if row.Process.State != "running" && info.ModTime().Before(now.Add(-age)) {
		return ProcessOutputResponse{}, os.ErrNotExist
	}
	offset := info.Size() - int64(limit)
	if offset < 0 {
		offset = 0
	}
	raw, err := io.ReadAll(io.NewSectionReader(file, offset, int64(limit)))
	if err != nil {
		return ProcessOutputResponse{}, err
	}
	for len(raw) > 0 && !utf8.RuneStart(raw[0]) {
		raw = raw[1:]
	}
	return ProcessOutputResponse{Text: strings.ToValidUTF8(string(raw), "�"), Truncated: offset > 0 || row.Notice.OutputTruncated}, nil
}
func (s *Server) processOutput(w http.ResponseWriter, r *http.Request) {
	limit, err := processTailLimit(r.URL.Query().Get("tail"))
	if err != nil {
		fail(w, 422, "bad_input", err.Error())
		return
	}
	follow := r.URL.Query().Get("follow")
	if follow != "" && follow != "0" && follow != "1" {
		fail(w, 422, "bad_input", "follow must be 0 or 1")
		return
	}
	row, err := s.findProcess(r.Context(), r.PathValue("pid"))
	if err != nil {
		agentFailure(w, err)
		return
	}
	_, age := config.AppOutputPolicyAt(s.options.ProfileDir)
	output, err := readProcessOutputWithAge(row, limit, s.options.Agents.now(), age)
	if err != nil {
		fail(w, 404, "not_found", fmt.Sprintf("process output is unavailable or expired after %d days", int(age/(24*time.Hour))))
		return
	}
	if follow != "1" {
		write(w, 200, output)
		return
	}
	s.followProcessOutput(w, r, row, limit)
}

// The follow reader tracks a byte offset and a small anchor. Compaction keeps
// the inode but moves its contents; the anchor then finds the unread suffix in
// the retained window, without rereading ten megabytes on every ordinary poll.
type processOutputCursor struct {
	offset      int64
	anchor      []byte
	initialized bool
}

func (c *processOutputCursor) read(row processRow, initialLimit int) ([]byte, error) {
	file, err := openProcessLog(row.File, row.Notice.LogPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	start := c.offset
	if !c.initialized {
		start = info.Size() - int64(initialLimit)
		if start < 0 {
			start = 0
		}
	} else if len(c.anchor) > 0 {
		probe := make([]byte, len(c.anchor))
		n, _ := file.ReadAt(probe, c.offset-int64(len(c.anchor)))
		if n != len(probe) || !bytes.Equal(probe, c.anchor) {
			raw, err := io.ReadAll(io.NewSectionReader(file, max(int64(0), info.Size()-MaxProcessOutputBytes), MaxProcessOutputBytes))
			if err != nil {
				return nil, err
			}
			at := bytes.LastIndex(raw, c.anchor)
			start = max(int64(0), info.Size()-MaxProcessOutputBytes)
			if at >= 0 {
				start = max(int64(0), info.Size()-MaxProcessOutputBytes) + int64(at+len(c.anchor))
			}
		}
	}
	if start > info.Size() {
		start = 0
	}
	raw, err := io.ReadAll(io.NewSectionReader(file, start, MaxProcessOutputBytes))
	if err != nil {
		return nil, err
	}
	if !c.initialized {
		for len(raw) > 0 && !utf8.RuneStart(raw[0]) {
			raw = raw[1:]
			start++
		}
	}
	c.offset = start + int64(len(raw))
	c.initialized = true
	count := int64(4096)
	if c.offset < count {
		count = c.offset
	}
	c.anchor = make([]byte, count)
	n, _ := file.ReadAt(c.anchor, c.offset-count)
	c.anchor = c.anchor[:n]
	return raw, nil
}
func (s *Server) followProcessOutput(w http.ResponseWriter, r *http.Request, row processRow, limit int) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	var pending []byte
	send := func(raw []byte) bool {
		if len(pending) > 0 {
			raw = append(pending, raw...)
			pending = nil
		}
		if row.Process.State == "running" && len(raw) > 0 {
			start := len(raw) - 1
			for start > 0 && !utf8.RuneStart(raw[start]) {
				start--
			}
			if !utf8.FullRune(raw[start:]) {
				pending = append([]byte{}, raw[start:]...)
				raw = raw[:start]
			}
		}
		_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if len(raw) > 0 {
			payload, _ := json.Marshal(ProcessOutputEvent{Process: row.Process.ID, Text: strings.ToValidUTF8(string(raw), "�")})
			if _, err := fmt.Fprintf(w, "event: process.output\ndata: %s\n\n", payload); err != nil {
				return false
			}
		}
		return controller.Flush() == nil
	}
	var cursor processOutputCursor
	raw, err := cursor.read(row, limit)
	if err != nil || !send(raw) {
		return
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	keepalive := time.NewTicker(20 * time.Second)
	defer keepalive.Stop()
	for {
		s.events.mu.Lock()
		changed, closed := s.events.changed, s.events.closed
		s.events.mu.Unlock()
		if closed {
			return
		}
		if row.Process.State != "running" {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-changed:
			continue
		case <-keepalive.C:
			_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
			if controller.Flush() != nil {
				return
			}
		case <-ticker.C:
			row, err = s.refreshProcess(row)
			if err != nil {
				return
			}
			raw, err = cursor.read(row, limit)
			if err != nil || !send(raw) {
				return
			}
		}
	}
}
