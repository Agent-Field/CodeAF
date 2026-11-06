package main

// ── codeaf app: THE FACTORY'S JIRA CONNECTION ───────────────────────────────
//
// The engine's half of appwire.FactoryJira, and the Jira poll that runs
// beside the GitHub one. The connection itself (what is kept, where, and how
// it is checked) is internal/factory/jira's; this file only decides WHEN the
// poll runs:
//
//   - only on a host whose read loops a person turned on ([appFactoryHost.readLoops]
//     or [appFactoryHost.pollLoop]), with the same after-poll intake those
//     loops chose, so Jira work starts by itself exactly when GitHub work
//     would;
//   - only while the factory is connected: NOT CONNECTED, NOTHING STARTS;
//   - again from the start whenever the person connects or disconnects, so a
//     new JQL or a forgotten token takes effect without a relaunch.
//
// The Jira answerer (inbox.RegisterReplier) follows the connection alone,
// not the loops: registered from the host's mount while connected, swapped
// out on a disconnect.
//
// Wave 1 connects only the factory on this machine: an attached factory keeps
// its own connections on the machine that serves it.

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/inbox"
	"github.com/Agent-Field/codeaf/internal/factory/jira"
	"github.com/Agent-Field/codeaf/internal/guard"
)

// appJiraLoop is the host's Jira state. after, stop and on are guarded by
// the host's loopMu, the same lock that starts and ends every read loop.
type appJiraLoop struct {
	once sync.Once
	conn *jira.Connection
	// after is the intake the host's read loops run after a poll; nil until
	// they started.
	after func(context.Context) error
	// stop ends the poll that is running, nil for none.
	stop context.CancelFunc
	// on counts the polls started, for tests.
	on int
	// http and every replace the transport and the interval; tests only.
	http  *http.Client
	every time.Duration
}

// jiraConnection is the host's connection over its floor and profile.
func (h *appFactoryHost) jiraConnection() *jira.Connection {
	h.jira.once.Do(func() {
		h.jira.conn = &jira.Connection{Store: h.floor, Profile: h.profile, HTTP: h.jira.http}
	})
	return h.jira.conn
}

// startJiraLocked (re)starts the Jira poll with after as its intake. The
// caller holds loopMu and has just turned the read loops on.
func (h *appFactoryHost) startJiraLocked(after func(context.Context) error) {
	h.jira.after = after
	h.restartJiraLocked()
}

// restartJira is a connect's or a disconnect's restart: it stops the poll
// that is running and starts one over the connection as it is now, and does
// nothing on a host whose read loops are off or closing.
func (h *appFactoryHost) restartJira() {
	h.loopMu.Lock()
	defer h.loopMu.Unlock()
	h.restartJiraLocked()
}

func (h *appFactoryHost) restartJiraLocked() {
	if h.jira.stop != nil {
		h.jira.stop()
		h.jira.stop = nil
	}
	h.syncJiraReplier()
	if !h.loopsOn || h.loopsClosed || h.ctx.Err() != nil || h.jira.after == nil {
		return
	}
	feed, ok := h.jiraConnection().Feed()
	if !ok {
		return
	}
	every := h.jira.every
	if every <= 0 {
		every = factoryPollEvery
	}
	ctx, cancel := context.WithCancel(h.ctx)
	h.jira.stop = cancel
	h.jira.on++
	after := h.jira.after
	h.loops.Add(1)
	guard.Go("app/factory jira poll", func() {
		defer h.loops.Done()
		inbox.Poll(ctx, feed, h.floor, every, after, h.jiraRecipe())
	})
}

// syncJiraReplier makes the answerer for Jira jobs match the connection:
// registered while connected, so a job's pull request is commented on the
// ticket whether or not the factory is reading Jira by itself, and
// unregistered once disconnected, so nothing answers for a site the person
// let go. It takes no lock of the host's, because the host's mount calls it
// under its own.
func (h *appFactoryHost) syncJiraReplier() {
	conn := h.jiraConnection()
	if _, ok := conn.Feed(); ok {
		inbox.RegisterReplier(jira.Source, conn.Replier())
		return
	}
	inbox.RegisterReplier(jira.Source, nil)
}

