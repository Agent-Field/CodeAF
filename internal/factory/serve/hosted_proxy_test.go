package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeChatHost stands in for the service the chats run on. It keeps only what
// its documented API promises: the shared secret is checked, a chat belongs to
// the person who made it, a chat that is not yours looks exactly like one that
// does not exist, and a chat with no model access refuses a wire.
type fakeChatHost struct {
	secret string
	mu     sync.Mutex
	owners map[string]string // chat id to person
	tokens map[string]string // chat id to the model token it holds, "" when none
	seen   []hostSeen
	pushes []hostPush
	// rowBearer, when set for a chat, is what its row reports as the model
	// bearer fingerprint instead of the truth: a host that restarts between the
	// org's read of the row and the wire.
	rowBearer map[string]string
	// A wire test can replace the echo with a recording engine after the host's usual admission.
	wire func(*websocket.Conn, *http.Request)
	// A wire refusal makes the host answer the wire with this status and its own sentence instead of upgrading.
	wireRefusal int
	// running names the chats whose row says "running". Every other chat is suspended, which is what a chat is until
	// somebody opens it.
	running map[string]bool
	// titles holds the name a chat has given itself, as the real host keeps it from the chat's own wire. A chat with
	// no entry has no title, and its rows carry no title field at all.
	titles map[string]string
}

type hostSeen struct {
	method, path string
	header       http.Header
	body         string
}

type hostPush struct{ id, person, url, token string }

const hostNoSuchChat = `{"error":"There is no such chat."}` + "\n"

func newFakeChatHost(secret string) *fakeChatHost {
	return &fakeChatHost{secret: secret, owners: map[string]string{}, tokens: map[string]string{}}
}

func (f *fakeChatHost) requests() []hostSeen {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]hostSeen(nil), f.seen...)
}

func (f *fakeChatHost) count() int { return len(f.requests()) }

// adopt makes a chat exist here with no credentials, as after the org server
// restarted or the chat was stopped.
func (f *fakeChatHost) adopt(id, person string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.owners[id], f.tokens[id] = person, ""
}

// setTitle is the chat naming itself (or being renamed): the host's rows say it from the next read on.
func (f *fakeChatHost) setTitle(id, title string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.titles == nil {
		f.titles = map[string]string{}
	}
	f.titles[id] = title
}

// setRunning makes a chat's row say it has a live process (or not).
func (f *fakeChatHost) setRunning(id string, running bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.running == nil {
		f.running = map[string]bool{}
	}
	f.running[id] = running
}

// wireRequests counts the attaches that reached the host's wire door, each of which starts a chat that is not running.
func (f *fakeChatHost) wireRequests(id string) int {
	n := 0
	for _, seen := range f.requests() {
		if seen.method == http.MethodGet && seen.path == "/v1/sessions/"+id+"/wire" {
			n++
		}
	}
	return n
}

// rowReads counts the reads of one chat's row.
func (f *fakeChatHost) rowReads(id string) int {
	n := 0
	for _, seen := range f.requests() {
		if seen.method == http.MethodGet && seen.path == "/v1/sessions/"+id {
			n++
		}
	}
	return n
}

func (f *fakeChatHost) ownerOf(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.owners[id]
}

func (f *fakeChatHost) tokenOf(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokens[id]
}

// fingerprintOf is what a chat's row says about the model token the host holds:
// the first sixteen hex characters of its sha256, or "" when it holds none.
func fingerprintOf(token string) string {
	if token == "" {
		return ""
	}
	return bearerFingerprint(bearerHash(token))
}

// lieAboutBearer makes a chat's row report fingerprint whatever the host holds.
func (f *fakeChatHost) lieAboutBearer(id, fingerprint string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rowBearer == nil {
		f.rowBearer = map[string]string{}
	}
	f.rowBearer[id] = fingerprint
}

func (f *fakeChatHost) pushList() []hostPush {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]hostPush(nil), f.pushes...)
}

