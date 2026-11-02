package main

import (
	"errors"
	"reflect"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// THE RENDER DOOR of the desktop app: render.list, render.get and render.cancel
// (appwire/render.go), answered from the engine's render ledger. The engine is
// asked through the conversation when this process holds it, and the ledger file
// is read directly when it does not. A conversation that cannot answer is
// reported as unsupported — never as an empty list.

// appRenderEngine is the conversation half: a connection that is not the
// engine's own session (an older host) does not have it.
type appRenderEngine interface {
	Renders(limit int, cursor string) (session.RenderPage, error)
	Render(id string) (session.RenderRecord, error)
	CancelRender(id string) (session.RenderCancel, error)
}

func appRenderOf(r session.RenderRecord) appwire.Render {
	return appwire.Render{
		ID: r.ID, Job: r.Job, Session: r.Session, Kind: r.Kind, State: string(r.State), Model: r.Model,
		Started: r.Started, Ended: r.Ended, DurationMS: r.DurationMS, Run: r.Run, Call: r.Call,
		Committed: r.Committed, Path: r.Path, Charge: string(r.Charge), CostUSD: r.CostUSD,
		Upstream: r.Upstream, Error: r.Error, Persistence: r.Persistence, Refusal: r.Refusal, Options: append([]string(nil), r.Options...),
	}
}

func appRenderError(err error) error {
	switch {
	case errors.Is(err, session.ErrRenderNotFound):
		return appwire.Errorf(appwire.CodeNotFound, "there is no such render")
	case errors.Is(err, session.ErrRenderBusy):
		return appwire.Errorf(appwire.CodeBusy, "the render history is too large to search right now; try again")
	case errors.Is(err, session.ErrRenderCursor):
		return appwire.Errorf(appwire.CodeBadParams, "the cursor is not valid; list again from the start")
	}
	return appEngineError(err)
}

// renderEngine is the live conversation's engine half, or nil when this process
// does not hold the conversation open. unsupported is true when it is held but
// cannot answer for renders.
func (b *appBackend) renderEngine(a *appAgent) (engine appRenderEngine, unsupported bool) {
	live := b.liveOf(a)
	if live == nil {
		return nil, false
	}
	engine, ok := live.conn.Conv.(appRenderEngine)
	return engine, !ok
}

func (b *appBackend) renderDir(a *appAgent) string {
	var transcript string
	b.locked(func() { transcript = a.transcript })
	if transcript == "" {
		return ""
	}
	return session.DirOf(transcript)
}

// ListRenders pages the conversation's whole render history, newest first.
func (b *appBackend) ListRenders(agent, cursor string, limit int) (appwire.RenderPage, error) {
	a, err := b.findCrewOwner(agent)
	if err != nil {
		return appwire.RenderPage{}, err
	}
	var page session.RenderPage
	engine, unsupported := b.renderEngine(a)
	switch {
	case unsupported:
		return appwire.RenderPage{}, appwire.Errorf(appwire.CodeUnknownMethod, "this host does not serve render.list")
	case engine != nil:
		page, err = engine.Renders(limit, cursor)
	default:
		page, err = session.ReadRenders(b.renderDir(a), limit, cursor)
	}
	if err != nil {
		return appwire.RenderPage{}, appRenderError(err)
	}
	out := appwire.RenderPage{Renders: make([]appwire.Render, 0, len(page.Renders)), Next: page.Next, Incomplete: page.Incomplete}
	for _, r := range page.Renders {
		out.Renders = append(out.Renders, appRenderOf(r))
	}
	return out, nil
}

// GetRender answers one render by its stable id from the whole history.
func (b *appBackend) GetRender(agent, id string) (appwire.Render, error) {
	a, err := b.findCrewOwner(agent)
	if err != nil {
		return appwire.Render{}, err
	}
	var record session.RenderRecord
	engine, unsupported := b.renderEngine(a)
	switch {
	case unsupported:
		return appwire.Render{}, appwire.Errorf(appwire.CodeUnknownMethod, "this host does not serve render.get")
	case engine != nil:
		record, err = engine.Render(id)
	default:
		record, err = session.ReadRender(b.renderDir(a), id)
	}
	if err != nil {
		return appwire.Render{}, appRenderError(err)
	}
	return appRenderOf(record), nil
}

// CancelRender stops a running render. It needs the conversation held: a render
// can only be running in a process that holds it.
func (b *appBackend) CancelRender(agent, id string) (appwire.RenderCancel, error) {
	a, err := b.findCrewOwner(agent)
	if err != nil {
		return appwire.RenderCancel{}, err
	}
	engine, unsupported := b.renderEngine(a)
	switch {
	case unsupported:
		return appwire.RenderCancel{}, appwire.Errorf(appwire.CodeUnknownMethod, "this host does not serve render.cancel")
	case engine == nil:
		// Not held here: nothing in this process is running it. Say what the
		// history says rather than pretend a stop happened.
		record, readErr := session.ReadRender(b.renderDir(a), id)
		if readErr != nil {
			return appwire.RenderCancel{}, appRenderError(readErr)
		}
		return appwire.RenderCancel{Outcome: string(session.RenderOutcomeEnded), Render: appRenderOf(record)}, nil
	}
	result, err := engine.CancelRender(id)
	if err != nil {
		return appwire.RenderCancel{}, appRenderError(err)
	}
	return appwire.RenderCancel{Outcome: string(result.Outcome), Render: appRenderOf(result.Render)}, nil
}

// renderChanged forwards one render's record to the app as render.changed. The
// record replaces the one with that id; a late charge receipt is the same
// event with the same state, never a second completion.
func (b *appBackend) renderChanged(agent string, live *appLive, record *session.RenderRecord) {
	render := appRenderOf(*record)
	same := false
	b.locked(func() {
		if live.renders == nil {
			live.renders = map[string]appwire.Render{}
		}
		before, known := live.renders[render.ID]
		same = known && reflect.DeepEqual(before, render)
		live.renders[render.ID] = render
	})
	if !same {
		live.box.Event(appwire.EventRenderChanged, appwire.RenderChanged{Agent: agent, Render: render})
	}
}

var _ appwire.RenderController = (*appBackend)(nil)
