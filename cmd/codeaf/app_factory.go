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
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/intake"
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
	// seam is the floor's door the service serves, kept for what the app asks
	// of this machine's factory directly (a job's conversations).
	seam                               factory.Seam
	defaultTarget                      func() (factoryTarget, error)
	parent                             func() session.Config
	mu                                 sync.Mutex
	ctx                                context.Context
	cancel                             context.CancelFunc
	done                               chan struct{}
	floor                              *store.Store
	address, token, workspace, profile string
	credentials                        factorySessions
	handler                            *serve.Handler
	release                            func()
	closeDiscovery                     func()
	// quiet serves the factory's HTTP API and nothing else: no triage (which
	// calls a model) and no GitHub poll. The local host is quiet from start,
	// resume and setup, so those spend nothing; only an explicit
	// factory.local_run ends the quiet, through [appFactoryHost.readLoops]
	// (appfactory_setup.go).
	quiet bool
	// loopMu guards the read loops' start and their shutdown, loopsOn says this
	// host already started them, loopsClosed that close has begun, and loops
	// counts the goroutines close waits for. THE START IS PER HOST, never per
	// process: a process-wide once would leave a later host in the same
	// process, after this one closed, without its reads for good.
	loopMu               sync.Mutex
	loopsOn, loopsClosed bool
	loops                sync.WaitGroup
	// pollSource replaces the GitHub source of the poll; tests only, because
	// the forge client has no transport seam.
	pollSource func(ctx context.Context) *factorygithub.Source
	// newIntake replaces [factoryIntake], which refreshes rules and owners from
	// the forge; tests only, so none reaches it.
	newIntake func(*store.Store) *intake.Intake
	// githubHTTP and rules replace the factory's GitHub transport and its
	// pipeline-file reader; tests only, so none reaches the real forge.
	githubHTTP *http.Client
	rules      func(ctx context.Context, repo, dir string) (pipeline.File, error)
	seamHook   func(factory.Seam) factory.Seam
}

func newAppFactoryHost(ctx context.Context, root, address, token string, credentials factorySessions) (*appFactoryHost, error) {
	floor, err := store.Open(filepath.Join(root, "v3", "factory"))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	workspace, _ := os.Getwd()
	h := &appFactoryHost{ctx: ctx, cancel: cancel, done: make(chan struct{}), floor: floor, address: address, token: token, workspace: workspace, profile: config.ProfileDir(), credentials: credentials}
	floor.SetOutputPolicy(appFactoryOutputPolicy(h.profile))
	guard.Go("app/factory worker", func() { defer close(h.done); h.workers() })
	return h, nil
}
func (h *appFactoryHost) close() {
	h.cancel()
	<-h.done
	// THE READ LOOPS END BEFORE THE FLOOR IS LET GO, so no poll or triage
	// write lands after close. Both stop on the host's context, which is
	// cancelled above, and none can start once loopsClosed is set. THIS WAIT
	// HAS NO TIMEOUT OF ITS OWN, ON PURPOSE: it ends when the one request each
	// loop may have in flight returns, and every such request is bound to the
	// host's context or a per-request timeout. It is not capped at
	// [appLocalStopWait], which bounds only the runner before this point,
	// because letting go of the floor while a loop could still write to it
	// would break the no-write-after-close law.
	h.stopLoops()
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
	if err := forgetFactoryPRJobs(h.ctx, h.floor, h.profile); err != nil {
		release()
		return nil, err
	}
	history, err := record.Open(h.floor, record.Options{})
	if err != nil {
		release()
		return nil, err
	}
	seam := factorySeam(h.floor, h.workspace, h.profile)
	if h.seamHook != nil {
		seam = h.seamHook(seam)
	}
	h.seam = seam
	handler := serve.New(seam, serve.Config{ProfileDir: h.profile, DefaultModel: h.defaultModel, GitHubToken: func(ctx context.Context) string { token, _ := factorygithub.TokenAt(ctx, h.profile); return token }, GitHubHTTPClient: h.githubHTTP, Mode: api.FactorySolo, ServedBy: "app", Address: h.address, AccessToken: h.token, Name: appLocalName, Store: h.floor, Root: h.floor.Root(), Record: history, PollingEvery: factoryPollEvery,
		Pipeline: func(ctx context.Context, repo, dir string) (pipeline.File, error) {
			if h.rules != nil {
				return h.rules(ctx, repo, dir)
			}
			return factoryRules().Load(ctx, repo, dir, home.Join("factory.yaml"))
		},
		ChangeAssignee: factorygithub.JobWrites{Token: func(ctx context.Context) string { token, _ := factorygithub.TokenAt(ctx, h.profile); return token }}.Assign,
		// A person's start reads the one item again first (serve's endedOnGitHub).
		Recheck: factorygithub.Rechecker(func(ctx context.Context) string { token, _ := factorygithub.TokenAt(ctx, h.profile); return token }),
		// A drop deletes the job's local branch and folder on this machine,
		// and its conversations and crew.
		Folders: newFactoryJobFolders(h.floor), DeleteChats: factoryDeleteJobChats(h.profile, h.floor),
		PersonProfile: factoryPeople().Profile, PersonKnown: factoryPeople().Known, CodeOwners: factoryPeople().Owners})
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
	if h.quiet {
		return handler, nil
	}
	h.readLoops()
	return handler, nil
}