func (f *fakeChatHost) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.seen = append(f.seen, hostSeen{r.Method, r.URL.Path, r.Header.Clone(), string(raw)})
	f.mu.Unlock()
	person := r.Header.Get("X-Codeaf-Person")
	if r.Header.Get("Authorization") != "Bearer "+f.secret || person == "" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/v1/sessions")
	if rest == "" && r.Method == http.MethodPost {
		id, url, token := jsonField(raw, "id"), jsonField(raw, "model_url"), jsonField(raw, "model_token")
		f.mu.Lock()
		f.owners[id], f.tokens[id] = person, token
		f.pushes = append(f.pushes, hostPush{id, person, url, token})
		f.mu.Unlock()
		// The id is deliberately left out of the answer: the org server adds it.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"state":"suspended","created":"2026-11-24T00:00:00Z"}`+"\n")
		return
	}
	if rest == "" && r.Method == http.MethodGet {
		f.mu.Lock()
		var mine []map[string]string
		for id, owner := range f.owners {
			if owner == person {
				mine = append(mine, map[string]string{
					"id": id, "state": "suspended", "created": "2026-11-24T00:00:00Z", "last_active": "2026-11-24T00:01:00Z",
					"model_bearer": fingerprintOf(f.tokens[id]), "warning": "A warning for the chat's owner.",
				})
				if title := f.titles[id]; title != "" {
					mine[len(mine)-1]["title"] = title
				}
			}
		}
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"sessions": mine})
		return
	}
	parts := strings.Split(strings.TrimPrefix(rest, "/"), "/")
	id := parts[0]
	f.mu.Lock()
	owner, exists := f.owners[id]
	f.mu.Unlock()
	if !exists || owner != person {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, hostNoSuchChat)
		return
	}
	switch {
	case len(parts) == 1 && r.Method == http.MethodGet:
		f.mu.Lock()
		fingerprint := fingerprintOf(f.tokens[id])
		if lie, ok := f.rowBearer[id]; ok {
			fingerprint = lie
		}
		state := "suspended"
		if f.running[id] {
			state = "running"
		}
		row := map[string]string{"id": id, "state": state, "model_bearer": fingerprint}
		if title := f.titles[id]; title != "" {
			row["title"] = title
		}
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(row)
	case len(parts) == 1 && r.Method == http.MethodDelete:
		f.mu.Lock()
		delete(f.owners, id)
		delete(f.tokens, id)
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	case len(parts) == 2 && parts[1] == "stop" && r.Method == http.MethodPost:
		f.mu.Lock()
		f.tokens[id] = ""
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	case len(parts) == 2 && parts[1] == "credentials" && r.Method == http.MethodPut:
		f.mu.Lock()
		f.tokens[id] = jsonField(raw, "model_token")
		f.pushes = append(f.pushes, hostPush{id, person, jsonField(raw, "model_url"), jsonField(raw, "model_token")})
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	case len(parts) == 2 && parts[1] == "wire" && r.Method == http.MethodGet:
		f.mu.Lock()
		held := f.tokens[id]
		wire := f.wire
		refusal := f.wireRefusal
		f.mu.Unlock()
		if refusal != 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(refusal)
			_, _ = io.WriteString(w, `{"error":"You already have 2 chats running; stop one first."}`+"\n")
			return
		}
		if held == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"error":"This chat has no model access yet."}`+"\n")
			return
		}
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, CompressionMode: websocket.CompressionDisabled})
		if err != nil {
			return
		}
		defer c.CloseNow()
		c.SetReadLimit(4 << 20)
		if wire != nil {
			wire(c, r)
			return
		}
		for {
			kind, data, err := c.Read(r.Context())
			if err != nil {
				return
			}
			if c.Write(r.Context(), kind, data) != nil {
				return
			}
		}
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// jsonField is one string field of a JSON object; fields that are not strings
// (the list of git hosts a push now carries) are ignored.
func jsonField(raw []byte, name string) string {
	var row map[string]json.RawMessage
	_ = json.Unmarshal(raw, &row)
	var value string
	_ = json.Unmarshal(row[name], &value)
	return value
}

// fakeUpstream stands in for the model endpoint behind /v1/model.
type fakeUpstream struct {
	mu      sync.Mutex
	seen    []upSeen
	handler http.HandlerFunc
}

type upSeen struct {
	method, uri string
	header      http.Header
	body        string
}

func (u *fakeUpstream) requests() []upSeen {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]upSeen(nil), u.seen...)
}

func (u *fakeUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(raw))
	u.mu.Lock()
	u.seen = append(u.seen, upSeen{r.Method, r.URL.RequestURI(), r.Header.Clone(), string(raw)})
	handler := u.handler
	u.mu.Unlock()
	if handler != nil {
		handler(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"ok":true}`)
}

// hostedRig is the organization scene with hosted chats turned on and a fake
// chats' host and upstream behind it.
type hostedRig struct {
	*wsOrg
	host    *fakeChatHost
	hostSrv *httptest.Server
	up      *fakeUpstream
	upSrv   *httptest.Server
}

const (
	rigSecret   = "shared-secret-0123456789-abcdefghij"
	rigModelURL = "http://codeaf.test/v1/model"
)

func newHostedRig(t *testing.T) *hostedRig {
	t.Helper()
	rg := &hostedRig{wsOrg: newWSOrg(t), host: newFakeChatHost(rigSecret), up: &fakeUpstream{}}
	rg.hostSrv = httptest.NewServer(rg.host)
	rg.upSrv = httptest.NewServer(rg.up)
	t.Cleanup(rg.hostSrv.Close)
	t.Cleanup(rg.upSrv.Close)
	rg.configure(t, func(*HostedConfig) {})
	return rg
}

