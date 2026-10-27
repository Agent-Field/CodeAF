package main

// ── codeaf app: THE FACTORY ON THIS MACHINE, AND ITS FIRST RUN ──────────────
//
// The engine's half of appwire.FactoryLocal. It serves the one solo factory of
// this machine on request, and walks the factory's own first-run setup route
// (/v1/factory-setup/local) with the verbs the factory defines. It reuses the
// real service ([appFactoryHost]), the real setup handler and the attached
// registry ([appFactoryHub]); nothing here is a second implementation of any.
//
// ONLY WHEN ASKED. Nothing starts at launch. factory.local_start is the one
// door, and a window that never calls it pays nothing: no listener, no folder,
// no lock.
//
// ONE AUTHORITY PER MACHINE. The election is the one the terminal windows
// already use: serve.json and serve.lock under the factory folder
// ([liveFactoryDiscovery], [factoryFileLock]). If another codeaf serves, this
// engine uses that factory and says so (authority other_codeaf); it opens no
// second service. If this engine wins, it serves on 127.0.0.1 with a bearer
// made for this launch and held in memory, and publishes the address through
// the same discovery record, so a terminal started later joins it.
//
// SETUP, START AND A COLD RESUME START NOTHING. Until the explicit run below
// the host is quiet ([appFactoryHost.quiet]): no triage, which calls a model,
// no GitHub poll, no intake and no runner: no job starts by itself, and none
// runs. Setup reads issues into Backlog; a person may queue one, which waits
// for the runner. Only the explicit run below ends the quiet.
//
// ONLY AN EXPLICIT RUN STARTS A RUNNER. factory.local_run is the one door that
// starts this engine's worker loop ([appFactoryHost.workers]) on the factory it
// serves: the loop the terminal's windows and the app's team connections
// already use, behind the same worker lock (one runner per home). It is not
// started at launch, by factory.local_start, or by setup, and it is refused
// when another codeaf serves the factory (that codeaf's own runner takes its
// jobs). Its model and its key are the engine's own: the app's keys in memory
// ([config.AppCredentials]) and the profile's model settings, the same road
// the worker has always read.
//
// IN THIS MAC'S OWN FACTORY THE RUNNER IS ON WHENEVER THE FACTORY IS, AND IT
// STARTS NOTHING BY ITSELF. Once the factory this engine serves knows whose
// GitHub it is, [appBackend.ensureLocalRunner] starts the runner and
// [appFactoryHost.pollLoop]: the GitHub poll, so issues filed after setup
// reach Backlog. That happens on the start, on a resume, after a setup step and
// on a status read, so a person who set up a factory never has a second switch
// to find. A job runs only after a person pressed Run (factory.job_start): an
// arriving issue starts nothing, the repository's start rules launch nothing
// here, and no model reads an item on its own (no triage), so a runner that is
// on spends only on work a person started. factory.local_run is the same start,
// asked for, and says when GitHub is not connected yet. Quitting stops the
// runner and the poll with the host; a host made later in the same process
// starts its own. An engine that joined another codeaf's service never runs or
// reads for it.
//
// QUITTING ENDS THE SERVICE, NOT THE CHOICE. [appBackend.closeLocalFactory] runs
// FIRST in the engine's Close, whichever way the engine leaves (a quit, the
// app's pipe closing, the app's process ending, a signal: appleave.go), so no
// wait on a conversation can keep this machine's factory from the next engine.
// The runner is told to stop and waited for a bounded while
// ([appLocalStopWait]), then the listener, the service, serve.json, the
// discovery socket and serve.lock all go, and the held bearer with them. A job
// the runner held goes back to the queue when its lease expires, as it does
// when any worker is killed. The registry row stays, joined service or served:
// it is the logical record of this machine's factory under the one stable id
// [appLocalID], and it holds no credential. Only [appFactoryRow.Resume], set
// when this engine served the factory on a person's explicit start, makes the
// next launch serve it again; a row that joined another codeaf's service, or
// that an earlier release left, stays listed and unreachable until a start.
// Detaching is the one removal.
//
// OPENING THE APP RESUMES IT. [appBackend.resumeLocal] runs before
// factory.list, factory.local_status or a read of that connection is answered.
// It serves the same floor again on a new port, under the same id, with a new
// bearer, and its runner and poll come back with it; no job, triage or model
// call starts. A profile with no Resume row stays unstarted; detaching the
// row ends the choice; and another codeaf that holds the election is never
// joined or duplicated by a resume: the list says so ([appLocalHeldElsewhere])
// and the first list after it lets go serves the factory here. A profile whose
// release predates the row's Resume mark keeps nothing to resume from, and
// starts again with factory.local_start.
//
// WHAT IS PRIVATE STAYS HERE. The bearer, the factory folder, the profile and
// the app's home are never answered. A folder the person chose is refused if
// it lies inside any of them, and is answered only as "~/…". GitHub custody is
// the factory's and the app's: a pasted token reaches config.AppCredentials
// (or the profile when there is no app) exactly as the terminal's setup does
// it, and is registered with the redactors before it is sent. The app keeps it
// in its keychain and hands it back with keys.set at every launch; a yes to gh
// is kept in the factory's profile instead, because gh keeps its own token.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/intake"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/trace"
)

