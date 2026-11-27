package relayserve

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/fsreplace"
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
	ns            *namespaces
	auth          wireauth.Authenticate
	mu            sync.Mutex
	slotWaiters   map[string]*slotWaiters
	clientWatches map[string]*clientWatch
	waiting       int
	stop          <-chan struct{}
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
			case errors.Is(err, wireauth.ErrRevoked), errors.Is(err, directory.ErrRevoked):
				e = fabricError{401, "revoked"}
			case errors.Is(err, wireauth.ErrSkew):
				e = fabricError{401, "skew"}
			case errors.Is(err, wireauth.ErrUnauthorized), errors.Is(err, directory.ErrUnauthorized):
				e = fabricError{401, "unauthorized"}
			case errors.Is(err, wireauth.ErrGone):
				e = fabricError{410, "gone"}
			case errors.Is(err, directory.ErrNotFound):
				e = fabricError{404, "not_found"}
			case errors.Is(err, directory.ErrExists):
				e = fabricError{409, "exists"}
			case errors.Is(err, directory.ErrSelfRevoke):
				e = fabricError{400, "self_revoke"}
			case errors.Is(err, directory.ErrBadRequest):
				e = fabricError{400, "bad_request"}
			case errors.Is(err, directory.ErrFull):
				e = fabricError{507, "full"}
			case errors.Is(err, wireauth.ErrRotated), errors.Is(err, directory.ErrRotated):
				e = fabricError{410, "rotated"}
			case errors.Is(err, wireauth.ErrUnavailable), errors.Is(err, directory.ErrUnreachable):
				e = fabricError{503, "unreachable"}
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
	header  http.Header
	body    []byte
	id, dev string
	ns      *nativeNamespace
	listing directory.Listing
}

type fabricRoute func(fabricCall) (any, error)

func (f *fabric) handler() http.Handler {
	mux := http.NewServeMux()
	f.mountQueue(mux)
	mux.HandleFunc("GET /v1/clients", f.serve(listEnrollments))
	mux.HandleFunc("PUT /v1/clients/{device}", f.serve(registerClient))
	mux.HandleFunc("PUT /v1/clients/{device}/enroll", f.serve(putEnrollment))
	mux.HandleFunc("GET /v1/clients/{device}/computers", f.serve(getComputers))
	mux.HandleFunc("PUT /v1/clients/{device}/slots/{name}", f.serve(putSlot))
	mux.HandleFunc("GET /v1/clients/{device}/slots", f.watchSlots)
	mux.HandleFunc("GET /v1/clients/{device}/slots/{name}", f.serve(getSlot))
	return mux
}

func (f *fabric) serve(route fabricRoute) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		isDir := strings.HasPrefix(r.URL.Path, "/v1/dir/")
		if isDir {
			w.Header().Set(directory.SavedTurnTimeHeader, "1")
			w.Header().Set(directory.HeadTakeHeader, "1")
			w.Header().Set(directory.UploadProgressHeader, "1")
			w.Header().Set(directory.WorkingBeatsHeader, "1")
			w.Header().Set(directory.ChatNotesHeader, "1")
			w.Header().Set(directory.ChatFoldersHeader, "1")
		}
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
			c.header = w.Header()
			return route(c)
		}()
		if isDir && err == nil {
			w.Header().Set(directory.ConditionalTakeHeader, "1")
		}
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
	if !(r.Method == http.MethodGet && r.URL.Path == "/v1/clients") && !fabricDevice.MatchString(r.PathValue("device")) {
		return c, badFabric(400, "bad_request")
	}
	c.ns, err = f.ns.activate(r.Context(), c.id, nil)
	if err != nil {
		return c, err
	}
	c.listing, err = c.ns.fabricListing(r.Context())
	return c, err
}

type enrolledClient struct {
	Device string                 `json:"device"`
	Enroll clientproto.Enrollment `json:"enroll"`
}

func (f *fabric) enrollPath(id string) string {
	return filepath.Join(f.ns.root, "fabric", id, "enroll.json")
}

