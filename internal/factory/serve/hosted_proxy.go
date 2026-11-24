package serve

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// This file lets a signed-in person run a whole chat on the organization's own
// machine. The chats themselves run in a separate service (their own container,
// their own volume, one Linux user for each chat); this server is the one door
// to it. It knows who is asking, mints the chat its model bearer, and then does
// nothing clever: it forwards, with its own shared secret and the VERIFIED
// person on every request and never a word of identity the caller supplied.

// HostedConfig turns hosted chats on. Nil is the default and means NOTHING is
// registered and no path answers differently from today (contract C8).
type HostedConfig struct {
	// URL is where the chats' host answers, for example http://chats:8790.
	URL string
	// Secret is the secret this server presents to that host. It is at least
	// 32 characters, is never sent to a client and never reaches a chat.
	Secret string
	// ModelURL is the address a CHAT uses to reach this server's /v1/model.
	ModelURL string
	// ModelUpstream is the model endpoint /v1/model forwards to.
	ModelUpstream string
	// ModelUpstreamKey is the upstream's own key, empty for a keyless stub.
	ModelUpstreamKey string

	// state is made by hostedRoutes, on the handler's own copy of this
	// struct, so two handlers built from one Config never share bearers.
	state *hostedState
}

// hostedState is everything a handler keeps for hosted chats.
type hostedState struct {
	base, upstream *url.URL
	secret         string
	modelURL       string
	upstreamKey    string
	calls          *http.Client
	transport      *http.Transport
	modelTransport *http.Transport
	bearers        bearerTable
	// attach serializes the first attach of a chat with the push of its
	// credentials. Without it a second window attaching at the same moment
	// would see a live bearer, skip the push and reach the chat's host before
	// the first window's push did. Attaches are a person's deliberate act, so
	// holding one lock across one bounded call is cheaper than a lock per chat.
	attach sync.Mutex
}

const (
	hostedCallTimeout = 15 * time.Second
	hostedBodyCap     = 1 << 20

	hostedSignIn      = "Sign in to use chats on this server."
	hostedNoSuchChat  = "There is no such chat."
	hostedUnreachable = "The place chats run on is not answering."
	hostedNotStarted  = "A chat could not be started just now."
)

// hostedNoSuchChatBody is the exact text the chats' host writes for a chat
// that does not exist, so a name that cannot be a chat at all is answered
// byte for byte as one that merely is not yours.
const hostedNoSuchChatBody = `{"error":"` + hostedNoSuchChat + `"}` + "\n"

// hostedIDForm is the only shape a chat's id can have; it is checked before
// the id goes into any address, so no caller can steer a request elsewhere.
var hostedIDForm = regexp.MustCompile(`^hs_[0-9a-f]{24}$`)

// ParseHostedConfig reads the five hosted settings as the operator wrote them.
// An empty URL is "off" and answers nil with no error. Anything else must be
// complete and well formed, because a half-set feature that starts anyway
// would run chats with a secret nobody checked; the error says which part and
// never repeats a value.
func ParseHostedConfig(rawURL, secret, modelURL, upstream, upstreamKey string) (*HostedConfig, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, nil
	}
	cfg := &HostedConfig{Secret: secret, ModelUpstreamKey: strings.TrimSpace(upstreamKey)}
	var err error
	if cfg.URL, _, err = hostedAddress("CODEAF_HOSTED_URL", rawURL); err != nil {
		return nil, err
	}
	if len(secret) < 32 {
		return nil, errors.New("CODEAF_HOSTED_SECRET must be at least 32 characters")
	}
	if strings.IndexFunc(secret, func(r rune) bool { return r < 0x21 || r > 0x7e }) >= 0 {
		return nil, errors.New("CODEAF_HOSTED_SECRET must use printable characters and no spaces")
	}
	if cfg.ModelURL, _, err = hostedAddress("CODEAF_HOSTED_MODEL_URL", modelURL); err != nil {
		return nil, err
	}
	if cfg.ModelUpstream, _, err = hostedAddress("CODEAF_HOSTED_MODEL_UPSTREAM", upstream); err != nil {
		return nil, err
	}
	return cfg, nil
}

