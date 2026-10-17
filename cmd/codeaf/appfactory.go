package main

// ── codeaf app: THE FACTORY, ATTACHED OVER HTTP ─────────────────────────────
//
// The engine's half of appwire.Factory. It keeps a registry of the factories
// this machine is attached to, reads them over their /v1 API, and signs a
// person in to them. It hosts no factory and runs no job; capabilities says so.
//
// WHAT IS KEPT WHERE. A connection's address (an origin: no path, no query, no
// credentials) is written to <home>/app/factories.json, 0600 in a 0700 folder.
// A session or access token is held in this process's memory and is gone when
// the process is, which means a person signs in again after a restart (gap:
// config.AppCredentials has no slot for factory sessions yet). A device code
// lives inside the flow it belongs to and goes nowhere.
//
// THE CALLER OF A SIGN-IN. A flow or an access link opened by one caller can
// be polled, cancelled or even seen only by that caller. The factory binds a
// device flow to a credential and an access link to a cookie, and this registry
// gives every flow its own cookie and its own handle, so two windows on one
// engine cannot read each other's sign-in. Pending flows are capped at 256 and
// expire, the way the factory's own are.

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/trace"
)

var _ appwire.Factory = (*appBackend)(nil)

const (
	// appFactoryTimeout bounds one request to a factory, and appFactoryProbe
	// the cheap one that only asks whether it is alive.
	appFactoryTimeout = 20 * time.Second
	appFactoryProbe   = 5 * time.Second
	// appFactoryBody is the most a read may be: a queue is the whole floor.
	// appFactorySmall bounds every other answer.
	appFactoryBody  = 8 << 20
	appFactorySmall = 1 << 20
	// appFactoryFlowLife is how long an access-link flow with no news is kept.
	appFactoryFlowLife = 10 * time.Minute
	// appFactoryPerCaller is the most sign-ins one caller may have pending.
	appFactoryPerCaller  = 8
	appFactoryJoinCookie = "factory_join"
)

// appFactoryRow is what is persisted of a connection: nothing secret.
type appFactoryRow struct {
	ID   string `json:"id"`
	URL  string `json:"url"`
	Name string `json:"name,omitempty"`
	Mode string `json:"mode,omitempty"`
	// Local marks the one row this engine made for the factory of its own
	// machine (appfactory_setup.go). It is the ONLY row that engine may forget
	// when it quits or replaces: a row a person attached by address, solo or
	// not, never carries it, and attaching an address clears it.
	Local bool `json:"local,omitempty"`
	// Resume is a person's standing choice, made by an explicit
	// factory.local_start that this engine itself served: serve this machine's
	// factory again, quietly, when the app next opens. It is set only on the
	// Local row, holds no credential, and is cleared by detaching the row. A
	// row from a release that predates it carries none, so nothing resumes by
	// inference.
	Resume bool `json:"resume,omitempty"`
}

type appFactoryFile struct {
	Factories []appFactoryRow `json:"factories"`
}

type appFactorySession struct {
	token   string
	expires time.Time
}

// appFactorySignIn is one pending device sign-in. code is GitHub's device
// code as the factory wrapped it; it is never answered.
type appFactorySignIn struct {
	caller, factory, code string
	// bearer is the credential the factory bound this flow to when it was
	// started, and the one the poll must present to be the same caller.
	bearer        string
	interval      int
	expires, next time.Time
	busy          bool
}

// appFactoryLink is one caller's pending access-link flow: the cookie the
// factory issued to that flow, and nothing the caller can name.
type appFactoryLink struct {
	cookie  *http.Cookie
	expires time.Time
	busy    bool
}

type appFactoryCheckCap struct {
	post    bool
	expires time.Time
}

type appFactoryHub struct {
	dir    func() string
	client *http.Client
	now    func() time.Time

	// resume, when set, brings the local factory back before a read of its
	// connection is answered (appfactory_setup.go). It takes the local factory's
	// lock, so it is never called with mu held.
	resume func(context.Context)

	mu       sync.Mutex
	loaded   bool
	rows     []appFactoryRow
	sessions map[string]appFactorySession
	signIns  map[string]*appFactorySignIn
	links    map[string]*appFactoryLink
	// checkCaps remembers, per origin, whether a factory advertised
	// access_check_post. It holds no secret, expires with a flow's life and is
	// bounded by appwire.MaxFactoryFlows.
	checkCaps map[string]appFactoryCheckCap
	seen      map[string]time.Time
	// adminBusy marks a connection whose sign-out or revoke-all is in flight
	// (appfactory_admin.go), so one press is one request.
	adminBusy map[string]bool
	// pipelineBusy marks a repository on a connection whose pull request is
	// being opened (appfactory_pipeline_pr.go), so one press is one request.
	pipelineBusy map[string]bool
	// linkSt is the invitations the engine holds and the lists it owes
	// (appfactory_links.go), made by the first use.
	linkSt *linkState
	closed bool
}

// newAppFactoryHub is a registry whose HTTP client takes no ambient proxy,
// follows no redirect, and gives up after appFactoryTimeout.
func newAppFactoryHub() *appFactoryHub {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// The address is the factory the person named. A proxy named by the
	// environment would be handed its bearer token.
	transport.Proxy = nil
	return &appFactoryHub{
		dir: func() string { return filepath.Join(home.Dir(), "app") },
		client: &http.Client{
			Transport:     transport,
			Timeout:       appFactoryTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		now:      time.Now,
		sessions: map[string]appFactorySession{},
		signIns:  map[string]*appFactorySignIn{},
		links:    map[string]*appFactoryLink{},
		seen:     map[string]time.Time{},
	}
}

// factoryHub is the backend's one registry, made when the first factory call
// arrives so an app that never opens a factory pays nothing.
func (b *appBackend) factoryHub() *appFactoryHub {
	b.factoryOnce.Do(func() {
		b.factories = newAppFactoryHub()
		b.factories.resume = b.resumeLocal
	})
	return b.factories
}

// closeFactories forgets every secret and abandons every pending flow.
func (b *appBackend) closeFactories() {
	b.factoryOnce.Do(func() {})
	if b.factories != nil {
		b.factories.close()
	}
}

func (h *appFactoryHub) close() {
	h.forgetAll()
	h.client.CloseIdleConnections()
}

// forgetAll is close's critical section, scoped so a panic in it cannot leave
// the registry locked.
func (h *appFactoryHub) forgetAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	h.sessions = map[string]appFactorySession{}
	h.signIns = map[string]*appFactorySignIn{}
	h.links = map[string]*appFactoryLink{}
	h.dropAllHeldLocked()
}

