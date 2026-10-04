package directorytest

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Agent-Field/codeaf/internal/directory"
)

// The waits of the watch suite. A frame the relay owes arrives well inside
// frameWait even on a slow live relay; a frame it must not send is given
// quietWindow to show up, which is long enough to catch a bump that is sent
// after the write's answer and short enough to keep a case fast.
const (
	frameWait   = 5 * time.Second
	quietWindow = 300 * time.Millisecond
)

var versionFrame = regexp.MustCompile(`^\{"v":[0-9]+\}$`)

// watchEnv is what one case works with.
type watchEnv struct {
	t       *testing.T
	rig     WatchRig
	env     env
	changes *int // how many device records changeSomething has written
}

func newWatchEnv(t *testing.T, r WatchRig) watchEnv {
	return watchEnv{t: t, rig: r, env: envOf(r.Rig), changes: new(int)}
}

func (w watchEnv) dirAs(device string) directory.Client { return w.rig.Devices(device) }

// frame is one thing a socket delivered: a message, or the error that ended it.
type frame struct {
	kind websocket.MessageType
	text string
	err  error
}

// sock is an open watch socket with a reader goroutine, so that a case can ask
// "is there a frame" without a cancelled read closing the connection.
type sock struct {
	t      *testing.T
	conn   *websocket.Conn
	frames chan frame
	last   uint64
	resp   *http.Response // the handshake answer, when the socket was dialled by a case
}

// open dials the watch route as a device and starts reading it.
func (w watchEnv) open(device string) *sock {
	w.t.Helper()
	return w.openWith(w.rig.Sign(device))
}

func (w watchEnv) openWith(sign func(*http.Request, []byte)) *sock {
	w.t.Helper()
	dialCtx, cancel := context.WithTimeout(context.Background(), frameWait)
	defer cancel()
	conn, resp, err := DialWatch(dialCtx, w.rig.Base, sign)
	if err != nil {
		w.t.Fatalf("watch dial: %v (%s)", err, describe(resp))
	}
	return startSock(w.t, conn)
}

func startSock(t *testing.T, conn *websocket.Conn) *sock {
	readCtx, stop := context.WithCancel(context.Background())
	s := &sock{t: t, conn: conn, frames: make(chan frame, 256)}
	t.Cleanup(func() { stop(); conn.CloseNow() })
	go s.pump(readCtx)
	return s
}

func (s *sock) pump(ctx context.Context) {
	for {
		kind, data, err := s.conn.Read(ctx)
		s.frames <- frame{kind: kind, text: string(data), err: err}
		if err != nil {
			return
		}
	}
}

// next is the next delivery, failing the case when none comes in time.
func (s *sock) next() frame {
	s.t.Helper()
	select {
	case f := <-s.frames:
		return f
	case <-time.After(frameWait):
		s.t.Fatalf("no frame within %s", frameWait)
		return frame{}
	}
}

// text is the next delivery, which must be a text message.
func (s *sock) text() string {
	s.t.Helper()
	f := s.next()
	if f.err != nil {
		s.t.Fatalf("socket ended while a frame was due: %v", f.err)
	}
	if f.kind != websocket.MessageText {
		s.t.Fatalf("frame type %v, want text", f.kind)
	}
	return f.text
}

// version is the next delivery, which must be a version frame that never goes
// down from the last one this socket saw.
func (s *sock) version() uint64 {
	s.t.Helper()
	text := s.text()
	n, ok := parseVersion(text)
	if !ok {
		s.t.Fatalf("frame %q does not match %s", text, versionFrame)
	}
	if n < s.last {
		s.t.Fatalf("version went down from %d to %d", s.last, n)
	}
	s.last = n
	return n
}

func parseVersion(text string) (uint64, bool) {
	if !versionFrame.MatchString(text) {
		return 0, false
	}
	var v struct{ V uint64 }
	if json.Unmarshal([]byte(text), &v) != nil {
		return 0, false
	}
	return v.V, true
}

// wantVersion asserts the next delivery is exactly {"v":n}.
func (s *sock) wantVersion(n uint64) {
	s.t.Helper()
	if got := s.version(); got != n {
		s.t.Fatalf("version frame %d, want %d", got, n)
	}
}

// wantSilent asserts nothing arrives, not even a close, for quietWindow.
func (s *sock) wantSilent() {
	s.t.Helper()
	select {
	case f := <-s.frames:
		s.t.Fatalf("unexpected delivery %q (err %v) on a socket that should be quiet", f.text, f.err)
	case <-time.After(quietWindow):
	}
}

// send writes one client frame.
func (s *sock) send(kind websocket.MessageType, msg string) {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), frameWait)
	defer cancel()
	if err := s.conn.Write(ctx, kind, []byte(msg)); err != nil {
		s.t.Fatalf("send %q: %v", msg, err)
	}
}

// wantClosed reads to the end of the socket and asserts it closed with code
// and reason. It returns the versions heard before the close, in order.
func (s *sock) wantClosed(code websocket.StatusCode, reason string) []uint64 {
	s.t.Helper()
	var heard []uint64
	for {
		f := s.next()
		if f.err == nil {
			n, ok := parseVersion(f.text)
			if !ok {
				s.t.Fatalf("frame %q before the close is not a version", f.text)
			}
			heard = append(heard, n)
			continue
		}
		s.wantCloseError(f.err, code, reason)
		return heard
	}
}

func (s *sock) wantCloseError(err error, code websocket.StatusCode, reason string) {
	s.t.Helper()
	var ce websocket.CloseError
	if !errors.As(err, &ce) || websocket.CloseStatus(err) != code || ce.Reason != reason {
		s.t.Fatalf("closed with %v, want code %d reason %q", err, code, reason)
	}
}

// describe names a refused handshake's status for a failure message.
func describe(resp *http.Response) string {
	if resp == nil {
		return "no response"
	}
	return resp.Status
}

// wantRefusal asserts a dial was refused before the upgrade with status and
// the wire body {"err":code}.
func wantRefusal(t *testing.T, conn *websocket.Conn, resp *http.Response, err error, status int, code string) {
	t.Helper()
	if conn != nil {
		conn.CloseNow()
		t.Fatalf("dial succeeded, want %d %s", status, code)
	}
	if err == nil || resp == nil {
		t.Fatalf("dial = %v, %v; want a %d answer", resp, err, status)
	}
	wantAnswer(t, resp, status, code)
}

// wantAnswer asserts an ordinary HTTP answer's status and error code.
func wantAnswer(t *testing.T, resp *http.Response, status int, code string) {
	t.Helper()
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, directory.MaxBody))
	var body struct{ Err string }
	_ = json.Unmarshal(raw, &body)
	if resp.StatusCode != status || body.Err != code {
		t.Fatalf("answer %d %q, want %d {\"err\":%q}", resp.StatusCode, raw, status, code)
	}
}

// listVersion reads the list as a device and returns its Codeaf-Dir-Version.
func (w watchEnv) listVersion(device string) uint64 {
	w.t.Helper()
	req, err := http.NewRequest(http.MethodGet, w.rig.Base+"/v1/dir/list", nil)
	if err != nil {
		w.t.Fatal(err)
	}
	w.rig.Sign(device)(req, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		w.t.Fatal(err)
	}
	defer resp.Body.Close()
	n, perr := strconv.ParseUint(resp.Header.Get(directory.VersionHeader), 10, 64)
	if resp.StatusCode != http.StatusOK || perr != nil {
		w.t.Fatalf("list answered %d with %s=%q", resp.StatusCode, directory.VersionHeader, resp.Header.Get(directory.VersionHeader))
	}
	return n
}