// hostedAddress checks one address setting: an http or https address with a
// host, no sign-in details, no query and no fragment. It answers the address
// without a trailing slash, so paths can be joined to it.
func hostedAddress(name, raw string) (string, *url.URL, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || raw == "" || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", nil, errors.New(name + " must be an http or https address with a host and nothing after the path")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u.String(), u, nil
}

// hostedRoutes mounts the hosted-chat routes when the setting is on, and does
// nothing at all when it is off. A setting that does not check out also leaves
// the routes off: the host's start has already refused it with a sentence.
func (h *Handler) hostedRoutes() {
	hc := h.config.Hosted
	if hc == nil {
		return
	}
	checked, err := ParseHostedConfig(hc.URL, hc.Secret, hc.ModelURL, hc.ModelUpstream, hc.ModelUpstreamKey)
	if err != nil || checked == nil {
		h.config.Hosted = nil
		return
	}
	_, base, _ := hostedAddress("", checked.URL)
	_, upstream, _ := hostedAddress("", checked.ModelUpstream)
	// The chats' host is on the organization's own network, so the call to it
	// never goes through a proxy from the environment: the shared secret goes
	// to that one address and to nothing in between.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	own := *checked
	own.state = &hostedState{
		base: base, upstream: upstream, secret: checked.Secret, modelURL: checked.ModelURL, upstreamKey: checked.ModelUpstreamKey,
		transport: transport,
		calls: &http.Client{Timeout: hostedCallTimeout, Transport: transport,
			// A redirect would carry the shared secret to wherever it points.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
	// The model proxy is different on purpose: the upstream may be a vendor on
	// the internet, so it honours the environment's proxy settings, and it has
	// no response-header timeout, because a model can think for minutes before
	// its first byte.
	own.state.modelTransport = http.DefaultTransport.(*http.Transport).Clone()
	h.config.Hosted = &own
	h.route("/v1/hosted/sessions", map[string]http.HandlerFunc{http.MethodGet: h.hostedList, http.MethodPost: h.hostedCreate})
	h.route("/v1/hosted/sessions/{id}", map[string]http.HandlerFunc{http.MethodGet: h.hostedGet, http.MethodDelete: h.hostedDelete})
	h.route("/v1/hosted/sessions/{id}/stop", map[string]http.HandlerFunc{http.MethodPost: h.hostedStop})
	h.read("/v1/hosted/sessions/{id}/wire", h.hostedWire)
}

// hostedPerson is the verified person behind a request, or a 401 written. The
// organization's own sign-in has already run; this asks the same question the
// rest of the server asks and REFUSES AN EMPTY ANSWER, because a chat's folder,
// its limits and its owner check are all keyed on the handle, and an anonymous
// request must never reach the chats' host as somebody.
func (h *Handler) hostedPerson(w http.ResponseWriter, r *http.Request) (string, bool) {
	handle, err := h.callerHandle(r)
	handle = factory.NormalizeHandle(handle)
	if err != nil || handle == "" || len(handle) > 128 ||
		strings.IndexFunc(handle, func(c rune) bool { return c < 0x21 || c > 0x7e }) >= 0 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="factory"`)
		h.fail(w, http.StatusUnauthorized, hostedSignIn)
		return "", false
	}
	return handle, true
}

// hostedChatID is the chat named in the path, or the answer to a name that
// cannot be a chat, written.
func hostedChatID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if !hostedIDForm.MatchString(id) {
		hostedNoSuchChatWrite(w)
		return "", false
	}
	return id, true
}

func hostedNoSuchChatWrite(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_, _ = io.WriteString(w, hostedNoSuchChatBody)
}

// call makes one request to the chats' host with the shared secret and the
// verified person, and nothing the caller sent.
func (hs *hostedState) call(ctx context.Context, method, path, person string, body []byte) (int, http.Header, []byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, hs.base.String()+path, reader)
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+hs.secret)
	req.Header.Set("X-Codeaf-Person", person)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := hs.calls.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, hostedBodyCap))
	return res.StatusCode, res.Header, data, err
}

// hostedRelay writes the chats' host's answer as it gave it: its status, its
// content type and its body, byte for byte. It adds nothing, so a chat that
// is not yours and a chat that does not exist cannot be told apart here.
func hostedRelay(w http.ResponseWriter, status int, header http.Header, body []byte) {
	if kind := header.Get("Content-Type"); kind != "" {
		w.Header().Set("Content-Type", kind)
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// hostedFailed says the call to the chats' host did not give an answer a
// person can use. A 401 from it is OUR secret being refused, which is the
// operator's to fix and not the person's sign-in, so it must not reach the
// client as a 401 (a client would read it as "sign in again").
func (h *Handler) hostedFailed(w http.ResponseWriter, status int, err error) bool {
	if err == nil && status != http.StatusUnauthorized {
		return false
	}
	h.fail(w, http.StatusBadGateway, hostedUnreachable)
	return true
}

func (h *Handler) hostedList(w http.ResponseWriter, r *http.Request) {
	person, ok := h.hostedPerson(w, r)
	if !ok {
		return
	}
	status, header, body, err := h.config.Hosted.state.call(r.Context(), http.MethodGet, "/v1/sessions", person, nil)
	if h.hostedFailed(w, status, err) {
		return
	}
	hostedRelay(w, status, header, body)
}

func (h *Handler) hostedGet(w http.ResponseWriter, r *http.Request) {
	person, ok := h.hostedPerson(w, r)
	if !ok {
		return
	}
	id, ok := hostedChatID(w, r)
	if !ok {
		return
	}
	status, header, body, err := h.config.Hosted.state.call(r.Context(), http.MethodGet, "/v1/sessions/"+id, person, nil)
	if h.hostedFailed(w, status, err) {
		return
	}
	hostedRelay(w, status, header, body)
}

// hostedCreate starts a chat. The id is this server's to choose, so a caller
// can never name a chat into existence or onto someone else's folder.
func (h *Handler) hostedCreate(w http.ResponseWriter, r *http.Request) {
	person, ok := h.hostedPerson(w, r)
	if !ok {
		return
	}
	hs := h.config.Hosted.state
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		h.fail(w, http.StatusInternalServerError, hostedNotStarted)
		return
	}
	id := "hs_" + hex.EncodeToString(raw[:])
	token := hs.bearers.mint(id, person, h.clock())
	body, _ := json.Marshal(map[string]string{"id": id, "model_url": hs.modelURL, "model_token": token})
	status, header, data, err := hs.call(r.Context(), http.MethodPost, "/v1/sessions", person, body)
	// A bearer for a chat that was not made is ended at once; there is
	// nothing for it to belong to.
	if h.hostedFailed(w, status, err) {
		hs.bearers.revokeToken(id, token)
		return
	}
	if status/100 != 2 {
		hs.bearers.revokeToken(id, token)
		hostedRelay(w, status, header, data)
		return
	}
	hostedRelay(w, status, header, hostedWithID(data, id))
}

// hostedWithID adds the id to a created chat's answer when the chats' host
// left it out, and changes nothing else about the answer.
func hostedWithID(body []byte, id string) []byte {
	var row map[string]json.RawMessage
	if json.Unmarshal(body, &row) != nil || row == nil {
		return body
	}
	if _, ok := row["id"]; ok {
		return body
	}
	row["id"], _ = json.Marshal(id)
	out, err := json.Marshal(row)
	if err != nil {
		return body
	}
	return append(out, '\n')
}

// hostedStop and hostedDelete end the chat's bearer as well as the chat. The
// bearer is ended whatever the chats' host answered, because a bearer that is
// ended too early costs one re-attach and one that is not costs a chat's model
// access after the person said stop; it is ended for its OWN person only.
func (h *Handler) hostedStop(w http.ResponseWriter, r *http.Request) {
	h.hostedEnd(w, r, http.MethodPost, "/stop")
}

func (h *Handler) hostedDelete(w http.ResponseWriter, r *http.Request) {
	h.hostedEnd(w, r, http.MethodDelete, "")
}

func (h *Handler) hostedEnd(w http.ResponseWriter, r *http.Request, method, suffix string) {
	person, ok := h.hostedPerson(w, r)
	if !ok {
		return
	}
	id, ok := hostedChatID(w, r)
	if !ok {
		return
	}
	hs := h.config.Hosted.state
	status, header, body, err := hs.call(r.Context(), method, "/v1/sessions/"+id+suffix, person, nil)
	hs.bearers.revokeFor(id, person)
	if h.hostedFailed(w, status, err) {
		return
	}
	hostedRelay(w, status, header, body)
}

// hostedWire joins a window to a chat. It makes sure the chat's host holds a
// live model bearer BEFORE the connection opens, then hands the connection
// over: from there it only copies bytes and reads no frame, so what the window
// and the chat say to each other is theirs alone.
func (h *Handler) hostedWire(w http.ResponseWriter, r *http.Request) {
	person, ok := h.hostedPerson(w, r)
	if !ok {
		return
	}
	id, ok := hostedChatID(w, r)
	if !ok {
		return
	}
	hs := h.config.Hosted.state
	if !h.hostedPush(w, r, hs, id, person) {
		return
	}
	target := *hs.base
	target.Path += "/v1/sessions/" + id + "/wire"
	(&httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL = &target
			pr.Out.Host = target.Host
			// Only what a WebSocket handshake needs is carried over: the
			// upgrade itself (which the proxy has already put back after
			// stripping hop-by-hop headers) and the handshake's own fields.
			// Every other header the window sent, its sign-in credential,
			// cookies, forwarding headers and any X-Codeaf header among them,
			// is left behind, and the two the chats' host trusts are ours.
			keep := http.Header{}
			for key, values := range pr.Out.Header {
				switch lower := strings.ToLower(key); {
				case lower == "connection", lower == "upgrade", strings.HasPrefix(lower, "sec-websocket-"):
					keep[key] = values
				}
			}
			pr.Out.Header = keep
			pr.Out.Header.Set("Authorization", "Bearer "+hs.secret)
			pr.Out.Header.Set("X-Codeaf-Person", person)
		},
		FlushInterval: -1,
		Transport:     hs.transport,
		ErrorLog:      log.New(io.Discard, "", 0),
		// A chat whose host says it has no model access (it restarted and
		// forgot) has its bearer ended here, so the next attach mints a new
		// one and pushes it instead of finding a live bearer and skipping.
		ModifyResponse: func(res *http.Response) error {
			if res.StatusCode == http.StatusConflict {
				hs.bearers.revokeFor(id, person)
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			h.fail(w, http.StatusBadGateway, hostedUnreachable)
		},
	}).ServeHTTP(w, r)
}

// hostedPush gives the chat's host a model bearer when this is the first
// attach of a chat that has none live: after a stop, after the twelve hours, or
// after this server restarted. It does nothing when a live one is already
// there, so an ordinary re-attach costs the chats' host no extra call. It
// answers false when it wrote the reply itself.
func (h *Handler) hostedPush(w http.ResponseWriter, r *http.Request, hs *hostedState, id, person string) bool {
	hs.attach.Lock()
	defer hs.attach.Unlock()
	token, minted, _ := hs.bearers.ensure(id, person, h.clock())
	if !minted {
		// Either a live bearer already stands, or it belongs to someone else
		// and the chats' host is about to answer for that on its own.
		return true
	}
	body, _ := json.Marshal(map[string]string{"model_url": hs.modelURL, "model_token": token})
	status, header, data, err := hs.call(r.Context(), http.MethodPut, "/v1/sessions/"+id+"/credentials", person, body)
	if h.hostedFailed(w, status, err) {
		hs.bearers.revokeToken(id, token)
		return false
	}
	if status != http.StatusNoContent {
		// A 404 here is "no such chat" or "not yours", and is relayed as the
		// chats' host said it. Either way the bearer just minted has nothing
		// to belong to.
		hs.bearers.revokeToken(id, token)
		hostedRelay(w, status, header, data)
		return false
	}
	return true
}