var _ appwire.FactoryLocal = (*appBackend)(nil)

const (
	// appSetupRoute is the one route setup is spoken on; "local" is the id the
	// factory keeps its own first run under.
	appSetupRoute = "/v1/factory-setup/local"
	// appLocalName is how the local factory is named before it knows whose
	// GitHub it is; after, the list says `<login>'s factory` (localFactoryName),
	// and a name the person gave it wins over both.
	appLocalName = "Your factory"
	// appLocalHeaderWait bounds a slow client of the loopback service.
	appLocalHeaderWait = 5 * time.Second
	// appLocalStopWait bounds how long quitting waits for the runner's loop to
	// wind down before the service goes anyway.
	appLocalStopWait = 10 * time.Second
)

// appLocalFactory is the factory this engine hosts or joined. The zero value
// is "not started".
type appLocalFactory struct {
	mu        sync.Mutex
	closed    bool
	running   bool
	authority string
	id        string
	origin    string
	host      *appFactoryHost
	server    *http.Server
	// runner is true once factory.local_run started the worker loop, and
	// runnerDone closes when that loop has returned.
	runner     bool
	runnerDone chan struct{}
	// githubHTTP, rules, seamHook and parent are test seams set before the
	// first start. parent, when set, is the conversation config the runner
	// opens its stages from, as a terminal launch hands one to its worker.
	githubHTTP *http.Client
	rules      func(ctx context.Context, repo, dir string) (pipeline.File, error)
	seamHook   func(factory.Seam) factory.Seam
	parent     func() session.Config
	// pollSource replaces the poll's GitHub source (appFactoryHost.pollSource).
	pollSource func(ctx context.Context) *factorygithub.Source
	newIntake  func(*store.Store) *intake.Intake
}

// ── start and status ───────────────────────────────────────────────────────

func (b *appBackend) FactoryLocalStart(ctx context.Context, _ string) (appwire.FactoryLocalStatus, error) {
	if err := b.startLocal(ctx); err != nil {
		return appwire.FactoryLocalStatus{}, err
	}
	b.ensureLocalRunner(ctx)
	return b.localStatus(), nil
}

// startLocal serves, or joins, the machine's factory under the local lock.
func (b *appBackend) startLocal(ctx context.Context) error {
	l := &b.local
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return appwire.Errorf(appwire.CodeClosed, "the engine is stopping")
	}
	if !l.running || l.host == nil {
		if l.running {
			// An explicit start rechecks the other authority and its current key.
			b.factoryHub().releaseLocal(l.id)
			l.running = false
		}
		return b.startLocalLocked(ctx, false)
	}
	// Reconnecting also restores authentication after an explicit detach.
	return b.joinLocalLocked(factoryTarget{l.host.address, l.host.token}, appwire.LocalAuthorityApp)
}

// localStatus is [appBackend.localStatusLocked] under the local lock.
func (b *appBackend) localStatus() appwire.FactoryLocalStatus {
	l := &b.local
	l.mu.Lock()
	defer l.mu.Unlock()
	return b.localStatusLocked()
}