// configure restarts the handler with hosted chats on, the way the scene's
// other tests restart theirs; the signed-in people survive the restart.
func (rg *hostedRig) configure(t *testing.T, edit func(*HostedConfig)) {
	t.Helper()
	cfg := HostedConfig{URL: rg.hostSrv.URL, Secret: rigSecret, ModelURL: rigModelURL, ModelUpstream: rg.upSrv.URL + "/api/v1"}
	edit(&cfg)
	rg.cfg.Hosted = &cfg
	restartAccessOrg(t, rg.wsOrg)
}

// send makes one request as who ("" for nobody) from a remote address, with
// extra headers set after the credential so a test may overwrite it.
func (rg *hostedRig) send(who, method, target string, body any, extra map[string]string) *httptest.ResponseRecorder {
	text := ""
	if body != nil {
		data, _ := json.Marshal(body)
		text = string(data)
	}
	r := httptest.NewRequest(method, target, strings.NewReader(text))
	r.RemoteAddr = "192.0.2.1:1234"
	if who != "" {
		r.Header.Set("Authorization", "Bearer "+rg.tokens[who])
	}
	for k, v := range extra {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	rg.h.ServeHTTP(w, r)
	return w
}

// create makes a chat as who and answers its id.
func (rg *hostedRig) create(t *testing.T, who string) string {
	t.Helper()
	w := rg.send(who, "POST", "/v1/hosted/sessions", nil, nil)
	var out struct{ ID string }
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &out) != nil || !hostedIDForm.MatchString(out.ID) {
		t.Fatalf("create as %s: %d %s", who, w.Code, w.Body)
	}
	return out.ID
}

// wireURL is the WebSocket address of one chat on a real server.
func wireURL(srv *httptest.Server, id string) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/hosted/sessions/" + id + "/wire"
}

func errorOf(t *testing.T, body []byte) string {
	t.Helper()
	var out struct{ Error string }
	if err := json.Unmarshal(body, &out); err != nil || out.Error == "" {
		t.Fatalf("not an error body: %q", body)
	}
	return out.Error
}

// forbiddenToHost lists the headers a client may send that must never be
// seen by the chats' host or by the upstream.
var forbiddenToHost = []string{"Cookie", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "Forwarded", "X-Real-Ip", "Origin", "X-Codeaf-Anything"}

// hostedClientHeaders is everything a hostile client might try to supply.
func hostedClientHeaders() map[string]string {
	return map[string]string{"X-Codeaf-Person": "someone-else", "X-Codeaf-Anything": "1", "Cookie": "sid=stolen",
		"X-Forwarded-For": "203.0.113.9", "X-Forwarded-Host": "evil.example", "X-Forwarded-Proto": "https",
		"Forwarded": "for=203.0.113.9", "X-Real-Ip": "203.0.113.9", "Origin": "https://evil.example"}
}

// THE ORGANIZATION SERVER IS OFF BY DEFAULT: with no hosted setting, and with
// a setting that is empty, /v1/hosted and /v1/model answer exactly as a path
// the server has never heard of, for a signed-in person and for a stranger
// alike, and a known endpoint is untouched.
func TestC8_HostedOffByDefault(t *testing.T) {
	plain := func(t *testing.T, o *wsOrg) {
		t.Helper()
		type probe struct{ method, path string }
		for _, who := range []string{"santosh", ""} {
			for _, p := range []probe{{"GET", "/v1/hosted/sessions"}, {"POST", "/v1/hosted/sessions"},
				{"GET", "/v1/hosted/sessions/hs_000000000000000000000000"}, {"POST", "/v1/hosted/sessions/hs_000000000000000000000000/stop"},
				{"GET", "/v1/hosted/sessions/hs_000000000000000000000000/wire"}, {"GET", "/v1/model/x"}, {"POST", "/v1/model/chat/completions"}} {
				got, want := dropRequestID(t, hostedAs(o, who, p.method, p.path)), dropRequestID(t, hostedAs(o, who, p.method, "/v1/nope"))
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("%s %s as %q answered %v; an unknown path answers %v", p.method, p.path, who, got, want)
				}
			}
		}
		// A model-looking bearer is no more welcome than any other credential.
		bearer := map[string]string{"Authorization": "Bearer cmt_" + strings.Repeat("A", 43)}
		got, want := dropRequestID(t, hostedWith(o, "GET", "/v1/model/x", bearer)), dropRequestID(t, hostedWith(o, "GET", "/v1/nope", bearer))
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("a model bearer on /v1/model/x answered %v; an unknown path answers %v", got, want)
		}
		if got := hostedAs(o, "santosh", "GET", "/v1/me"); got.status != 200 {
			t.Fatalf("/v1/me = %d %s", got.status, got.body)
		}
	}
	t.Run("unset", func(t *testing.T) { plain(t, newWSOrg(t)) })
	t.Run("empty", func(t *testing.T) {
		o := newWSOrg(t)
		o.cfg.Hosted = &HostedConfig{}
		restartAccessOrg(t, o)
		plain(t, o)
	})
}

type hostedResult struct {
	status int
	kind   string
	body   []byte
}

