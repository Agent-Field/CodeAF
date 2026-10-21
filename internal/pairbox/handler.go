package pairbox

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// PeerFunc names the network a request comes from, which is what the limits
// count.
type PeerFunc func(*http.Request) string

// SocketPeer is the address the connection came from.
func SocketPeer(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ForwardedPeer is the first address in X-Forwarded-For, for a relay that sits
// behind a proxy it trusts, and the socket address when there is none.
func ForwardedPeer(r *http.Request) string {
	first, _, _ := strings.Cut(r.Header.Get(PeerHeader), ",")
	if first = strings.TrimSpace(first); first != "" {
		return first
	}
	return SocketPeer(r)
}

// Handler serves a Box on the wire. It authenticates nothing itself, because a
// device that is pairing has no identity yet; the keys in the mailbox are the
// whole of it.
func Handler(box Box, peer PeerFunc) http.Handler {
	h := &handler{box: box, peer: peer}
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+Path, h.create)
	mux.HandleFunc("GET "+Path+"/limits", h.limits)
	mux.HandleFunc("DELETE "+Path+"/{plate}", h.remove)
	mux.HandleFunc("POST "+Path+"/{plate}/{side}", h.post)
	mux.HandleFunc("GET "+Path+"/{plate}/{side}", h.poll)
	// Anything else, a listing included, is simply not there.
	mux.HandleFunc(Path, http.NotFound)
	mux.HandleFunc(Path+"/", http.NotFound)
	return mux
}

type handler struct {
	box  Box
	peer PeerFunc
}

func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	key, ok := keyOf(w, r)
	if !ok {
		return
	}
	made, err := h.box.Create(h.context(r), key)
	if err != nil {
		fail(w, err)
		return
	}
	reply(w, http.StatusCreated, createdBody{made.Nameplate, made.ExpiresIn.Milliseconds()})
}

func (h *handler) limits(w http.ResponseWriter, r *http.Request) {
	l, err := h.box.Limits(h.context(r))
	if err != nil {
		fail(w, err)
		return
	}
	reply(w, http.StatusOK, limitsBody{l, l.TTL.Milliseconds()})
}

func (h *handler) remove(w http.ResponseWriter, r *http.Request) {
	if !plateOK(w, r) {
		return
	}
	key, ok := keyOf(w, r)
	if !ok {
		return
	}
	if err := h.box.Delete(h.context(r), r.PathValue("plate"), key); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) post(w http.ResponseWriter, r *http.Request) {
	side, ok := sideOf(w, r)
	if !ok {
		return
	}
	key, ok := keyOf(w, r)
	if !ok {
		return
	}
	body, err := h.body(r)
	if err != nil {
		fail(w, err)
		return
	}
	n, err := h.box.Post(h.context(r), r.PathValue("plate"), side, key, body)
	if err != nil {
		fail(w, err)
		return
	}
	reply(w, http.StatusCreated, postedBody{n})
}

// body reads a message, refusing one over the limit without reading the rest.
func (h *handler) body(r *http.Request) ([]byte, error) {
	l, err := h.box.Limits(r.Context())
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, int64(l.MaxMsg)+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > l.MaxMsg {
		return nil, ErrTooBig
	}
	return raw, nil
}

func (h *handler) poll(w http.ResponseWriter, r *http.Request) {
	side, ok := sideOf(w, r)
	if !ok {
		return
	}
	after, _ := strconv.Atoi(r.URL.Query().Get("after"))
	waitMS, _ := strconv.Atoi(r.URL.Query().Get("wait"))
	got, err := h.box.Poll(h.context(r), r.PathValue("plate"), side, after, time.Duration(waitMS)*time.Millisecond)
	switch {
	case err != nil:
		fail(w, err)
	case len(got.Msgs) == 0:
		w.WriteHeader(http.StatusNoContent)
	default:
		reply(w, http.StatusOK, batchBody{got.Msgs, got.Next})
	}
}

func (h *handler) context(r *http.Request) context.Context {
	return WithPeer(r.Context(), h.peer(r))
}

// keyOf reads the side key, answering 403 for one that is missing or not a key:
// a request without a valid key owns nothing, which is what 403 says.
func keyOf(w http.ResponseWriter, r *http.Request) (Key, bool) {
	key, err := ParseKey(r.Header.Get(KeyHeader))
	if err != nil {
		fail(w, ErrForbidden)
		return key, false
	}
	return key, true
}

// sideOf reads the side and the nameplate, answering 404 for shapes that can
// name nothing, so a probe learns no more than a wrong nameplate teaches.
func sideOf(w http.ResponseWriter, r *http.Request) (Side, bool) {
	side := Side(r.PathValue("side"))
	if !side.Valid() {
		fail(w, ErrGone)
		return side, false
	}
	return side, plateOK(w, r)
}

// plateOK answers 404 for a nameplate that cannot exist and says whether the
// request may go on.
func plateOK(w http.ResponseWriter, r *http.Request) bool {
	plate := r.PathValue("plate")
	if _, err := strconv.ParseUint(plate, 10, 32); err == nil && len(plate) <= 4 {
		return true
	}
	fail(w, ErrGone)
	return false
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, err error) {
	f := statusOf(err)
	var limited RateLimited
	if errors.As(err, &limited) {
		w.Header().Set("Retry-After", strconv.Itoa(int(limited.RetryAfter.Round(time.Second)/time.Second)))
	}
	reply(w, f.status, errorBody{f.word})
}