// localRunnerOn says whether this engine's runner is on.
func (b *appBackend) localRunnerOn() bool {
	l := &b.local
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.runner
}

// resumeLocal brings back the factory a person chose to serve on this machine,
// and only that. It is called before the list or a read of the local
// connection is answered, so the row and the held bearer name this launch's
// service rather than the last one's.
//
// THE CHOICE IS THE SAVED ROW. A profile that never ran factory.local_start has
// none, and nothing here starts for it; a row detached by the person is gone, so
// it cannot come back. Resuming serves the same floor under the same connection
// id, on whatever port is free, with its runner and poll ([ensureLocalRunner]);
// no job, triage or model call starts. It never joins another codeaf's
// service: if one holds the election, the row stays as saved and unreachable,
// deliberately, and the list says why ([appLocalHeldElsewhere]). Every list
// asks again, so once that holder lets go (it closes its service, or its
// process is gone and its receipt names a dead pid) the next list serves it.
func (b *appBackend) resumeLocal(ctx context.Context) {
	if b.resumeLocalServe(ctx) {
		b.ensureLocalRunner(ctx)
	}
}

// resumeLocalServe serves the saved factory again, and answers whether it did.
func (b *appBackend) resumeLocalServe(ctx context.Context) bool {
	l := &b.local
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || l.running {
		return false
	}
	rows, err := b.factoryHub().snapshot()
	if err != nil {
		return false
	}
	for _, row := range rows {
		if row.ID == appLocalID && row.Local && row.Resume {
			return b.startLocalLocked(ctx, true) == nil
		}
	}
	return false
}

// appLocalHeldElsewhere is the reason factory.list gives the local row while
// a resume is refused because another live codeaf holds the election. The
// commonest such holder is an earlier engine of this same app still leaving.
const appLocalHeldElsewhere = "Another codeaf on this machine is still running this factory. It comes back here once that one stops."

// localHeldElsewhere says whether a live process other than this one holds
// this machine's factory election.
func localHeldElsewhere() bool {
	holder, ok := factoryServeHolder()
	return ok && holder.PID != os.Getpid()
}

// ensureLocalRunner starts the runner of the factory this engine serves once
// that factory knows whose GitHub it is, and does nothing otherwise: before
// GitHub is connected, for a factory another codeaf serves, after quitting, and
// when it is on already. It is cheap to ask again, and every road that may have
// just made the runner possible asks (the start, a resume, a setup step, a
// status read). It starts no job.
func (b *appBackend) ensureLocalRunner(ctx context.Context) {
	if b.localRunnerOn() {
		return
	}
	_, _ = b.FactoryLocalRun(ctx, "")
}

// FactoryLocalRun starts this engine's runner and its GitHub poll for the
// factory it serves, and only here: the same start [ensureLocalRunner] makes on
// its own, asked for. It is idempotent and starts no job; a job runs only once
// a person pressed Run, and the repository's start rules launch nothing.
func (b *appBackend) FactoryLocalRun(ctx context.Context, _ string) (appwire.FactoryLocalStatus, error) {
	id, err := b.localRunnable()
	if err != nil {
		return appwire.FactoryLocalStatus{}, err
	}
	// THE FACTORY REFUSES A WORKER WITH NO HANDLE, and a runner that is refused
	// retries in silence for as long as the engine lives, so a person who has
	// not connected GitHub is told so here instead of waiting on a job that
	// can never start.
	var me api.Me
	if err := b.factoryHub().read(ctx, id, "/v1/me", appFactorySmall, &me); err != nil {
		return appwire.FactoryLocalStatus{}, err
	}
	if strings.TrimSpace(me.Handle) == "" {
		return appwire.FactoryLocalStatus{}, appwire.Errorf(appwire.CodeConflict, "connect GitHub before turning the runner on: the factory needs to know whose jobs these are")
	}
	return b.startLocalRunner()
}

