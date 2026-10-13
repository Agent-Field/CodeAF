package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/Agent-Field/codeaf/internal/guard"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// The elected runner accepts memory-only factory sessions over a private Unix
// socket. Another app or terminal shares its worker rather than registering one.
func (h *appFactoryHost) runnerControl() func() {
	path, err := factoryRunnerSocket(h.floor.Root())
	if err != nil {
		return func() {}
	}
	// The worker lock proves the previous listener's owner is gone.
	_ = os.Remove(path)
	listener, err := net.Listen("unix", path)
	if err != nil {
		return func() {}
	}
	if os.Chmod(path, 0600) != nil {
		listener.Close()
		return func() {}
	}
	server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/sessions" {
			w.WriteHeader(404)
			return
		}
		var sessions map[string]string
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		if decoder.Decode(&sessions) != nil || decoder.Decode(new(any)) != io.EOF {
			w.WriteHeader(422)
			return
		}
		for address, token := range sessions {
			if base, err := factoryURL(address); err == nil && base == address {
				h.credentials.SetFactorySession(address, token)
			}
		}
		w.WriteHeader(204)
	})}
	guard.Go("factory/runner control", func() { _ = server.Serve(listener) })
	return func() { _ = server.Close(); _ = os.Remove(path) }
}
func (h *appFactoryHost) shareRunnerSessions() {
	_, sessions := h.credentials.Snapshot()
	if len(sessions) == 0 {
		return
	}
	data, _ := json.Marshal(sessions)
	ctx, cancel := context.WithTimeout(h.ctx, 300*time.Millisecond)
	defer cancel()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		path, err := factoryRunnerSocket(h.floor.Root())
		if err != nil {
			return nil, err
		}
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}}
	defer transport.CloseIdleConnections()
	request, _ := http.NewRequestWithContext(ctx, "POST", "http://runner/sessions", bytes.NewReader(data))
	response, err := (&http.Client{Transport: transport}).Do(request)
	if err == nil {
		response.Body.Close()
	}
}
func (h *appFactoryHost) electWorker(ticks <-chan time.Time) (func(), bool) {
	for h.ctx.Err() == nil {
		if release, ok := h.floor.TryWorker(); ok {
			return release, true
		}
		h.shareRunnerSessions()
		select {
		case <-h.ctx.Done():
			return nil, false
		case <-ticks:
		}
	}
	return nil, false
}

// The runtime socket stays outside the state home, whose files are portable
// records and can be scanned for secrets. Its path is stable across TMPDIRs.
func factoryRunnerSocket(root string) (string, error) {
	runtimeDir := filepath.Join("/tmp", "codeaf-runners-"+strconv.Itoa(os.Getuid()))
	if err := os.MkdirAll(runtimeDir, 0700); err != nil {
		return "", err
	}
	info, err := os.Lstat(runtimeDir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("factory runner runtime directory is not private")
	}
	if err := os.Chmod(runtimeDir, 0700); err != nil {
		return "", err
	}
	hash := sha256.Sum256([]byte(filepath.Clean(root)))
	return filepath.Join(runtimeDir, hex.EncodeToString(hash[:16])+".sock"), nil
}