// The small critical sections below each take the registry's lock for one
// step and release it from a defer, so a panic that something absorbs cannot
// become a deadlock.

// touch records that id answered just now.
func (h *appFactoryHub) touch(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seen[id] = h.now()
}

// tokenFor is the bearer held for id, or "".
func (h *appFactoryHub) tokenFor(id string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.tokenLocked(id)
}

// forgetSignIn ends one device sign-in.
func (h *appFactoryHub) forgetSignIn(flow string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.signIns, flow)
}

// unclaim frees a sign-in a poll claimed, if it is still the one in place.
func (h *appFactoryHub) unclaim(flow string, f *appFactorySignIn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if cur := h.signIns[flow]; cur == f {
		f.busy = false
	}
}

// releaseLink frees an access-link flow an exchange claimed.
func (h *appFactoryHub) releaseLink(key string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if l := h.links[key]; l != nil {
		l.busy = false
	}
}

// snapshot is the saved rows, loaded if they are not yet.
func (h *appFactoryHub) snapshot() ([]appFactoryRow, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	err := h.load()
	return append([]appFactoryRow(nil), h.rows...), err
}

// ── addresses and the saved list ───────────────────────────────────────────

// appFactoryID is the same id for the same address wherever it is computed.
func appFactoryID(origin string) string {
	sum := sha256.Sum256([]byte(origin))
	return "factory_" + hex.EncodeToString(sum[:8])
}

// idForLocked is the id the saved list already gives origin, or the one its
// address makes. ONE ROW PER CONNECTED FACTORY: the local row has a fixed id, so
// an address that is the live local service must resolve to it rather than to a
// second, hashed id. Callers hold h.mu and have loaded.
func (h *appFactoryHub) idForLocked(origin string) string {
	for _, row := range h.rows {
		if row.URL == origin {
			return row.ID
		}
	}
	return appFactoryID(origin)
}

// appLocalID names the one connection that is this machine's own factory. It
// is not derived from the address: the service listens on a port made at each
// launch, and a saved tab holds this id across launches. It cannot collide with
// appFactoryID, whose suffix is hexadecimal.
const appLocalID = "factory_local"

// appFactoryOrigin is raw as an origin, or why it is not one. Plain http is
// for this machine only: a token must not cross a network in the clear.
func appFactoryOrigin(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "https" && u.Scheme != "http") {
		return "", appwire.Errorf(appwire.CodeBadParams, "use a factory address like https://host, with no path or credentials")
	}
	if u.Scheme == "http" {
		ip := net.ParseIP(u.Hostname())
		if u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return "", appwire.Errorf(appwire.CodeBadParams, "a factory on another machine needs https")
		}
	}
	return u.Scheme + "://" + strings.ToLower(u.Host), nil
}

func (h *appFactoryHub) path() string { return filepath.Join(h.dir(), "factories.json") }

// load reads the saved list once. A row that is no longer an origin is
// dropped rather than trusted. Callers hold h.mu.
func (h *appFactoryHub) load() error {
	if h.loaded {
		return nil
	}
	data, err := os.ReadFile(h.path())
	if errors.Is(err, os.ErrNotExist) {
		h.loaded = true
		return nil
	}
	if err != nil {
		return appwire.Errorf(appwire.CodeEngine, "the saved factory connections could not be read")
	}
	var saved appFactoryFile
	if json.Unmarshal(data, &saved) != nil {
		return appwire.Errorf(appwire.CodeEngine, "the saved factory connections are not readable")
	}
	haveLocal := false
	for _, row := range saved.Factories {
		origin, err := appFactoryOrigin(row.URL)
		if err != nil {
			continue
		}
		id := appFactoryID(origin)
		if row.Local {
			// ONE LOCAL ROW, UNDER ITS OWN ID. A row an earlier release marked
			// Local carried an id hashed from a port; it is this row now.
			if haveLocal {
				continue
			}
			haveLocal, id = true, appLocalID
		}
		h.rows = append(h.rows, appFactoryRow{ID: id, URL: origin, Name: row.Name, Mode: row.Mode, Local: row.Local, Resume: row.Local && row.Resume})
	}
	// A conflicting saved local marker must never discard an address the
	// person attached. Keep the manual row; only our own local metadata may
	// be dropped. An ambiguous hosting choice needs another explicit start.
	kept := h.rows[:0:0]
	for _, row := range h.rows {
		dup := false
		for _, other := range h.rows {
			dup = dup || (row.Local && !other.Local && other.URL == row.URL)
		}
		if !dup {
			kept = append(kept, row)
		}
	}
	h.rows = kept
	h.loaded = true
	return nil
}

// save writes the list atomically and privately. Callers hold h.mu.
func (h *appFactoryHub) save() error {
	dir := h.dir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return appwire.Errorf(appwire.CodeEngine, "the factory connections could not be saved")
	}
	data, err := json.Marshal(appFactoryFile{Factories: h.rows})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".factories-*")
	if err != nil {
		return appwire.Errorf(appwire.CodeEngine, "the factory connections could not be saved")
	}
	defer os.Remove(tmp.Name())
	err = tmp.Chmod(0o600)
	if err == nil {
		_, err = tmp.Write(append(data, '\n'))
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), h.path())
	}
	if err != nil {
		return appwire.Errorf(appwire.CodeEngine, "the factory connections could not be saved")
	}
	return nil
}

// target is the connection an id names and the token to speak to it with.
func (h *appFactoryHub) target(id string) (appFactoryRow, string, error) {
	if id == appLocalID && h.resume != nil {
		h.resume(context.Background())
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return appFactoryRow{}, "", appwire.Errorf(appwire.CodeClosed, "the engine is stopping")
	}
	if err := h.load(); err != nil {
		return appFactoryRow{}, "", err
	}
	for _, row := range h.rows {
		if row.ID == id {
			return row, h.tokenLocked(id), nil
		}
	}
	return appFactoryRow{}, "", appwire.Errorf(appwire.CodeNotFound, "no such factory")
}

func (h *appFactoryHub) tokenLocked(id string) string {
	s, ok := h.sessions[id]
	if !ok || (!s.expires.IsZero() && !h.now().Before(s.expires)) {
		return ""
	}
	return s.token
}

