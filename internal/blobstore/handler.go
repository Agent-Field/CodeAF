package blobstore

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// Handler serves the store wire (contract §3) over one Store per identity:
// open is asked once for each identity that authenticates, and that identity
// only ever reaches the Store open gave it. It logs nothing, so no body, and
// no id inside one, can leak through it.
func Handler(auth wireauth.Authenticate, open func(identity string) (Store, error)) http.Handler {
	return HandlerAt(time.Now, auth, open)
}

// HandlerAt is Handler with an injected clock for the Codeaf-Now header.
func HandlerAt(now func() time.Time, auth wireauth.Authenticate, open func(identity string) (Store, error)) http.Handler {
	h := &handler{now: now, auth: auth, open: open, tenants: map[string]*tenant{}, arriving: newFlight()}
	mux := http.NewServeMux()
	mux.Handle("POST "+pathFrames, h.route(MaxFrame, (*tenant).putFrame, true))
	mux.Handle("GET "+pathObjects+"{rid}", h.route(maxSmallBody, (*tenant).getObject, false))
	mux.Handle("POST "+pathMany, h.route(maxSmallBody, (*tenant).getMany, false))
	mux.Handle("POST "+pathHas, h.route(maxSmallBody, (*tenant).has, false))
	mux.Handle("GET "+pathStats, h.route(maxSmallBody, (*tenant).statsReply, false))
	return h.stamped(mux)
}

type handler struct {
	now  func() time.Time
	auth wireauth.Authenticate
	open func(identity string) (Store, error)

	mu      sync.Mutex
	tenants map[string]*tenant

	// arriving counts puts whose identity is not known yet: the signature
	// covers the body, so a put cannot be attributed until it has been read.
	arriving *flight
}

// tenant is one identity's namespace: its store and the server's own count of
// what that identity asked of it. The count is taken at the store boundary,
// so it equals what a Memory store logs for the same requests.
type tenant struct {
	store             Store
	inflight          *flight // puts of this identity being received or stored
	arriving          *flight // the handler's puts that belong to no identity yet
	puts, gets, hases atomic.Int64
	bytesIn, bytesOut atomic.Int64
}

// reply is a success answer: a content type and the bytes.
type reply struct {
	ctype string
	body  []byte
}

func jsonReply(v any) reply { return reply{"application/json", mustJSON(v)} }

// serveFunc answers one verb for one tenant. Errors it returns go on the wire
// through the code table.
type serveFunc func(t *tenant, ctx context.Context, r *http.Request, body []byte) (reply, error)

// stamped puts the server clock on every answer, refusals included, because
// a client that was refused for skew needs the time most of all.
func (h *handler) stamped(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(nowHeader, strconv.FormatInt(h.now().UnixMilli(), 10))
		next.ServeHTTP(w, r)
	})
}

// route wraps a verb in the steps every verb shares, in the order that keeps
// an unauthenticated caller cheap: cap the body, read it, prove the sender,
// find the namespace, serve.
//
// A put counts as arriving from its first byte, and becomes its identity's
// in-flight put the moment the identity is known, with no gap between the two,
// so Has can wait for it (see flight).
func (h *handler) route(limit int64, serve serveFunc, isPut bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		unattributed := h.arrive(isPut)
		defer unattributed()
		body, err := readCapped(w, r, limit)
		if err != nil {
			writeErr(w, err)
			return
		}
		t, err := h.tenantFor(r, body)
		if err != nil {
			writeErr(w, err)
			return
		}
		if isPut {
			defer t.inflight.enter()()
		}
		unattributed()
		out, err := serve(t, r.Context(), r, body)
		if err != nil {
			writeErr(w, err)
			return
		}
		w.Header().Set("Content-Type", out.ctype)
		_, _ = w.Write(out.body)
	})
}

// arrive records a put's arrival, and answers the function that ends it. Any
// other verb has nothing to record.
func (h *handler) arrive(isPut bool) func() {
	if !isPut {
		return func() {}
	}
	return h.arriving.enter()
}

// errTooLarge marks a body over its cap; it is the one refusal with its own status.
var errTooLarge = errors.New("blobstore: body over the limit")

// readCapped reads the body through http.MaxBytesReader, which stops at the
// cap instead of after the whole body, so an oversized upload costs the
// server at most limit+1 bytes and the connection is closed on it.
func readCapped(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, error) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig):
		return nil, errTooLarge
	case err != nil:
		return nil, ErrUnreachable // the caller's connection broke mid-body
	}
	return body, nil
}

