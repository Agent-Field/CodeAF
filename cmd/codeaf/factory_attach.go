package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/processgroup"
)

// ── WHICH FACTORY A WINDOW'S PAGE TALKS TO (ARCHITECTURE §6) ───────────────
//
// The factory page is always handed a client seam over HTTP, composed with
// this machine's own doors ([api.Compose]). Which factory the client talks to
// is decided in one order, here and nowhere else:
//
//	attach.json   → the factory `codeaf attach` named (a team's, or your other machine's)
//	serve.json    → the factory another codeaf on this machine already serves
//	else          → serve one in this process on 127.0.0.1:0 and write serve.json
//
// Execution is not decided here: every codeaf runs a worker loop against the
// factory this order answers (factory_run.go), and the factory's verbs reach
// it through the lease.

// factoryAttachment is attach.json: the factory this machine's windows talk to
// instead of serving their own.
type factoryAttachment struct {
	URL   string `json:"url"`
	Name  string `json:"name,omitempty"`
	Token string `json:"token,omitempty"`
}

// factoryTarget is one factory a client can be opened on.
type factoryTarget struct {
	url, token string
}

func factoryAttachPath() string { return filepath.Join(v3FactoryRoot(), "attach.json") }

// runAttach writes attach.json. It does not dial the factory: a team box that
// is down now is still the one the person means, and the page says so when it
// cannot read it.
func runAttach(args []string) error {
	flags := commandFlags("attach")
	name := flags.String("name", "", "the name this machine goes by on the factory (default its hostname)")
	token := flags.String("token", "", "the access token codeaf factory token prints on that factory's machine")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return wrongCall("use codeaf attach <url> [--name name] [--token token]")
	}
	base, err := factoryURL(flags.Arg(0))
	if err != nil {
		return wrongCall(err.Error())
	}
	secret := strings.TrimSpace(*token)
	who := strings.TrimSpace(*name)
	if who == "" {
		who, _ = os.Hostname()
	}
	if err := writeFactoryAttachment(factoryAttachment{URL: base, Name: who, Token: secret}); err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, "attached to "+base+" as "+who+"; new codeaf windows use it")
	return err
}

// writeFactoryAttachment keeps a as attach.json, readable by this person only:
// it may hold the session the factory issued them.
func writeFactoryAttachment(a factoryAttachment) error {
	body, err := json.Marshal(a)
	if err != nil {
		return err
	}
	return writeFactoryPrivate(factoryAttachPath(), append(body, '\n'))
}

// runDetach removes attach.json, so the next window serves or finds this
// machine's own factory again.
func runDetach(args []string) error {
	flags := commandFlags("detach")
	if err := parseCommandFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return wrongCall("codeaf detach takes no arguments")
	}
	err := os.Remove(factoryAttachPath())
	if errors.Is(err, os.ErrNotExist) {
		_, err = fmt.Fprintln(os.Stdout, "not attached; windows use this machine's factory")
		return err
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, "detached; new codeaf windows use this machine's factory")
	return err
}

// factoryURL is raw as a factory's origin, or why it is not one.
func factoryURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return "", fmt.Errorf("%q is not a factory address; use https://host or http://host:port", raw)
	}
	return u.Scheme + "://" + u.Host, nil
}

// readFactoryAttachment is attach.json, when there is a usable one.
func readFactoryAttachment() (factoryAttachment, bool) {
	body, err := os.ReadFile(factoryAttachPath())
	if err != nil {
		return factoryAttachment{}, false
	}
	var a factoryAttachment
	if json.Unmarshal(body, &a) != nil {
		return factoryAttachment{}, false
	}
	base, err := factoryURL(a.URL)
	if err != nil {
		return factoryAttachment{}, false
	}
	a.URL = base
	return a, true
}

// factoryProbeWait bounds the one read that says a discovered factory is
// alive. It is loopback, so a second is generous, and a launch waits on it.
const factoryProbeWait = time.Second

// factoryServeHolder is the receipt serve.json holds, when the process it
// names is alive: who holds this machine's factory election, whether or not
// its service answers. A receipt a killed process left behind is no holder.
func factoryServeHolder() (factoryDiscovery, bool) {
	body, err := os.ReadFile(filepath.Join(v3FactoryRoot(), "serve.json"))
	if err != nil {
		return factoryDiscovery{}, false
	}
	var d factoryDiscovery
	if json.Unmarshal(body, &d) != nil || d.PID <= 0 || !processgroup.ProcessAlive(d.PID) {
		return factoryDiscovery{}, false
	}
	return d, true
}