// hold keeps a session in memory and tells the redactors it is a secret.
func (h *appFactoryHub) hold(id, token string, lives time.Duration) {
	if token == "" {
		return
	}
	redact.Register(token)
	trace.Secret(token)
	s := appFactorySession{token: token}
	if lives > 0 {
		s.expires = h.now().Add(lives)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.closed {
		h.sessions[id] = s
		// A different caller does not inherit an invitation held for the last.
		if h.linkSt != nil {
			if l := h.linkSt.held[id]; l != nil && l.digest != bearerDigest(token) {
				h.dropHeldLocked(id)
			}
		}
	}
}

// ── one request ────────────────────────────────────────────────────────────

type appFactoryReply struct {
	status int
	header http.Header
	body   []byte
}

// send makes one request to a factory with a bearer token if there is one.
// An answer above limit bytes is refused, not truncated.
func (h *appFactoryHub) send(ctx context.Context, origin, token, method, path string, in any, limit int64, extra func(*http.Request)) (appFactoryReply, error) {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return appFactoryReply{}, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, origin+path, body)
	if err != nil {
		return appFactoryReply{}, appwire.Errorf(appwire.CodeBadParams, "that is not a request a factory can take")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "codeaf-app")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if extra != nil {
		extra(req)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		// The Go error names the address; the person already knows it, and a
		// proxy or a library may add more, so only the fact is passed on.
		return appFactoryReply{}, appwire.Errorf(appwire.CodeUnreachable, "the factory is not answering")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return appFactoryReply{}, appwire.Errorf(appwire.CodeUnreachable, "the factory stopped answering")
	}
	if int64(len(raw)) > limit {
		return appFactoryReply{}, appwire.Errorf(appwire.CodeTooLarge, "the factory's answer is too large to read")
	}
	return appFactoryReply{status: resp.StatusCode, header: resp.Header, body: raw}, nil
}

