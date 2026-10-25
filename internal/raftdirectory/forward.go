package raftdirectory

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/clusterctl"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftstore"
	"github.com/Agent-Field/codeaf/internal/rafttransport"
	"github.com/coder/websocket"
)

type forwardLink struct {
	send    chan clusterctl.Forward
	pending map[string]chan clusterctl.Forward
	conn    *websocket.Conn
}

func (g *Native) signal() {
	if g.changed != nil {
		close(g.changed)
	}
	g.changed = make(chan struct{})
}

// waitProgress releases ONLY the member queue. Public callers retain opMu,
// while authenticated peer callbacks finish persistence and application.
func (g *Native) waitProgress(ctx context.Context) error {
	if g.changed == nil {
		g.changed = make(chan struct{})
	}
	changed := g.changed
	g.mu.Unlock()
	select {
	case <-ctx.Done():
		g.mu.Lock()
		return directory.ErrUnreachable
	case <-changed:
	}
	g.mu.Lock()
	if g.closed || g.failed != nil {
		return directory.ErrUnreachable
	}
	return nil
}
func (g *Native) replica() int {
	if i := g.leader(); i >= 0 {
		return i
	}
	best := -1
	for i, s := range g.status {
		if g.nodes[i] != nil && (best < 0 || s.Applied > g.status[best].Applied) {
			best = i
		}
	}
	return best
}
func (g *Native) leaderID() uint64 {
	if i := g.leader(); i >= 0 {
		return g.status[i].NodeID
	}
	i := g.replica()
	if i < 0 {
		return 0
	}
	return g.status[i].Leader
}
func (g *Native) forward(ctx context.Context, node uint64, work clusterctl.Forward) (clusterctl.Forward, error) {
	var empty clusterctl.Forward
	if work.ID == "" {
		g.serial++
		work.ID = fmt.Sprintf("forward-%d", g.serial)
	}
	empty.ID = work.ID
	link := g.forwarders[node]
	if link == nil || len(link.pending) >= rafttransport.MaxInflight {
		return empty, directory.ErrUnreachable
	}

	work.Scope = g.peers.view.Scope
	work.Generation = g.peers.view.Generation
	work.From = g.manifest.Config.Anchors[0]
	work.To = node
	work.Sign(func(b []byte) []byte { return ed25519.Sign(g.peers.keys[0], b) })
	result := make(chan clusterctl.Forward, 1)
	link.pending[work.ID] = result
	defer delete(link.pending, work.ID)
	select {
	case link.send <- work:
	default:
		return empty, directory.ErrUnreachable
	}
	g.mu.Unlock()
	select {
	case empty = <-result:
	case <-ctx.Done():
	}
	g.mu.Lock()
	if ctx.Err() != nil {
		return empty, directory.ErrUnreachable
	}
	if empty.ID != work.ID || empty.Error != "" {
		return empty, directory.ErrUnreachable
	}
	return empty, nil
}