// localRunnable is the id of the local factory when this engine serves it and
// may run its jobs, or why not.
func (b *appBackend) localRunnable() (string, error) {
	l := &b.local
	l.mu.Lock()
	defer l.mu.Unlock()
	switch {
	case l.closed:
		return "", appwire.Errorf(appwire.CodeClosed, "the engine is stopping")
	case !l.running:
		return "", appwire.Errorf(appwire.CodeNotFound, "the factory on this machine is not started")
	case l.host == nil:
		return "", appwire.Errorf(appwire.CodeConflict, "another codeaf serves this machine's factory; its runner takes the jobs")
	}
	return l.id, nil
}

// startLocalRunner starts the worker loop once, and answers the status.
func (b *appBackend) startLocalRunner() (appwire.FactoryLocalStatus, error) {
	l := &b.local
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || !l.running || l.host == nil {
		return appwire.FactoryLocalStatus{}, appwire.Errorf(appwire.CodeClosed, "the engine is stopping")
	}
	if !l.runner {
		host := l.host
		done := make(chan struct{})
		l.runner, l.runnerDone = true, done
		// THE HOST SERVES THIS FACTORY (checked above), so its GitHub poll may
		// start; the host starts it once. No triage runs beside it, and its
		// start rules run only while the person's "Start on its own" is on
		// (appFactoryHost.startOnItsOwn): off, nothing here starts a job.
		host.pollLoop()
		guard.Go("app/factory local runner", func() { defer close(done); host.workers() })
	}
	return b.localStatusLocked(), nil
}

func (b *appBackend) FactoryLocalStatus(ctx context.Context, _ string) (appwire.FactoryLocalStatus, error) {
	b.resumeLocal(ctx)
	b.ensureLocalRunner(ctx)
	return b.localStatus(), nil
}

func (b *appBackend) localStatusLocked() appwire.FactoryLocalStatus {
	l := &b.local
	out := appwire.FactoryLocalStatus{State: appwire.LocalStopped, Attachment: b.localAttachment()}
	if l.running && !l.closed {
		out.State, out.Authority, out.Factory, out.URL, out.Name, out.Runner = appwire.LocalRunning, l.authority, l.id, l.origin, appLocalName, l.runner
	}
	return out
}

// localProfile is the profile the factory reads GitHub through: the app's own,
// and the app's home when it has none. It is never the ambient environment's.
func (b *appBackend) localProfile() string { return factoryProfile(b.profileDir) }

// startLocalLocked serves or joins the machine's factory. resume is a start the
// person did not just ask for: it serves our own factory or nothing, and never
// joins another codeaf's.
func (b *appBackend) startLocalLocked(ctx context.Context, resume bool) error {
	l := &b.local
	if target, ok := liveFactoryDiscovery(); ok {
		if resume {
			return appwire.Errorf(appwire.CodeConflict, "another codeaf serves this machine's factory")
		}
		return b.joinLocalLocked(target, appwire.LocalAuthorityOther)
	}
	floor, err := store.Open(v3FactoryRoot())
	if err != nil {
		return appwire.Errorf(appwire.CodeEngine, "the factory folder could not be opened")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return appwire.Errorf(appwire.CodeEngine, "the factory could not listen on this machine")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		listener.Close()
		return appwire.Errorf(appwire.CodeEngine, "the factory could not make its key")
	}
	token := hex.EncodeToString(raw)
	redact.Register(token)
	trace.Secret(token)
	hostCtx, cancel := context.WithCancel(context.Background())
	// done is closed already: a quiet host runs no worker loop to wait for.
	done := make(chan struct{})
	close(done)
	address := "http://" + listener.Addr().String()
	host := &appFactoryHost{ctx: hostCtx, cancel: cancel, done: done, floor: floor, address: address, token: token, workspace: home.Dir(), profile: b.localProfile(), credentials: newFactorySessions(), quiet: true, githubHTTP: l.githubHTTP, rules: l.rules, seamHook: l.seamHook, parent: l.parent, pollSource: l.pollSource, newIntake: l.newIntake,
		// A runner, once asked for, serves THIS factory with this launch's own bearer.
		defaultTarget: func() (factoryTarget, error) { return factoryTarget{address, token}, nil }}
	floor.SetOutputPolicy(appFactoryOutputPolicy(host.profile))
	mount, err := host.mountSolo(ctx)
	if err != nil {
		listener.Close()
		host.close()
		return appwire.Errorf(appwire.CodeEngine, "the factory on this machine could not start")
	}
	if host.handler == nil {
		// Another codeaf won the election while this one was looking: it is the
		// authority, and this engine's listener has nothing to serve.
		listener.Close()
		host.close()
		if resume {
			return appwire.Errorf(appwire.CodeConflict, "another codeaf serves this machine's factory")
		}
		target, err := waitForFactoryDiscovery(factoryLockWait)
		if err != nil {
			return appwire.Errorf(appwire.CodeUnreachable, "another codeaf holds this machine's factory but does not answer")
		}
		return b.joinLocalLocked(target, appwire.LocalAuthorityOther)
	}
	// EVERY REQUEST LIVES UNDER THE HOST: quitting cancels the host first, so a
	// request held open (a runner's long poll) ends at once and the graceful
	// shutdown in [appBackend.closeLocalFactory] waits only for real work.
	server := &http.Server{Handler: mount, ReadHeaderTimeout: appLocalHeaderWait, BaseContext: func(net.Listener) context.Context { return hostCtx }}
	guard.Go("app/factory local", func() { _ = server.Serve(listener) })
	l.host, l.server = host, server
	if err := b.joinLocalLocked(factoryTarget{host.address, token}, appwire.LocalAuthorityApp); err != nil {
		server.Close()
		host.close()
		l.host, l.server = nil, nil
		return err
	}
	return nil
}