// tenantFor authenticates the request and returns the sender's namespace.
func (h *handler) tenantFor(r *http.Request, body []byte) (*tenant, error) {
	id, _, err := h.auth(r, body)
	if err != nil {
		return nil, refusal(err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if t, ok := h.tenants[id]; ok {
		return t, nil
	}
	s, err := h.open(id)
	if err != nil {
		return nil, err
	}
	t := &tenant{store: s, inflight: newFlight(), arriving: h.arriving}
	h.tenants[id] = t
	return t, nil
}

// refusal narrows every authentication failure to the three the wire names.
func refusal(err error) error { return wireauth.Narrow(err) }

func (t *tenant) putFrame(ctx context.Context, _ *http.Request, body []byte) (reply, error) {
	t.puts.Add(1)
	t.bytesIn.Add(int64(len(body)))
	id, err := t.store.PutFrame(ctx, body)
	if err != nil {
		return reply{}, err
	}
	// The store has accepted the frame, so decoding it again cannot fail; it
	// is done here so the object count does not depend on which Store answered.
	_, objects, err := Decode(body)
	if err != nil {
		return reply{}, err
	}
	return jsonReply(putAnswer{Frame: id, Objects: len(objects)}), nil
}

func (t *tenant) getObject(ctx context.Context, r *http.Request, _ []byte) (reply, error) {
	t.gets.Add(1)
	b, err := t.store.Get(ctx, r.PathValue("rid"))
	if err != nil {
		return reply{}, err
	}
	t.bytesOut.Add(int64(len(b)))
	return reply{"application/octet-stream", b}, nil
}

// getMany answers a batch of rids with one frame holding the objects the store
// gave, whichever Store is behind it.
func (t *tenant) getMany(ctx context.Context, _ *http.Request, body []byte) (reply, error) {
	var req getManyRequest
	if err := json.Unmarshal(body, &req); err != nil || len(req.Rids) == 0 {
		return reply{}, errBadRequest
	}
	t.gets.Add(1)
	objects, err := t.store.GetMany(ctx, req.Rids)
	if err != nil {
		return reply{}, err
	}
	frame, err := Encode(manyKeyID, objects)
	if err != nil {
		return reply{}, err
	}
	t.bytesOut.Add(sizeOf(objects))
	return reply{"application/octet-stream", frame}, nil
}

func (t *tenant) has(ctx context.Context, _ *http.Request, body []byte) (reply, error) {
	var req hasRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return reply{}, errBadRequest
	}
	t.hases.Add(1)
	if err := t.settle(ctx); err != nil {
		return reply{}, err
	}
	have, err := t.store.Has(ctx, req.Rids)
	if err != nil {
		return reply{}, err
	}
	return jsonReply(hasAnswer{Have: have}), nil
}

// settle waits for the puts Has must not overtake, for at most BaseTimeout
// or until the caller gives up, and then reports the relay as unreachable
// rather than answer no: a stuck put must not hang Has, and must not make it lie.
func (t *tenant) settle(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, BaseTimeout)
	defer cancel()
	if err := waitIdle(ctx, t.inflight, t.arriving); err != nil {
		return ErrUnreachable
	}
	return nil
}

func (t *tenant) statsReply(context.Context, *http.Request, []byte) (reply, error) {
	return jsonReply(statsBody{
		Puts: t.puts.Load(), Gets: t.gets.Load(), Has: t.hases.Load(),
		BytesIn: t.bytesIn.Load(), BytesOut: t.bytesOut.Load(),
	}), nil
}

// errBadRequest is a body the wire could not parse; it has no §2.1 name.
var errBadRequest = errors.New("blobstore: malformed request")

// writeErr answers a failure. The two refusals the handler makes itself get
// their statuses here; every store error goes through the code table, and an
// error the table does not know is the server's fault, not the caller's.
func writeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errTooLarge):
		send(w, http.StatusRequestEntityTooLarge, "bad_frame")
	case errors.Is(err, errBadRequest):
		send(w, http.StatusBadRequest, "bad_request")
	default:
		code, status, ok := codeOf(err)
		if !ok {
			code, status = "internal", http.StatusInternalServerError
		}
		send(w, status, code)
	}
}

func send(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(mustJSON(errBody{Err: code}))
}