// liveFactoryDiscovery is the factory serve.json names, when its process is
// alive and it answers GET /v1/queue. A receipt a killed process left behind
// is neither, and is ignored rather than trusted.
func liveFactoryDiscovery() (factoryTarget, bool) {
	d, ok := factoryServeHolder()
	if !ok {
		return factoryTarget{}, false
	}
	if d.App {
		live, err := appFactoryDiscovery(v3FactoryRoot())
		if err != nil || live.URL != d.URL || live.PID != d.PID || live.Token == "" {
			return factoryTarget{}, false
		}
		d.Token = live.Token
	}
	base, err := factoryURL(d.URL)
	if err != nil {
		return factoryTarget{}, false
	}
	t := factoryTarget{url: factoryDialable(base), token: d.Token}
	// Only a running server owned by this process makes the HTTP probe
	// redundant. PIDs in persisted receipts can be reused after a restart.
	if d.PID == os.Getpid() && ownsFactoryTarget(t) {
		return t, true
	}
	return t, factoryAnswers(t)
}

// ownsFactoryTarget checks service ownership while leaving network probes
// outside the serving-state lock.
func ownsFactoryTarget(t factoryTarget) bool {
	factoryServingHere.mu.Lock()
	defer factoryServingHere.mu.Unlock()
	s := factoryServingHere.service
	return s != nil && s.running.Load() && s.url == t.url && filepath.Dir(s.discovery) == v3FactoryRoot()
}

// factoryDialable turns a wildcard listen address into the loopback one this
// machine dials, as revoke-sessions does.
func factoryDialable(base string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && ip.IsUnspecified() {
		peer := "::1"
		if ip.To4() != nil {
			peer = "127.0.0.1"
		}
		u.Host = net.JoinHostPort(peer, u.Port())
	}
	return u.String()
}

// factoryAnswers says whether t answers GET /v1/queue within [factoryProbeWait].
func factoryAnswers(t factoryTarget) bool {
	ctx, cancel := context.WithTimeout(context.Background(), factoryProbeWait)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.url+"/v1/queue", nil)
	if err != nil {
		return false
	}
	if t.token != "" {
		req.Header.Set("Authorization", "Bearer "+t.token)
	}
	resp, err := (&http.Client{Transport: &http.Transport{Proxy: nil}}).Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// factoryServingHere is the one service this process runs for its windows,
// and the serve.lock it holds while it does. Both are kept until the process
// ends ([closeFactoryServingHere]), and the next window finds serve.json gone
// and serves its own.
var factoryServingHere struct {
	mu      sync.Mutex
	service *factoryService
	release func()
}

// factoryLockWait is how long a window that lost the race for serve.lock waits
// for the winner's receipt before saying no factory answered.
var factoryLockWait = 3 * time.Second

// serveFactoryHere serves seam from this process on 127.0.0.1:0 and answers
// where, or answers the factory another process on this machine started while
// this one was looking. The service has no access token: it listens on
// loopback only, and loopback callers are let in by the socket's peer.
func serveFactoryHere(st *store.Store, seam factory.Seam) (factoryTarget, error) {
	factoryServingHere.mu.Lock()
	defer factoryServingHere.mu.Unlock()
	// ONE SERVICE PER FLOOR: a process opens one codeaf home, so this is the
	// one it already serves; a test that moves the home gets its own.
	if s := factoryServingHere.service; s != nil && filepath.Dir(s.discovery) == st.Root() {
		return factoryTarget{url: s.url}, nil
	}
	release, err := factoryFileLock(filepath.Join(st.Root(), "serve.lock"), true)
	if err != nil {
		release()
		return waitForFactoryDiscovery(factoryLockWait)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		release()
		return factoryTarget{}, err
	}
	host, err := readFactoryHostConfig(home.Join("factory", "config.yaml"))
	if err != nil {
		listener.Close()
		release()
		return factoryTarget{}, err
	}
	service, err := startFactoryService(context.Background(), st, seam, listener, "", factoryTokensPath(home.Join("factory", "config.yaml")), host)
	if err != nil {
		listener.Close()
		release()
		return factoryTarget{}, err
	}
	factoryServingHere.service, factoryServingHere.release = service, release
	return factoryTarget{url: service.url}, nil
}

// closeFactoryServingHere stops the service this process runs for its windows
// and gives back serve.lock, taking serve.json down first: a receipt that
// outlives its service sends the next window to a dead address.
func closeFactoryServingHere() {
	factoryServingHere.mu.Lock()
	defer factoryServingHere.mu.Unlock()
	if s := factoryServingHere.service; s != nil {
		s.close()
	}
	if release := factoryServingHere.release; release != nil {
		release()
	}
	factoryServingHere.service, factoryServingHere.release = nil, nil
}

