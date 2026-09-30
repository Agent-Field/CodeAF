package directory

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// MaxBody caps a request body. The largest honest body is a CellInit with its
// key map, far below this; the cap keeps an anonymous caller from making the
// relay buffer more, since the body must be read whole before it can be verified.
const MaxBody = 1 << 20

// call is everything one route needs: the Client of the verified device and
// the request it answers. Routes never see an identity or a device to trust.
type call struct {
	cl     Client
	device string
	r      *http.Request
	body   []byte
}

func (c call) ctx() context.Context { return c.r.Context() }
func (c call) id() string           { return c.r.PathValue("id") }

// route answers one call. A nil result is an empty 204.
type route func(call) (any, error)

// Handler serves the directory wire. Each request is authenticated first, and
// then answered by open(identity).For(device), both taken from auth, so a body
// can never name the acting device or reach another identity's records.
func Handler(auth wireauth.Authenticate, open func(identity string) (Directory, error)) http.Handler {
	h := &handler{auth: auth, open: open}
	mux := http.NewServeMux()
	for pattern, rt := range routes {
		mux.HandleFunc(pattern, h.serve(rt))
	}
	return mux
}

type handler struct {
	auth wireauth.Authenticate
	open func(string) (Directory, error)
}

func (h *handler) serve(rt route) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := h.admit(w, r)
		if err != nil {
			refuse(w, err)
			return
		}
		out, err := rt(c)
		if err != nil {
			refuse(w, err)
			return
		}
		reply(w, out)
	}
}

// admit reads the bounded body, authenticates it and opens the caller's Client.
func (h *handler) admit(w http.ResponseWriter, r *http.Request) (call, error) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBody))
	if err != nil {
		return call{}, errTooLarge
	}
	identity, device, err := h.auth(r, body)
	if err != nil {
		return call{}, denial(err)
	}
	dir, err := h.open(identity)
	if err != nil {
		return call{}, err
	}
	return call{cl: dir.For(device), device: device, r: r, body: body}, nil
}

// denial names a refusal the way the wire does: skew and revoked have their
// own codes because a person can act on them, every other one is unauthorized.
func denial(err error) error {
	if err = wireauth.Narrow(err); errors.Is(err, wireauth.ErrUnauthorized) {
		return ErrUnauthorized
	}
	return err
}

func refuse(w http.ResponseWriter, err error) {
	we := classify(err)
	send(w, we.status, errBody{Err: we.code})
}

func reply(w http.ResponseWriter, out any) {
	if out == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	send(w, http.StatusOK, out)
}

func send(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// routes is the §4.4 table, one entry per method and path.
var routes = map[string]route{
	"GET " + dirBase + "/list":                  func(c call) (any, error) { return pair(c.cl.List(c.ctx())) },
	"GET " + dirBase + "/cells/{id}":            func(c call) (any, error) { return pair(c.cl.Cell(c.ctx(), c.id())) },
	"PUT " + dirBase + "/devices/{id}":          withBody(putDevice),
	"POST " + dirBase + "/devices/{id}/revoke":  func(c call) (any, error) { return nothing(c.cl.Revoke(c.ctx(), c.id())) },
	"POST " + dirBase + "/vault":                withBody(setVault),
	"POST " + dirBase + "/cells/{id}":           withBody(create),
	"POST " + dirBase + "/cells/{id}/acquire":   optionalBody(acquire),
	"POST " + dirBase + "/cells/{id}/heartbeat": withBody(heartbeat),
	"POST " + dirBase + "/cells/{id}/publish":   withBody(publish),
	"POST " + dirBase + "/cells/{id}/release":   withBody(release),
	"POST " + dirBase + "/cells/{id}/archive":   func(c call) (any, error) { return nothing(c.cl.Archive(c.ctx(), c.id())) },
}

// pair and nothing adapt a Client's return shapes to a route's.
func pair[T any](v T, err error) (any, error) { return v, err }
func nothing(err error) (any, error)          { return nil, err }

// withBody decodes the request body as T before running f.
func withBody[T any](f func(call, T) (any, error)) route {
	return func(c call) (any, error) {
		var v T
		if err := json.Unmarshal(c.body, &v); err != nil {
			return nil, errBadRequest
		}
		return f(c, v)
	}
}

// optionalBody is withBody for a request whose body may be absent, which then
// decodes as the zero T, so a client that sends none still gets its old meaning.
func optionalBody[T any](f func(call, T) (any, error)) route {
	return func(c call) (any, error) {
		if len(c.body) == 0 {
			var zero T
			return f(c, zero)
		}
		return withBody(f)(c)
	}
}

// putDevice refuses a record for any device but the signer's: a device may
// only ever write its own record.
func putDevice(c call, d Device) (any, error) {
	if c.id() != c.device {
		return nil, ErrUnauthorized
	}
	return nothing(c.cl.PutDevice(c.ctx(), c.id(), d))
}

// vaultSwap is the body of the vault compare-and-swap.
type vaultSwap struct {
	Old string `json:"old"`
	New string `json:"new"`
}

// fenceBody is the body of a release.
type fenceBody struct {
	Fence uint64 `json:"fence"`
}

func setVault(c call, s vaultSwap) (any, error)  { return nothing(c.cl.SetVault(c.ctx(), s.Old, s.New)) }
func create(c call, in CellInit) (any, error)    { return pair(c.cl.Create(c.ctx(), c.id(), in)) }
func acquire(c call, o AcquireOpts) (any, error) { return pair(c.cl.Acquire(c.ctx(), c.id(), o)) }
func heartbeat(c call, b Beat) (any, error)      { return pair(c.cl.Heartbeat(c.ctx(), c.id(), b)) }
func publish(c call, p Publish) (any, error)     { return pair(c.cl.Publish(c.ctx(), c.id(), p)) }
func release(c call, f fenceBody) (any, error) {
	return nothing(c.cl.Release(c.ctx(), c.id(), f.Fence))
}
