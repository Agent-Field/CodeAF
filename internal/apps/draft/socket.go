package draft

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Request and Reply are the server verbs' wire over SocketFile.
type Request struct {
	Verb string   `json:"verb"`
	Args []string `json:"args"`
}

type Reply struct {
	OK   bool   `json:"ok"`
	Text string `json:"text"`
}

// Handler answers one server verb.
type Handler func(context.Context, Request) Reply

const (
	dialTimeout    = 2 * time.Second
	requestTimeout = 10 * time.Second
)

// Serve answers server verbs on <dir>/builder.sock, one request per
// connection, and removes the socket when stop is called or ctx ends. The
// socket is owner-only: the folder is the build's, and so is the answer.
func Serve(ctx context.Context, dir string, h Handler) (stop func(), err error) {
	path := socketPath(dir)
	_ = os.Remove(path) // a stale socket from a build that died
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	if err := publish(dir, path); err != nil {
		ln.Close()
		os.Remove(path)
		return nil, err
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); acceptLoop(ctx, ln, h, &wg) }()
	var once sync.Once
	stop = func() {
		once.Do(func() {
			ln.Close()
			wg.Wait()
			_ = os.Remove(path)
			_ = os.Remove(filepath.Join(dir, AddrFile))
		})
	}
	go func() { <-ctx.Done(); stop() }()
	return stop, nil
}

func acceptLoop(ctx context.Context, ln net.Listener, h Handler, wg *sync.WaitGroup) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		wg.Add(1)
		go func() { defer wg.Done(); serveConn(ctx, conn, h) }()
	}
}

func serveConn(ctx context.Context, conn net.Conn, h Handler) {
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(requestTimeout))
	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(Reply{Text: "bad request: " + err.Error()})
		return
	}
	_ = json.NewEncoder(conn).Encode(h(ctx, req))
}

// maxSocketPath keeps below the sun_path limit of the platforms codeaf runs on
// (104 on macOS, 108 on Linux), with room to spare.
const maxSocketPath = 100

// socketPath is the one place that decides where a build's socket lives: in
// the build folder, or, when that path is too long for a unix socket, in the
// temp folder under a name taken from the folder's hash. Serve and the CLI
// both go through it.
func socketPath(dir string) string {
	inside := filepath.Join(dir, SocketFile)
	if len(inside) <= maxSocketPath {
		return inside
	}
	sum := sha256.Sum256([]byte(dir))
	return filepath.Join(os.TempDir(), "codeaf-b-"+hex.EncodeToString(sum[:])[:12]+".sock")
}

// publish writes builder.addr when the socket lives elsewhere, so the CLI
// finds it from the build folder alone.
func publish(dir, path string) error {
	if path == filepath.Join(dir, SocketFile) {
		// Never leave a pointer from an earlier, deeper run.
		if err := os.Remove(filepath.Join(dir, AddrFile)); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return os.WriteFile(filepath.Join(dir, AddrFile), []byte(path), 0o600)
}

// dialPath is where the CLI dials: the address file when present, else the
// socket path for the folder.
func dialPath(dir string) string {
	if raw, err := os.ReadFile(filepath.Join(dir, AddrFile)); err == nil && len(raw) > 0 {
		return strings.TrimSpace(string(raw))
	}
	return socketPath(dir)
}

// errNoBuilder is what a server verb answers when nothing listens.
var errNoBuilder = &Error{Code: ExitNoBuilder, Lines: []string{"the Builder is not running"}}

// call dials the Builder once. There is no retry: a worker that sees exit 3
// knows at once that server verbs are not available.
func call(dir string, req Request) (Reply, error) {
	conn, err := net.DialTimeout("unix", dialPath(dir), dialTimeout)
	if err != nil {
		return Reply{}, errNoBuilder
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Reply{}, errNoBuilder
	}
	var reply Reply
	if err := json.NewDecoder(conn).Decode(&reply); err != nil {
		return Reply{}, errors.New("the Builder closed the connection without an answer")
	}
	return reply, nil
}
