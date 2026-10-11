package serve

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

type replay struct {
	At     time.Time
	Status int
	Header http.Header
	Body   []byte
}

// replayCache is the fallback for hosts without a store. Store-backed hosts
// lock and persist each key so concurrent servers and restarts share responses.
type replayCache struct {
	mu   sync.Mutex
	rows map[string]replay
}

func (h *Handler) idempotent(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Idempotency-Key")
	if r.Method != http.MethodPost || key == "" {
		h.mux.ServeHTTP(w, r)
		return
	}
	// A response belongs to one credential and route, never another person's
	// bearer, browser cookie or worker key. Raw credentials never reach disk.
	sum := sha256.Sum256([]byte(r.Header.Get("Authorization") + "\x00" + r.Header.Get("Cookie") + "\x00" + r.URL.RequestURI() + "\x00" + key))
	id := hex.EncodeToString(sum[:])
	if h.config.Store == nil {
		h.replays.mu.Lock()
		defer h.replays.mu.Unlock()
		if h.replays.rows == nil {
			h.replays.rows = map[string]replay{}
		}
		for k, row := range h.replays.rows {
			if h.clock().Sub(row.At) >= 24*time.Hour {
				delete(h.replays.rows, k)
			}
		}
		row, ok := h.replays.rows[id]
		if !ok {
			row = h.capture(r)
			h.replays.rows[id] = row
		}
		sendReplay(w, row)
		return
	}
	dir := filepath.Join(h.config.Store.Root(), "idempotency")
	if h.failed(w, os.MkdirAll(dir, 0o700)) {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, id+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if h.failed(w, err) {
		return
	}
	defer f.Close()
	if h.failed(w, filelock.Lock(f, true, false)) {
		return
	}
	defer func() { _ = filelock.Unlock(f) }()
	path := filepath.Join(dir, id+".json")
	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		h.failed(w, err)
		return
	}
	var row replay
	if err == nil && h.failed(w, json.Unmarshal(raw, &row)) {
		return
	}
	if row.At.IsZero() || h.clock().Sub(row.At) >= 24*time.Hour {
		row = h.capture(r)
		raw, err = json.Marshal(row)
		if h.failed(w, err) {
			return
		}
		if h.failed(w, saveReplay(path+".tmp", raw)) {
			return
		}
		if h.failed(w, os.Rename(path+".tmp", path)) {
			return
		}
	}
	sendReplay(w, row)
}

func (h *Handler) capture(r *http.Request) replay {
	w := &replayWriter{header: make(http.Header)}
	h.mux.ServeHTTP(w, r)
	return replay{At: h.clock(), Status: w.status(), Header: w.Header().Clone(), Body: w.body.Bytes()}
}

func sendReplay(w http.ResponseWriter, row replay) {
	for k, vals := range row.Header {
		w.Header()[k] = append([]string(nil), vals...)
	}
	w.WriteHeader(row.Status)
	_, _ = w.Write(row.Body)
}

// replayWriter buffers only keyed POST responses; SSE remains on the real writer.
type replayWriter struct {
	header http.Header
	body   bytes.Buffer
	code   int
}

func (w *replayWriter) Header() http.Header { return w.header }
func (w *replayWriter) WriteHeader(code int) {
	if w.code == 0 {
		w.code = code
	}
}
func (w *replayWriter) Write(data []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	return w.body.Write(data)
}
func (w *replayWriter) status() int {
	if w.code == 0 {
		return http.StatusOK
	}
	return w.code
}

// Sync the first response before returning it, just as approval receipts sync
// before publishing. Rename then replaces an expired response atomically.
func saveReplay(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return f.Close()
}