// failure is the wire error for a factory's refusal. The factory's own
// sentence is passed on, trimmed; its status code is not.
func appFactoryFailure(r appFactoryReply) error {
	var body struct {
		Error  string `json:"error"`
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(bytes.TrimSpace(r.body), &body)
	words := strings.TrimSpace(redact.Secrets(body.Error))
	if len(words) > 300 {
		words = words[:300]
	}
	switch {
	case r.status >= 300 && r.status < 400:
		return appwire.Errorf(appwire.CodeUnreachable, "the factory redirected the request, and redirects are not followed")
	case r.status == http.StatusUnauthorized && body.Reason == api.ReasonGitHubExpired:
		return appwire.Errorf(appwire.CodeGitHubExpired, "GitHub access to this factory has expired; reconnect GitHub")
	case r.status == http.StatusUnauthorized:
		return appwire.Errorf(appwire.CodeSignedOut, "sign in to this factory")
	case r.status == http.StatusNotFound:
		if words == "" {
			words = "the factory has no such thing"
		}
		return appwire.Errorf(appwire.CodeNotFound, "%s", words)
	case r.status == http.StatusTooManyRequests:
		if words == "" {
			words = "the factory is busy; try again"
		}
		return appwire.Errorf(appwire.CodeBusy, "%s", words)
	case r.status == http.StatusUnprocessableEntity || r.status == http.StatusBadRequest:
		if words == "" {
			words = "the factory refused the request"
		}
		return appwire.Errorf(appwire.CodeBadParams, "%s", words)
	}
	if words == "" {
		words = "the factory refused"
	}
	return appwire.Errorf(appwire.CodeEngine, "%s", words)
}

// credentialed is target for a request that carries a person's authority. A
// connection with no held credential is answered only when the factory says,
// right now, that it is solo.
//
// A TEAM FACTORY NEVER HANDS ITS HOST'S AUTHORITY TO A CALLER WITH NO BEARER.
// A loopback team service resolves a request with no bearer as its own
// operator, so sending one would hand this app the host's role after sign-out,
// after a restart, or on a bare address. No credential held (never signed in,
// signed out, expired, or lost with the process) therefore means signed_out
// before anything private is sent, unless a fresh probe finds the factory solo.
// A saved mode is never trusted for that, because an address can change hands.
func (h *appFactoryHub) credentialed(ctx context.Context, id string) (appFactoryRow, string, error) {
	row, token, err := h.target(id)
	if err != nil || token != "" {
		return row, token, err
	}
	switch {
	case row.Local || row.ID == appLocalID:
		// A marker on disk is not authority over whoever now owns that port.
		// Quiet resume installs the live owned service key before reaching here.
		return row, "", appwire.Errorf(appwire.CodeUnreachable, "the local factory is not running for this app; start it again")
	case row.Mode != "" && row.Mode != api.FactorySolo:
		// Learned while a credential was held: refuse without sending a thing.
	case h.probeMode(ctx, row) == api.FactorySolo:
		return row, "", nil
	}
	return row, "", appwire.Errorf(appwire.CodeSignedOut, "sign in to this factory")
}

// learnModeLocked keeps the mode a factory reported to a held credential, so a
// restart that has lost that credential still knows to ask for one. Mode is the
// only fact kept; a reply with no credential behind it is never learned from.
func (h *appFactoryHub) learnModeLocked(id, mode string) {
	for i := range h.rows {
		if h.rows[i].ID == id && h.rows[i].Mode != mode && !h.closed {
			h.rows[i].Mode = mode
			_ = h.save()
		}
	}
}

// probeMode asks a factory with no bearer which mode it runs in and keeps
// nothing else of the answer: on a loopback team service that answer is the
// host's own identity, which no caller may be shown.
func (h *appFactoryHub) probeMode(ctx context.Context, row appFactoryRow) string {
	ctx, cancel := context.WithTimeout(ctx, appFactoryProbe)
	defer cancel()
	r, err := h.send(ctx, row.URL, "", http.MethodGet, "/v1/me", nil, appFactorySmall, nil)
	if err != nil || r.status < 200 || r.status >= 300 {
		return ""
	}
	var me struct {
		Mode string `json:"mode"`
	}
	if json.Unmarshal(r.body, &me) != nil {
		return ""
	}
	return me.Mode
}

// staleCredential is the refusal for a reply that came back after the
// credential it was asked with was removed or replaced. The reply described the
// old caller, so none of it is used for the new context.
func (h *appFactoryHub) staleCredential(id, asked string) error {
	now := h.tokenFor(id)
	if now == asked {
		return nil
	}
	if now == "" {
		return appwire.Errorf(appwire.CodeSignedOut, "sign in to this factory")
	}
	return appwire.Errorf(appwire.CodeConflict, "the sign-in changed while this was loading; try again")
}

// read is a GET decoded into out.
func (h *appFactoryHub) read(ctx context.Context, id, path string, limit int64, out any) error {
	row, token, err := h.credentialed(ctx, id)
	if err != nil {
		return err
	}
	r, err := h.send(ctx, row.URL, token, http.MethodGet, path, nil, limit, nil)
	if err != nil {
		return err
	}
	if err := h.staleCredential(id, token); err != nil {
		return err
	}
	if r.status < 200 || r.status >= 300 {
		return appFactoryFailure(r)
	}
	if json.Unmarshal(r.body, out) != nil {
		return appwire.Errorf(appwire.CodeEngine, "the factory sent an answer this app cannot read")
	}
	h.touch(id)
	return nil
}

// ── connections ────────────────────────────────────────────────────────────

func (h *appFactoryHub) view(ctx context.Context, row appFactoryRow) appwire.FactoryConnection {
	out := appwire.FactoryConnection{ID: row.ID, URL: row.URL, Name: row.Name, Mode: row.Mode, State: appwire.FactoryUnreachable}
	if out.Name == "" {
		out.Name = "Factory"
	}
	ctx, cancel := context.WithTimeout(ctx, appFactoryProbe)
	defer cancel()
	token := h.tokenFor(row.ID)
	if token == "" {
		if row.Local || row.ID == appLocalID {
			return out
		}
		if row.Mode != "" && row.Mode != api.FactorySolo {
			out.State = appwire.FactorySignedOut
			return out
		}
	}
	r, err := h.send(ctx, row.URL, token, http.MethodGet, "/v1/me", nil, appFactorySmall, nil)
	h.mu.Lock()
	defer h.mu.Unlock()
	if err != nil {
		if t, ok := h.seen[row.ID]; ok {
			out.LastSeen = &t
		}
		return out
	}
	// A reply for a credential that has since changed describes the old caller:
	// nothing of it is promoted into the new context.
	if now := h.tokenLocked(row.ID); now != token {
		if now == "" {
			out.State = appwire.FactorySignedOut
		}
		return out
	}
	switch {
	case r.status == http.StatusUnauthorized:
		out.State = appwire.FactorySignedOut
		if appFactoryFailure(r).(*appwire.Error).Code == appwire.CodeGitHubExpired {
			out.State = appwire.FactoryGitHubExpired
		}
	case r.status >= 200 && r.status < 300:
		var me api.Me
		if json.Unmarshal(r.body, &me) != nil {
			return out
		}
		if me.Factory.Name != "" {
			out.Name = me.Factory.Name
		}
		if me.Mode != "" {
			out.Mode = me.Mode
		}
		// NO CREDENTIAL, NO CLAIM: without a held bearer only a factory that
		// says it is solo is OK; a team answer here is its host, not a caller.
		if token == "" && me.Mode != api.FactorySolo {
			out.State = appwire.FactorySignedOut
			return out
		}
		out.State, out.Operator = appwire.FactoryOK, me.Operator
		if token != "" && me.Mode != "" {
			h.learnModeLocked(row.ID, me.Mode)
		}
		now := h.now()
		h.seen[row.ID] = now
		out.LastSeen = &now
	}
	if t, ok := h.seen[row.ID]; ok && out.LastSeen == nil {
		out.LastSeen = &t
	}
	return out
}

func (b *appBackend) FactoryCapabilities(context.Context) (appwire.FactoryCapabilities, error) {
	return appwire.FactoryCapabilities{
		Contract: api.Contract, Remote: true, Registry: true, AccessLinks: true, SignIn: true,
		Local: true, Runner: true, Writes: false, Admin: true,
		Links: true, CopyLink: b.copier().Available(), ReposManage: true, PipelinePR: true,
		MaxConnections: appwire.MaxFactoryConnections, MaxFlows: appwire.MaxFactoryFlows,
	}, nil
}

func (b *appBackend) FactoryList(ctx context.Context) (appwire.FactoryList, error) {
	h := b.factoryHub()
	// THE LOCAL FACTORY IS BACK BEFORE IT IS LISTED, so its row carries this
	// launch's address and the list probes the service that is actually there.
	b.resumeLocal(ctx)
	rows, err := h.snapshot()
	if err != nil {
		return appwire.FactoryList{}, err
	}
	out := make([]appwire.FactoryConnection, len(rows))
	var wg sync.WaitGroup
	slots := make(chan struct{}, 8)
	for i := range rows {
		wg.Add(1)
		slots <- struct{}{}
		guard.Go("app/factory view", func() {
			defer wg.Done()
			defer func() { <-slots }()
			out[i] = h.view(ctx, rows[i])
		})
	}
	wg.Wait()
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return appwire.FactoryList{Factories: out}, nil
}

func (b *appBackend) FactoryAttach(ctx context.Context, raw, token string) (appwire.FactoryConnection, error) {
	h := b.factoryHub()
	origin, err := appFactoryOrigin(raw)
	if err != nil {
		return appwire.FactoryConnection{}, err
	}
	id, known, full, err := h.admitAttach(origin)
	if err != nil {
		return appwire.FactoryConnection{}, err
	}
	if full {
		return appwire.FactoryConnection{}, appwire.Errorf(appwire.CodeBusy, "this app is attached to as many factories as it can be (%d)", appwire.MaxFactoryConnections)
	}
	// THE ADDRESS OF THE SERVICE THIS ENGINE IS SERVING IS ALREADY PROVEN, and
	// its own bearer is held: there is nothing to prove, save or swap, and the
	// local service does not answer /v1/about without that bearer. The one row
	// is answered as it stands.
	if known && h.holdsLocal(id) {
		return h.view(ctx, h.savedRow(appFactoryRow{ID: id, URL: origin})), nil
	}
	// ABOUT ANSWERS WITHOUT A SIGN-IN, so a factory is proven to be one before
	// anything is saved or any token is sent to it.
	r, err := h.send(ctx, origin, "", http.MethodGet, "/v1/about", nil, appFactorySmall, nil)
	if err != nil {
		return appwire.FactoryConnection{}, err
	}
	var about struct {
		Factory  api.Factory `json:"factory"`
		Name     string      `json:"name"`
		Contract int         `json:"contract"`
	}
	if r.status < 200 || r.status >= 300 || json.Unmarshal(r.body, &about) != nil || about.Contract < 1 {
		return appwire.FactoryConnection{}, appwire.Errorf(appwire.CodeBadParams, "that address is not a codeaf factory")
	}
	name := strings.TrimSpace(about.Name)
	if name == "" {
		name = about.Factory.Name
	}
	if token != "" {
		h.hold(id, token, 0)
	}
	row := appFactoryRow{ID: id, URL: origin, Name: name}
	if err := h.saveAttached(row, known); err != nil {
		return appwire.FactoryConnection{}, err
	}
	return h.view(ctx, h.savedRow(row)), nil
}

// savedRow is the saved row under row's id, or row if it is no longer saved.
func (h *appFactoryHub) savedRow(row appFactoryRow) appFactoryRow {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, saved := range h.rows {
		if saved.ID == row.ID {
			return saved
		}
	}
	return row
}

// holdsLocal says whether id is the local row with a bearer held for it.
func (h *appFactoryHub) holdsLocal(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, row := range h.rows {
		if row.ID == id && row.Local {
			return h.tokenLocked(id) != ""
		}
	}
	return false
}

// admitAttach is the id origin is saved under (an existing row's, if one has
// this address), whether it is already saved and whether the list is full.
func (h *appFactoryHub) admitAttach(origin string) (id string, known, full bool, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	err = h.load()
	id = h.idForLocked(origin)
	for _, row := range h.rows {
		known = known || row.ID == id
	}
	return id, known, len(h.rows) >= appwire.MaxFactoryConnections && !known, err
}

// saveAttached saves row, or names an unnamed row already saved under its id.
func (h *appFactoryHub) saveAttached(row appFactoryRow, known bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if known {
		for i := range h.rows {
			if h.rows[i].ID != row.ID {
				continue
			}
			if h.rows[i].Name == "" {
				h.rows[i].Name = row.Name
			}
			// The row keeps whatever it is: naming an address already saved
			// neither revokes the local factory's Resume choice nor makes
			// a second connection.
		}
	} else {
		h.rows = append(h.rows, row)
	}
	return h.save()
}

func (b *appBackend) FactoryDetach(_ context.Context, id string) error {
	h := b.factoryHub()
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.load(); err != nil {
		return err
	}
	kept := h.rows[:0:0]
	for _, row := range h.rows {
		if row.ID != id {
			kept = append(kept, row)
		}
	}
	if len(kept) == len(h.rows) {
		return appwire.Errorf(appwire.CodeNotFound, "no such factory")
	}
	h.rows = kept
	delete(h.sessions, id)
	delete(h.seen, id)
	h.dropHeldLocked(id)
	for key, flow := range h.signIns {
		if flow.factory == id {
			delete(h.signIns, key)
		}
	}
	return h.save()
}

// ── reads ──────────────────────────────────────────────────────────────────

func (b *appBackend) FactoryMe(ctx context.Context, id string) (appwire.FactoryMe, error) {
	var me api.Me
	if err := b.factoryHub().read(ctx, id, "/v1/me", appFactorySmall, &me); err != nil {
		return appwire.FactoryMe{}, err
	}
	// The session's id is the factory's handle on a credential, so it never
	// leaves; NewFactoryMe keeps only whether one existed.
	return appwire.NewFactoryMe(me), nil
}

func (b *appBackend) FactoryQueue(ctx context.Context, id string) (api.Queue, error) {
	var q api.Queue
	err := b.factoryHub().read(ctx, id, "/v1/queue", appFactoryBody, &q)
	return q, err
}

func (b *appBackend) FactoryJob(ctx context.Context, id string, job int) (api.Job, error) {
	var out api.Job
	err := b.factoryHub().read(ctx, id, "/v1/jobs/"+strconv.Itoa(job), appFactoryBody, &out)
	return out, err
}

func (b *appBackend) FactoryRepos(ctx context.Context, id string) (appwire.FactoryRepos, error) {
	var body struct {
		Factory   api.Factory `json:"factory"`
		Watched   []string    `json:"watched"`
		Available []struct {
			Full    string    `json:"full"`
			Name    string    `json:"name"`
			Owner   string    `json:"owner"`
			Private bool      `json:"private"`
			Pushed  time.Time `json:"pushed"`
			Open    int       `json:"open"`
		} `json:"available"`
	}
	if err := b.factoryHub().read(ctx, id, "/v1/repos", appFactoryBody, &body); err != nil {
		return appwire.FactoryRepos{}, err
	}
	out := appwire.FactoryRepos{Factory: body.Factory, Watched: body.Watched, Available: make([]appwire.FactoryRepo, len(body.Available))}
	if out.Watched == nil {
		out.Watched = []string{}
	}
	for i, r := range body.Available {
		out.Available[i] = appwire.FactoryRepo{Full: r.Full, Name: r.Name, Owner: r.Owner, Private: r.Private, Pushed: r.Pushed, Open: r.Open}
	}
	return out, nil
}

func (b *appBackend) FactoryPipeline(ctx context.Context, id, repo string) (appwire.FactoryPipeline, error) {
	owner, name, _ := strings.Cut(repo, "/")
	var body struct {
		Factory api.Factory `json:"factory"`
		api.Pipeline
		Problems []string `json:"problems"`
	}
	path := "/v1/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name) + "/pipeline"
	if err := b.factoryHub().read(ctx, id, path, appFactoryBody, &body); err != nil {
		return appwire.FactoryPipeline{}, err
	}
	return appwire.FactoryPipeline{Factory: body.Factory, Pipeline: body.Pipeline, Problems: body.Problems}, nil
}

func (b *appBackend) FactoryRunners(ctx context.Context, id string) (api.Runners, error) {
	var out api.Runners
	err := b.factoryHub().read(ctx, id, "/v1/runners", appFactoryBody, &out)
	return out, err
}

func (b *appBackend) FactoryPeople(ctx context.Context, id string) (api.People, error) {
	var out api.People
	err := b.factoryHub().read(ctx, id, "/v1/people", appFactoryBody, &out)
	return out, err
}

func (b *appBackend) FactoryActivity(ctx context.Context, id string, since time.Time, limit int) (appwire.FactoryActivity, error) {
	path := "/v1/activity"
	if !since.IsZero() {
		path += "?since=" + url.QueryEscape(since.UTC().Format(time.RFC3339Nano))
	}
	var body struct {
		Factory api.Factory                      `json:"factory"`
		Events  []record.Event                   `json:"events"`
		People  map[string]factory.PersonSummary `json:"people"`
	}
	if err := b.factoryHub().read(ctx, id, path, appFactoryBody, &body); err != nil {
		return appwire.FactoryActivity{}, err
	}
	out := appwire.FactoryActivity{Factory: body.Factory, Events: body.Events}
	if len(out.Events) > limit {
		out.Events, out.Truncated = out.Events[len(out.Events)-limit:], true
	}
	out.People = activityPeopleFor(out.Events, body.People)
	return out, nil
}

// activityPeopleFor carries only the verified profiles mentioned by the returned event window.
// A response trimmed to limit must not retain profiles found only in its discarded events.
func activityPeopleFor(events []record.Event, profiles map[string]factory.PersonSummary) map[string]factory.PersonSummary {
	out := map[string]factory.PersonSummary{}
	for _, event := range events {
		handle := factory.NormalizeHandle(event.Handle)
		if handle == "" && event.Actor.Kind == "person" {
			handle = factory.NormalizeHandle(event.Actor.Name)
		}
		if profile, ok := profiles[handle]; ok {
			out[handle] = profile
		}
	}
	return out
}

// FactorySetup is the factory's own first-run progress ("local" is the id the
// server keeps its own setup under); this app's folder choices are not part of
// it, which the handoff lists as a gap.
func (b *appBackend) FactorySetup(ctx context.Context, id string) (api.FactorySetup, error) {
	var out api.FactorySetup
	if err := b.factoryHub().read(ctx, id, "/v1/factory-setup/local", appFactoryBody, &out); err != nil {
		return api.FactorySetup{}, err
	}
	out.ID = id
	row, _, err := b.factoryHub().target(id)
	if err == nil {
		out.Server = row.URL
	}
	return out, nil
}

// ── access links ───────────────────────────────────────────────────────────

// appFactoryJoin is a link as an origin and the key its flow is kept under.
func appFactoryJoin(caller, link string) (origin, key string, err error) {
	u, e := url.Parse(link)
	if e != nil || u.Scheme != "codeaf" || u.Host != "factory" || u.Path != "/join" {
		return "", "", appwire.Errorf(appwire.CodeBadParams, "use a factory access link")
	}
	origin, err = appFactoryOrigin(u.Query().Get("url"))
	if err != nil {
		return "", "", err
	}
	return origin, caller + "\x00" + link, nil
}

// prune drops flows that have expired. Callers hold h.mu.
func (h *appFactoryHub) prune() {
	now := h.now()
	for key, l := range h.links {
		if !l.busy && !now.Before(l.expires) {
			delete(h.links, key)
		}
	}
	for key, f := range h.signIns {
		if !f.busy && !now.Before(f.expires) {
			delete(h.signIns, key)
		}
	}
	for origin, c := range h.checkCaps {
		if !now.Before(c.expires) {
			delete(h.checkCaps, origin)
		}
	}
}

// checkByPost says how to check a link at origin without a guess. A factory
// that advertises access_check_post is asked with POST {link, verb:"check"}, so
// the secret never rides in a URL. ONLY a valid /v1/about that does not
// advertise it is legacy GET: a probe that fails, is refused or is malformed
// is an error, never a reason to put the link in a query string. The probe
// carries no link.
func (h *appFactoryHub) checkByPost(ctx context.Context, origin, token string) (bool, error) {
	if post, ok := h.cachedCheckCap(origin); ok {
		return post, nil
	}
	r, err := h.send(ctx, origin, token, http.MethodGet, "/v1/about", nil, appFactorySmall, nil)
	if err != nil {
		return false, err
	}
	var about struct {
		Contract int             `json:"contract"`
		Post     json.RawMessage `json:"access_check_post"`
	}
	if r.status < 200 || r.status >= 300 || json.Unmarshal(r.body, &about) != nil || about.Contract < 1 {
		return false, appwire.Errorf(appwire.CodeEngine, "the factory did not say how to check a link safely")
	}
	post := false
	if len(about.Post) > 0 && string(about.Post) != "null" {
		if json.Unmarshal(about.Post, &post) != nil {
			return false, appwire.Errorf(appwire.CodeEngine, "the factory did not say how to check a link safely")
		}
	}
	h.rememberCheckCap(origin, post)
	return post, nil
}

func (h *appFactoryHub) cachedCheckCap(origin string) (post, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	c, ok := h.checkCaps[origin]
	return c.post, ok && h.now().Before(c.expires)
}

// rememberCheckCap keeps the answer for one flow life; a full table keeps what it has.
func (h *appFactoryHub) rememberCheckCap(origin string, post bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.checkCaps == nil {
		h.checkCaps = map[string]appFactoryCheckCap{}
	}
	if _, ok := h.checkCaps[origin]; ok || len(h.checkCaps) < appwire.MaxFactoryFlows {
		h.checkCaps[origin] = appFactoryCheckCap{post: post, expires: h.now().Add(appFactoryFlowLife)}
	}
}

func (b *appBackend) FactoryAccessCheck(ctx context.Context, caller, link string) (appwire.FactoryAccess, error) {
	return b.factoryHub().access(ctx, caller, link, "")
}

func (b *appBackend) FactoryAccessSignIn(ctx context.Context, caller, link string) (appwire.FactoryAccess, error) {
	return b.factoryHub().access(ctx, caller, link, "sign_in")
}

func (b *appBackend) FactoryAccessCancel(ctx context.Context, caller, link string) (appwire.FactoryAccess, error) {
	return b.factoryHub().access(ctx, caller, link, "cancel")
}

// access is the factory's /v1/factory-access for one caller's link. verb ""
// asks (and polls a started sign-in), sign_in starts one, cancel ends it.
func (h *appFactoryHub) access(ctx context.Context, caller, link, verb string) (appwire.FactoryAccess, error) {
	origin, key, err := appFactoryJoin(caller, link)
	if err != nil {
		return appwire.FactoryAccess{}, err
	}
	id, cookie, token, err := h.admitAccess(origin, key, verb)
	if err != nil {
		return appwire.FactoryAccess{}, err
	}
	release := func() { h.releaseLink(key) }

	var in any
	method, path := http.MethodGet, "/v1/factory-access?link="+url.QueryEscape(link)
	wire := verb
	var r appFactoryReply
	if verb == "" {
		// A check is a read; it must not leave a secret in a URL on a factory
		// that can take it in a body. A failed probe ends the check here: it
		// never degrades to the GET.
		post, perr := h.checkByPost(ctx, origin, token)
		err = perr
		if perr == nil && post {
			wire = "check"
		}
	}
	if wire != "" {
		method, path, in = http.MethodPost, "/v1/factory-access", api.FactoryAccessRequest{Link: link, Verb: wire}
	}
	if err == nil {
		r, err = h.send(ctx, origin, token, method, path, in, appFactorySmall, func(req *http.Request) {
			if cookie != nil {
				req.AddCookie(cookie)
			}
		})
	}
	var out api.FactoryAccess
	switch {
	case err != nil:
		release()
		var w *appwire.Error
		if errors.As(err, &w) && w.Code == appwire.CodeUnreachable {
			out = api.FactoryAccess{State: "unreachable", Message: "The factory is not answering."}
			err = nil
		} else {
			return appwire.FactoryAccess{}, err
		}
	case r.status != http.StatusOK:
		release()
		return appwire.FactoryAccess{}, appFactoryFailure(r)
	case json.Unmarshal(r.body, &out) != nil:
		release()
		return appwire.FactoryAccess{}, appwire.Errorf(appwire.CodeEngine, "the factory sent an answer this app cannot read")
	}

	joined := (out.State == "joined" || out.State == "already_joined")
	granted := out.AccessToken
	h.mu.Lock()
	defer h.mu.Unlock()
	if l := h.links[key]; l != nil {
		l.busy = false
	}
	if h.closed {
		return appwire.FactoryAccess{}, appwire.Errorf(appwire.CodeClosed, "the engine is stopping")
	}
	for _, c := range readSetCookies(r.header) {
		if c.Name == appFactoryJoinCookie && verb == "sign_in" {
			h.links[key] = &appFactoryLink{cookie: &http.Cookie{Name: c.Name, Value: c.Value}, expires: h.now().Add(appFactoryFlowLife)}
		}
	}
	if l := h.links[key]; l != nil {
		l.expires = h.now().Add(appFactoryFlowLife)
	}
	if verb == "cancel" {
		delete(h.links, key)
	}
	if joined && granted != "" {
		redact.Register(granted)
		trace.Secret(granted)
		// An explicit completed sign-in changes the caller, including when a
		// local authority was previously reached through its operator bearer.
		h.sessions[id] = appFactorySession{token: granted}
	}
	if joined {
		delete(h.links, key)
		if saveErr := h.attachLocked(id, origin, out.Factory.Name); saveErr != nil {
			return appwire.FactoryAccess{}, saveErr
		}
	}
	// THE ANSWER HAS NO LINK AND NO GRANT: the link carries the invitation's
	// secret and the caller already holds it, and the grant stays in h.sessions
	// (appwire.FactoryAccess has no field for either).
	out.Factory.ID, out.Factory.Host = id, origin
	if out.Factory.Repos == nil {
		out.Factory.Repos = []string{}
	}
	return appwire.FactoryAccess{State: out.State, Factory: out.Factory, Detail: out.Detail, Message: out.Message,
		Help: out.Help, SignInURL: out.SignInURL, UserCode: out.UserCode}, nil
}

// admitAccess is the admission of one access-link exchange: it refuses a
// closing registry, a full one and a link already being checked, and otherwise
// claims the link and answers the cookie the factory gave it and the bearer
// held for id.
func (h *appFactoryHub) admitAccess(origin, key, verb string) (id string, cookie *http.Cookie, token string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return "", nil, "", appwire.Errorf(appwire.CodeClosed, "the engine is stopping")
	}
	if err := h.load(); err != nil {
		return "", nil, "", err
	}
	id = h.idForLocked(origin)
	h.prune()
	flow := h.links[key]
	if verb == "sign_in" {
		known := false
		for _, row := range h.rows {
			known = known || row.ID == id
		}
		switch {
		case flow == nil && len(h.links)+len(h.signIns) >= appwire.MaxFactoryFlows:
			return "", nil, "", appwire.Errorf(appwire.CodeBusy, "too many pending factory links")
		case !known && len(h.rows) >= appwire.MaxFactoryConnections:
			return "", nil, "", appwire.Errorf(appwire.CodeBusy, "this app is attached to as many factories as it can be (%d)", appwire.MaxFactoryConnections)
		}
	}
	if flow != nil && flow.busy {
		return "", nil, "", appwire.Errorf(appwire.CodeBusy, "this link is already being checked")
	}
	if flow != nil {
		flow.busy, cookie = true, flow.cookie
	}
	return id, cookie, h.tokenLocked(id), nil
}

