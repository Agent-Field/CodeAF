// Package canvasrpc runs canvas-core for an organization server.
//
// An organization's canvases live on its server, and every read and write of
// one goes through the `canvas` command's server door, `canvas rpc` (the desk
// repository's crates/canvas, CONTRACT §10): one JSON request on stdin, one
// JSON answer on stdout. NOTHING ABOUT A CANVAS IS RE-IMPLEMENTED IN GO — the
// validation, the op log, versions, marks and the one-writer-per-frame rule
// are canvas-core's, exactly as on a laptop, so an org canvas and a Personal
// canvas cannot drift apart.
//
// The server decides who is asking and whether they may; this package only
// carries the request. Requests for one canvas are taken one at a time, so a
// hand check and the write it guards are never split by another person's
// write.
package canvasrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/env"
)

// BinEnv names the `canvas` binary when it is not beside codeaf or on PATH.
const BinEnv = "CODEAF_CANVAS_BIN"

// callTimeout bounds one request: canvas-core answers in milliseconds, and a
// request that hangs must not hold a canvas's turn forever.
const callTimeout = 30 * time.Second

// maxAnswer bounds what one answer may be (a large canvas's snapshot or op
// log), so a runaway answer is refused rather than read into memory whole.
const maxAnswer = 64 << 20

// Find is the `canvas` binary this server runs, or "" when it has none: the
// one $CODEAF_CANVAS_BIN names, else `canvas` beside the running codeaf, else
// `canvas` on PATH. A server without one keeps no canvases, and says so by
// leaving the routes off rather than answering every one with an error.
func Find() string {
	if p := strings.TrimSpace(env.Get(BinEnv)); p != "" {
		if runnable(p) {
			return p
		}
		return ""
	}
	if exe, err := os.Executable(); err == nil {
		if p := filepath.Join(filepath.Dir(exe), "canvas"); runnable(p) {
			return p
		}
	}
	if p, err := exec.LookPath("canvas"); err == nil {
		return p
	}
	return ""
}

func runnable(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular() && st.Mode().Perm()&0o111 != 0
}

// Error is canvas-core's refusal: a stable code (invalid, not_found, stale,
// usage, io, db, …) and a sentence that says what to fix.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Sidecar runs `Bin rpc --root Root`. Root holds one directory per canvas.
type Sidecar struct {
	Bin  string
	Root string

	mu    sync.Mutex
	turns map[string]*sync.Mutex
}

// New is a sidecar keeping its canvases under root.
func New(bin, root string) *Sidecar {
	return &Sidecar{Bin: bin, Root: root, turns: map[string]*sync.Mutex{}}
}

// turn is canvas id's lock; "" (a create or an import, which make a new
// canvas) shares one.
func (s *Sidecar) turn(id string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.turns == nil {
		s.turns = map[string]*sync.Mutex{}
	}
	m := s.turns[id]
	if m == nil {
		m = &sync.Mutex{}
		s.turns[id] = m
	}
	return m
}

// Call sends req (a JSON object with a "cmd") about canvas id and decodes the
// answer into out (nil to drop it). A refusal is an [*Error]; anything else
// that goes wrong (the binary missing, killed, an answer that is not JSON) is
// a plain error.
func (s *Sidecar) Call(ctx context.Context, id string, req map[string]any, out any) error {
	raw, err := s.Raw(ctx, id, req)
	if err != nil || out == nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

// Raw is [Sidecar.Call]'s answer as canvas-core wrote it.
func (s *Sidecar) Raw(ctx context.Context, id string, req map[string]any) (json.RawMessage, error) {
	if id != "" {
		req["id"] = id
	}
	in, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(s.Root, 0o700); err != nil {
		return nil, err
	}
	t := s.turn(id)
	t.Lock()
	defer t.Unlock()
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.Bin, "rpc", "--root", s.Root)
	cmd.Stdin = bytes.NewReader(in)
	// canvas-core reads no other setting for rpc; a clean environment keeps
	// the server's own (its tokens, its HOME) out of the child.
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
	var stdout limited
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	if stdout.over {
		return nil, fmt.Errorf("the canvas answer is larger than %d bytes", maxAnswer)
	}
	answer := bytes.TrimSpace(stdout.buf.Bytes())
	var refused struct {
		Error *Error `json:"error"`
	}
	if len(answer) > 0 && json.Unmarshal(answer, &refused) == nil && refused.Error != nil && refused.Error.Code != "" {
		return nil, refused.Error
	}
	if runErr != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("the canvas did not answer within %s", callTimeout)
		}
		return nil, fmt.Errorf("the canvas program failed: %v %s", runErr, strings.TrimSpace(stderr.String()))
	}
	if !json.Valid(answer) {
		return nil, errors.New("the canvas program answered something that is not JSON")
	}
	return json.RawMessage(answer), nil
}

// limited is a buffer that stops taking bytes past maxAnswer.
type limited struct {
	buf  bytes.Buffer
	over bool
}

func (l *limited) Write(p []byte) (int, error) {
	if l.buf.Len()+len(p) > maxAnswer {
		l.over = true
		return 0, errors.New("answer too large")
	}
	return l.buf.Write(p)
}