// stopLoops forbids any later start and waits for the loops that began.
func (h *appFactoryHost) stopLoops() {
	h.forbidLoops()
	h.loops.Wait()
}

func (h *appFactoryHost) forbidLoops() {
	h.loopMu.Lock()
	defer h.loopMu.Unlock()
	h.loopsClosed = true
}

// readLoops starts what a serving factory reads by itself: the GitHub poll
// with its intake, and the cheap model read of new items (triage). Both run
// under the host's context, so closing the host stops them, and the host waits
// for them. It is idempotent, and a host that closed starts nothing.
//
// A QUIET HOST CALLS THIS ONLY WHEN A PERSON TURNS THE RUNNER ON
// (factory.local_run, after the factory knows whose GitHub this is), and only
// on the factory this host serves itself: it reads nothing of a team factory
// and nothing of another codeaf's. THIS CALL IS THE CONSENT TO THE
// REPOSITORY'S START RULES: the intake it wires in runs after every poll and
// triage tick and launches any New item the repository's factory.yaml allows,
// where before the app never ran an intake. The model never authorizes: triage
// only reads and proposes, and the rules alone decide. The model is the saved
// one, strictly: [factoryTriageModel] has no fallback.
func (h *appFactoryHost) readLoops() {
	h.loopMu.Lock()
	defer h.loopMu.Unlock()
	if h.loopsOn || h.loopsClosed || h.ctx.Err() != nil {
		return
	}
	h.loopsOn = true
	h.loops.Add(2)
	guard.Go("app/factory triage", func() { defer h.loops.Done(); runFactoryTriage(h.ctx, h.floor, h.intakeFor) })
	guard.Go("app/factory poll", func() {
		defer h.loops.Done()
		var source *factorygithub.Source
		if h.pollSource != nil {
			source = h.waitForPollSource()
		} else {
			token := func(ctx context.Context) string { t, _ := factorygithub.TokenAt(ctx, h.profile); return t }
			source = waitForFactoryGitHub(h.ctx, h.floor, token, factoryWatchEvery)
		}
		if source != nil {
			factorygithub.Poll(h.ctx, source, h.floor, factoryPollEvery, nil, h.intakeFor(h.floor).Once)
		}
	})
}

// pollLoop starts the GitHub poll alone, for this Mac's own factory: issues
// filed after setup reach Backlog, and nothing rides on the poll. No intake
// applies start rules after it and no triage reads what it brought, so it
// starts no job and asks no model. It shares [readLoops]' once-per-host guard,
// and closing the host stops it.
func (h *appFactoryHost) pollLoop() {
	h.loopMu.Lock()
	defer h.loopMu.Unlock()
	if h.loopsOn || h.loopsClosed || h.ctx.Err() != nil {
		return
	}
	h.loopsOn = true
	h.loops.Add(1)
	guard.Go("app/factory poll", func() {
		defer h.loops.Done()
		var source *factorygithub.Source
		if h.pollSource != nil {
			source = h.waitForPollSource()
		} else {
			token := func(ctx context.Context) string { t, _ := factorygithub.TokenAt(ctx, h.profile); return t }
			source = waitForFactoryGitHub(h.ctx, h.floor, token, factoryWatchEvery)
		}
		if source != nil {
			factorygithub.Poll(h.ctx, source, h.floor, factoryPollEvery, nil)
		}
	})
}