func hostedAs(o *wsOrg, who, method, path string) hostedResult {
	if who == "" {
		return hostedWith(o, method, path, nil)
	}
	return hostedWith(o, method, path, map[string]string{"Authorization": "Bearer " + o.tokens[who]})
}

func hostedWith(o *wsOrg, method, path string, header map[string]string) hostedResult {
	r := httptest.NewRequest(method, path, nil)
	r.RemoteAddr = "192.0.2.1:1234"
	for k, v := range header {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	o.h.ServeHTTP(w, r)
	return hostedResult{w.Code, w.Header().Get("Content-Type"), w.Body.Bytes()}
}

// dropRequestID reads an answer without the one field that differs by design.
func dropRequestID(t *testing.T, res hostedResult) map[string]any {
	t.Helper()
	out := map[string]any{"status": res.status, "type": res.kind}
	var body map[string]any
	if json.Unmarshal(res.body, &body) == nil {
		delete(body, "request_id")
		out["body"] = body
	} else {
		out["body"] = string(res.body)
	}
	return out
}

// A CLIENT CANNOT SUPPLY WHO IT IS: whatever identity headers, credentials and
// forwarding headers a signed-in person sends on a list, a create, a read, a
// stop, a delete and a wire, the chats' host receives the shared secret and the
// VERIFIED handle, and none of what the client sent.
func TestC10_ClientCannotSupplyIdentityHeaders(t *testing.T) {
	rg := newHostedRig(t)
	hostile := hostedClientHeaders()
	check := func(t *testing.T, what string, before int) {
		t.Helper()
		got := rg.host.requests()[before:]
		if len(got) == 0 {
			t.Fatalf("%s: the chats' host saw nothing", what)
		}
		for _, seen := range got {
			if seen.header.Get("Authorization") != "Bearer "+rigSecret {
				t.Fatalf("%s: %s %s authorization = %q", what, seen.method, seen.path, seen.header.Get("Authorization"))
			}
			if seen.header.Get("X-Codeaf-Person") != "santosh" {
				t.Fatalf("%s: %s %s person = %q", what, seen.method, seen.path, seen.header.Get("X-Codeaf-Person"))
			}
			for _, name := range forbiddenToHost {
				if values := seen.header.Values(name); len(values) != 0 {
					t.Fatalf("%s: %s %s carried %s: %v", what, seen.method, seen.path, name, values)
				}
			}
			// The two headers that name an owner's window are this server's own, set on the wire and nowhere else.
			onWire := strings.HasSuffix(seen.path, "/wire")
			for name := range seen.header {
				ours := onWire && (name == hostedWindowHeader || name == hostedWindowDeviceHeader)
				if strings.HasPrefix(strings.ToLower(name), "x-codeaf-") && name != "X-Codeaf-Person" && !ours {
					t.Fatalf("%s: %s %s carried %s", what, seen.method, seen.path, name)
				}
			}
			if onWire && (seen.header.Get(hostedWindowHeader) != "santosh" || seen.header.Get(hostedWindowDeviceHeader) != "own") {
				t.Fatalf("%s: the window was named %q (device %q), and only the server may name it",
					what, seen.header.Get(hostedWindowHeader), seen.header.Get(hostedWindowDeviceHeader))
			}
		}
	}
	before := rg.host.count()
	w := rg.send("santosh", "POST", "/v1/hosted/sessions", nil, hostile)
	var made struct{ ID string }
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &made) != nil || !hostedIDForm.MatchString(made.ID) {
		t.Fatalf("create = %d %s", w.Code, w.Body)
	}
	check(t, "create", before)
	if owner := rg.host.ownerOf(made.ID); owner != "santosh" {
		t.Fatalf("the chat belongs to %q, not the signed-in person", owner)
	}
	for _, step := range []struct{ name, method, path string }{
		{"list", "GET", "/v1/hosted/sessions"}, {"read", "GET", "/v1/hosted/sessions/" + made.ID},
		{"stop", "POST", "/v1/hosted/sessions/" + made.ID + "/stop"}, {"delete", "DELETE", "/v1/hosted/sessions/" + made.ID}} {
		before = rg.host.count()
		if w := rg.send("santosh", step.method, step.path, nil, hostile); w.Code >= 400 {
			t.Fatalf("%s = %d %s", step.name, w.Code, w.Body)
		}
		check(t, step.name, before)
	}
	// The wire, on a real server so the handshake and the splice are real.
	id := "hs_" + strings.Repeat("a", 24)
	rg.host.adopt(id, "santosh")
	srv := httptest.NewServer(rg.h)
	t.Cleanup(srv.Close)
	header := http.Header{"Authorization": {"Bearer " + rg.tokens["santosh"]}}
	for k, v := range hostile {
		header.Set(k, v)
	}
	// A window cannot choose the name the others read for it by sending the headers that carry it.
	header.Set(hostedWindowHeader, "mallory")
	header.Set(hostedWindowDeviceHeader, "nothing")
	// A page this server serves sends this server's own origin, and that must
	// still not reach the chats' host. A page from another site is refused
	// before the host is asked (upgrade_origin_test.go).
	header.Set("Origin", srv.URL)
	before = rg.host.count()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wireURL(srv, id), &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Write(ctx, websocket.MessageBinary, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Read(ctx); err != nil {
		t.Fatal(err)
	}
	c.Close(websocket.StatusNormalClosure, "")
	check(t, "wire", before)
}

// NO VERIFIED PERSON MEANS NO REQUEST TO THE CHATS' HOST: a stranger, a caller
// the server lets in without a person (the machine itself, or the operator's
// own access token, neither of which names a GitHub handle) and a wire from
// either all get a 401, and the host is never asked anything on their behalf.
func TestC10_NoIdentityIsRefused(t *testing.T) {
	rg := newHostedRig(t)
	loopback := func(method, target string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, target, nil)
		r.RemoteAddr = "127.0.0.1:9"
		r.Header.Set("Connection", "Upgrade")
		r.Header.Set("Upgrade", "websocket")
		w := httptest.NewRecorder()
		rg.h.ServeHTTP(w, r)
		return w
	}
	operator := func(method, target string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, target, nil)
		r.RemoteAddr = "192.0.2.1:9"
		r.Header.Set("Authorization", "Bearer operator-secret")
		w := httptest.NewRecorder()
		rg.h.ServeHTTP(w, r)
		return w
	}
	id := "hs_" + strings.Repeat("b", 24)
	for _, step := range []struct{ method, path string }{
		{"GET", "/v1/hosted/sessions"}, {"POST", "/v1/hosted/sessions"}, {"GET", "/v1/hosted/sessions/" + id},
		{"POST", "/v1/hosted/sessions/" + id + "/stop"}, {"DELETE", "/v1/hosted/sessions/" + id}, {"GET", "/v1/hosted/sessions/" + id + "/wire"}} {
		for name, w := range map[string]*httptest.ResponseRecorder{
			"stranger": rg.send("", step.method, step.path, nil, nil), "this machine": loopback(step.method, step.path), "operator token": operator(step.method, step.path)} {
			if w.Code != 401 {
				t.Fatalf("%s %s as %s = %d %s; want 401", step.method, step.path, name, w.Code, w.Body)
			}
		}
	}
	if n := rg.host.count(); n != 0 {
		t.Fatalf("the chats' host was asked %d times for callers with no verified person: %+v", n, rg.host.requests())
	}
}