// joinLocalLocked records the factory at target as this engine's local one and
// shows it in the registry, with its bearer in memory only.
func (b *appBackend) joinLocalLocked(target factoryTarget, authority string) error {
	l := &b.local
	origin, err := appFactoryOrigin(target.url)
	if err != nil {
		return appwire.Errorf(appwire.CodeEngine, "the factory on this machine has no usable address")
	}
	h := b.factoryHub()
	// THE ID IS THE MACHINE'S, NOT THE ADDRESS'S: the port is new at every
	// launch, and the tabs a person left open name this id.
	id := appLocalID
	if authority != appwire.LocalAuthorityApp {
		if knownID, known, _, err := h.admitAttach(origin); err != nil {
			return err
		} else if known {
			id = knownID
		}
	}
	if err := h.showLocal(id, origin, authority == appwire.LocalAuthorityApp); err != nil {
		return err
	}
	h.hold(id, target.token, 0)
	l.running, l.authority, l.id, l.origin = true, authority, id, origin
	return nil
}

// showLocal puts the local factory's row in the list at this launch's address,
// and forgets the stale rows an earlier release left. A STALE ROW IS ONE
// POSITIVELY MARKED [appFactoryRow.Local] AND NOT THIS FACTORY'S: a row a person
// attached by address, whatever its mode, is never touched, and neither is its
// session. owned records the person's choice to have this engine serve the
// factory again at the next launch ([appFactoryRow.Resume]); joining another
// codeaf's service neither sets nor clears it.
func (h *appFactoryHub) showLocal(id, origin string, owned bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.load(); err != nil {
		return err
	}
	for _, row := range h.rows {
		if row.URL != origin || row.Local {
			continue
		}
		if !owned && row.ID == id {
			return nil // explicit use of this authority keeps the manual connection unchanged
		}
		return appwire.Errorf(appwire.CodeConflict, "this address is already attached; disconnect that saved connection before starting the local factory")
	}
	kept := h.rows[:0:0]
	known := false
	for _, row := range h.rows {
		if row.Local && row.ID != id {
			delete(h.sessions, row.ID)
			continue
		}
		if row.ID == id {
			known = true
			row.URL, row.Local, row.Resume = origin, true, row.Resume || owned
		}
		kept = append(kept, row)
	}
	if !known {
		kept = append(kept, appFactoryRow{ID: id, URL: origin, Name: appLocalName, Mode: "solo", Local: true, Resume: owned})
	}
	h.rows = kept
	return h.save()
}

