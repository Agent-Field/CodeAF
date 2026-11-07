package agentfield

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/redact"
)

// ── THE LINK: THE NODE DIALS OUT, THE CONTROL PLANE TALKS BACK ──────────────
//
// The node opens GET /api/v1/nodes/<id>/link on the control plane, a
// WebSocket authenticated by `Authorization: Bearer <API key>` on the upgrade
// (never the query string), and keeps it open. Down it come the control
// plane's dispatches and cancels; up it go the job's status, progress and
// completion, and the lease's heartbeat. BECAUSE THE NODE DIALS, A FACTORY
// BEHIND NAT NEEDS NO LISTENING PORT AT ALL.
//
// The link is a carrier, not a second implementation, on both ends:
//
//   - a dispatch frame carries the HTTP dispatch's exact body and headers,
//     and is fed to the SAME routes the HTTP reasoner serves (reasoners.go),
//     as the request it replaces; the route's answer is the ack (202 →
//     accepted, other 2xx → done with the body as result, else rejected);
//   - a status or progress frame is the POST /executions/:id/status body,
//     which the control plane re-enters through its own handler and answers
//     with that handler's status code, read here as an HTTP answer would be;
//   - a heartbeat is the lease PATCH, with the open executions as inflight.
//
// EVERY DELIVERY IS DEDUPED HERE: by its delivery id, by the dispatch's
// X-Execution-ID and by the trigger's idempotency key. A redelivery (the
// control plane resends until acked, and resends everything unacked on a
// reconnect) is acked `duplicate` and runs nothing; the inbox dedupes again
// durably on (source, id).
//
// Reconnect is exponential backoff with full jitter, half a second to thirty,
// reset after a minute connected. A control plane that answers the upgrade
// 404 with no node message offers no link: the node says so, keeps the lease
// and the HTTP door (when it has a base_url), and asks again every few
// minutes.

// Frame types, the control plane's internal/nodelink spelling.
const (
	tWelcome   = "welcome"
	tDeliver   = "deliver"
	tOK        = "ok"
	tHello     = "hello"
	tAck       = "ack"
	tStatus    = "status"
	tProgress  = "progress"
	tLog       = "log"
	tHeartbeat = "heartbeat"
)

// frameVersion is the envelope's version, and maxFrame the largest frame.
const (
	frameVersion = 1
	maxFrame     = 1 << 20
)

var knownFrames = map[string]bool{tWelcome: true, tDeliver: true, tOK: true, tHello: true, tAck: true, tStatus: true, tProgress: true, tLog: true, tHeartbeat: true}

// frame is the one envelope.
type frame struct {
	V    int             `json:"v"`
	T    string          `json:"t"`
	ID   string          `json:"id,omitempty"`
	Exec string          `json:"exec,omitempty"`
	Seq  int64           `json:"seq,omitempty"`
	TS   string          `json:"ts,omitempty"`
	Body json.RawMessage `json:"body,omitempty"`
}

type deliverBody struct {
	Kind           string            `json:"kind"`
	Reasoner       string            `json:"reasoner,omitempty"`
	Input          json.RawMessage   `json:"input,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
	Reason         string            `json:"reason,omitempty"`
}

type ackBody struct {
	Outcome string          `json:"outcome"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   string          `json:"error,omitempty"`
}

type helloBody struct {
	InstanceID string   `json:"instance_id"`
	SDK        string   `json:"sdk,omitempty"`
	Version    string   `json:"version,omitempty"`
	Inflight   []string `json:"inflight,omitempty"`
}

type heartbeat struct {
	Inflight []string `json:"inflight,omitempty"`
}

type welcomeBody struct {
	LinkID     string `json:"link_id"`
	HeartbeatS int    `json:"heartbeat_s"`
}

type okBody struct {
	Code  int    `json:"code"`
	Error string `json:"error,omitempty"`
}

// encodeFrame builds one frame with body (nil for none).
func encodeFrame(f frame, body any) ([]byte, error) {
	f.V = frameVersion
	if f.TS == "" {
		f.TS = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if !knownFrames[f.T] {
		return nil, fmt.Errorf("unknown frame type %q", f.T)
	}
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		f.Body = raw
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return nil, err
	}
	if len(raw) > maxFrame {
		return nil, errors.New("a link frame is at most 1 MiB")
	}
	return raw, nil
}