// A CHAT THAT IS NOT YOURS LOOKS LIKE ONE THAT DOES NOT EXIST: for a read, a
// stop, a delete and a wire, the answer for another person's chat is byte for
// byte the answer for an id nobody has and for a name that cannot be an id, and
// naming someone's chat costs its owner nothing.
func TestC10_OtherPersonsSessionLooksNonexistent(t *testing.T) {
	rg := newHostedRig(t)
	mine := rg.create(t, "santosh")
	token := rg.host.tokenOf(mine)
	if token == "" {
		t.Fatal("create gave the chat no model access")
	}
	ghost := "hs_" + strings.Repeat("0", 24)
	// Santosh's chat as the chats' host knows it, which this server has no
	// bearer for (as after a restart): the wire must be answered by the host.
	known := "hs_" + strings.Repeat("c", 24)
	rg.host.adopt(known, "santosh")
	upgrade := map[string]string{"Connection": "Upgrade", "Upgrade": "websocket", "Sec-WebSocket-Version": "13", "Sec-WebSocket-Key": "dGhlIHNhbXBsZSBub25jZQ=="}
	for _, step := range []struct {
		name, method, suffix string
		extra                map[string]string
	}{{"read", "GET", "", nil}, {"stop", "POST", "/stop", nil}, {"delete", "DELETE", "", nil}, {"wire", "GET", "/wire", upgrade}} {
		answer := func(who, id string) (int, string) {
			w := rg.send(who, step.method, "/v1/hosted/sessions/"+id+step.suffix, nil, step.extra)
			return w.Code, w.Body.String()
		}
		wantCode, wantBody := answer("abir", ghost)
		if wantCode != 404 || wantBody != hostNoSuchChat {
			t.Fatalf("%s of an unknown chat = %d %q", step.name, wantCode, wantBody)
		}
		for _, id := range []string{mine, known, "hs_zzz", "hs_" + strings.Repeat("A", 24), "hs_" + strings.Repeat("a", 23)} {
			if code, body := answer("abir", id); code != wantCode || body != wantBody {
				t.Fatalf("%s of %s as another person = %d %q; an unknown chat answers %d %q", step.name, id, code, body, wantCode, wantBody)
			}
		}
	}
	if pushes := rg.host.pushList(); len(pushes) != 1 || pushes[0].id != mine {
		t.Fatalf("another person's attach made the host take credentials: %+v", pushes)
	}
	// Nothing abir did touched the owner's chat or its model access.
	if w := rg.send("santosh", "GET", "/v1/hosted/sessions/"+mine, nil, nil); w.Code != 200 {
		t.Fatalf("the owner's chat after all that = %d %s", w.Code, w.Body)
	}
	if w := rg.send("", "GET", "/v1/model/models", nil, map[string]string{"Authorization": "Bearer " + token}); w.Code != 200 {
		t.Fatalf("the owner's model access after another person's stop and delete = %d %s", w.Code, w.Body)
	}
	if w := rg.send("santosh", "GET", "/v1/hosted/sessions", nil, nil); w.Code != 200 || !strings.Contains(w.Body.String(), mine) {
		t.Fatalf("the owner's list = %d %s", w.Code, w.Body)
	}
	if w := rg.send("abir", "GET", "/v1/hosted/sessions", nil, nil); w.Code != 200 || strings.Contains(w.Body.String(), mine) {
		t.Fatalf("another person's list = %d %s", w.Code, w.Body)
	}
}