// attachLocked saves a connection if it is not saved already.
func (h *appFactoryHub) attachLocked(id, origin, name string) error {
	for _, row := range h.rows {
		if row.ID == id {
			return nil
		}
	}
	h.rows = append(h.rows, appFactoryRow{ID: id, URL: origin, Name: strings.TrimSpace(name), Mode: api.FactoryTeam})
	return h.save()
}

func readSetCookies(header http.Header) []*http.Cookie {
	return (&http.Response{Header: header}).Cookies()
}

// ── GitHub sign-in on an attached factory ──────────────────────────────────

func appFactoryHandle() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", appwire.Errorf(appwire.CodeEngine, "the engine could not make a sign-in handle")
	}
	return hex.EncodeToString(raw[:]), nil
}

func errAppFactoryNoFlow() error {
	return appwire.Errorf(appwire.CodeNotFound, "no such sign-in for this caller")
}

func (b *appBackend) FactorySignInStart(ctx context.Context, caller, id string) (appwire.FactorySignIn, error) {
	h := b.factoryHub()
	row, bearer, err := h.target(id)
	if err != nil {
		return appwire.FactorySignIn{}, err
	}
	mine, over := h.crowd(caller)
	if over || mine >= appFactoryPerCaller {
		return appwire.FactorySignIn{}, appwire.Errorf(appwire.CodeBusy, "too many sign-ins are waiting; finish or cancel one")
	}
	// THE FACTORY BINDS A DEVICE FLOW TO THE CREDENTIAL THAT STARTED IT, so the
	// credential this connection holds (an access token, or the session being
	// renewed) starts it and polls it; a factory that asks for none gets none.
	r, err := h.send(ctx, row.URL, bearer, http.MethodPost, "/v1/auth/device", struct{}{}, appFactorySmall, nil)
	if err != nil {
		return appwire.FactorySignIn{}, err
	}
	if r.status != http.StatusOK {
		return appwire.FactorySignIn{}, appFactoryFailure(r)
	}
	var dev api.Device
	if json.Unmarshal(r.body, &dev) != nil || dev.DeviceCode == "" || dev.UserCode == "" || dev.Interval < 1 || dev.ExpiresIn < 1 {
		return appwire.FactorySignIn{}, appwire.Errorf(appwire.CodeEngine, "the factory sent an invalid sign-in code")
	}
	if u, e := url.Parse(dev.VerificationURI); e != nil || u.Scheme != "https" || u.Host == "" {
		return appwire.FactorySignIn{}, appwire.Errorf(appwire.CodeEngine, "the factory sent an invalid sign-in address")
	}
	flow, err := appFactoryHandle()
	if err != nil {
		return appwire.FactorySignIn{}, err
	}
	redact.Register(dev.DeviceCode)
	trace.Secret(dev.DeviceCode)
	now := h.now()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return appwire.FactorySignIn{}, appwire.Errorf(appwire.CodeClosed, "the engine is stopping")
	}
	h.signIns[flow] = &appFactorySignIn{
		caller: caller, factory: id, code: dev.DeviceCode, bearer: bearer, interval: dev.Interval,
		expires: now.Add(time.Duration(dev.ExpiresIn) * time.Second), next: now.Add(time.Duration(dev.Interval) * time.Second),
	}
	return appwire.FactorySignIn{Flow: flow, Factory: id, UserCode: dev.UserCode, VerificationURI: dev.VerificationURI, Interval: dev.Interval, ExpiresIn: dev.ExpiresIn}, nil
}