// releaseLocal lets go of the bearer held for the local connection when its
// service ends. THE ROW STAYS, WHATEVER ITS RESUME: it is the logical record of
// the machine's factory under its one id and holds no credential, Resume alone
// decides whether the next launch serves it again, and detaching it
// ([appBackend.FactoryDetach]) is the one thing that removes it.
func (h *appFactoryHub) releaseLocal(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.sessions, id)
}

// closeLocalFactory ends what start began. It is idempotent and runs before
// the registry is closed, so the bearer it held is let go with the service.
func (b *appBackend) closeLocalFactory() {
	server, host, id, running, runnerDone := b.takeLocal()
	if host != nil {
		// THE RUNNER STOPS FIRST, so no worker is dialling a service that is
		// going away; and it is waited for only so long.
		host.cancel()
		if runnerDone != nil {
			select {
			case <-runnerDone:
			case <-time.After(appLocalStopWait):
			}
		}
	}
	if server != nil {
		// A REQUEST ALREADY BEING ANSWERED FINISHES BEFORE THE FLOOR IS LET GO.
		// Close alone cuts the connections and returns while a handler may still
		// be writing (a runner attaching records it), which is a write after
		// close; Shutdown waits for it, as long as the runner is waited for.
		ctx, cancel := context.WithTimeout(context.Background(), appLocalStopWait)
		_ = server.Shutdown(ctx)
		cancel()
		_ = server.Close()
	}
	if host != nil {
		host.close()
	}
	if running && b.factories != nil {
		b.factories.releaseLocal(id)
	}
}

// takeLocal marks the local factory closed and hands over what it held.
func (b *appBackend) takeLocal() (server *http.Server, host *appFactoryHost, id string, running bool, runnerDone chan struct{}) {
	l := &b.local
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	server, host, id, running, runnerDone = l.server, l.host, l.id, l.running, l.runnerDone
	l.server, l.host, l.running, l.id, l.origin = nil, nil, false, "", ""
	l.runner, l.runnerDone = false, nil
	return
}

// ── the CLI's attachment ───────────────────────────────────────────────────

// localAttachment is what `codeaf attach` left, as the app may see it. An
// attachment is safe to show when its address is an origin the app would
// accept itself: https, or http on this machine. The bearer in attach.json is
// read only by [FactoryLocalAdopt] and is never part of this answer.
func (b *appBackend) localAttachment() appwire.FactoryLocalAttachment {
	a, ok := readFactoryAttachment()
	if !ok {
		return appwire.FactoryLocalAttachment{State: appwire.AttachmentNone}
	}
	origin, err := appFactoryOrigin(a.URL)
	if err != nil {
		return appwire.FactoryLocalAttachment{State: appwire.AttachmentUnsafe}
	}
	out := appwire.FactoryLocalAttachment{State: appwire.AttachmentAvailable, URL: origin, Name: a.Name}
	h := b.factoryHub()
	h.mu.Lock()
	defer h.mu.Unlock()
	// The id is the saved row's when this address is already listed (the local
	// factory's fixed id included), so adopting it again makes no second row.
	if h.load() == nil {
		out.Factory = h.idForLocked(origin)
		for _, row := range h.rows {
			if row.ID == out.Factory {
				out.State = appwire.AttachmentAdopted
			}
		}
	} else {
		out.Factory = appFactoryID(origin)
	}
	return out
}

func (b *appBackend) localClosed() bool {
	l := &b.local
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closed
}

func (b *appBackend) FactoryLocalAdopt(ctx context.Context, _ string) (appwire.FactoryLocalStatus, error) {
	if b.localClosed() {
		return appwire.FactoryLocalStatus{}, appwire.Errorf(appwire.CodeClosed, "the engine is stopping")
	}
	a, ok := readFactoryAttachment()
	if !ok {
		return appwire.FactoryLocalStatus{}, appwire.Errorf(appwire.CodeNotFound, "no codeaf attach on this machine")
	}
	if _, err := appFactoryOrigin(a.URL); err != nil {
		return appwire.FactoryLocalStatus{}, appwire.Errorf(appwire.CodeForbidden, "that attachment is not safe to use here: a factory on another machine needs https")
	}
	// FactoryAttach proves the address is a factory before saving it, keeps the
	// row without a credential, and holds the bearer in memory only.
	if _, err := b.FactoryAttach(ctx, a.URL, a.Token); err != nil {
		return appwire.FactoryLocalStatus{}, err
	}
	l := &b.local
	l.mu.Lock()
	defer l.mu.Unlock()
	return b.localStatusLocked(), nil
}

