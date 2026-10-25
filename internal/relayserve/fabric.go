package relayserve

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"sync"

	"github.com/Agent-Field/codeaf/internal/clientproto"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// Fabric limits bound the opaque data held for each client.
const (
	MaxFabricBody    = clientproto.MaxFabricBody
	MaxClientMethods = clientproto.MaxClientMethods
)

var fabricName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)
var fabricDevice = regexp.MustCompile(`^dev_[0-9a-f]{32}$`)

// fabric serializes client registration, data mutations and directory writes.
// A store belongs to one relay process, like the directory's revocation feed.
type fabric struct {
	ns          *namespaces
	auth        wireauth.Authenticate
	mu          sync.Mutex
	slotWaiters map[string]*slotWaiters
	waiting     int
	stop        <-chan struct{}
}

type fabricError struct {
	status int
	code   string
}

func (e fabricError) Error() string           { return e.code }
func badFabric(status int, code string) error { return fabricError{status, code} }

func fabricReply(w http.ResponseWriter, out any, err error) {
	if err != nil {
		e := fabricError{500, "internal"}
		if !errors.As(err, &e) {
			switch {
			case errors.Is(err, wireauth.ErrRevoked):
				e = fabricError{401, "revoked"}
			case errors.Is(err, wireauth.ErrSkew):
				e = fabricError{401, "skew"}
			case errors.Is(err, wireauth.ErrUnauthorized):
				e = fabricError{401, "unauthorized"}
			case errors.Is(err, wireauth.ErrGone):
				e = fabricError{410, "gone"}
			case errors.Is(err, directory.ErrNotFound):
				e = fabricError{404, "not_found"}
			case errors.Is(err, directory.ErrSelfRevoke):
				e = fabricError{400, "self_revoke"}
			case errors.Is(err, wireauth.ErrRotated):
				e = fabricError{410, "rotated"}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(e.status)
		_ = json.NewEncoder(w).Encode(map[string]string{"err": e.code})
		return
	}
	if out == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func decodeFabric(body []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if dec.Decode(out) != nil {
		return badFabric(400, "bad_request")
	}
	if dec.Decode(new(any)) != io.EOF {
		return badFabric(400, "bad_request")
	}
	return nil
}

func (f *fabric) fullAuth(r *http.Request, body []byte) (string, string, error) {
	id, dev, err := f.auth(r, body)
	if err != nil {
		return id, dev, err
	}
	d, err := f.ns.directory(id)
	if err != nil {
		return "", "", err
	}
	l, err := d.For(dev).List(r.Context())
	if err != nil {
		return "", "", err
	}
	if l.Devices[dev].Kind == "client" {
		return "", "", wireauth.ErrUnauthorized
	}
	return id, dev, nil
}

// directoryHandler keeps revocation and a fabric mutation from crossing.
// Watch upgrades stay outside the lock because they can last for hours.
func (f *fabric) directoryHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, directory.MaxBody))
		if err != nil {
			fabricReply(w, nil, badFabric(413, "too_large"))
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		reply := &fabricResponse{headers: make(http.Header)}
		func() { f.mu.Lock(); defer f.mu.Unlock(); next.ServeHTTP(reply, r) }()
		for key, values := range reply.headers {
			w.Header()[key] = values
		}
		if reply.status != 0 {
			w.WriteHeader(reply.status)
		}
		if reply.body.Len() != 0 {
			_, _ = w.Write(reply.body.Bytes())
		}
	})
}

// fabricResponse keeps network writes outside the mutation lock. Directory
// writes have bounded JSON answers; streaming watch replies never pass here.
type fabricResponse struct {
	headers http.Header
	status  int
	body    bytes.Buffer
}

func (w *fabricResponse) Header() http.Header { return w.headers }
func (w *fabricResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *fabricResponse) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	return w.body.Write(p)
}

type fabricCall struct {
	f       *fabric
	r       *http.Request
	body    []byte
	id, dev string
	dir     directory.Directory
	listing directory.Listing
}

type fabricRoute func(fabricCall) (any, error)

func (f *fabric) handler() http.Handler {
	mux := http.NewServeMux()
	f.mountQueue(mux)
	mux.HandleFunc("PUT /v1/clients/{device}", f.serve(registerClient))
	mux.HandleFunc("PUT /v1/clients/{device}/slots/{name}", f.serve(putSlot))
	mux.HandleFunc("GET /v1/clients/{device}/slots", f.watchSlots)
	mux.HandleFunc("GET /v1/clients/{device}/slots/{name}", f.serve(getSlot))
	return mux
}

func (f *fabric) serve(route fabricRoute) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxFabricBody))
		if err != nil {
			fabricReply(w, nil, badFabric(413, "too_large"))
			return
		}
		out, err := func() (any, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			c, err := f.admit(r, body)
			if err != nil {
				return nil, err
			}
			return route(c)
		}()
		fabricReply(w, out, err)
	}
}

func (f *fabric) admit(r *http.Request, body []byte) (fabricCall, error) {
	c := fabricCall{f: f, r: r, body: body}
	var err error
	c.id, c.dev, err = f.auth(r, body)
	if err != nil {
		return c, err
	}
	if !fabricDevice.MatchString(r.PathValue("device")) {
		return c, badFabric(400, "bad_request")
	}
	c.dir, err = f.ns.directory(c.id)
	if err != nil {
		return c, err
	}
	c.listing, err = c.dir.For(c.dev).List(r.Context())
	return c, err
}

func registerClient(c fabricCall) (any, error) {
	if c.listing.Devices[c.dev].Kind == "client" {
		return nil, wireauth.ErrUnauthorized
	}
	if c.listing.Identity.Rotation != nil {
		return nil, wireauth.ErrRotated
	}
	var in struct {
		Cert    identity.Cert `json:"cert"`
		Methods []string      `json:"methods"`
		Name    string        `json:"name"`
	}
	if err := decodeFabric(c.body, &in); err != nil {
		return nil, err
	}
	if len(in.Methods) > MaxClientMethods || len(in.Name) > 256 {
		return nil, badFabric(413, "too_large")
	}
	seen := map[string]bool{}
	for _, method := range in.Methods {
		if !fabricName.MatchString(method) || seen[method] {
			return nil, badFabric(400, "bad_request")
		}
		seen[method] = true
	}
	pub, keyErr := hex.DecodeString(in.Cert.Device)
	if keyErr != nil || len(pub) != ed25519.PublicKeySize {
		return nil, badFabric(400, "bad_request")
	}
	key, err := base64.RawURLEncoding.DecodeString(c.r.Header.Get(reqsign.HeaderIdentity))
	if err != nil || len(key) != ed25519.PublicKeySize || in.Cert.Verify(key) != nil || in.Cert.DeviceID() != c.r.PathValue("device") {
		return nil, badFabric(400, "bad_request")
	}
	if _, exists := c.listing.Devices[in.Cert.DeviceID()]; exists {
		return nil, badFabric(409, "exists")
	}
	d := directory.Device{V: 1, Kind: "client", Methods: in.Methods, Name: in.Name, AddedBy: c.id}
	return nil, c.dir.For(in.Cert.DeviceID()).PutDevice(c.r.Context(), in.Cert.DeviceID(), d)
}
