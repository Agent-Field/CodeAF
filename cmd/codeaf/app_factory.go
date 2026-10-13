package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appapi"
	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	factoryworker "github.com/Agent-Field/codeaf/internal/factory/worker"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

// appFactoryHost mounts the solo floor without opening a second listener.
// Service election and worker election use the same files as terminal windows.
type appFactoryHost struct {
	defaultTarget                      func() (factoryTarget, error)
	parent                             func() session.Config
	mu                                 sync.Mutex
	ctx                                context.Context
	cancel                             context.CancelFunc
	done                               chan struct{}
	floor                              *store.Store
	address, token, workspace, profile string
	credentials                        *config.AppCredentials
	handler                            *serve.Handler
	release                            func()
	closeDiscovery                     func()
}

func newAppFactoryHost(ctx context.Context, root, address, token string, credentials *config.AppCredentials) (*appFactoryHost, error) {
	floor, err := store.Open(filepath.Join(root, "v3", "factory"))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	workspace, _ := os.Getwd()
	h := &appFactoryHost{ctx: ctx, cancel: cancel, done: make(chan struct{}), floor: floor, address: address, token: token, workspace: workspace, profile: config.ProfileDir(), credentials: credentials}
	guard.Go("app/factory worker", func() { defer close(h.done); h.workers() })
	return h, nil
}
func (h *appFactoryHost) close() {
	h.cancel()
	<-h.done
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.handler != nil {
		h.handler.Close()
		path := filepath.Join(h.floor.Root(), "serve.json")
		body, _ := os.ReadFile(path)
		var saved factoryDiscovery
		if json.Unmarshal(body, &saved) == nil && saved.URL == h.address && saved.PID == os.Getpid() {
			_ = os.Remove(path)
		}
	}
	if h.closeDiscovery != nil {
		h.closeDiscovery()
	}
	if h.release != nil {
		h.release()
	}
}
func (h *appFactoryHost) solo(_ context.Context) (http.Handler, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.handler != nil {
		return h.handler, nil
	}
	if target, ok := liveFactoryDiscovery(); ok {
		return appFactoryProxy(target), nil
	}
	release, err := factoryFileLock(filepath.Join(h.floor.Root(), "serve.lock"), true)
	if err != nil {
		release()
		target, e := waitForFactoryDiscovery(factoryLockWait)
		if e != nil {
			return nil, e
		}
		return appFactoryProxy(target), nil
	}
	history, err := record.Open(h.floor, record.Options{})
	if err != nil {
		release()
		return nil, err
	}
	seam := factorySeam(h.floor, h.workspace, h.profile)
	handler := serve.New(seam, serve.Config{GitHubToken: func(ctx context.Context) string { token, _ := factorygithub.TokenAt(ctx, h.profile); return token }, Mode: api.FactorySolo, ServedBy: "app", Address: h.address, AccessToken: h.token, Name: "This machine", Store: h.floor, Root: h.floor.Root(), Record: history, PollingEvery: factoryPollEvery,
		Pipeline: func(ctx context.Context, repo, dir string) (pipeline.File, error) {
			return factoryRules().Load(ctx, repo, dir, home.Join("factory.yaml"))
		},
		ChangeAssignee: factorygithub.JobWrites{Token: func(ctx context.Context) string { token, _ := factorygithub.TokenAt(ctx, h.profile); return token }}.Assign,
		PersonProfile:  factoryPeople().Profile, CodeOwners: factoryPeople().Owners})
	closeDiscovery, err := serveAppFactoryDiscovery(h.floor.Root(), factoryTarget{h.address, h.token})
	if err != nil {
		handler.Close()
		release()
		return nil, err
	}
	data, _ := json.Marshal(factoryDiscovery{URL: h.address, PID: os.Getpid(), App: true})
	if err = writeFactoryPrivate(filepath.Join(h.floor.Root(), "serve.json"), append(data, '\n')); err != nil {
		handler.Close()
		closeDiscovery()
		release()
		return nil, err
	}
	h.handler, h.release = handler, release
	h.closeDiscovery = closeDiscovery
	startFactoryTriage(h.ctx, h.floor)
	guard.Go("app/factory poll", func() {
		token := func(ctx context.Context) string { t, _ := factorygithub.TokenAt(ctx, h.profile); return t }
		source := waitForFactoryGitHub(h.ctx, h.floor, token, factoryWatchEvery)
		if source != nil {
			factorygithub.Poll(h.ctx, source, h.floor, factoryPollEvery, nil, factoryIntake(h.floor).Once)
		}
	})
	return handler, nil
}

