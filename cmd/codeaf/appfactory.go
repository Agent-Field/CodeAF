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
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
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

type appFactoryHub struct {
	dir    func() string
	client *http.Client
	now    func() time.Time

	mu       sync.Mutex
	loaded   bool
	rows     []appFactoryRow
	sessions map[string]appFactorySession
	signIns  map[string]*appFactorySignIn
	links    map[string]*appFactoryLink
	seen     map[string]time.Time
	closed   bool
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
	b.factoryOnce.Do(func() { b.factories = newAppFactoryHub() })
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
	h.mu.Lock()
	h.closed = true
	h.sessions = map[string]appFactorySession{}
	h.signIns = map[string]*appFactorySignIn{}
	h.links = map[string]*appFactoryLink{}
	h.mu.Unlock()
	h.client.CloseIdleConnections()
}

// ── addresses and the saved list ───────────────────────────────────────────

// appFactoryID is the same id for the same address wherever it is computed.
func appFactoryID(origin string) string {
	sum := sha256.Sum256([]byte(origin))
	return "factory_" + hex.EncodeToString(sum[:8])
}

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
	for _, row := range saved.Factories {
		origin, err := appFactoryOrigin(row.URL)
		if err != nil {
			continue
		}
		h.rows = append(h.rows, appFactoryRow{ID: appFactoryID(origin), URL: origin, Name: row.Name, Mode: row.Mode})
	}
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
	if !h.closed {
		h.sessions[id] = s
	}
	h.mu.Unlock()
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

