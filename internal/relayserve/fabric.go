package relayserve

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"sync"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// Fabric limits bound the opaque data held for each client.
const (
	MaxFabricBody    = 96 << 10
	MaxClientMethods = 64
)

var fabricName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)
var fabricDevice = regexp.MustCompile(`^dev_[0-9a-f]{32}$`)

// fabric serializes client registration, data mutations and directory writes.
// A store belongs to one relay process, like the directory's revocation feed.
type fabric struct {
	ns   *namespaces
	auth wireauth.Authenticate
	mu   sync.Mutex
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
		if r.Method != http.MethodGet {
			f.mu.Lock()
			defer f.mu.Unlock()
		}
		next.ServeHTTP(w, r)
	})
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
	mux.HandleFunc("PUT /v1/clients/{device}", f.serve(registerClient))
	return mux
}

func (f *fabric) serve(route fabricRoute) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		c, err := f.admit(w, r)
		if err != nil {
			fabricReply(w, nil, err)
			return
		}
		out, err := route(c)
		fabricReply(w, out, err)
	}
}

func (f *fabric) admit(w http.ResponseWriter, r *http.Request) (fabricCall, error) {
	c := fabricCall{f: f, r: r}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxFabricBody))
	if err != nil {
		return c, badFabric(413, "too_large")
	}
	c.body = body
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
	key, err := base64.RawURLEncoding.DecodeString(c.r.Header.Get(reqsign.HeaderIdentity))
	if err != nil || len(key) != ed25519.PublicKeySize || in.Cert.Verify(key) != nil || in.Cert.DeviceID() != c.r.PathValue("device") {
		return nil, badFabric(400, "bad_request")
	}
	if _, exists := c.listing.Devices[in.Cert.DeviceID()]; exists {
		return nil, badFabric(409, "exists")
	}
	d := directory.Device{V: 1, Kind: "client", Methods: in.Methods, Name: in.Name, AddedBy: c.dev}
	return nil, c.dir.For(in.Cert.DeviceID()).PutDevice(c.r.Context(), in.Cert.DeviceID(), d)
}