// decodeFrame reads one frame, refusing another version, an unknown type and
// anything over 1 MiB.
func decodeFrame(raw []byte) (frame, error) {
	if len(raw) > maxFrame {
		return frame{}, errors.New("a link frame is at most 1 MiB")
	}
	var f frame
	if err := json.Unmarshal(raw, &f); err != nil {
		return frame{}, fmt.Errorf("not a link frame: %w", err)
	}
	if f.V != frameVersion {
		return frame{}, fmt.Errorf("link frame version %d, want %d", f.V, frameVersion)
	}
	if !knownFrames[f.T] {
		return frame{}, fmt.Errorf("unknown frame type %q", f.T)
	}
	return f, nil
}

// linkSession is one live link.
type linkSession struct {
	conn    *websocket.Conn
	id      string
	wmu     sync.Mutex
	seq     atomic.Int64
	mu      sync.Mutex
	waiting map[int64]chan okBody
	closed  chan struct{}
	once    sync.Once
}

// callTimeoutLink bounds one up-frame's wait for its ok.
const callTimeoutLink = 15 * time.Second

func (ls *linkSession) write(ctx context.Context, raw []byte) error {
	ls.wmu.Lock()
	defer ls.wmu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return ls.conn.Write(ctx, websocket.MessageText, raw)
}

// call sends one up-frame and waits for the control plane's ok to it.
func (ls *linkSession) call(ctx context.Context, f frame, body any) (okBody, error) {
	f.Seq = ls.seq.Add(1)
	raw, err := encodeFrame(f, body)
	if err != nil {
		return okBody{}, err
	}
	ch := make(chan okBody, 1)
	ls.mu.Lock()
	if ls.waiting == nil {
		ls.mu.Unlock()
		return okBody{}, errors.New("the link is down")
	}
	ls.waiting[f.Seq] = ch
	ls.mu.Unlock()
	defer func() {
		ls.mu.Lock()
		if ls.waiting != nil {
			delete(ls.waiting, f.Seq)
		}
		ls.mu.Unlock()
	}()
	if err := ls.write(ctx, raw); err != nil {
		return okBody{}, err
	}
	t := time.NewTimer(callTimeoutLink)
	defer t.Stop()
	select {
	case ok := <-ch:
		return ok, nil
	case <-ls.closed:
		return okBody{}, errors.New("the link dropped")
	case <-ctx.Done():
		return okBody{}, ctx.Err()
	case <-t.C:
		return okBody{}, errors.New("the control plane did not answer down the link in time")
	}
}

func (ls *linkSession) answered(seq int64, ok okBody) {
	ls.mu.Lock()
	ch := ls.waiting[seq]
	ls.mu.Unlock()
	if ch != nil {
		select {
		case ch <- ok:
		default:
		}
	}
}

func (ls *linkSession) close() {
	ls.once.Do(func() {
		close(ls.closed)
		ls.mu.Lock()
		ls.waiting = nil
		ls.mu.Unlock()
	})
}

func (ls *linkSession) send(ctx context.Context, f frame, body any) error {
	raw, err := encodeFrame(f, body)
	if err != nil {
		return err
	}
	return ls.write(ctx, raw)
}

// liveLink is the link now, or nil.
func (n *Node) liveLink() *linkSession {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.running == nil {
		return nil
	}
	return n.running.link
}

// LinkURL is the link's address on the control plane at base for node.
func LinkURL(base, node string) (string, error) {
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	default:
		return "", errors.New("the AgentField address is not http or https")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/v1/nodes/" + url.PathEscape(node) + "/link"
	return u.String(), nil
}

// linkBackoff is the wait before the next try after failed tries in a row:
// full jitter over half a second doubling to thirty.
func linkBackoff(failed int) time.Duration {
	ceiling := 500 * time.Millisecond
	for i := 1; i < failed && ceiling < 30*time.Second; i++ {
		ceiling *= 2
	}
	ceiling = min(ceiling, 30*time.Second)
	return time.Duration(rand.Int64N(int64(ceiling))) + 50*time.Millisecond
}

// linkNotOfferedWait is how long a node waits to ask again a control plane
// that has no link route.
const linkNotOfferedWait = 2 * time.Minute