// A WINDOW AND ITS CHAT TALK THROUGH THIS SERVER AND IT CHANGES NOTHING: binary
// frames survive both ways unchanged, the chats' host is given the model bearer
// once, before the connection opens, only when the chat has none live, and a
// stop makes the next attach give it a new one.
func TestC1_WireProxyCarriesFramesBothWays(t *testing.T) {
	rg := newHostedRig(t)
	srv := httptest.NewServer(rg.h)
	t.Cleanup(srv.Close)
	id := "hs_" + strings.Repeat("d", 24)
	rg.host.adopt(id, "santosh")
	attach := func() *websocket.Conn {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		c, _, err := websocket.Dial(ctx, wireURL(srv, id), &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + rg.tokens["santosh"]}}})
		if err != nil {
			t.Fatal(err)
		}
		c.SetReadLimit(1 << 20)
		return c
	}
	frames := [][]byte{{0}, {}, bytes.Repeat([]byte{0, 1, 2, 254, 255, '\n', '{'}, 20000)}
	roundTrip := func(c *websocket.Conn) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		for _, frame := range frames {
			if err := c.Write(ctx, websocket.MessageBinary, frame); err != nil {
				t.Fatal(err)
			}
			kind, got, err := c.Read(ctx)
			if err != nil || kind != websocket.MessageBinary || !bytes.Equal(got, frame) {
				t.Fatalf("a %d-byte frame came back as %v, %d bytes, %v", len(frame), kind, len(got), err)
			}
		}
	}
	first := attach()
	roundTrip(first)
	pushes := rg.host.pushList()
	if len(pushes) != 1 || pushes[0].id != id || pushes[0].person != "santosh" || pushes[0].url != rigModelURL ||
		!strings.HasPrefix(pushes[0].token, "cmt_") || len(pushes[0].token) != 47 {
		t.Fatalf("before the first attach the host was given %+v", pushes)
	}
	first.Close(websocket.StatusNormalClosure, "")
	second := attach()
	roundTrip(second)
	second.Close(websocket.StatusNormalClosure, "")
	if got := rg.host.pushList(); len(got) != 1 {
		t.Fatalf("a re-attach with a live bearer pushed again: %+v", got)
	}
	if w := rg.send("santosh", "POST", "/v1/hosted/sessions/"+id+"/stop", nil, nil); w.Code != 204 {
		t.Fatalf("stop = %d %s", w.Code, w.Body)
	}
	third := attach()
	roundTrip(third)
	third.Close(websocket.StatusNormalClosure, "")
	got := rg.host.pushList()
	if len(got) != 2 || got[1].token == got[0].token || got[1].url != rigModelURL {
		t.Fatalf("after a stop the host was given %+v", got)
	}
	// A host that forgot its credentials (it restarted) says so in its row, so
	// the next attach pushes again BEFORE the wire opens and the window never
	// meets a refusal (P1-7).
	rg.host.adopt(id, "santosh")
	healed := attach()
	roundTrip(healed)
	healed.Close(websocket.StatusNormalClosure, "")
	if got := rg.host.pushList(); len(got) != 3 {
		t.Fatalf("after the host forgot, the next attach pushed %d times in all: %+v", len(got), got)
	}
	// And a host that forgets between the org's read of its row and the wire
	// (the row still reported the org's bearer) answers the wire with a
	// refusal; that ends the bearer, so the attach after it pushes again
	// instead of finding a live bearer and skipping.
	rg.host.lieAboutBearer(id, fingerprintOf(rg.host.tokenOf(id)))
	rg.host.adopt(id, "santosh")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, resp, err := websocket.Dial(ctx, wireURL(srv, id), &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + rg.tokens["santosh"]}}}); err == nil || resp == nil || resp.StatusCode != 409 {
		t.Fatalf("a host with no credentials let the wire open: %v %v", resp, err)
	}
	rg.host.mu.Lock()
	delete(rg.host.rowBearer, id)
	rg.host.mu.Unlock()
	again := attach()
	roundTrip(again)
	again.Close(websocket.StatusNormalClosure, "")
	if got := rg.host.pushList(); len(got) != 4 {
		t.Fatalf("after the host forgot between the row and the wire, the next attach pushed %d times in all: %+v", len(got), got)
	}
}