// ForwardHTTP authorizes every frame against CURRENT committed enrollment.
// The socket belongs to the computer dialing out; the relay never dials P.
func (g *Native) ForwardHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.RawQuery != "" {
		http.Error(w, "invalid member", http.StatusBadRequest)
		return
	}
	raw, err := base64.RawURLEncoding.DecodeString(r.Header.Get(rafttransport.SocketAuthHeader))
	var auth rafttransport.Request
	if err != nil || len(raw) > rafttransport.MaxExchangeBytes || rafttransport.Decode(raw, &auth) != nil || len(auth.Messages) != 0 {
		http.Error(w, "invalid member", 403)
		return
	}
	g.mu.Lock()
	valid := g.forwardAuthorized(auth)
	if valid && g.forwarders[auth.Node] != nil {
		valid = false
	}
	g.mu.Unlock()
	if !valid {
		http.Error(w, "member authorization refused", 403)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(rafttransport.MaxExchangeBytes)
	link := &forwardLink{send: make(chan clusterctl.Forward, rafttransport.MaxInflight), pending: map[string]chan clusterctl.Forward{}, conn: conn}
	g.mu.Lock()
	if !g.forwardAuthorized(auth) || g.forwarders[auth.Node] != nil {
		g.mu.Unlock()
		return
	}
	if g.forwarders == nil {
		g.forwarders = map[uint64]*forwardLink{}
	}
	g.forwarders[auth.Node] = link
	g.signal()
	g.mu.Unlock()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case f := <-link.send:
				g.mu.Lock()
				valid := g.forwardAuthorized(auth)
				g.mu.Unlock()
				if !valid {
					return
				}
				b, _ := rafttransport.Encode(f)
				write, c := context.WithTimeout(ctx, clusterctl.ForwardTimeout)
				err := conn.Write(write, websocket.MessageBinary, b)
				c()
				if err != nil {
					return
				}
			}
		}
	}()
	defer func() {
		cancel()
		conn.CloseNow()
		<-done
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.forwarders[auth.Node] == link {
			delete(g.forwarders, auth.Node)
		}
		for _, ch := range link.pending {
			select {
			case ch <- clusterctl.Forward{}:
			default:
			}
		}
		g.signal()
	}()
	for {
		kind, b, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var f clusterctl.Forward
		if kind != websocket.MessageBinary || rafttransport.Decode(b, &f) != nil {
			return
		}
		g.mu.Lock()
		peer, ok := g.peer(auth.Node)
		valid := g.forwardAuthorized(auth) && ok && f.Scope == auth.Scope && f.Generation == auth.Generation && f.From == auth.Node && f.To == g.manifest.Config.Anchors[0] && f.Verify(peer.Key)
		if valid {
			if ch := link.pending[f.ID]; ch != nil {
				select {
				case ch <- f:
				default:
				}
			}
		}
		g.mu.Unlock()
		if !valid {
			return
		}
	}
}
func (g *Native) forwardAuthorized(auth rafttransport.Request) bool {
	if g.closed || g.peers == nil || auth.Scope != g.peers.view.Scope || auth.Generation != g.peers.view.Generation {
		return false
	}
	peer, ok := g.peer(auth.Node)
	return ok && peer.Incarnation == auth.Incarnation && auth.Verify(peer.Key) == nil
}

// appliedReplica waits for the relay's OWN durable application through the
// exact forwarded admission/read index. A cached result alone cannot reply.
func (g *Native) appliedReplica(ctx context.Context, index uint64) (int, raftstore.Application, error) {
	for {
		i := g.replica()
		if i < 0 || index == 0 {
			return -1, raftstore.Application{}, directory.ErrUnreachable
		}
		app, _, err := g.stores[i].ApplicationView(ctx)
		if err != nil {
			return i, app, err
		}
		if app.Applied >= index {
			return i, app, nil
		}
		if err = g.waitProgress(ctx); err != nil {
			return i, app, err
		}
	}
}

// boundedCall refuses a caller already gone BEFORE admission. Once admitted,
// its existing control deadline bounds the wait, while disk completion outlives
// a lost HTTP reply. Expired queued calls must not create more durable work.
func boundedCall(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx.Err() != nil {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(context.WithoutCancel(ctx), clusterctl.ForwardTimeout)
}

// PersonalDeparted treats a leading voter's outbound connection closing as
// activity, including a graceful sync-off. It wakes the ordinary election
// clock; it grants no vote, removal authority or successful quorum response.
func (g *Native) PersonalDeparted(node uint64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || g.peers == nil {
		return false
	}
	peer, ok := g.peer(node)
	if !ok || !peer.Voter {
		return false
	}
	g.peers.sleeping = false
	for _, core := range g.nodes {
		if core != nil {
			_, _ = core.Schedule(raftcore.ScheduleInput{Busy: true})
		}
	}
	g.signal()
	return true
}

// refreshHint polls only already hosted cores. The resulting address remains a
// hint: a retry must still commit the original request or finish a fresh read.
func (g *Native) refreshHint(ctx context.Context) error {
	for i, node := range g.nodes {
		if node != nil {
			if err := g.drive(ctx, i, raftcore.Poll{}); err != nil {
				return err
			}
		}
	}
	return g.drain(ctx)
}
