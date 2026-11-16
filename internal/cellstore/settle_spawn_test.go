//go:build !windows

package cellstore

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// settleDaemon answers health as engine and every other verb as a durable
// sync, writing mark when a sync arrives, and keeps the verbs it was asked.
type settleDaemon struct {
	mu    sync.Mutex
	verbs []string
}

func (d *settleDaemon) serve(t *testing.T, socket, engine, mark string) {
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				r := bufio.NewReader(conn)
				for {
					line, err := r.ReadBytes('\n')
					if err != nil {
						return
					}
					var req wireRequest
					_ = json.Unmarshal(line, &req)
					d.mu.Lock()
					d.verbs = append(d.verbs, req.Verb)
					d.mu.Unlock()
					ok, _ := json.Marshal(map[string]any{"engine": engine})
					if req.Verb == "sync" {
						ok, _ = json.Marshal(map[string]any{"durable": true})
						_ = os.WriteFile(mark, nil, 0o600)
					}
					reply, _ := json.Marshal(wireResponse{ID: req.ID, Ok: ok})
					_, _ = conn.Write(append(reply, '\n'))
				}
			}()
		}
	}()
}

func (d *settleDaemon) asked() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.verbs...)
}

// settleEngine is a one-shot engine program that succeeds only once mark
// exists, so it fails if it runs before the daemon was asked to sync.
func settleEngine(t *testing.T, mark string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "furrow-settle-test")
	script := "#!/bin/sh\ntest -f '" + mark + "' || { echo 'spawned before the daemon settled the store' >&2; exit 3; }\necho '{}'\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return bin
}

// A one-shot engine process opens a store the daemon may hold with a deferred
// tail, and such an open re-verifies the whole pack (a 600 MB chat's take-back
// paid it three times, docs/design/app-v3/MOVE-TIMING.md). So a spawn asks the
// running daemon to sync that store FIRST. Removing the settle makes the
// engine below run before the mark exists, and this test fails.
func TestSpawnSettlesTheStoreThroughARunningDaemonFirst(t *testing.T) {
	socket := testSocket(t)
	mark := filepath.Join(t.TempDir(), "synced")
	bin := settleEngine(t, mark)
	var daemon settleDaemon
	daemon.serve(t, socket, filepath.Base(bin), mark)
	data := t.TempDir()
	if _, err := (Spawn{Socket: socket, Binary: bin}).Do(context.Background(), Target{Tree: t.TempDir(), DataDir: data}, planOp{Snapshot: "s", Repo: data}); err != nil {
		t.Fatalf("spawn after settle: %v (daemon asked %v)", err, daemon.asked())
	}
	asked := daemon.asked()
	if len(asked) == 0 || asked[len(asked)-1] != "sync" {
		t.Fatalf("daemon was asked %v, want a sync before the spawn", asked)
	}
}

// With no daemon answering, a spawn neither starts one nor waits for one: it
// runs at once, as it always did.
func TestSpawnWithNoDaemonDoesNotWaitOrStartOne(t *testing.T) {
	socket := testSocket(t)
	mark := filepath.Join(t.TempDir(), "synced")
	if err := os.WriteFile(mark, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	bin := settleEngine(t, mark)
	data := t.TempDir()
	began := time.Now()
	if _, err := (Spawn{Socket: socket, Binary: bin}).Do(context.Background(), Target{Tree: t.TempDir(), DataDir: data}, planOp{Snapshot: "s", Repo: data}); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(began); took > 2*time.Second {
		t.Fatalf("spawn with no daemon took %s", took)
	}
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Fatalf("a spawn started a daemon at %s (stat: %v)", socket, err)
	}
}

// Every engine this process derives shares one ledger of files the secret
// scan found clean. A take's fresh engine screens the person's folder before
// it restores into it; with a ledger of its own it read every byte again (7 s
// of a 9.8 s take-back of a 600 MB folder, docs/design/app-v3/MOVE-TIMING.md).
func TestEveryDerivedEngineSharesTheCleanFileLedger(t *testing.T) {
	session, take := EngineFor(t.TempDir()), EngineFor("")
	if session.Guard.Ledger == nil || session.Guard.Ledger != take.Guard.Ledger {
		t.Fatal("a take's engine and a session's engine keep separate clean-file ledgers; the take re-reads every file the session already screened")
	}
}
