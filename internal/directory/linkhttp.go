package directory

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// LinkPath is the base of the open link-pairing routes.
const LinkPath = "/v1/link"

// Linker is the store LinkHandler serves. *Links is the one implementation.
type Linker interface {
	Open(peer string, in NewRequest) (Opened, error)
	Read(ctx context.Context, peer, code string, wait time.Duration) (Request, error)
	Limits() LinkLimits
}

// PeerFunc names the network a request comes from, which the limits count.
type PeerFunc func(*http.Request) string

// LinkHandler serves the open link-pairing routes (contract 3.1, 3.2, 3.6).
// They take no signature: the device asking has no identity yet.
func LinkHandler(l Linker, peer PeerFunc) http.Handler {
	h := linkRoutes{l: l, peer: peer}
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+LinkPath+"/requests", h.create)
	mux.HandleFunc("GET "+LinkPath+"/requests/{code}", h.read)
	mux.HandleFunc("GET "+LinkPath+"/limits", func(w http.ResponseWriter, _ *http.Request) { send(w, http.StatusOK, l.Limits()) })
	return mux
}

type linkRoutes struct {
	l    Linker
	peer PeerFunc
}

func (h linkRoutes) create(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxNewBody))
	var in NewRequest
	if err != nil {
		refuse(w, ErrTooBig)
		return
	}
	if json.Unmarshal(body, &in) != nil {
		refuse(w, ErrBadRequest)
		return
	}
	o, err := h.l.Open(h.peer(r), in)
	if err != nil {
		refuse(w, err)
		return
	}
	send(w, http.StatusCreated, o)
}

func (h linkRoutes) read(w http.ResponseWriter, r *http.Request) {
	ms, _ := strconv.Atoi(r.URL.Query().Get("wait"))
	rec, err := h.l.Read(r.Context(), h.peer(r), r.PathValue("code"), time.Duration(ms)*time.Millisecond)
	switch {
	case errors.Is(err, ErrStillPending):
		w.WriteHeader(http.StatusNoContent)
	case err != nil:
		refuse(w, err)
	default:
		send(w, http.StatusOK, rec)
	}
}

// LinkHTTP is Requests over the wire. It signs nothing.
type LinkHTTP struct{ h *HTTP }

// NewLinkHTTP returns the open-side client for the relay at base.
func NewLinkHTTP(base string, hc *http.Client) *LinkHTTP {
	return &LinkHTTP{h: NewHTTP(base, func(*http.Request, []byte) {}, hc)}
}

var _ Requests = (*LinkHTTP)(nil)

func (c *LinkHTTP) CreateRequest(ctx context.Context, in NewRequest) (o Opened, err error) {
	return o, c.h.do(ctx, http.MethodPost, LinkPath+"/requests", in, &o)
}

func (c *LinkHTTP) GetRequest(ctx context.Context, code string, wait time.Duration) (r Request, err error) {
	path := LinkPath + "/requests/" + url.PathEscape(code)
	if wait > 0 {
		path += "?wait=" + strconv.FormatInt(wait.Milliseconds(), 10)
	}
	return r, c.h.do(ctx, http.MethodGet, path, nil, &r)
}

// Limits reads the numbers the relay enforces.
func (c *LinkHTTP) Limits(ctx context.Context) (l LinkLimits, err error) {
	return l, c.h.do(ctx, http.MethodGet, LinkPath+"/limits", nil, &l)
}