// read is a GET decoded into out.
func (h *appFactoryHub) read(ctx context.Context, id, path string, limit int64, out any) error {
	row, token, err := h.target(id)
	if err != nil {
		return err
	}
	r, err := h.send(ctx, row.URL, token, http.MethodGet, path, nil, limit, nil)
	if err != nil {
		return err
	}
	if r.status < 200 || r.status >= 300 {
		return appFactoryFailure(r)
	}
	if json.Unmarshal(r.body, out) != nil {
		return appwire.Errorf(appwire.CodeEngine, "the factory sent an answer this app cannot read")
	}
	h.mu.Lock()
	h.seen[id] = h.now()
	h.mu.Unlock()
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
	h.mu.Lock()
	token := h.tokenLocked(row.ID)
	h.mu.Unlock()
	r, err := h.send(ctx, row.URL, token, http.MethodGet, "/v1/me", nil, appFactorySmall, nil)
	h.mu.Lock()
	defer h.mu.Unlock()
	if err != nil {
		if t, ok := h.seen[row.ID]; ok {
			out.LastSeen = &t
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
		out.State, out.Operator = appwire.FactoryOK, me.Operator
		if me.Factory.Name != "" {
			out.Name = me.Factory.Name
		}
		if me.Mode != "" {
			out.Mode = me.Mode
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
		Local: false, Runner: false, Writes: false,
		MaxConnections: appwire.MaxFactoryConnections, MaxFlows: appwire.MaxFactoryFlows,
	}, nil
}

func (b *appBackend) FactoryList(ctx context.Context) (appwire.FactoryList, error) {
	h := b.factoryHub()
	h.mu.Lock()
	err := h.load()
	rows := append([]appFactoryRow(nil), h.rows...)
	h.mu.Unlock()
	if err != nil {
		return appwire.FactoryList{}, err
	}
	out := make([]appwire.FactoryConnection, len(rows))
	var wg sync.WaitGroup
	slots := make(chan struct{}, 8)
	for i := range rows {
		wg.Add(1)
		slots <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-slots }()
			out[i] = h.view(ctx, rows[i])
		}(i)
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
	id := appFactoryID(origin)
	h.mu.Lock()
	err = h.load()
	known := false
	for _, row := range h.rows {
		known = known || row.ID == id
	}
	full := len(h.rows) >= appwire.MaxFactoryConnections && !known
	h.mu.Unlock()
	if err != nil {
		return appwire.FactoryConnection{}, err
	}
	if full {
		return appwire.FactoryConnection{}, appwire.Errorf(appwire.CodeBusy, "this app is attached to as many factories as it can be (%d)", appwire.MaxFactoryConnections)
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
	h.mu.Lock()
	if known {
		for i := range h.rows {
			if h.rows[i].ID == id && h.rows[i].Name == "" {
				h.rows[i].Name = name
			}
		}
	} else {
		h.rows = append(h.rows, row)
	}
	err = h.save()
	h.mu.Unlock()
	if err != nil {
		return appwire.FactoryConnection{}, err
	}
	return h.view(ctx, row), nil
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
	for key, flow := range h.signIns {
		if flow.factory == id {
			delete(h.signIns, key)
		}
	}
	return h.save()
}

// ── reads ──────────────────────────────────────────────────────────────────

func (b *appBackend) FactoryMe(ctx context.Context, id string) (api.Me, error) {
	var me api.Me
	if err := b.factoryHub().read(ctx, id, "/v1/me", appFactorySmall, &me); err != nil {
		return api.Me{}, err
	}
	// The session's id is the factory's handle on a credential. The app has no
	// use for it, so it does not leave.
	me.SessionID = ""
	return me, nil
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
		Factory api.Factory    `json:"factory"`
		Events  []record.Event `json:"events"`
	}
	if err := b.factoryHub().read(ctx, id, path, appFactoryBody, &body); err != nil {
		return appwire.FactoryActivity{}, err
	}
	out := appwire.FactoryActivity{Factory: body.Factory, Events: body.Events}
	if len(out.Events) > limit {
		out.Events, out.Truncated = out.Events[len(out.Events)-limit:], true
	}
	return out, nil
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
	id := appFactoryID(origin)
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return appwire.FactoryAccess{}, appwire.Errorf(appwire.CodeClosed, "the engine is stopping")
	}
	if err = h.load(); err != nil {
		h.mu.Unlock()
		return appwire.FactoryAccess{}, err
	}
	h.prune()
	flow := h.links[key]
	if verb == "sign_in" {
		known := false
		for _, row := range h.rows {
			known = known || row.ID == id
		}
		switch {
		case flow == nil && len(h.links)+len(h.signIns) >= appwire.MaxFactoryFlows:
			h.mu.Unlock()
			return appwire.FactoryAccess{}, appwire.Errorf(appwire.CodeBusy, "too many pending factory links")
		case !known && len(h.rows) >= appwire.MaxFactoryConnections:
			h.mu.Unlock()
			return appwire.FactoryAccess{}, appwire.Errorf(appwire.CodeBusy, "this app is attached to as many factories as it can be (%d)", appwire.MaxFactoryConnections)
		}
	}
	if flow != nil && flow.busy {
		h.mu.Unlock()
		return appwire.FactoryAccess{}, appwire.Errorf(appwire.CodeBusy, "this link is already being checked")
	}
	var cookie *http.Cookie
	if flow != nil {
		flow.busy, cookie = true, flow.cookie
	}
	token := h.tokenLocked(id)
	h.mu.Unlock()
	release := func() {
		h.mu.Lock()
		if l := h.links[key]; l != nil {
			l.busy = false
		}
		h.mu.Unlock()
	}

	var in any
	method, path := http.MethodGet, "/v1/factory-access?link="+url.QueryEscape(link)
	if verb != "" {
		method, path, in = http.MethodPost, "/v1/factory-access", api.FactoryAccessRequest{Link: link, Verb: verb}
	}
	r, err := h.send(ctx, origin, token, method, path, in, appFactorySmall, func(req *http.Request) {
		if cookie != nil {
			req.AddCookie(cookie)
		}
	})
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
	h.mu.Lock()
	h.prune()
	mine := 0
	for _, f := range h.signIns {
		if f.caller == caller {
			mine++
		}
	}
	over := len(h.links)+len(h.signIns) >= appwire.MaxFactoryFlows
	h.mu.Unlock()
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

func (b *appBackend) FactorySignInPoll(ctx context.Context, caller, flow string) (appwire.FactorySignInState, error) {
	h := b.factoryHub()
	f, err := h.own(caller, flow, true)
	if err != nil {
		return appwire.FactorySignInState{}, err
	}
	h.mu.Lock()
	now := h.now()
	switch {
	case !now.Before(f.expires):
		delete(h.signIns, flow)
		h.mu.Unlock()
		return appwire.FactorySignInState{State: appwire.SignInExpired}, nil
	case now.Before(f.next):
		f.busy = false
		interval := f.interval
		h.mu.Unlock()
		// TOO SOON TO ASK GITHUB AGAIN: answer without a request.
		return appwire.FactorySignInState{State: appwire.SignInPending, Interval: interval}, nil
	}
	f.next = now.Add(time.Duration(f.interval) * time.Second)
	code, factoryID, bearer := f.code, f.factory, f.bearer
	h.mu.Unlock()
	unclaim := func() {
		h.mu.Lock()
		if cur := h.signIns[flow]; cur == f {
			f.busy = false
		}
		h.mu.Unlock()
	}

	row, _, err := h.target(factoryID)
	if err != nil {
		h.mu.Lock()
		delete(h.signIns, flow)
		h.mu.Unlock()
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
		h.mu.Lock()
		if body.Interval > f.interval {
			f.interval = body.Interval
			f.next = h.now().Add(time.Duration(f.interval) * time.Second)
		}
		interval := f.interval
		f.busy = false
		h.mu.Unlock()
		return appwire.FactorySignInState{State: appwire.SignInPending, Interval: interval}, nil
	case http.StatusOK:
		var signed api.SignedIn
		if json.Unmarshal(r.body, &signed) != nil || signed.AccessToken == "" {
			h.mu.Lock()
			delete(h.signIns, flow)
			h.mu.Unlock()
			return appwire.FactorySignInState{}, appwire.Errorf(appwire.CodeEngine, "the factory sent an answer this app cannot read")
		}
		h.hold(factoryID, signed.AccessToken, time.Duration(signed.ExpiresIn)*time.Second)
		h.mu.Lock()
		delete(h.signIns, flow)
		h.mu.Unlock()
		me := signed.Me
		me.SessionID = ""
		return appwire.FactorySignInState{State: appwire.SignInSignedIn, Me: &me}, nil
	case http.StatusGone, http.StatusForbidden:
		// expired_token, access_denied, a handle off the team, a link for
		// someone else: each ends the flow, and the factory's sentence says why.
		h.mu.Lock()
		delete(h.signIns, flow)
		h.mu.Unlock()
		if r.status == http.StatusGone {
			return appwire.FactorySignInState{State: appwire.SignInExpired}, nil
		}
		msg := body.Error
		if msg == "access_denied" {
			msg = ""
		}
		return appwire.FactorySignInState{State: appwire.SignInDenied, Message: msg}, nil
	}
	h.mu.Lock()
	delete(h.signIns, flow)
	h.mu.Unlock()
	return appwire.FactorySignInState{}, appFactoryFailure(r)
}

func (b *appBackend) FactorySignInCancel(ctx context.Context, caller, flow string) error {
	h := b.factoryHub()
	f, err := h.own(caller, flow, false)
	if err != nil {
		return err
	}
	h.mu.Lock()
	delete(h.signIns, flow)
	h.mu.Unlock()
	// Telling the factory lets it free the code; if it cannot be told, the
	// code expires on its own.
	if row, _, e := h.target(f.factory); e == nil {
		ctx, cancel := context.WithTimeout(ctx, appFactoryProbe)
		defer cancel()
		_, _ = h.send(ctx, row.URL, f.bearer, http.MethodDelete, "/v1/auth/device", api.DevicePoll{DeviceCode: f.code}, appFactorySmall, nil)
	}
	return nil
}
