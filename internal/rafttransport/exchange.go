package rafttransport

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/coder/websocket"
)

const (
	ExchangePath     = "/v2/cluster/exchange"
	SocketPath       = "/v2/cluster/socket"
	SocketAuthHeader = "Codeaf-Cluster-Auth"
	socketTimeout    = 30 * time.Second
)

// Request signs polls and acknowledgments as well as outbound digests. A
// stolen unsigned cursor must not allow a third party to drain a peer's inbox.
type Request struct {
	// Applied is signed evidence from the member after its application transaction.
	// A transport acknowledgment or leader Match alone never makes it ready.
	Applied     uint64            `json:"applied,string,omitempty"`
	Scope       Scope             `json:"scope"`
	Node        uint64            `json:"node,string"`
	Incarnation uint64            `json:"incarnation,string"`
	Generation  uint64            `json:"generation,string"`
	Ack         [laneCount]uint64 `json:"ack"`
	Messages    []Envelope        `json:"messages"`
	Signature   []byte            `json:"signature"`
}

func decimal(n uint64) string { return strconv.FormatUint(n, 10) }
func parseDecimal(s string) (uint64, error) {
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil || decimal(n) != s {
		return 0, ErrWire
	}
	return n, nil
}

// Ack is encoded through decimal strings even though it is a fixed array.
func (r Request) MarshalJSON() ([]byte, error) {
	type raw Request
	return json.Marshal(struct {
		raw
		Ack [laneCount]string `json:"ack"`
	}{raw(r), [laneCount]string{decimal(r.Ack[0]), decimal(r.Ack[1]), decimal(r.Ack[2])}})
}
func (r *Request) UnmarshalJSON(b []byte) error {
	type raw Request
	var v struct {
		raw
		Ack [laneCount]string `json:"ack"`
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&v) != nil {
		return ErrWire
	}
	*r = Request(v.raw)
	for i, s := range v.Ack {
		n, err := parseDecimal(s)
		if err != nil {
			return err
		}
		r.Ack[i] = n
	}
	return nil
}
func (r Request) signingBytes() []byte {
	r.Signature = nil
	b, _ := Encode(r)
	return append([]byte("codeaf/raft/exchange/1\n"), b...)
}
func (r *Request) Sign(key ed25519.PrivateKey) error {
	if len(key) != ed25519.PrivateKeySize {
		return ErrAuth
	}
	return r.SignWith(func(b []byte) []byte { return ed25519.Sign(key, b) })
}

// SignWith uses the same canonical bytes without exporting a device seed.
func (r *Request) SignWith(sign func([]byte) []byte) error {
	if sign == nil {
		return ErrAuth
	}
	if err := r.validate(); err != nil {
		return err
	}
	r.Signature = sign(r.signingBytes())
	if len(r.Signature) != ed25519.SignatureSize {
		return ErrAuth
	}
	return nil
}
func (r Request) Verify(key ed25519.PublicKey) error {
	if err := r.validate(); err != nil {
		return err
	}
	if len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, r.signingBytes(), r.Signature) {
		return ErrAuth
	}
	return nil
}
func (r Request) validate() error {
	if !validScope(r.Scope) || !node(r.Node) || r.Incarnation == 0 || r.Generation == 0 || len(r.Messages) > MaxMessages {
		return ErrWire
	}
	normal, chunks := 0, 0
	for _, e := range r.Messages {
		if e.Chunk != nil {
			chunks++
		} else {
			normal += len(e.Payload)
		}
	}
	if normal > MaxBatchBytes || chunks > 1 || chunks != 0 && len(r.Messages) != 1 {
		return ErrWire
	}
	b, err := Encode(r)
	if err != nil || len(b) > MaxExchangeBytes {
		return ErrWire
	}
	return nil
}

