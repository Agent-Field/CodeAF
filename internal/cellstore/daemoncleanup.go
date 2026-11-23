package cellstore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// pruneStaleSockets only visits the managed socket directory on a cold start.
// Furrow holds socket.lock for its lifetime, including before it listens.
func pruneStaleSockets(ctx context.Context, dir string) {
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if ctx.Err() != nil {
			return
		}
		name := entry.Name()
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(name, ".sock") || (!strings.HasPrefix(name, "e-") && !strings.HasPrefix(name, "engine-")) {
			continue
		}
		pruneStaleSocket(ctx, filepath.Join(dir, name))
	}
}

func pruneStaleSocket(ctx context.Context, path string) {
	if validateSocket(path) != nil {
		return
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return
	}
	defer lock.Close()
	if filelock.Lock(lock, true, true) != nil {
		return
	}
	defer filelock.Unlock(lock)
	// An older listener without a lock is live too. A timed-out probe is
	// inconclusive, so it keeps its endpoint rather than risking a live client.
	probe, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	conn, err := dial(probe, path)
	if err == nil {
		conn.Close()
		return
	}
	if probe.Err() == nil {
		_ = os.Remove(path)
	}
	// Never unlink the lock: a concurrent starter may already have it open.
}
