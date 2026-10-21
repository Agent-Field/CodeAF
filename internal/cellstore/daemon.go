package cellstore

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"sync/atomic"
	"time"
)

// wireVersion is the daemon protocol's V; every message carries it (L11).
const wireVersion = 1

// Daemon is the Transport that talks to the engine's long-lived process over
// its unix socket, starting it on first use. One dial per verb: the daemon,
// not the connection, is what stays warm.
type Daemon struct {
	Socket string
	// Binary is the engine to start the daemon from; empty means the
	// configured or embedded one.
	Binary string
}

var _ Transport = Daemon{}

type wireRequest struct {
	V      int             `json:"V"`
	ID     uint64          `json:"id"`
	Verb   string          `json:"verb"`
	Target *wireTarget     `json:"target,omitempty"`
	Args   json.RawMessage `json:"args"`
}

type wireTarget struct {
	DataDir string `json:"data_dir"`
	Tree    string `json:"tree"`
	CellDir string `json:"cell_dir,omitempty"`
}

type wireResponse struct {
	ID  uint64          `json:"id"`
	Ok  json.RawMessage `json:"ok"`
	Err string          `json:"err"`
}

var nextID atomic.Uint64

// Do implements Transport. Any failure before the daemon has answered is
// ErrUnavailable; an answer that is an error is the engine's own.
func (d Daemon) Do(ctx context.Context, t Target, op Op) ([]byte, error) {
	conn, err := d.connect(ctx)
	if err != nil {
		return nil, unavailable(err)
	}
	defer conn.Close()
	// The watch on ctx starts before the health check, so a cancelled take
	// frees a check that is waiting on a daemon that never answers.
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if err := d.verify(conn); err != nil {
		return nil, unavailable(err)
	}
	response, err := exchange(conn, newRequest(t, op))
	if err != nil {
		return nil, unavailable(err)
	}
	if response.Err != "" {
		return nil, fmt.Errorf("engine %s: %s", op.Verb(), response.Err)
	}
	return response.Ok, nil
}

// verify is the once-per-dial check that the daemon behind the socket is this
// engine and not a stale one from before an upgrade: any other identity is
// treated as no daemon at all, so the caller falls back to spawning. It only
// checks when the engine's name is known (d.Binary set).
func (d Daemon) verify(conn net.Conn) error {
	if d.Binary == "" {
		return nil
	}
	_ = conn.SetDeadline(time.Now().Add(healthWait))
	response, err := exchange(conn, wireRequest{V: wireVersion, ID: nextID.Add(1), Verb: "health"})
	_ = conn.SetDeadline(time.Time{}) // the verb itself may take as long as it needs
	if err != nil {
		return err
	}
	var health struct{ Engine string }
	_ = json.Unmarshal(response.Ok, &health)
	if want := filepath.Base(d.Binary); health.Engine != want {
		return fmt.Errorf("daemon is engine %q, want %q", health.Engine, want)
	}
	return nil
}

// healthWait is how long a daemon has to answer the health check. The check is
// one line each way on a local socket, so a daemon that takes longer is wedged;
// it is then treated as no daemon and the verb falls back to a spawned engine,
// where without this bound a caller with no deadline of its own waited for ever.
var healthWait = 5 * time.Second

// Stop asks the daemon to finish what it is doing and exit. A daemon that is
// not running is already stopped.
func (d Daemon) Stop(ctx context.Context) error {
	conn, err := dial(ctx, d.Socket)
	if err != nil {
		return nil
	}
	defer conn.Close()
	_, err = exchange(conn, wireRequest{V: wireVersion, ID: nextID.Add(1), Verb: "shutdown"})
	return err
}

func unavailable(err error) error { return fmt.Errorf("%w: %v", ErrUnavailable, err) }

func newRequest(t Target, op Op) wireRequest {
	args, _ := json.Marshal(op) // an Op is plain data
	return wireRequest{
		V: wireVersion, ID: nextID.Add(1), Verb: op.Verb(), Args: args,
		Target: &wireTarget{DataDir: t.DataDir, Tree: t.Tree, CellDir: t.CellDir},
	}
}

// exchange sends one request and reads its answer.
func exchange(conn net.Conn, req wireRequest) (wireResponse, error) {
	line, err := json.Marshal(req)
	if err != nil {
		return wireResponse{}, err
	}
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return wireResponse{}, err
	}
	reply, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return wireResponse{}, err
	}
	var response wireResponse
	if err := json.Unmarshal(reply, &response); err != nil {
		return wireResponse{}, err
	}
	if response.ID != req.ID {
		return wireResponse{}, fmt.Errorf("answer %d is not for request %d", response.ID, req.ID)
	}
	return response, nil
}

// connect dials the daemon, starting it when nothing listens.
func (d Daemon) connect(ctx context.Context) (net.Conn, error) {
	if conn, err := dial(ctx, d.Socket); err == nil {
		return conn, nil
	}
	return startAndDial(ctx, d)
}

func dial(ctx context.Context, socket string) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, "unix", socket)
}