// Handler mounts only transport routes. It never discovers or bootstraps an
// identity, and protocol traffic bypasses the public command admission budget.
func Handler(router *Router, now func() time.Time) http.Handler {
	if now == nil {
		now = time.Now
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.RawQuery != "" {
			refuse(w, ErrWire)
			return
		}
		switch r.URL.Path {
		case ExchangePath:
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxExchangeBytes))
			if err != nil {
				refuse(w, ErrWire)
				return
			}
			var req Request
			if Decode(b, &req) != nil {
				refuse(w, ErrWire)
				return
			}
			resp, err := router.Exchange(req, now().UnixMilli())
			if err != nil {
				refuse(w, err)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			data, _ := Encode(resp)
			_, _ = w.Write(data)
		case SocketPath:
			if r.Method != http.MethodGet {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			auth := r.Header.Get(SocketAuthHeader)
			if len(auth) > MaxExchangeBytes {
				refuse(w, ErrWire)
				return
			}
			b, err := base64.RawURLEncoding.DecodeString(auth)
			var req Request
			if err != nil || Decode(b, &req) != nil || len(req.Messages) != 0 {
				refuse(w, ErrAuth)
				return
			}
			release, err := router.openSocket(req)
			if err != nil {
				refuse(w, err)
				return
			}
			defer release()
			conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
			if err != nil {
				return
			}
			defer conn.CloseNow()
			conn.SetReadLimit(MaxExchangeBytes)
			serveSocket(r.Context(), conn, router, req, now)
		default:
			http.NotFound(w, r)
		}
	})
}
func refuse(w http.ResponseWriter, err error) {
	code, status := "invalid", http.StatusBadRequest
	switch err {
	case ErrAuth:
		code, status = "unauthorized", http.StatusForbidden
	case ErrGeneration:
		code, status = "generation", http.StatusConflict
	case ErrCapacity:
		code, status = "busy", http.StatusTooManyRequests
	case ErrCursor:
		code, status = "cursor", http.StatusConflict
	}
	w.Header().Set("Content-Type", "application/json")
	if err == ErrCapacity {
		w.Header().Set("Retry-After", "1")
	}
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":"` + code + `"}`))
}