// errNotOffered is an upgrade the control plane does not serve at all.
var errNotOffered = errors.New("this AgentField control plane offers no node link")

// linkLoop keeps the link up until ctx ends.
func (n *Node) linkLoop(ctx context.Context, r *running) {
	select {
	case <-ctx.Done():
		return
	case <-r.registered:
	}
	target, err := LinkURL(r.settings.URL, r.settings.NodeID)
	if err != nil {
		n.trouble("the AgentField link address: " + err.Error())
		return
	}
	failed := 0
	for ctx.Err() == nil {
		began := time.Now()
		err := n.runLink(ctx, r, target)
		if ctx.Err() != nil {
			return
		}
		wait := time.Duration(0)
		switch {
		case errors.Is(err, errNotOffered):
			n.linkState("not offered")
			wait = linkNotOfferedWait
		default:
			if time.Since(began) > time.Minute {
				failed = 0
			}
			failed++
			n.linkState("connecting")
			if err != nil {
				n.trouble("the AgentField link: " + err.Error())
			}
			wait = linkBackoff(failed)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func (n *Node) linkState(state string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.health.Link != state {
		n.health.Link, n.health.LinkSince = state, n.now()
	}
}

// runLink dials once and serves the link until it drops.
func (n *Node) runLink(ctx context.Context, r *running, target string) error {
	hdr := http.Header{}
	if r.secrets.Key != "" {
		hdr.Set("Authorization", "Bearer "+r.secrets.Key)
	}
	hdr.Set("X-Agent-Instance-ID", r.instance)
	hdr.Set("User-Agent", "codeaf-factory")
	dialCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	conn, resp, err := websocket.Dial(dialCtx, target, &websocket.DialOptions{HTTPHeader: hdr, HTTPClient: n.Conn.HTTP})
	cancel()
	if err != nil {
		if resp != nil {
			var raw []byte
			if resp.Body != nil {
				raw, _ = io.ReadAll(io.LimitReader(resp.Body, 2048))
			}
			switch {
			case resp.StatusCode == http.StatusNotFound && !strings.Contains(string(raw), "not registered"):
				return errNotOffered
			case resp.StatusCode == http.StatusUnauthorized:
				return errors.New("AgentField did not accept that API key for the link")
			}
			return fmt.Errorf("the upgrade was answered %d: %s", resp.StatusCode, answerMessage(raw))
		}
		return errors.New(redact.Secrets(err.Error()))
	}
	conn.SetReadLimit(maxFrame)
	defer conn.CloseNow()

	_, raw, err := conn.Read(ctx)
	if err != nil {
		return err
	}
	f, err := decodeFrame(raw)
	if err != nil || f.T != tWelcome {
		return errors.New("the control plane did not welcome the link")
	}
	var welcome welcomeBody
	_ = json.Unmarshal(f.Body, &welcome)
	ls := &linkSession{conn: conn, id: welcome.LinkID, waiting: map[int64]chan okBody{}, closed: make(chan struct{})}
	defer ls.close()

	readErr := make(chan error, 1)
	guard.Go("factory/agentfield link read", func() { readErr <- n.readLink(ctx, r, ls) })

	ok, err := ls.call(ctx, frame{T: tHello}, helloBody{InstanceID: r.instance, SDK: "codeaf-factory", Version: n.Version, Inflight: n.inflight()})
	if err != nil {
		return fmt.Errorf("the link's hello was not answered: %w", err)
	}
	if ok.Code == http.StatusNotFound {
		return errors.New("the control plane does not know this node yet")
	}

	n.mu.Lock()
	if n.running == r {
		r.link = ls
	}
	n.health.Link, n.health.LinkSince = "up", n.now()
	n.health.LinkUps++
	ups := n.health.LinkUps
	n.health.LastError, n.health.LastErrorAt = "", time.Time{}
	n.mu.Unlock()
	log.Printf("agentfield: link up %s to %s as %s (instance %s, up #%d)", ls.id, redact.Secrets(target), r.settings.NodeID, r.instance, ups)
	defer func() {
		n.mu.Lock()
		if r.link == ls {
			r.link = nil
		}
		n.mu.Unlock()
		log.Printf("agentfield: link %s down", ls.id)
	}()

	// A REPORT THAT DID NOT LAND WHILE THE LINK WAS DOWN GOES NOW: a control
	// plane that restarted mid-run hears the job's completion on reconnect.
	guard.Go("factory/agentfield link catch-up", func() { n.catchUp(ctx, r) })

	pingDone := make(chan struct{})
	defer close(pingDone)
	guard.Go("factory/agentfield link ping", func() {
		t := time.NewTicker(20 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-pingDone:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
				err := conn.Ping(pctx)
				cancel()
				if err != nil {
					conn.CloseNow()
					return
				}
			}
		}
	})

	select {
	case <-ctx.Done():
		_ = conn.Close(websocket.StatusGoingAway, "node stopping")
		return nil
	case err := <-readErr:
		return err
	}
}

// readLink reads frames until the link drops.
func (n *Node) readLink(ctx context.Context, r *running, ls *linkSession) error {
	for {
		_, raw, err := ls.conn.Read(ctx)
		if err != nil {
			ls.close()
			if s := websocket.CloseStatus(err); s != -1 {
				return fmt.Errorf("the control plane closed the link (%d)", s)
			}
			return errors.New("the link dropped: " + redact.Secrets(err.Error()))
		}
		f, err := decodeFrame(raw)
		if err != nil {
			continue
		}
		switch f.T {
		case tOK:
			var ok okBody
			_ = json.Unmarshal(f.Body, &ok)
			ls.answered(f.Seq, ok)
		case tDeliver:
			// A delivery runs beside the reader, so pings and oks keep
			// flowing while a dispatch is filed.
			guard.Go("factory/agentfield link deliver", func() { n.onDeliver(ctx, r, ls, f) })
		}
	}
}

// viaHeader marks a request the link fed to the routes. The HTTP door strips
// it, so only the link can say a call came by the link.
const viaHeader = "X-Codeaf-Via"

// viaHTTP is the HTTP door's routes: it strips viaHeader before they run.
func viaHTTP(_ *Node, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Del(viaHeader)
		next.ServeHTTP(w, r)
	})
}