// waitForFactoryDiscovery is the factory another process is starting, once its
// receipt answers, or why there is none after wait.
func waitForFactoryDiscovery(wait time.Duration) (factoryTarget, error) {
	deadline := time.Now().Add(wait)
	for {
		if t, ok := liveFactoryDiscovery(); ok {
			return t, nil
		}
		if time.Now().After(deadline) {
			return factoryTarget{}, errors.New("another codeaf holds this machine's factory but does not answer")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// resolveFactory is ARCHITECTURE §6's order. here is the seam a service
// started in this process serves; st nil means no floor on this machine, so
// only an attachment can answer.
func resolveFactory(st *store.Store, here factory.Seam) (factoryTarget, error) {
	if a, ok := readFactoryAttachment(); ok {
		return factoryTarget{url: a.URL, token: a.Token}, nil
	}
	if st == nil {
		return factoryTarget{}, errors.New("no factory folder on this machine")
	}
	if t, ok := liveFactoryDiscovery(); ok {
		return t, nil
	}
	return serveFactoryHere(st, here)
}

// factoryConnection is the factory a window's page talks to now. A read that
// cannot reach it resolves again ([resolveFactory]) and reads once more, so a
// window whose serving window closed takes the serving over (or finds who did)
// instead of drawing an error until it is restarted.
type factoryConnection struct {
	mu      sync.Mutex
	at      factoryTarget
	seam    factory.Seam
	resolve func() (factoryTarget, error)
}

func newFactoryConnection(resolve func() (factoryTarget, error)) *factoryConnection {
	c := &factoryConnection{resolve: resolve}
	c.dial()
	return c
}

// dial resolves and opens a client on the answer. On no answer the seam is
// the zero one, whose page says nothing is connected.
func (c *factoryConnection) dial() {
	t, err := c.resolve()
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.at, c.seam = factoryTarget{}, factory.Seam{}
		return
	}
	c.at, c.seam = t, client.Open(t.url, t.token).Seam()
}

func (c *factoryConnection) current() factory.Seam {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.seam
}

// address is the factory's host and port, for the page's one quiet line.
func (c *factoryConnection) address() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.TrimPrefix(strings.TrimPrefix(c.at.url, "http://"), "https://")
}

// load is Load with one re-resolve when the factory did not answer at all. A
// factory that answered with a refusal is not re-resolved: it is there.
func (c *factoryConnection) load() (factory.Snapshot, error) {
	snap, err := c.loadNow()
	var op *net.OpError
	if err == nil || !errors.As(err, &op) || op.Op != "dial" {
		return snap, err
	}
	c.dial()
	return c.loadNow()
}

func (c *factoryConnection) loadNow() (factory.Snapshot, error) {
	if load := c.current().Load; load != nil {
		return load()
	}
	return factory.Snapshot{}, errors.New("no factory answers")
}

// remote is the connection as a seam: every wired door follows the
// connection, and Load re-resolves on an unreachable factory.
func (c *factoryConnection) remote() factory.Seam {
	seam := api.Through(c.current)
	if seam.Load != nil {
		seam.Load = c.load
	}
	return seam
}

// factoryLaunchSeam is the factory page's seam for a window on this machine,
// and the address line's source. The still fixture stands in front of it
// when it is switched on, unchanged.
func factoryLaunchSeam(st *store.Store, workspace, profileDir string) (factory.Seam, func() string) {
	local := factorySeam(st, workspace, profileDir)
	if strings.TrimSpace(env.Get(factoryFixtureEnv)) == "1" {
		return local, nil
	}
	conn := newFactoryConnection(func() (factoryTarget, error) { return resolveFactory(st, local) })
	return factoryHere(conn.compose(local, st, workspace, profileDir), workspace), conn.address
}

// factoryAttachedSeam is the page's seam for a --host or --at window: the
// floor named in this machine's attach.json, composed with this machine's own
// doors, or the zero seam when nothing is attached. The far engine's own floor
// listens on the far machine's loopback, which this window cannot dial, so it
// is never guessed at.
func factoryAttachedSeam(profileDir string) (factory.Seam, func() string) {
	a, ok := readFactoryAttachment()
	if !ok {
		return factory.Seam{}, nil
	}
	st := v3Factory()
	local := factorySeam(st, "", profileDir)
	conn := newFactoryConnection(func() (factoryTarget, error) {
		return factoryTarget{url: a.URL, token: a.Token}, nil
	})
	return conn.compose(local, st, "", profileDir), conn.address
}