func (r *Router) openSocket(req Request) (func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, box, err := r.authorize(req)
	if err != nil {
		return nil, err
	}
	if box.sockets >= MaxInflight {
		return nil, ErrCapacity
	}
	box.sockets++
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		g := r.groups[req.Scope]
		if g != nil && g.view.Generation == req.Generation {
			if m, e := g.member(req.Node, req.Incarnation); e == nil {
				g.inboxes[m.Node].sockets--
			}
		}
	}, nil
}
func (r *Router) changed(req Request) (<-chan struct{}, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, box, err := r.authorize(req)
	if err != nil {
		return nil, err
	}
	return box.changed, nil
}
func serveSocket(ctx context.Context, conn *websocket.Conn, router *Router, initial Request, now func() time.Time) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type frame struct {
		data []byte
		err  error
	}
	frames := make(chan frame, 1)
	go func() {
		for {
			kind, data, err := conn.Read(ctx)
			if kind != websocket.MessageBinary && err == nil {
				err = ErrWire
			}
			select {
			case frames <- frame{data, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	// An idle member owes no keepalive. The existing response deadline applies
	// only while a bounded delivery awaits acknowledgment, so a sleeping group
	// can wake this socket without a recurring request loop.
	var deadline <-chan time.Time
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	arm := func(resp Response, received bool) {
		if len(resp.Messages) != 0 && timer != nil && !received {
			return
		}
		if timer != nil {
			timer.Stop()
			timer = nil
		}
		deadline = nil
		if len(resp.Messages) != 0 {
			timer = time.NewTimer(socketTimeout)
			deadline = timer.C
		}
	}
	req := initial
	changed, err := router.changed(req)
	if err != nil {
		return
	}
	response, err := router.exchange(req, now().UnixMilli(), true)
	if err != nil || !writeSocket(ctx, conn, response) {
		return
	}
	arm(response, true)
	lastChunks := response.Chunks
	for {
		receiveOnly := false
		select {
		case <-ctx.Done():
			return
		case <-deadline:
			return
		case <-changed:
			receiveOnly = true
		case f := <-frames:
			if f.err != nil {
				return
			}
			var next Request
			if Decode(f.data, &next) != nil || next.Scope != initial.Scope || next.Node != initial.Node || next.Incarnation != initial.Incarnation || next.Generation != initial.Generation {
				return
			}
			req = next
		}
		// Subscribe before draining so an enqueue cannot hide between those steps.
		changed, err = router.changed(req)
		if err != nil {
			return
		}
		response, err = router.exchange(req, now().UnixMilli(), receiveOnly)
		rejected := err == ErrCapacity && !receiveOnly
		if rejected {
			response, err = router.exchange(req, now().UnixMilli(), true)
			for _, envelope := range req.Messages {
				response.Rejected = append(response.Rejected, decimal(envelope.Sequence))
			}
		} else if err == nil && !receiveOnly {
			for _, envelope := range req.Messages {
				response.Accepted = append(response.Accepted, decimal(envelope.Sequence))
			}
		}
		if err != nil {
			return
		}
		if receiveOnly && len(response.Messages) == 0 && slices.Equal(lastChunks, response.Chunks) {
			continue
		}
		if !writeSocket(ctx, conn, response) {
			return
		}
		lastChunks = response.Chunks
		arm(response, !receiveOnly)
	}
}

func writeSocket(ctx context.Context, conn *websocket.Conn, resp Response) bool {
	data, _ := Encode(resp)
	writeCtx, cancel := context.WithTimeout(ctx, socketTimeout)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageBinary, data) == nil
}

// Client performs a single bounded outbound exchange. It does not start timers,
// retry commands or open listeners. Hosts apply quiet-mode policy BEFORE use,
// own exponential backoff (up to 60 s), and verify each delivered peer key.
type Client struct {
	URL  string
	HTTP *http.Client
}

func (c Client) Exchange(ctx context.Context, req Request) (Response, error) {
	b, err := Encode(req)
	if err != nil || req.validate() != nil {
		return Response{}, ErrWire
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL+ExchangePath, bytes.NewReader(b))
	if err != nil {
		return Response{}, ErrWire
	}
	r.Header.Set("Content-Type", "application/json")
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: socketTimeout}
	}
	// A redirect must not leak a signed peer batch to another origin.
	copyClient := *client
	if copyClient.Timeout <= 0 || copyClient.Timeout > socketTimeout {
		copyClient.Timeout = socketTimeout
	}
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := copyClient.Do(r)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxExchangeBytes+1))
	if err != nil {
		return Response{}, err
	}
	if resp.StatusCode != http.StatusOK {
		switch resp.StatusCode {
		case http.StatusForbidden:
			return Response{}, ErrAuth
		case http.StatusTooManyRequests:
			return Response{}, ErrCapacity
		case http.StatusConflict:
			var refusal struct {
				Error string `json:"error"`
			}
			if json.Unmarshal(data, &refusal) != nil {
				return Response{}, ErrWire
			}
			if refusal.Error == "cursor" {
				return Response{}, ErrCursor
			}
			if refusal.Error == "generation" {
				return Response{}, ErrGeneration
			}
			return Response{}, ErrWire
		default:
			return Response{}, ErrWire
		}
	}
	var result Response
	if Decode(data, &result) != nil || result.Generation != req.Generation || len(result.Messages) > MaxMessages {
		return Response{}, ErrWire
	}
	for _, d := range result.Messages {
		if d.Lane >= laneCount || d.Cursor == 0 || d.Envelope.Scope != req.Scope || d.Envelope.To != req.Node || d.Envelope.Destination != req.Incarnation {
			return Response{}, ErrWire
		}
	}
	return result, nil
}