// onDeliver handles one deliver frame and acks it.
func (n *Node) onDeliver(ctx context.Context, r *running, ls *linkSession, f frame) {
	ack := func(a ackBody) {
		if err := ls.send(ctx, frame{T: tAck, ID: f.ID, Exec: f.Exec}, a); err != nil {
			log.Printf("agentfield: ack of %s not sent: %v", f.ID, redact.Secrets(err.Error()))
		}
	}
	var b deliverBody
	if err := json.Unmarshal(f.Body, &b); err != nil {
		ack(ackBody{Outcome: "rejected", Error: "the delivery's body is not JSON"})
		return
	}
	exec := f.Exec
	if exec == "" && b.Headers != nil {
		exec = b.Headers["X-Execution-ID"]
	}
	keys := []string{"dlv:" + f.ID}
	if b.Kind == "dispatch" {
		if exec != "" {
			keys = append(keys, "exec:"+exec)
		}
		if b.IdempotencyKey != "" {
			keys = append(keys, "idem:"+b.IdempotencyKey)
		}
	}
	_, filed := n.table().byID(exec)
	if (b.Kind == "dispatch" && exec != "" && filed) || !r.seen.claim(keys...) {
		log.Printf("agentfield: %s %s for execution %s arrived over the link again: acked duplicate", b.Kind, f.ID, exec)
		ack(ackBody{Outcome: "duplicate"})
		return
	}
	switch b.Kind {
	case "dispatch":
		log.Printf("agentfield: dispatch %s for execution %s (%s) arrived over the link", f.ID, exec, b.Reasoner)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/reasoners/"+url.PathEscape(b.Reasoner), bytes.NewReader(b.Input))
		if err != nil {
			r.seen.forget(keys...)
			ack(ackBody{Outcome: "rejected", Error: "the delivery names no reasoner this node serves"})
			return
		}
		for k, v := range b.Headers {
			req.Header.Set(k, v)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(viaHeader, "link")
		rec := httptest.NewRecorder()
		r.mux.ServeHTTP(rec, req)
		switch {
		case rec.Code == http.StatusAccepted:
			ack(ackBody{Outcome: "accepted"})
		case rec.Code >= 200 && rec.Code < 300:
			ack(ackBody{Outcome: "done", Result: json.RawMessage(bytes.TrimSpace(rec.Body.Bytes()))})
		default:
			// A refusal is not remembered: the same delivery may be tried
			// again (after the person fixes what was refused).
			r.seen.forget(keys...)
			ack(ackBody{Outcome: "rejected", Error: strconv.Itoa(rec.Code) + ": " + answerMessage(rec.Body.Bytes())})
		}
		log.Printf("agentfield: dispatch %s answered %d over the link", f.ID, rec.Code)
	case "cancel":
		log.Printf("agentfield: cancel %s for execution %s arrived over the link", f.ID, exec)
		n.cancelExecution(ctx, exec, b.Reason, "link")
		ack(ackBody{Outcome: "accepted"})
	default:
		ack(ackBody{Outcome: "rejected", Error: "this node takes dispatch and cancel deliveries only"})
	}
}

// cancelCall is POST /_internal/executions/{id}/cancel over HTTP.
func (n *Node) cancelCall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reason string `json:"reason"`
	}
	raw, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, maxCall))
	_ = json.Unmarshal(raw, &body)
	via := "http"
	if r.Header.Get(viaHeader) == "link" {
		via = "link"
	}
	found := n.cancelExecution(r.Context(), r.PathValue("id"), body.Reason, via)
	writeJSON(w, http.StatusOK, map[string]any{"cancelled": found})
}