// jiraRecipe gives a ticket filed on a known repository that repository's
// steps, read through the floor's door exactly as POST /api/intake does
// (internal/factory/serve's intake). The door is looked up when a ticket
// arrives, never when the loop starts, because the loop can start while the
// host's mount holds the lock the door sits behind.
func (h *appFactoryHost) jiraRecipe() inbox.Option {
	return inbox.WithRecipe(func(repo string) (factory.Recipe, error) {
		seam := h.currentSeam()
		if seam.RecipeAt == nil {
			return factory.Recipe{}, errors.New("this factory reads no recipes")
		}
		recipe, _, _, err := seam.RecipeAt(repo)
		return recipe, err
	})
}

// jiraHost is the host of this machine's factory when id names it and it is
// started, or why not, in words about Jira.
func (b *appBackend) jiraHost(id string) (*appFactoryHost, error) {
	l := &b.local
	l.mu.Lock()
	defer l.mu.Unlock()
	switch {
	case l.closed:
		return nil, appwire.Errorf(appwire.CodeClosed, "the engine is stopping")
	case (!l.running || l.host == nil) && id == appLocalID:
		return nil, appwire.Errorf(appwire.CodeNotFound, "the factory on this machine is not started")
	case !l.running || l.host == nil || id != l.id:
		return nil, appwire.Errorf(appwire.CodeNotFound, "Jira connects only to the factory on this machine")
	}
	return l.host, nil
}

var _ appwire.FactoryJira = (*appBackend)(nil)

func (b *appBackend) FactoryJiraConnect(ctx context.Context, p appwire.FactoryJiraConnectParams) (appwire.FactoryJiraStatus, error) {
	host, err := b.jiraHost(p.Factory)
	if err != nil {
		return appwire.FactoryJiraStatus{}, err
	}
	st, err := host.jiraConnection().Connect(ctx, p.Site, p.Email, p.Token, p.JQL)
	if err != nil {
		return appwire.FactoryJiraStatus{}, jiraWireError(err)
	}
	host.restartJira()
	return jiraWireStatus(p.Factory, st), nil
}

func (b *appBackend) FactoryJiraStatus(_ context.Context, id string) (appwire.FactoryJiraStatus, error) {
	host, err := b.jiraHost(id)
	if err != nil {
		return appwire.FactoryJiraStatus{}, err
	}
	return jiraWireStatus(id, host.jiraConnection().Status()), nil
}

func (b *appBackend) FactoryJiraDisconnect(_ context.Context, id string) (appwire.FactoryJiraStatus, error) {
	host, err := b.jiraHost(id)
	if err != nil {
		return appwire.FactoryJiraStatus{}, err
	}
	conn := host.jiraConnection()
	err = conn.Disconnect()
	host.restartJira()
	if err != nil {
		return appwire.FactoryJiraStatus{}, jiraWireError(err)
	}
	return jiraWireStatus(id, conn.Status()), nil
}

// jiraWireStatus is a connection's status on the wire. It has no token to
// carry: jira.Status never holds one.
func jiraWireStatus(id string, st jira.Status) appwire.FactoryJiraStatus {
	out := appwire.FactoryJiraStatus{Factory: id, Connected: st.Connected, Site: st.Site, Email: st.Email, JQL: st.JQL, Account: st.Account, LastError: st.LastError}
	if !st.LastRead.IsZero() {
		at := st.LastRead
		out.LastRead = &at
	}
	return out
}

// jiraWireError says a refused connect in the wire's codes: Jira that never
// answered is unreachable, this machine failing to keep the answer is the
// engine's, and everything else (a wrong token, a JQL Jira refused, a site
// that is not a site) is the request's.
func jiraWireError(err error) error {
	switch {
	case errors.Is(err, jira.ErrUnreachable):
		return appwire.Errorf(appwire.CodeUnreachable, "%s", err.Error())
	case errors.Is(err, jira.ErrNotKept):
		return appwire.Errorf(appwire.CodeEngine, "%s", err.Error())
	}
	return appwire.Errorf(appwire.CodeBadParams, "%s", err.Error())
}
