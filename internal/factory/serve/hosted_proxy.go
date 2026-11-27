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
	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/provider/modelapi"
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
	// SessionCapUSD is what one chat may spend before its model calls are
	// refused, in dollars; zero means the default of $5.00
	// (CODEAF_HOSTED_SESSION_CAP_USD).
	SessionCapUSD float64
	// Heartbeat is how often a guest wire checks access. Zero uses DefaultHostedHeartbeat.
	Heartbeat time.Duration
	// CompleterFor builds the funnel one call on a model goes out through, for
	// an upstream address and a key. Nil keeps the pass-through of P0 for
	// chat completions (the unmetered road a server without the model road has).
	CompleterFor func(base, key string) func(model string) modelapi.Completer

	// ServerKey is the raw text of CODEAF_SERVER_KEY: 64 hexadecimal
	// characters, or the base64 of 32 bytes. It seals the keys people store on
	// this server (credentials.go). Empty means this server keeps none, and
	// every chat runs on the organization's key.
	ServerKey string
	// GitHosts are the origins a chat's git reaches through this server, for
	// example https://github.com. Empty means github.com alone.
	GitHosts []string

	// removalAfter and removalLogf are how a test takes the removal retry's
	// clock and log into its own hands, so the retry is proved without sleeping
	// and its lines can be counted. Nil in every running server: the retry then
	// waits on a real timer and writes to the standard log.
	removalAfter func(d time.Duration, run func()) (stop func() bool)
	removalLogf  func(format string, args ...any)

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
	// gitURL is the address a chat reaches git on, and gitHosts the origins it
	// may name there. gitURL is empty when the model address does not end in
	// /v1/model, which leaves git out of a chat entirely.
	gitURL   string
	gitHosts []string
	// keys is the sealed store of people's own keys, nil when this server has
	// no store. keysErr says why it cannot be used (no server key, a wrong one,
	// a file it cannot read): a person's key that EXISTS but cannot be opened
	// is refused, never replaced by the organization's.
	keys    *personKeys
	keysErr error
	// models is the registry of each chat's metered model server (model_route.go).
	models modelRegistry
	// refusals keeps the operator's log to one line per refused key (model_refused.go).
	refusals keyRefusals
	// attach serializes the first attach of a chat with the push of its
	// credentials. Without it a second window attaching at the same moment
	// would see a live bearer, skip the push and reach the chat's host before
	// the first window's push did. Attaches are a person's deliberate act, so
	// holding one lock across one bounded call is cheaper than a lock per chat.
	attach sync.Mutex
	// Registry mutations exclude backfill so an earlier host reply cannot restore a deleted chat.
	registry sync.RWMutex
	// removals finishes, by itself, the deletion of a removed person's chats
	// that the chats' host could not do when they were removed.
	removals hostedRemovalRetry
	// The model door and wire may record or read the current asker concurrently.
	askerMu sync.RWMutex
	askers  map[string]string
	// Guest wires are hijacked, so the handler closes them during its own shutdown.
	guestMu      sync.Mutex
	guests       map[*hostedGuest]struct{}
	guestsClosed bool
	guestWG      sync.WaitGroup
}

func (hs *hostedState) noteAsker(id, person string) {
	hs.askerMu.Lock()
	defer hs.askerMu.Unlock()
	if person == "" {
		delete(hs.askers, id)
		return
	}
	if hs.askers == nil {
		hs.askers = make(map[string]string)
	}
	hs.askers[id] = person
}