// mountSolo follows the elected service when its hosting window closes.
// A surviving app can serve the same floor without requiring a restart.
func (h *appFactoryHost) mountSolo(ctx context.Context) (http.Handler, error) {
	if _, err := h.solo(ctx); err != nil {
		return nil, err
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler, err := h.solo(r.Context())
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"reason":"no_factory","error":"the solo factory is unavailable"}`))
			return
		}
		handler.ServeHTTP(w, r)
	}), nil
}
func appFactoryProxy(target factoryTarget) http.Handler {
	address, _ := url.Parse(target.url)
	proxy := httputil.NewSingleHostReverseProxy(address)
	director := proxy.Director
	proxy.Director = func(r *http.Request) {
		director(r)
		r.Host = address.Host
		if !appapi.FactoryWorkerPath(r.URL.Path) {
			r.Header.Set("Authorization", "Bearer "+target.token)
		}
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(503)
		_, _ = w.Write([]byte(`{"reason":"no_factory","error":"the solo factory is unavailable"}`))
	}
	return proxy
}

// targets reads only addresses. App session credentials remain in the key handoff.
func (h *appFactoryHost) targets() []factoryTarget {
	data, _ := os.ReadFile(filepath.Join(h.floor.Root(), "attach.json"))
	var saved struct {
		URL       string                       `json:"url"`
		Factories []struct{ URL, Mode string } `json:"factories"`
	}
	_ = json.Unmarshal(data, &saved)
	out := []factoryTarget{}
	for _, row := range saved.Factories {
		if row.Mode == "solo" {
			if target, ok := liveFactoryDiscovery(); ok {
				out = append(out, target)
			}
		} else if token := h.credentials.FactorySession(row.URL); token != "" {
			out = append(out, factoryTarget{row.URL, token})
		}
	}
	if len(saved.Factories) == 0 && saved.URL != "" {
		if token := h.credentials.FactorySession(saved.URL); token != "" {
			out = append(out, factoryTarget{saved.URL, token})
		}
	}
	if len(out) == 0 && h.defaultTarget != nil {
		if target, err := h.defaultTarget(); err == nil {
			if token := h.credentials.FactorySession(target.url); token != "" {
				target.token = token
			}
			out = append(out, target)
		}
	}
	return out
}
func (h *appFactoryHost) workers() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	release, ok := h.electWorker(ticker.C)
	if !ok {
		return
	}
	defer release()
	clearDeadFactoryMirrors(filepath.Join(h.floor.Root(), "workers"))
	closeControl := h.runnerControl()
	defer closeControl()
	type running struct {
		cancel context.CancelFunc
		done   chan struct{}
		token  string
	}
	active := map[string]running{}
	defer func() {
		for _, run := range active {
			run.cancel()
		}
		for _, run := range active {
			<-run.done
		}
	}()
	for h.ctx.Err() == nil {
		wanted := map[string]factoryTarget{}
		for _, target := range h.targets() {
			wanted[target.url] = target
		}
		for address, run := range active {
			select {
			case <-run.done:
				delete(active, address)
				continue
			default:
			}
			target, ok := wanted[address]
			if !ok || target.token != run.token {
				run.cancel()
				<-run.done
				delete(active, address)
			}
		}
		for address, target := range wanted {
			if _, ok := active[address]; ok {
				continue
			}
			ctx, cancel := context.WithCancel(h.ctx)
			done := make(chan struct{})
			active[address] = running{cancel, done, target.token}
			guard.Go("app/factory slots", func() { defer close(done); h.runWorker(ctx, target) })
		}
		select {
		case <-h.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (h *appFactoryHost) runWorker(ctx context.Context, target factoryTarget) {
	sum := sha256.Sum256([]byte(target.url))
	mirror, err := store.Open(filepath.Join(h.floor.Root(), "workers", strconv.Itoa(os.Getpid()), hex.EncodeToString(sum[:8])))
	if err != nil {
		return
	}
	defer os.RemoveAll(mirror.Root())
	current := h.parent
	if current == nil {
		settings, err := config.LoadKeyless()
		if err != nil {
			return
		}
		base, err := applyV3Governance(session.Config{Workspace: h.workspace, Model: settings.Model, Sources: settings.Sources}, h.profile, false, false)
		if err != nil {
			return
		}
		current = func() session.Config {
			settings, err := config.LoadKeyless()
			if err != nil {
				return base
			}
			fresh, err := applyV3Governance(session.Config{Workspace: h.workspace, Model: settings.Model, Sources: settings.Sources}, h.profile, false, false)
			if err != nil {
				return base
			}
			return fresh
		}
	}
	turns := factoryShapeTurnsFrom(h.floor, h.workspace, h.profile, current)
	setFactoryProcess(func() {
		if factoryProcess.workerShapes == nil {
			factoryProcess.workerShapes = map[string]func(context.Context, int) (string, error){}
		}
		factoryProcess.workerShapes[mirror.Root()] = factoryShapeDoor(mirror, h.workspace, h.profile, turns)
	})
	defer setFactoryProcess(func() { delete(factoryProcess.workerShapes, mirror.Root()) })
	runner := buildFactoryRunner(mirror, h.floor, h.workspace, h.profile, factoryStageMakerFrom(h.floor, h.workspace, h.profile, current), turns)
	dial := newFactoryWorkerDial(h.floor, func() (factoryTarget, error) { return target, nil })
	machine, _ := os.Hostname()
	kind := api.RunnerCI
	if runtime.GOOS == "darwin" {
		kind = api.RunnerMac
		if target.url == h.address {
			kind = api.RunnerThisMac
		}
	}
	dirs, _ := h.floor.Checkouts()
	checkouts := []api.Checkout{}
	takes := map[string]bool{}
	for repo, path := range dirs {
		checkouts = append(checkouts, api.Checkout{Repo: repo, Path: path})
		takes[repo] = true
	}
	if target.url == h.address {
		takes[lease.EveryRepo] = true
	}
	var me api.Me
	for ctx.Err() == nil {
		if appFactoryRequest(ctx, target, "GET", "/v1/me", nil, &me) == nil {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
	if me.Mode == api.FactorySolo || (me.Mode == "" && factoryLoopback(target.url)) {
		takes[lease.EveryRepo] = true
		if runtime.GOOS == "darwin" {
			kind = api.RunnerThisMac
		}
	}
	factoryworker.Run(ctx, factoryworker.Options{Dial: dial.dial, Keep: dial.keep, Attach: api.WorkerAttach{Machine: machine, Name: machine, Handle: me.Handle, OS: runtime.GOOS, CodeafVersion: buildinfo.String(), Kind: kind, Slots: factoryBenches(), Checkouts: checkouts, TakesUnassigned: takes}, Slots: factoryBenches(), Runner: runner, Mirror: mirror, LocalLease: factoryLocalLease(mirror), Reply: func(ctx context.Context, it factory.Item, words string) error {
		_, err := turns.through(ctx, it, words, nil)
		return err
	}})
}

// Power uses the elected runner's saved registration, so a second process
// never registers a duplicate merely to report this machine sleeping.
func (h *appFactoryHost) power(ctx context.Context, asleep bool) error {
	dial := newFactoryWorkerDial(h.floor, nil)
	all := dial.readCreds()
	data, err := os.ReadFile(filepath.Join(h.floor.Root(), "attach.json"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var attached struct {
		URL       string                       `json:"url"`
		Factories []struct{ URL, Mode string } `json:"factories"`
	}
	if len(data) != 0 && json.Unmarshal(data, &attached) != nil {
		return errors.New("invalid factory connections")
	}
	active := map[string]bool{}
	if attached.URL != "" {
		active[attached.URL] = true
	}
	for _, row := range attached.Factories {
		if row.Mode == "team" {
			active[row.URL] = true
		}
	}
	if target, ok := liveFactoryDiscovery(); ok {
		active[target.url] = true
	}
	var failures []error
	for address, credentials := range all {
		if !active[address] {
			continue
		}
		for _, credential := range credentials {
			if credential == "" {
				continue
			}
			target := factoryTarget{address, credential}
			if err := appFactoryRequest(ctx, target, "POST", "/v1/runners/presence", api.RunnerPresence{Asleep: asleep}, nil); err != nil {
				failures = append(failures, errors.New("factory runner is unavailable"))
			}
			break
		}
	}
	return errors.Join(failures...)
}
func appFactoryRequest(ctx context.Context, target factoryTarget, method, path string, body, out any) error {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	data, _ := json.Marshal(body)
	r, err := http.NewRequestWithContext(ctx, method, target.url+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	r.Header.Set("Authorization", "Bearer "+target.token)
	r.Header.Set("Content-Type", "application/json")
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	response, err := (&http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(r)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return errors.New("factory refused the request")
	}
	if out != nil {
		return json.NewDecoder(response.Body).Decode(out)
	}
	return nil
}