// intakeFor is the intake over st: the factory's own, or the test seam.
func (h *appFactoryHost) intakeFor(st *store.Store) *intake.Intake {
	if h.newIntake != nil {
		return h.newIntake(st)
	}
	return factoryIntake(st)
}

// waitForPollSource is [waitForFactoryGitHub] over the test seam.
func (h *appFactoryHost) waitForPollSource() *factorygithub.Source {
	for {
		if source := h.pollSource(h.ctx); source != nil {
			return source
		}
		select {
		case <-h.ctx.Done():
			return nil
		case <-time.After(10 * time.Millisecond):
		}
	}
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
		if !factoryWorkerPath(r.URL.Path) {
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
	mirror.SetOutputPolicy(appFactoryOutputPolicy(h.profile))
	defer os.RemoveAll(mirror.Root())
	source, raw, ok := h.parentSource()
	if !ok {
		return
	}
	current := source.Config
	turns := factoryShapeTurnsFrom(h.floor, h.workspace, h.profile, current)
	turns.refusal = source.Refusal
	turns.liveOK = factoryLiveOK(raw, h.profile)
	runner := buildFactoryRunner(mirror, h.floor, h.workspace, h.profile, factoryStageMakerFrom(h.floor, h.workspace, h.profile, current).withRefusal(source.Refusal), turns)
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
	factoryworker.Run(ctx, factoryworker.Options{Dial: dial.dial, Keep: dial.keep, Attach: api.WorkerAttach{Machine: machine, Name: machine, Handle: me.Handle, OS: runtime.GOOS, CodeafVersion: buildinfo.String(), Kind: kind, Slots: factoryBenches(), Checkouts: checkouts, TakesUnassigned: takes}, Slots: factoryBenches(), Desired: dial.desiredSlots, Runner: runner, Mirror: mirror, DeliveryRoot: filepath.Join(h.floor.Root(), "worker-reports"), LocalLease: factoryLocalLease(mirror), Reply: func(ctx context.Context, it factory.Item, words string) error {
		_, err := turns.through(ctx, it, words, nil)
		return err
	}})
}

// parentSource is the parent every conversation of this host opens from: the
// injected launch config, or the worker's own governed one, settled on the saved
// Task models policy either way. It is false when the worker cannot build one.
func (h *appFactoryHost) parentSource() (*factoryParentSource, func() session.Config, bool) {
	raw := h.parent
	if raw == nil {
		settings, err := config.LoadKeyless()
		if err != nil {
			return nil, nil, false
		}
		// The worker's own ordinary governed config; the saved Task models policy
		// is settled on it below, exactly as it is on an injected parent.
		base, err := applyV3Governance(session.Config{Workspace: h.workspace, Model: settings.Model, Sources: settings.Sources}, h.profile, false, false)
		if err != nil {
			return nil, nil, false
		}
		raw = func() session.Config {
			settings, err := config.LoadKeyless()
			if err != nil {
				// THE SAVED POLICY IS STILL READ: its rows do not depend on this load, so
				// a profile switched to one model refuses or runs the one model here
				// rather than the stale Auto crew. RESIDUAL: a config.json that is
				// itself unreadable answers Auto, so the intent cannot be proven and the
				// auto base stays; refusing every load error would change Auto.
				return base
			}
			fresh, err := applyV3Governance(session.Config{Workspace: h.workspace, Model: settings.Model, Sources: settings.Sources}, h.profile, false, false)
			if err != nil {
				return base
			}
			return fresh
		}
	}
	// THE SAVED TASK MODELS POLICY IS THE FACTORY JOB'S (factory_taskmodels.go),
	// whichever parent this host was given.
	source := &factoryParentSource{raw: raw, profile: h.profile}
	return source, raw, true
}

// defaultModel is the model this machine's runner opens a step on when the job
// names none: the parent every stage opens from, settled on the saved Task
// models policy, exactly as the stage maker reads it.
func (h *appFactoryHost) defaultModel() string {
	source, _, ok := h.parentSource()
	if !ok {
		return ""
	}
	return strings.TrimSpace(source.Config().Model)
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

// Local log policy follows this Mac's profile, including for mirrors of a team factory.
//
// THE POLICY IS THE STORE'S OWN DEFAULT until the profile carries one:
// config.AppOutputPolicyAt is not in this branch's config package, and a nil
// policy is the store's documented "no override", so nothing is invented here.
func appFactoryOutputPolicy(profile string) func() (int, int) { return nil }