// THE WIRE GOES THROUGH THE SERVER'S OWN HANDLER, not around it: a GET with no
// idempotency key reaches the route, and the same GET with a key is not made
// into a replay that would hold a connection's answer.
func TestC1_WireIsServedByTheRealHandler(t *testing.T) {
	rg := newHostedRig(t)
	id := "hs_" + strings.Repeat("e", 24)
	rg.host.adopt(id, "santosh")
	srv := httptest.NewServer(rg.h)
	t.Cleanup(srv.Close)
	for _, key := range []string{"", "k-1"} {
		header := http.Header{"Authorization": {"Bearer " + rg.tokens["santosh"]}}
		if key != "" {
			header.Set("Idempotency-Key", key)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		c, _, err := websocket.Dial(ctx, wireURL(srv, id), &websocket.DialOptions{HTTPHeader: header})
		if err != nil {
			cancel()
			t.Fatalf("key %q: %v", key, err)
		}
		if err := c.Write(ctx, websocket.MessageBinary, []byte("ping")); err != nil {
			t.Fatal(err)
		}
		if _, got, err := c.Read(ctx); err != nil || string(got) != "ping" {
			t.Fatalf("key %q: %q %v", key, got, err)
		}
		c.Close(websocket.StatusNormalClosure, "")
		cancel()
	}
}

// EVERY SENTENCE THIS SERVER SAYS ON ITS OWN ABOUT HOSTED CHATS IS PLAIN: each
// way the hosted routes and the model address can refuse or fail, driven
// through the real handler, says one sentence with none of the words a person
// has no use for.
func TestC9_PlainWords(t *testing.T) {
	rg := newHostedRig(t)
	id := rg.create(t, "santosh")
	token := rg.host.tokenOf(id)
	upgrade := map[string]string{"Connection": "Upgrade", "Upgrade": "websocket", "Sec-WebSocket-Version": "13", "Sec-WebSocket-Key": "dGhlIHNhbXBsZSBub25jZQ=="}
	bearerOf := func(t string) map[string]string { return map[string]string{"Authorization": "Bearer " + t} }
	type step struct {
		name         string
		who          string
		method, path string
		extra        map[string]string
		status       int
	}
	ended := func(w *httptest.ResponseRecorder) string { return errorOf(t, w.Body.Bytes()) }
	banned := []string{"uid", "sandbox", "sessionhost", "fence", "landlock", "daemon"}
	run := func(steps []step) {
		t.Helper()
		for _, s := range steps {
			w := rg.send(s.who, s.method, s.path, nil, s.extra)
			if w.Code != s.status {
				t.Fatalf("%s: %s %s = %d %s; want %d", s.name, s.method, s.path, w.Code, w.Body, s.status)
			}
			words := ended(w)
			if !strings.HasSuffix(words, ".") || strings.Contains(words, "\n") {
				t.Fatalf("%s: %q is not one plain sentence", s.name, words)
			}
			for _, word := range banned {
				if strings.Contains(strings.ToLower(words), word) {
					t.Fatalf("%s says %q, which has %q in it", s.name, words, word)
				}
			}
		}
	}
	// Model address refusals, before anything is down.
	run([]step{
		{"no bearer", "", "GET", "/v1/model/models", nil, 401},
		{"unknown bearer", "", "GET", "/v1/model/models", bearerOf("cmt_" + strings.Repeat("Z", 43)), 401},
		{"an organization credential", "", "GET", "/v1/model/models", bearerOf(rg.tokens["santosh"]), 401},
		{"wrong method", "", "DELETE", "/v1/model/models", bearerOf(token), 405},
		{"path out of the upstream", "", "GET", "/v1/model/../../x", bearerOf(token), 404},
		{"malformed id", "santosh", "GET", "/v1/hosted/sessions/hs_nope", nil, 404},
	})
	// A caller the server lets in without a verified person (this machine). The server no longer trusts its own computer by default
	// (loopback_trust), so the test turns that on to keep checking the hosted sentence, and then checks the refusal without it.
	trust := true
	rg.h.config.LoopbackTrust = &trust
	own := httptest.NewRequest("GET", "/v1/hosted/sessions", nil)
	own.RemoteAddr = "127.0.0.1:9"
	w := httptest.NewRecorder()
	rg.h.ServeHTTP(w, own)
	if w.Code != 401 || !strings.HasSuffix(errorOf(t, w.Body.Bytes()), ".") {
		t.Fatalf("no verified person = %d %s", w.Code, w.Body)
	}
	trust = false
	untrusted := httptest.NewRequest("GET", "/v1/hosted/sessions", nil)
	untrusted.RemoteAddr = "127.0.0.1:9"
	w = httptest.NewRecorder()
	rg.h.ServeHTTP(w, untrusted)
	if w.Code != 401 {
		t.Fatalf("this machine without loopback trust = %d %s; want 401", w.Code, w.Body)
	}
	big := httptest.NewRequest("POST", "/v1/model/chat/completions", strings.NewReader("{}"))
	big.RemoteAddr, big.ContentLength = "192.0.2.1:1", 17<<20
	big.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	rg.h.ServeHTTP(w, big)
	if w.Code != 413 || !strings.HasSuffix(errorOf(t, w.Body.Bytes()), ".") {
		t.Fatalf("an oversized request = %d %s", w.Code, w.Body)
	}
	if w := rg.send("santosh", "POST", "/v1/hosted/sessions/"+id+"/stop", nil, nil); w.Code != 204 {
		t.Fatalf("stop = %d", w.Code)
	}
	run([]step{{"revoked bearer", "", "GET", "/v1/model/models", bearerOf(token), 401}})
	// The upstream goes down: a live bearer, and nothing to answer it.
	live := rg.create(t, "santosh")
	liveToken := rg.host.tokenOf(live)
	rg.upSrv.Close()
	run([]step{{"upstream down", "", "POST", "/v1/model/chat/completions", bearerOf(liveToken), 502}})
	// The chats' host goes down.
	rg.hostSrv.Close()
	run([]step{
		{"list with the host down", "santosh", "GET", "/v1/hosted/sessions", nil, 502},
		{"create with the host down", "santosh", "POST", "/v1/hosted/sessions", nil, 502},
		{"read with the host down", "santosh", "GET", "/v1/hosted/sessions/" + live, nil, 502},
		{"wire with a live bearer and the host down", "santosh", "GET", "/v1/hosted/sessions/" + live + "/wire", upgrade, 502},
		{"stop with the host down", "santosh", "POST", "/v1/hosted/sessions/" + live + "/stop", nil, 502},
		{"wire with no bearer and the host down", "santosh", "GET", "/v1/hosted/sessions/hs_" + strings.Repeat("f", 24) + "/wire", upgrade, 502},
	})
}

// A HALF-WRITTEN SETTING NEVER STARTS HOSTED CHATS: with no address the feature
// is off and nothing is said; with an address, a short secret, a bad address or
// a missing model setting is refused with a sentence that names the setting and
// never repeats a value.
func TestC8_HostedSettingsOffWhenEmptyAndRefusedWhenHalfSet(t *testing.T) {
	good := [5]string{"http://chats:8790", "0123456789abcdef0123456789abcdef", "http://codeaf:8787/v1/model/", "http://stub:18890/api/v1/", ""}
	if cfg, err := ParseHostedConfig("", good[1], good[2], good[3], good[4]); cfg != nil || err != nil {
		t.Fatalf("an empty address = %v, %v; want off", cfg, err)
	}
	if cfg, err := ParseHostedConfig("   ", "", "", "", ""); cfg != nil || err != nil {
		t.Fatalf("a blank address = %v, %v; want off", cfg, err)
	}
	cfg, err := ParseHostedConfig(good[0], good[1], good[2], good[3], " upstream-key ")
	if err != nil || cfg == nil || cfg.ModelURL != "http://codeaf:8787/v1/model" || cfg.ModelUpstream != "http://stub:18890/api/v1" || cfg.ModelUpstreamKey != "upstream-key" {
		t.Fatalf("a good setting = %+v, %v", cfg, err)
	}
	for name, bad := range map[string][5]string{
		"short secret":       {good[0], "short-secret-value-31-characters-x"[:31], good[2], good[3], ""},
		"secret with space":  {good[0], "0123456789abcdef 123456789abcdef-", good[2], good[3], ""},
		"no scheme":          {"chats:8790", good[1], good[2], good[3], ""},
		"file scheme":        {"file:///etc", good[1], good[2], good[3], ""},
		"sign-in in address": {"http://u:p@chats:8790", good[1], good[2], good[3], ""},
		"missing model url":  {good[0], good[1], "", good[3], ""},
		"missing upstream":   {good[0], good[1], good[2], "", ""},
		"bad upstream":       {good[0], good[1], good[2], "://nope", ""},
	} {
		got, err := ParseHostedConfig(bad[0], bad[1], bad[2], bad[3], bad[4])
		if got != nil || err == nil {
			t.Fatalf("%s: got %+v, %v", name, got, err)
		}
		if !strings.Contains(err.Error(), "CODEAF_HOSTED_") {
			t.Fatalf("%s: %q names no setting", name, err)
		}
		for _, value := range []string{bad[1], bad[4], bad[0]} {
			if len(value) > 8 && strings.Contains(err.Error(), value) {
				t.Fatalf("%s: the error repeats a value: %q", name, err)
			}
		}
	}
}