// loadEnrollments reads the stored enrollments for one identity. The caller holds f.mu.
func (f *fabric) loadEnrollments(id string) (map[string]clientproto.Enrollment, error) {
	all := map[string]clientproto.Enrollment{}
	raw, err := os.ReadFile(f.enrollPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return all, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, err
	}
	return all, nil
}

// saveEnrollments atomically replaces the enrollment map on disk so readers never see a partial file.
func (f *fabric) saveEnrollments(id string, all map[string]clientproto.Enrollment) error {
	raw, err := json.Marshal(all)
	if err != nil {
		return err
	}
	path := f.enrollPath(id)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".enroll-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, werr := file.Write(raw)
	err = errors.Join(werr, file.Sync(), file.Close())
	if err != nil {
		return err
	}
	if err := fsreplace.Rename(file.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}

func putEnrollment(c fabricCall) (any, error) {
	if c.listing.Devices[c.dev].Kind == "client" {
		return nil, wireauth.ErrUnauthorized
	}
	if c.listing.Identity.Rotation != nil {
		return nil, wireauth.ErrRotated
	}
	var in clientproto.Enrollment
	if err := decodeFabric(c.body, &in); err != nil || in.Methods == nil || in.Reads == nil {
		return nil, badFabric(400, "bad_request")
	}
	if in.Device != c.r.PathValue("device") {
		return nil, badFabric(400, "bad_request")
	}
	key, err := base64.RawURLEncoding.DecodeString(c.r.Header.Get(reqsign.HeaderIdentity))
	if err != nil || in.Verify(key) != nil {
		return nil, badFabric(400, "bad_request")
	}
	if d, ok := c.listing.Devices[in.Device]; ok && (d.Kind != "client" || d.Revoked) {
		return nil, badFabric(409, "exists")
	}
	all, err := c.f.loadEnrollments(c.id)
	if err != nil {
		return nil, err
	}
	if old, ok := all[in.Device]; ok {
		if in.Same(old) {
			return nil, nil
		}
		if in.Ver <= old.Ver {
			return nil, badFabric(409, "version")
		}
	} else if len(all) >= clientproto.MaxEnrollments {
		return nil, badFabric(429, "full")
	}
	all[in.Device] = in
	return nil, c.f.saveEnrollments(c.id, all)
}

func listEnrollments(c fabricCall) (any, error) {
	if c.listing.Devices[c.dev].Kind == "client" {
		return nil, wireauth.ErrUnauthorized
	}
	if len(c.body) != 0 || c.r.URL.RawQuery != "" {
		return nil, badFabric(400, "bad_request")
	}
	all, err := c.f.loadEnrollments(c.id)
	if err != nil {
		return nil, err
	}
	devices := slices.Sorted(maps.Keys(all))
	clients := make([]enrolledClient, 0, len(devices))
	for _, dev := range devices {
		clients = append(clients, enrolledClient{Device: dev, Enroll: all[dev]})
	}
	return struct {
		Clients []enrolledClient `json:"clients"`
	}{clients}, nil
}

type computerEntry struct {
	Device  string `json:"device"`
	Serving bool   `json:"serving"`
	At      int64  `json:"at"`
}

func getComputers(c fabricCall) (any, error) {
	if err := c.target(true); err != nil {
		return nil, err
	}
	if len(c.body) != 0 {
		return nil, badFabric(400, "bad_request")
	}
	now := c.f.ns.now().UnixMilli()
	ids := slices.Sorted(maps.Keys(c.listing.Devices))
	computers := make([]computerEntry, 0, len(ids))
	for _, id := range ids {
		d := c.listing.Devices[id]
		if d.Kind == "client" || d.Revoked {
			continue
		}
		serving, at := false, d.LastSeen
		if c.ns != nil && c.ns.feed != nil {
			serving, at = c.ns.feed.ComputerStatus(id, d.LastSeen, now)
		}
		computers = append(computers, computerEntry{Device: id, Serving: serving, At: at})
	}
	return struct {
		Now       int64           `json:"now"`
		Computers []computerEntry `json:"computers"`
	}{Now: now, Computers: computers}, nil
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
	return nil, c.ns.commitFabricV1(c.r.Context(), c.dev, c.r, c.body)
}