// crowd is how many sign-ins caller has pending and whether the registry holds
// as many flows as it may.
func (h *appFactoryHub) crowd(caller string) (mine int, over bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.prune()
	for _, f := range h.signIns {
		if f.caller == caller {
			mine++
		}
	}
	return mine, len(h.links)+len(h.signIns) >= appwire.MaxFactoryFlows
}

// own is the flow a caller started, marked busy for one exchange. Another
// caller's flow and a flow that does not exist answer the same.
func (h *appFactoryHub) own(caller, flow string, claim bool) (*appFactorySignIn, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	f := h.signIns[flow]
	if f == nil || f.caller != caller {
		return nil, errAppFactoryNoFlow()
	}
	if f.busy {
		return nil, appwire.Errorf(appwire.CodeBusy, "this sign-in is being checked")
	}
	if claim {
		f.busy = true
	}
	return f, nil
}

// pollTurn decides whether a poll may ask the factory yet. It answers early
// when the flow has expired or it is too soon to ask GitHub again, and
// otherwise books the next ask and answers what to send.
func (h *appFactoryHub) pollTurn(flow string, f *appFactorySignIn) (early *appwire.FactorySignInState, code, factoryID, bearer string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	switch {
	case !now.Before(f.expires):
		delete(h.signIns, flow)
		return &appwire.FactorySignInState{State: appwire.SignInExpired}, "", "", ""
	case now.Before(f.next):
		f.busy = false
		// TOO SOON TO ASK GITHUB AGAIN: answer without a request.
		return &appwire.FactorySignInState{State: appwire.SignInPending, Interval: f.interval}, "", "", ""
	}
	f.next = now.Add(time.Duration(f.interval) * time.Second)
	return nil, f.code, f.factory, f.bearer
}

