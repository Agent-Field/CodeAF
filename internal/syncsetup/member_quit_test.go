package syncsetup

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/rafttransport"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

type quitPartitionConn struct {
	net.Conn
	down *atomic.Bool
}

func (c quitPartitionConn) Read(b []byte) (int, error) {
	for {
		n, err := c.Conn.Read(b)
		if err != nil || !c.down.Load() {
			return n, err
		}
	}
}
func (c quitPartitionConn) Write(b []byte) (int, error) {
	if c.down.Load() {
		return len(b), nil
	}
	return c.Conn.Write(b)
}

type quitPartitionWriter struct {
	http.ResponseWriter
	down *atomic.Bool
}

func (w quitPartitionWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	c, rw, err := w.ResponseWriter.(http.Hijacker).Hijack()
	if err != nil {
		return nil, nil, err
	}
	wrapped := quitPartitionConn{c, w.down}
	rw.Writer.Reset(wrapped)
	return wrapped, rw, nil
}

func partitionQuitPeers(server *http.Server) *atomic.Bool {
	down := &atomic.Bool{}
	original := server.Handler
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == rafttransport.SocketPath {
			w = quitPartitionWriter{w, down}
		}
		original.ServeHTTP(w, r)
	})
	return down
}

type quitChat struct {
	remote.WrappedAgent
	batch  *cellsync.Batcher
	result chan error
}

func (*quitChat) Model() string                      { return "" }
func (*quitChat) Title() string                      { return "" }
func (*quitChat) Usage() session.Usage               { return session.Usage{} }
func (*quitChat) ContextTokens() int                 { return 0 }
func (*quitChat) ReasoningLevels() map[string]string { return nil }
func (*quitChat) InterruptFor(session.StopDoor)      {}
func (a *quitChat) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	err := a.batch.Close(ctx)
	a.result <- err
	return err
}

func quitChatView(t *testing.T, s *Sync, id string) (*remote.Client, <-chan error) {
	t.Helper()
	v, err := s.Dir.Create(t.Context(), id, directory.CellInit{Head: strings.Repeat("1", 64)})
	mustNil(t, err)
	a := &quitChat{batch: &cellsync.Batcher{Publisher: &cellsync.Publisher{Dir: s.Dir}, Driving: &cellsync.Driving{Cell: cell.Cell{ID: id}, Fence: v.Cell.Lease.Fence}}, result: make(chan error, 1)}
	sess := remote.NewSession(&remote.Engine{Agent: a, ReleaseOnLeave: func(remote.WrappedAgent) bool { return true }}, true)
	surface, engine := remote.Pipe()
	go func() {
		_ = remote.ServeAttach(engine, engine, remote.AttachOptions{Open: func(remote.Hello) (*remote.Session, error) { return sess, nil }})
		_ = engine.Close()
	}()
	client, err := remote.Dial(surface, "loopback", remote.Hello{Version: remote.Version})
	mustNil(t, err)
	t.Cleanup(func() { _ = client.Close() })
	return client, a.result
}

// A clean quit requires closing releases to commit before the terminal's
// coordination member leaves, with no lease expiry or election ticks.
func TestNativeLeaderCleanQuitReleasesAndHandsBack(t *testing.T) {
	r := newMemberRig(t)
	p := r.home(nil)
	stop := hostedOwner(t, p)
	receipt := personalLeader(t, r, p)
	id := p.Identity
	b := r.home(&id)
	cells := []string{"01J00000000000000000QUIT1", "01J00000000000000000QUIT2"}
	results := []<-chan error{}
	for _, id := range cells {
		client, result := quitChatView(t, p, id)
		mustNil(t, client.Agent().DetachAndWait())
		mustNil(t, client.Close())
		results = append(results, result)
	}
	// The surface hands coordination back while its state owner still runs.
	mustNil(t, p.HandBack(t.Context()))
	stop()
	for i, id := range cells {
		select {
		case err := <-results[i]:
			mustNil(t, err)
		case <-time.After(requestTimeout):
			t.Fatal("closing release never completed")
		}
		v, err := b.Dir.Cell(t.Context(), id)
		mustNil(t, err)
		if v.Cell.Lease.ReleasedAt == 0 || v.Cell.Lease.Expires != 0 {
			t.Fatal("release did not commit", v)
		}
		_, err = b.Dir.Acquire(t.Context(), id, directory.AcquireOpts{})
		mustNil(t, err)
	}
	status, err := b.clusterClient().Status(t.Context())
	mustNil(t, err)
	if !slices.Contains([]uint64{1, 2, 3}, status.Leader) {
		t.Fatal("no anchor immediately coordinates", status)
	}
	for tick := 0; tick < 8; tick++ {
		r.clock.Advance(DefaultInterval)
	}
	status, err = b.clusterClient().Status(t.Context())
	mustNil(t, err)
	if !slices.Equal(status.Configuration.Voters, []uint64{1, 2, 3}) || status.Configuration.Joint() || !slices.Contains(status.Configuration.Learners, receipt.Node) || status.Membership.Leadership != nil {
		t.Fatal("anchors did not restore their topology", status)
	}
	readyMember(t, p)
	status, err = p.clusterClient().Status(t.Context())
	mustNil(t, err)
	if status.Leader == receipt.Node || !slices.Contains(status.Configuration.Learners, receipt.Node) {
		t.Fatal("returning member regained leadership", status)
	}
}

// A clean quit requires a learner or a never-started member to add no quit traffic.
func TestNativeNonLeaderAndAbsentMemberQuitIsQuiet(t *testing.T) {
	for _, started := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "learner"}[started], func(t *testing.T) {
			r := newMemberRig(t)
			p := r.home(nil)
			if started {
				readyMember(t, p)
				p.member.mu.Lock()
				defer p.member.mu.Unlock()
			}
			before := p.relayAttempts.Load() + p.clusterFrames.Load()
			done := make(chan error, 1)
			go func() { done <- p.HandBack(t.Context()) }()
			select {
			case err := <-done:
				mustNil(t, err)
			case <-time.After(time.Second):
				t.Fatal("non-leading quit waited for ongoing member work")
			}
			if p.relayAttempts.Load()+p.clusterFrames.Load() != before {
				t.Fatal("quiet quit sent requests")
			}
		})
	}
}

// The hand-back bounds a clean departure when an existing peer connection is lost;
// the same ordinary election clocks still restore the surviving anchors.
func TestNativeUnreachableLeaderQuitIsBounded(t *testing.T) {
	r := newMemberRig(t)
	down := partitionQuitPeers(r.server.Config)
	p := r.home(nil)
	stop := hostedOwner(t, p)
	receipt := personalLeader(t, r, p)
	down.Store(true)
	start := time.Now()
	if err := p.HandBack(t.Context()); err == nil {
		t.Fatal("unreachable anchors claimed a hand-back")
	}
	stop()
	if time.Since(start) > LeaderQuitWithin+time.Second {
		t.Fatal("quit exceeded its bound")
	}
	down.Store(false)
	for tick := 0; tick < 21; tick++ {
		r.clock.Advance(DefaultInterval)
	}
	status, err := p.clusterClient().Status(t.Context())
	mustNil(t, err)
	if status.Leader == receipt.Node || !slices.Equal(status.Configuration.Voters, []uint64{1, 2, 3}) {
		t.Fatal("ordinary fallback failed", status)
	}
}
