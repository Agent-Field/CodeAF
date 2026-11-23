package teams

import (
	"bufio"
	"errors"
	"github.com/Agent-Field/codeaf/internal/filememo"
	"io"
	"os"
	"sync"
)

// trafficMemory retains bounded queries, so a first tail still reads only its
// last windows. Later reads resume at the last complete line, including when
// a writer finished a partial line after the previous read.
type trafficMemory struct {
	mu    sync.Mutex
	held  map[trafficQuery]trafficReading
	tick  uint64
	reads int
	bytes int64
}

type trafficQuery struct {
	path, after string
	limit       int
}
type trafficReading struct {
	info     os.FileInfo
	offset   int64
	entries  []Entry
	used     uint64
	complete bool
}

var trafficCache trafficMemory

func (m *trafficMemory) read(q trafficQuery) ([]Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	info, err := os.Stat(q.path)
	if errors.Is(err, os.ErrNotExist) {
		delete(m.held, q)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	held, known := m.lookup(q, info)
	m.tick++
	if known && os.SameFile(held.info, info) && held.info.Size() == info.Size() && held.info.ModTime().Equal(info.ModTime()) {
		held.used = m.tick
		m.keep(q, held)
		return copyTraffic(selectTraffic(held.entries, q.after, q.limit)), nil
	}
	m.reads++
	var entries []Entry
	var offset int64
	complete := held.complete
	if known && os.SameFile(held.info, info) && info.Size() > held.info.Size() {
		entries, offset, err = m.appended(q.path, held.offset, info.Size())
		entries = append(copyTraffic(held.entries), entries...)
	} else {
		switch {
		case q.limit <= 0:
			entries, offset, err = m.appended(q.path, 0, info.Size())
		case q.after == "":
			entries, err = tailTrafficFile(q.path, q.limit)
		default:
			entries, err = forwardTrafficFile(q.path, q.after, q.limit)
		}
		complete = q.limit <= 0 || q.after == "" || len(entries) < q.limit
		if err == nil && q.limit > 0 {
			offset, err = completeTrafficOffset(q.path, info.Size())
		}
	}
	if err != nil {
		return nil, err
	}
	if q.after != "" && q.limit > 0 && len(selectTraffic(entries, q.after, 0)) > q.limit {
		// A truncated forward page no longer covers the suffix at its offset.
		complete = false
	}
	entries = selectTraffic(entries, q.after, q.limit)
	m.keep(q, trafficReading{info: info, offset: offset, entries: entries, used: m.tick, complete: complete})
	return copyTraffic(entries), nil
}

// lookup finds either the exact query or a newer suffix covering its cursor.
// The caller holds m.mu across lookup, reading and publication.
func (m *trafficMemory) lookup(q trafficQuery, info os.FileInfo) (trafficReading, bool) {
	held, known := m.held[q]
	// A cursor advancing through a remembered suffix can reuse its offset;
	// historical pages outside that suffix keep the binary-search reader.
	if q.after != "" {
		for key, candidate := range m.held {
			if key.path != q.path || !candidate.complete {
				continue
			}
			covered := len(candidate.entries) > 0 && q.after >= candidate.entries[0].ID
			if len(candidate.entries) == 0 {
				covered = q.after >= key.after && key.after != ""
			}
			if covered && (!known || newerTraffic(candidate, held, info)) {
				held, known = candidate, true
			}
		}
	}
	return held, known
}

// newerTraffic prefers an already current suffix over an older cursor's
// snapshot, so advancing a cursor cannot reread an append paid for elsewhere.
func newerTraffic(candidate, held trafficReading, now os.FileInfo) bool {
	if sameSpendFile(candidate.info, now) {
		return !sameSpendFile(held.info, now)
	}
	return !sameSpendFile(held.info, now) && candidate.info.ModTime().After(held.info.ModTime())
}

func (m *trafficMemory) keep(q trafficQuery, held trafficReading) {
	if m.held == nil {
		m.held = make(map[trafficQuery]trafficReading)
	}
	if _, ok := m.held[q]; !ok && len(m.held) >= filememo.MaxEntries {
		var oldest trafficQuery
		least := ^uint64(0)
		for key, value := range m.held {
			if value.used < least {
				oldest, least = key, value.used
			}
		}
		delete(m.held, oldest)
	}
	m.held[q] = held
}

func selectTraffic(entries []Entry, after string, limit int) []Entry {
	var out []Entry
	seen := map[string]bool{}
	for _, e := range entries {
		if !seen[e.ID] && (after == "" || e.ID > after) {
			out = append(out, e)
			seen[e.ID] = true
		}
	}
	if limit > 0 && len(out) > limit {
		if after == "" {
			out = out[len(out)-limit:]
		} else {
			out = out[:limit]
		}
	}
	return out
}

func copyTraffic(entries []Entry) []Entry {
	out := append([]Entry(nil), entries...)
	for i := range out {
		out[i].Handles = append([]string(nil), out[i].Handles...)
		out[i].Files = append([]string(nil), out[i].Files...)
	}
	return out
}

func (m *trafficMemory) appended(path string, offset, size int64) ([]Entry, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, offset, err
	}
	defer file.Close()
	r := bufio.NewReader(io.NewSectionReader(file, offset, max(0, size-offset)))
	var out []Entry
	for {
		line, err := r.ReadBytes('\n')
		m.bytes += int64(len(line))
		if len(line) > 0 && line[len(line)-1] == '\n' {
			offset += int64(len(line))
			if e, ok := parseTrafficLine(line); ok {
				out = append(out, e)
			}
		}
		if err == io.EOF {
			return out, offset, nil
		}
		if err != nil {
			return nil, offset, err
		}
	}
}

// completeTrafficOffset finds the last committed newline without decoding the
// tail again. Usually the last byte is already a newline.
func completeTrafficOffset(path string, size int64) (int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	var buf [4096]byte
	for end := size; end > 0; {
		start := max(int64(0), end-int64(len(buf)))
		n, err := file.ReadAt(buf[:end-start], start)
		if err != nil && err != io.EOF {
			return 0, err
		}
		for i := n - 1; i >= 0; i-- {
			if buf[i] == '\n' {
				return start + int64(i) + 1, nil
			}
		}
		end = start
	}
	return 0, nil
}