// ── the first run ──────────────────────────────────────────────────────────

// localRunning is the id of the local factory, or why setup cannot reach it.
func (b *appBackend) localRunning() (string, error) {
	l := &b.local
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return "", appwire.Errorf(appwire.CodeClosed, "the engine is stopping")
	}
	if !l.running {
		return "", appwire.Errorf(appwire.CodeNotFound, "the factory on this machine is not started")
	}
	return l.id, nil
}

func (b *appBackend) FactorySetupRead(ctx context.Context, _ string) (appwire.FactorySetupView, error) {
	id, err := b.localRunning()
	if err != nil {
		return appwire.FactorySetupView{}, err
	}
	var out api.FactorySetup
	if err := b.factoryHub().read(ctx, id, appSetupRoute, appFactoryBody, &out); err != nil {
		return appwire.FactorySetupView{}, err
	}
	return b.setupView(out), nil
}

func (b *appBackend) FactorySetupAct(ctx context.Context, _ string, act appwire.FactorySetupAct) (appwire.FactorySetupView, error) {
	id, err := b.localRunning()
	if err != nil {
		return appwire.FactorySetupView{}, err
	}
	req := api.FactorySetupRequest{Verb: act.Verb}
	switch act.Verb {
	case appwire.SetupVerbDiscover, appwire.SetupVerbBack, appwire.SetupVerbDone:
	case appwire.SetupVerbConnect:
		req.Source = act.Source
		if act.Source == appwire.SetupSourceToken {
			// Registered before it is sent, so no line it appears in can say it.
			redact.Register(act.Token)
			trace.Secret(act.Token)
			req.Token = act.Token
		}
	case appwire.SetupVerbSelect:
		req.RepoIDs = act.Repos
	case appwire.SetupVerbRetryRead, appwire.SetupVerbSkip:
		req.RepoID = act.Repo
	case appwire.SetupVerbClone, appwire.SetupVerbUseFolder:
		path, err := b.setupFolder(act.Folder)
		if err != nil {
			return appwire.FactorySetupView{}, err
		}
		req.RepoID, req.Path = act.Repo, path
	default:
		return appwire.FactorySetupView{}, appwire.Errorf(appwire.CodeBadParams, "that is not a setup step")
	}
	h := b.factoryHub()
	row, token, err := h.credentialed(ctx, id)
	if err != nil {
		return appwire.FactorySetupView{}, err
	}
	reply, err := h.send(ctx, row.URL, token, http.MethodPost, appSetupRoute, req, appFactoryBody, nil)
	if err != nil {
		return appwire.FactorySetupView{}, err
	}
	if reply.status < 200 || reply.status >= 300 {
		return appwire.FactorySetupView{}, appFactoryFailure(reply)
	}
	var out api.FactorySetup
	if jsonErr := json.Unmarshal(reply.body, &out); jsonErr != nil {
		return appwire.FactorySetupView{}, appwire.Errorf(appwire.CodeEngine, "the factory sent an answer this app cannot read")
	}
	// A STEP MAY HAVE JUST TOLD THE FACTORY WHOSE GITHUB THIS IS, which is all
	// its runner was waiting for.
	b.ensureLocalRunner(ctx)
	return b.setupView(out), nil
}

// ── what a person may be shown, and what they may name ─────────────────────

// privateRoots are the folders a checkout may never be made in or kept from:
// the app's home, the profile, and the factory's own floor.
func (b *appBackend) privateRoots() []string {
	roots := []string{home.Dir(), b.localProfile(), v3FactoryRoot()}
	out := roots[:0]
	for _, r := range roots {
		if r = strings.TrimSpace(r); r != "" {
			out = append(out, resolvedPath(filepath.Clean(r)))
		}
	}
	return out
}