// slowDown records the interval the factory asked for and frees the flow.
func (h *appFactoryHub) slowDown(f *appFactorySignIn, asked int) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	if asked > f.interval {
		f.interval = asked
		f.next = h.now().Add(time.Duration(f.interval) * time.Second)
	}
	f.busy = false
	return f.interval
}

func (b *appBackend) FactorySignInPoll(ctx context.Context, caller, flow string) (appwire.FactorySignInState, error) {
	h := b.factoryHub()
	f, err := h.own(caller, flow, true)
	if err != nil {
		return appwire.FactorySignInState{}, err
	}
	early, code, factoryID, bearer := h.pollTurn(flow, f)
	if early != nil {
		return *early, nil
	}
	unclaim := func() { h.unclaim(flow, f) }

	row, _, err := h.target(factoryID)
	if err != nil {
		h.forgetSignIn(flow)
		return appwire.FactorySignInState{}, err
	}
	r, err := h.send(ctx, row.URL, bearer, http.MethodPost, "/v1/auth/device/poll", api.DevicePoll{DeviceCode: code}, appFactorySmall, nil)
	if err != nil {
		unclaim()
		return appwire.FactorySignInState{}, err
	}
	var body struct {
		Error    string `json:"error"`
		Interval int    `json:"interval"`
	}
	_ = json.Unmarshal(r.body, &body)
	switch r.status {
	case http.StatusAccepted:
		interval := h.slowDown(f, body.Interval)
		return appwire.FactorySignInState{State: appwire.SignInPending, Interval: interval}, nil
	case http.StatusOK:
		var signed api.SignedIn
		if json.Unmarshal(r.body, &signed) != nil || signed.AccessToken == "" {
			h.forgetSignIn(flow)
			return appwire.FactorySignInState{}, appwire.Errorf(appwire.CodeEngine, "the factory sent an answer this app cannot read")
		}
		h.hold(factoryID, signed.AccessToken, time.Duration(signed.ExpiresIn)*time.Second)
		h.forgetSignIn(flow)
		me := signed.Me
		me.SessionID = ""
		return appwire.FactorySignInState{State: appwire.SignInSignedIn, Me: &me}, nil
	case http.StatusGone, http.StatusForbidden:
		// expired_token, access_denied, a handle off the team, a link for
		// someone else: each ends the flow, and the factory's sentence says why.
		h.forgetSignIn(flow)
		if r.status == http.StatusGone {
			return appwire.FactorySignInState{State: appwire.SignInExpired}, nil
		}
		msg := body.Error
		if msg == "access_denied" {
			msg = ""
		}
		return appwire.FactorySignInState{State: appwire.SignInDenied, Message: msg}, nil
	}
	h.forgetSignIn(flow)
	return appwire.FactorySignInState{}, appFactoryFailure(r)
}

func (b *appBackend) FactorySignInCancel(ctx context.Context, caller, flow string) error {
	h := b.factoryHub()
	f, err := h.own(caller, flow, false)
	if err != nil {
		return err
	}
	h.forgetSignIn(flow)
	// Telling the factory lets it free the code; if it cannot be told, the
	// code expires on its own.
	if row, _, e := h.target(f.factory); e == nil {
		ctx, cancel := context.WithTimeout(ctx, appFactoryProbe)
		defer cancel()
		_, _ = h.send(ctx, row.URL, f.bearer, http.MethodDelete, "/v1/auth/device", api.DevicePoll{DeviceCode: f.code}, appFactorySmall, nil)
	}
	return nil
}