func (hs *hostedState) askerOf(id string) string {
	hs.askerMu.RLock()
	defer hs.askerMu.RUnlock()
	return hs.askers[id]
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
func ParseHostedConfig(rawURL, secret, modelURL, upstream, upstreamKey string, defaultUpstream ...bool) (*HostedConfig, error) {
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
	// The metered caller can supply its provider's default when no override is set.
	if strings.TrimSpace(upstream) != "" || len(defaultUpstream) == 0 || !defaultUpstream[0] {
		if cfg.ModelUpstream, _, err = hostedAddress("CODEAF_HOSTED_MODEL_UPSTREAM", upstream); err != nil {
			return nil, err
		}
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
	checked, err := ParseHostedConfig(hc.URL, hc.Secret, hc.ModelURL, hc.ModelUpstream, hc.ModelUpstreamKey, hc.CompleterFor != nil)
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
	// Everything the operator set is kept, not only the five settings the parse
	// checks, so a setting that has no check of its own is not dropped here.
	own := *hc
	own.URL, own.Secret, own.ModelURL = checked.URL, checked.Secret, checked.ModelURL
	own.ModelUpstream, own.ModelUpstreamKey = checked.ModelUpstream, checked.ModelUpstreamKey
	own.state = &hostedState{
		base: base, upstream: upstream, secret: checked.Secret, modelURL: checked.ModelURL, upstreamKey: checked.ModelUpstreamKey,
		transport: transport,
		calls: &http.Client{Timeout: hostedCallTimeout, Transport: transport,
			// A redirect would carry the shared secret to wherever it points.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		gitURL:   hostedGitURL(checked.ModelURL),
		gitHosts: hostedGitOrigins(&own),
	}
	// The model proxy is different on purpose: the upstream may be a vendor on
	// the internet, so it honours the environment's proxy settings, and it has
	// no response-header timeout, because a model can think for minutes before
	// its first byte.
	own.state.modelTransport = http.DefaultTransport.(*http.Transport).Clone()
	h.config.Hosted = &own
	h.hostedLoadBearers(own.state)
	h.hostedOpenKeys(own.state, own.ServerKey)
	h.route("/v1/hosted/sessions", map[string]http.HandlerFunc{http.MethodGet: h.hostedList, http.MethodPost: h.hostedCreate})
	h.route("/v1/hosted/sessions/{id}", map[string]http.HandlerFunc{http.MethodGet: h.hostedGet, http.MethodDelete: h.hostedDelete})
	h.route("/v1/hosted/sessions/{id}/stop", map[string]http.HandlerFunc{http.MethodPost: h.hostedStop})
	h.read("/v1/hosted/sessions/{id}/wire", h.hostedWire)
	h.route("/v1/hosted/keys", map[string]http.HandlerFunc{http.MethodGet: h.hostedKeyList})
	h.route("/v1/hosted/keys/{provider}", map[string]http.HandlerFunc{http.MethodPut: h.hostedKeyPut, http.MethodDelete: h.hostedKeyDelete})
	h.hostedGitRoutes()
	h.startHostedRemovals()
}

// hostedLoadBearers brings back the model access the chats already hold, from
// the store, and makes every later mint and revoke save the table. With no
// store the table stays in memory. A saved file that cannot be read starts the
// table empty and says so for the operator: an empty table only means each chat
// is given a new bearer at its next attach, which fails safe.
func (h *Handler) hostedLoadBearers(hs *hostedState) {
	st := h.config.Store
	if st == nil {
		return
	}
	save := func(rows map[string]persistedRow) error {
		out := make(map[string]store.HostedBearer, len(rows))
		for hash, row := range rows {
			out[hash] = store.HostedBearer{Session: row.Session, Person: row.Person, Expires: row.Expires}
		}
		return st.SaveHostedBearers(out)
	}
	saved, err := st.HostedBearers()
	if err != nil {
		log.Printf("hosted chats: %v; starting with no saved model access", err)
		saved = nil
	}
	rows := make(map[string]persistedRow, len(saved))
	for hash, row := range saved {
		rows[hash] = persistedRow{Session: row.Session, Person: row.Person, Expires: row.Expires}
	}
	now := h.clock()
	dropped := hs.bearers.load(rows, now)
	hs.bearers.persist = save
	// Rows that ran out while the server was off are removed from the file now
	// and not left for the next mint to find.
	if dropped > 0 || err != nil {
		if err := hs.bearers.save(now); err != nil {
			log.Printf("hosted chats: the saved model access could not be cleaned: %v", err)
		}
	}
}

// hostedGitURL is where a chat reaches git: the model address with its
// /v1/model replaced by /v1/hosted/git. A model address that does not end that
// way (an operator who put the model somewhere else) leaves git off.
func hostedGitURL(modelURL string) string {
	base, ok := strings.CutSuffix(modelURL, "/v1/model")
	if !ok {
		return ""
	}
	return base + "/v1/hosted/git"
}

// credentialFields is what a chat's host is told about its model access (and
// git, when this server offers it): the model address, the chat's bearer and,
// when git is on, the git address and the origins it may reach. None of it is
// the person's own secret.
func (hs *hostedState) credentialFields(token string) map[string]any {
	fields := map[string]any{"model_url": hs.modelURL, "model_token": token}
	if hs.gitURL != "" {
		fields["git_url"] = hs.gitURL
		fields["git_hosts"] = hs.gitHosts
	}
	return fields
}

// credentialBody is the body of a push of credentials to a chat's host.
func (hs *hostedState) credentialBody(token string) []byte {
	body, _ := json.Marshal(hs.credentialFields(token))
	return body
}

// hostedPerson is the verified person behind a request, or a 401 written. The
// organization's own sign-in has already run; this asks the same question the
// rest of the server asks and REFUSES AN EMPTY ANSWER, because a chat's folder,
// its limits and its owner check are all keyed on the handle, and an anonymous
// request must never reach the chats' host as somebody.
func (h *Handler) hostedPerson(w http.ResponseWriter, r *http.Request) (string, bool) {
	handle, err := h.callerHandle(r)
	handle = factory.NormalizeHandle(handle)
	return handle, h.hostedSignedIn(w, handle, err)
}

func (h *Handler) hostedSignedIn(w http.ResponseWriter, handle string, err error) bool {
	if err != nil || handle == "" || len(handle) > 128 ||
		strings.IndexFunc(handle, func(c rune) bool { return c < 0x21 || c > 0x7e }) >= 0 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="factory"`)
		h.fail(w, http.StatusUnauthorized, hostedSignIn)
		return false
	}
	return true
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
	v, ok := h.hostedViewer(w, r)
	if !ok {
		return
	}
	own, others, ok := h.hostedOwnList(w, r, v)
	if !ok {
		return
	}
	shared, ok := h.hostedSharedList(w, r, v, others)
	if !ok {
		return
	}
	policy := v.acc.rules().Hosting
	h.write(w, http.StatusOK, struct {
		hostedSessionsList
		Shared  []hostedSharedRow `json:"shared"`
		Hosting hostedListPolicy  `json:"hosting"`
	}{own, shared, hostedListPolicy{LocalSessions: policy.LocalSessionsFor(true), Words: policy.Words(true)}})
}

type hostedListPolicy struct {
	LocalSessions string `json:"local_sessions"`
	Words         string `json:"words,omitempty"`
}

func (h *Handler) hostedGet(w http.ResponseWriter, r *http.Request) {
	v, ok := h.hostedViewer(w, r)
	if !ok {
		return
	}
	id, ok := hostedChatID(w, r)
	if !ok {
		return
	}
	hs := h.config.Hosted.state
	owner := v.Handle
	var chat hostedChatLookup
	if h.config.Store != nil {
		chat, ok = h.resolveHostedChat(w, r, id, v)
		if !ok {
			return
		}
		if !can(v, chat.item, acl.VerbView) {
			hostedNoSuchChatWrite(w)
			return
		}
		owner = chat.owner
		if chat.legacyBody != nil {
			hostedRelay(w, http.StatusOK, chat.legacyHeader, chat.legacyBody)
			return
		}
	}
	status, header, body, err := hs.call(r.Context(), http.MethodGet, "/v1/sessions/"+id, owner, nil)
	if h.hostedFailed(w, status, err) {
		return
	}
	if owner != v.Handle && status == http.StatusOK {
		row := hostedMetadata(v, chat.item)
		if json.Unmarshal(body, &row.hostedStatusFields) != nil || row.ID != id {
			h.fail(w, http.StatusBadGateway, hostedUnreachable)
			return
		}
		row.hideUnreadTitle()
		h.write(w, http.StatusOK, row)
		return
	}
	hostedRelay(w, status, header, body)
}

// hostedCreate starts a chat. The id is this server's to choose, so a caller
// can never name a chat into existence or onto someone else's folder.
func (h *Handler) hostedCreate(w http.ResponseWriter, r *http.Request) {
	v, ok := h.hostedViewer(w, r)
	if !ok || !h.mayCreateResource(w, v) {
		return
	}
	st := h.config.Store
	person := v.Handle
	hs := h.config.Hosted.state
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		h.fail(w, http.StatusInternalServerError, hostedNotStarted)
		return
	}
	id := "hs_" + hex.EncodeToString(raw[:])
	// A BOT STARTS A CHAT ONLY WHEN IT COULD RUN ONE. The chat is its creator's
	// from its first moment, and a bot holds on it what its creator holds
	// narrowed by its scope, which is the same `run` the wire asks of anyone
	// who types into a chat. A bot whose scope leaves `run` out would make a
	// chat it can only watch, so it is refused here, before anything is made.
	if v.Agent != nil && !hostedGuestVerbs(v, h.hostedStartItem(id, person)).Has(acl.VerbRun) {
		h.fail(w, http.StatusForbidden, hostedBotNeedsRun)
		return
	}
	token, err := hs.bearers.mint(id, person, h.clock())
	if err != nil {
		log.Printf("hosted chats: a chat could not be started: %v", err)
		h.fail(w, http.StatusInternalServerError, hostedNotStarted)
		return
	}
	var row store.Resource
	if st != nil {
		hs.registry.Lock()
		defer hs.registry.Unlock()
		row, err = h.registerHostedChat(v, id)
		if err != nil {
			hs.bearers.revokeToken(id, token)
			h.accessFailed(w, err)
			return
		}
	}
	fields := hs.credentialFields(token)
	fields["id"] = id
	body, _ := json.Marshal(fields)
	status, header, data, err := hs.call(r.Context(), http.MethodPost, "/v1/sessions", person, body)
	// A bearer for a chat that was not made is ended at once; there is
	// nothing for it to belong to.
	if err != nil || status/100 != 2 {
		hs.bearers.revokeToken(id, token)
		if st != nil {
			if err := st.DeleteResource(row.Kind, row.ID); err != nil {
				log.Printf("hosted chats: a failed registration could not be removed: %v", err)
			}
		}
		if !h.hostedFailed(w, status, err) {
			hostedRelay(w, status, header, data)
		}
		return
	}
	answer := hostedWithID(data, id)
	if st != nil {
		item := h.resourceItem(row, factory.OrgWorkstream{})
		h.publishAccessEvent(row.Resource, person, h.audienceOf(item))
		// The person who made the chat learns their verbs on it from this answer, as a list row
		// tells them for every other chat, so no client has to work them out from a role table.
		answer = append(hostedWithVerbs(answer, hostedVerbsOn(v, item)), '\n')
	}
	hostedRelay(w, status, header, answer)
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
	v, ok := h.hostedViewer(w, r)
	if !ok {
		return
	}
	id, ok := hostedChatID(w, r)
	if !ok {
		return
	}
	hs := h.config.Hosted.state
	owner := v.Handle
	var before itemAudience
	st := h.config.Store
	if st != nil {
		hs.registry.Lock()
		defer hs.registry.Unlock()
		chat, ok := h.lookupHostedChat(w, r, id, v)
		if !ok {
			return
		}
		if !can(v, chat.item, acl.VerbView) {
			hostedNoSuchChatWrite(w)
			return
		}
		if !can(hostedWithoutOperator(v), chat.item, acl.VerbManage) {
			h.fail(w, http.StatusForbidden, hostedOwnerManage)
			return
		}
		owner = chat.owner
		before = v.acc.audienceOf(chat.item)
	}
	status, header, body, err := hs.call(r.Context(), method, "/v1/sessions/"+id+suffix, owner, nil)
	hs.bearers.revokeFor(id, owner)
	hs.models.forget(id, owner)
	if h.hostedFailed(w, status, err) {
		return
	}
	if st != nil && (status/100 == 2 || method == http.MethodDelete && status == http.StatusNotFound) {
		if method == http.MethodDelete {
			if h.failed(w, st.DeleteResource(store.SessionKind, id)) {
				return
			}
			hs.noteAsker(id, "")
		}
		h.publishAccessEvent(Ref{Kind: store.SessionKind, ID: id}, v.Handle, before)
	}
	hostedRelay(w, status, header, body)
}

// hostedWire checks who may attach to a chat. The owner proxy pushes the live
// model bearer before opening the connection and copies bytes without reading frames.
func (h *Handler) hostedWire(w http.ResponseWriter, r *http.Request) {
	v, ok := h.hostedViewer(w, r)
	if !ok {
		return
	}
	id, ok := hostedChatID(w, r)
	if !ok {
		return
	}
	person := v.Handle
	if h.config.Store != nil {
		chat, ok := h.resolveHostedChat(w, r, id, v)
		if !ok {
			return
		}
		switch hostedWireClassFor(v, chat.item, chat.owner) {
		case hostedWireOwner:
			person = chat.owner
		case hostedWireGuest:
			h.hostedGuestWire(w, r, chat.item, chat.owner, hostedWithoutOperator(v))
			return
		default:
			hostedNoSuchChatWrite(w)
			return
		}
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
			// The proxy never reads a frame, so the chats' host is asked to name this window for the others: the person,
			// whoever the window's own computer says it is. See hostedWindowHeader.
			pr.Out.Header.Set(hostedWindowHeader, hostedGuestLabel(v))
			if v.Agent == nil {
				pr.Out.Header.Set(hostedWindowDeviceHeader, hostedWindowDeviceOwn)
			}
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
			if res.StatusCode == http.StatusSwitchingProtocols {
				// Only an accepted attach clears the previous guest's attribution.
				hs.noteAsker(id, person)
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			h.fail(w, http.StatusBadGateway, hostedUnreachable)
		},
	}).ServeHTTP(w, r)
}

// hostedPush makes sure the chat's host holds the model bearer this server
// believes in BEFORE a wire opens. It asks the host which bearer it holds (the
// host answers with a short fingerprint, never the bearer) and compares that
// with this server's own live bearer for the chat. Equal means nothing is
// pushed, so an ordinary re-attach costs one small read. Anything else, a host
// that restarted and forgot, a stop, the seven days running out, or this
// server's own table having been lost, mints a new bearer (which ends the old
// one) and pushes it first, so the chat's first use after any of those works
// and no person sees "no model access yet". The row is read as the person, so a
// chat that is not theirs is answered exactly as the host answers it, as one
// that does not exist. It answers false when it wrote the reply itself.
func (h *Handler) hostedPush(w http.ResponseWriter, r *http.Request, hs *hostedState, id, person string) bool {
	hs.attach.Lock()
	defer hs.attach.Unlock()
	status, header, data, err := hs.call(r.Context(), http.MethodGet, "/v1/sessions/"+id, person, nil)
	if h.hostedFailed(w, status, err) {
		return false
	}
	if status != http.StatusOK {
		hostedRelay(w, status, header, data)
		return false
	}
	var row struct {
		ModelBearer string `json:"model_bearer"`
	}
	_ = json.Unmarshal(data, &row)
	// Only the owner's request can supply a sign-in credential for this chat.
	if hs.keys != nil {
		if requester, err := h.callerHandle(r); err == nil && factory.NormalizeHandle(requester) == person {
			h.keepSignInGitHub(r, person)
		}
	}
	token, minted, err := hs.bearers.ensureCurrent(id, person, h.clock(), row.ModelBearer)
	if err != nil {
		log.Printf("hosted chats: a chat could not be given model access: %v", err)
		h.fail(w, http.StatusInternalServerError, hostedNotStarted)
		return false
	}
	if !minted {
		// Either the host already holds the live bearer, or a live bearer
		// belongs to someone else and the chats' host is about to answer for
		// that on its own.
		return true
	}
	status, header, data, err = hs.call(r.Context(), http.MethodPut, "/v1/sessions/"+id+"/credentials", person, hs.credentialBody(token))
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