// resolvedPath follows symlinks through the deepest part of p that exists, so
// a link cannot carry a checkout into a private folder.
func resolvedPath(p string) string {
	rest := ""
	for cur := p; ; cur = filepath.Dir(cur) {
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(real, rest)
		}
		if parent := filepath.Dir(cur); parent == cur {
			return p
		}
		rest = filepath.Join(filepath.Base(cur), rest)
	}
}

func setupWithin(p, root string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// setupFolder is the folder the person named as the absolute path the factory
// takes, or why it may not be used.
func (b *appBackend) setupFolder(raw string) (string, error) {
	path := strings.TrimSpace(raw)
	if path == "~" || strings.HasPrefix(path, "~/") {
		base, err := os.UserHomeDir()
		if err != nil {
			return "", appwire.Errorf(appwire.CodeEngine, "this machine has no home folder to put that in")
		}
		path = filepath.Join(base, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
	}
	if !filepath.IsAbs(path) {
		return "", appwire.Errorf(appwire.CodeBadParams, "folder must be a full path or start with ~/")
	}
	path = filepath.Clean(path)
	if path == filepath.Dir(path) {
		return "", appwire.Errorf(appwire.CodeForbidden, "that folder cannot hold a checkout")
	}
	real := resolvedPath(path)
	for _, root := range b.privateRoots() {
		if setupWithin(real, root) || setupWithin(path, root) {
			return "", appwire.Errorf(appwire.CodeForbidden, "that folder belongs to codeaf; choose one of your own")
		}
	}
	return path, nil
}

// shownFolder is path as a person is shown it: "~/…" under their home, only
// the last name elsewhere, and nothing at all inside a private root.
func (b *appBackend) shownFolder(path string) string {
	if path = strings.TrimSpace(path); path == "" {
		return ""
	}
	path = filepath.Clean(path)
	real := resolvedPath(path)
	for _, root := range b.privateRoots() {
		if setupWithin(real, root) || setupWithin(path, root) {
			return ""
		}
	}
	if base, err := os.UserHomeDir(); err == nil && setupWithin(path, base) {
		rel, _ := filepath.Rel(base, path)
		if rel == "." {
			return "~"
		}
		return "~/" + filepath.ToSlash(rel)
	}
	return ".../" + filepath.Base(path)
}

// setupView is the factory's setup state without anything private. A clone has
// no percentage because the factory reports none; Clone and CloneMB are all
// there is.
func (b *appBackend) setupView(in api.FactorySetup) appwire.FactorySetupView {
	out := appwire.FactorySetupView{State: in.State, Repos: make([]appwire.FactorySetupRepo, 0, len(in.Repos))}
	out.GitHub = appwire.FactorySetupGitHub{Handle: in.GitHub.Handle, Name: in.GitHub.Name, AvatarURL: in.GitHub.AvatarURL, Connected: in.GitHub.Connected, Organization: in.GitHub.Organization, VisibleRepos: in.GitHub.VisibleRepos}
	if in.GitHub.GH != nil {
		out.GitHub.GHLogin = in.GitHub.GH.Login
	}
	if in.GitHub.Error != nil {
		out.GitHub.Error = trimWords(*in.GitHub.Error)
	}
	for _, r := range in.Repos {
		repo := appwire.FactorySetupRepo{ID: r.ID, OpenIssues: r.OpenIssues, Archived: r.Archived, Selected: r.Selected, Read: r.Read, ReadCount: r.ReadCount, WaitingS: r.WaitingS, ResumesAt: r.ResumesAt, Clone: r.Clone, CloneMB: r.CloneMB, SuggestedFolder: b.shownFolder(r.SuggestedCheckout)}
		if r.Pipeline != nil {
			repo.Pipeline = *r.Pipeline
		}
		if r.Checkout != nil {
			repo.Folder = b.shownFolder(*r.Checkout)
		}
		if r.CloneError != nil {
			repo.CloneError = trimWords(*r.CloneError)
		}
		out.Repos = append(out.Repos, repo)
	}
	return out
}

// trimWords is a factory sentence with every registered secret taken out and
// at most 300 characters kept.
func trimWords(s string) string {
	s = strings.TrimSpace(redact.Secrets(s))
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}