// cancelExecution stops the job behind execution exec, the way a person's
// Stop does, and closes the execution here: the control plane has already
// ended it, so nothing more is told about it. It says whether exec was one
// this node holds.
func (n *Node) cancelExecution(ctx context.Context, exec, reason, via string) bool {
	e, ok := n.table().byID(exec)
	if !ok || !e.Ended.IsZero() {
		return ok
	}
	_ = n.table().said(e.ID, "cancelled", true, n.now())
	what := "AgentField cancelled execution " + exec + ": stopped job " + strconv.Itoa(e.Job)
	if reason != "" {
		what += " (" + oneLine(reason, 120) + ")"
	}
	if n.Cancel != nil {
		if err := n.Cancel(ctx, e.Job); err != nil {
			n.trouble("stopping job " + strconv.Itoa(e.Job) + " for a cancel from AgentField: " + err.Error())
			what = "AgentField cancelled execution " + exec + "; job " + strconv.Itoa(e.Job) + " could not be stopped: " + err.Error()
		}
	}
	n.mu.Lock()
	n.health.LastEventVia = via
	n.mu.Unlock()
	n.heard(SourceName(ExecuteSource), what)
	log.Printf("agentfield: %s", what)
	return true
}

// report tells the control plane u for execution id: up the link when it is
// up, over HTTP when it is not or the link could not carry it. The answer is
// read the same way either way.
func (n *Node) report(ctx context.Context, c *Client, id string, u ExecutionUpdate) error {
	if ls := n.liveLink(); ls != nil {
		t := tStatus
		if u.Status == ExecRunning {
			t = tProgress
		}
		ok, err := ls.call(ctx, frame{T: t, Exec: id}, u)
		if err == nil {
			if ok.Code >= 200 && ok.Code < 300 {
				return nil
			}
			return &Error{Code: ok.Code, Refusal: refusalOf(ok.Code), Op: "execution", Message: answerMessage([]byte(ok.Error))}
		}
	}
	return c.Report(ctx, id, u)
}

// seenSet is a bounded set of delivery keys, oldest forgotten first.
type seenSet struct {
	mu    sync.Mutex
	max   int
	m     map[string]bool
	order []string
}

func newSeenSet(max int) *seenSet { return &seenSet{max: max, m: map[string]bool{}} }

// claim records keys and says true when none was seen before; when any was,
// it records nothing and says false.
func (s *seenSet) claim(keys ...string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range keys {
		if s.m[k] {
			return false
		}
	}
	for _, k := range keys {
		s.m[k] = true
		s.order = append(s.order, k)
	}
	for len(s.order) > s.max {
		delete(s.m, s.order[0])
		s.order = s.order[1:]
	}
	return true
}

func (s *seenSet) forget(keys ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range keys {
		delete(s.m, k)
	}
}
