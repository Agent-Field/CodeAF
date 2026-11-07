package main

// ── codeaf app: THE FACTORY AS AN AGENTFIELD NODE ───────────────────────────
//
// The engine's half of appwire.FactoryAgentField, and WHEN the node runs. What
// the node is (its reasoners, its lease, its translators and what it reports)
// is internal/factory/agentfield's; this file only decides:
//
//   - the node runs while the factory is connected, from the host's mount (a
//     factory that starts already connected registers again, as a new
//     instance) and from a connect, and it stops on a disconnect and when the
//     host closes. NOT CONNECTED, NOTHING LISTENS;
//   - a job the node files is routed and started by the SAME intake pass the
//     host's read loops chose (the one the Jira poll runs after each read),
//     so AgentField work starts by itself exactly when GitHub work would. With
//     the loops off the job is filed and waits in Backlog for them;
//   - a github event wakes the host's own GitHub read (factorygithub.Nudge),
//     the same thing "Check GitHub now" does, and files nothing;
//   - a message reaches the job's manager through factory.job_send's own path.
//
// Wave 1 connects only the factory on this machine. `codeaf factory serve` (a
// team factory) has no wire door to connect it and does not run the node.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/agentfield"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/inbox"
)

// appAgentField is the host's AgentField state.
type appAgentField struct {
	once sync.Once
	node *agentfield.Node
	// http replaces the transport; tests only.
	http *http.Client
}

// agentFieldNode is the host's node over its floor and profile.
func (h *appFactoryHost) agentFieldNode() *agentfield.Node {
	h.agentfield.once.Do(func() {
		conn := &agentfield.Connection{Store: h.floor, Profile: h.profile, HTTP: h.agentfield.http}
		h.agentfield.node = &agentfield.Node{
			Conn:    conn,
			Store:   h.floor,
			After:   h.agentFieldAfter,
			Options: func() []inbox.Option { return []inbox.Option{h.jiraRecipe()} },
			Nudge:   func() { factorygithub.Nudge(h.floor) },
			Cancel:  h.agentFieldCancel,
			Version: agentfield.NodeVersion,
			Build:   buildinfo.String(),
		}
	})
	return h.agentfield.node
}

// agentFieldAfter is the intake pass the host's read loops chose, or nothing
// while they are off.
func (h *appFactoryHost) agentFieldAfter(ctx context.Context) error {
	if after := h.loopAfter(); after != nil {
		return after(ctx)
	}
	return nil
}

// loopAfter is the after-read intake the host's loops started with, nil
// while they are off. It is the Jira poll's own (appJiraLoop.after).
func (h *appFactoryHost) loopAfter() func(context.Context) error {
	h.loopMu.Lock()
	defer h.loopMu.Unlock()
	return h.jira.after
}

// startAgentField starts the node when the factory is connected. A start that
// fails is on the node's status as its last error; the factory goes on.
func (h *appFactoryHost) startAgentField() {
	node := h.agentFieldNode()
	if _, _, ok := node.Conn.Kept(); !ok || h.ctx.Err() != nil {
		return
	}
	_ = node.Start(h.ctx)
}

// agentFieldCancel is the Stop a cancel from AgentField makes of a job: the
// factory's own POST /v1/jobs/<n>/stop, with this host's access token, so it
// is recorded and undone exactly like a person's Stop.
func (h *appFactoryHost) agentFieldCancel(ctx context.Context, job int) error {
	if h.address == "" {
		return errors.New("the factory is not serving")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(h.address, "/")+jobPath(job, factoryVerbStop), strings.NewReader("{}"))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if h.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 400))
		return fmt.Errorf("the factory answered %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

// stopAgentField stops the node, telling the control plane it is offline.
func (h *appFactoryHost) stopAgentField() {
	if h.agentfield.node != nil {
		h.agentfield.node.Stop()
	}
}

// agentFieldHost is the host of this machine's factory when id names it and
// it is started, or why not, in words about AgentField.
func (b *appBackend) agentFieldHost(id string) (*appFactoryHost, error) {
	l := &b.local
	l.mu.Lock()
	defer l.mu.Unlock()
	switch {
	case l.closed:
		return nil, appwire.Errorf(appwire.CodeClosed, "the engine is stopping")
	case (!l.running || l.host == nil) && id == appLocalID:
		return nil, appwire.Errorf(appwire.CodeNotFound, "the factory on this machine is not started")
	case !l.running || l.host == nil || id != l.id:
		return nil, appwire.Errorf(appwire.CodeNotFound, "AgentField connects only to the factory on this machine")
	}
	return l.host, nil
}

// localFactoryID is the id this machine's factory answers to now.
func (b *appBackend) localFactoryID() string {
	b.local.mu.Lock()
	defer b.local.mu.Unlock()
	return b.local.id
}

var _ appwire.FactoryAgentField = (*appBackend)(nil)

// registerWait is how long a connect waits for the first registration's
// answer, inside the wire's own budget.
const registerWait = 10 * time.Second

func (b *appBackend) FactoryAgentFieldConnect(ctx context.Context, p appwire.FactoryAgentFieldConnectParams) (appwire.FactoryAgentFieldStatus, error) {
	host, err := b.agentFieldHost(p.Factory)
	if err != nil {
		return appwire.FactoryAgentFieldStatus{}, err
	}
	settings, err := agentfield.Clean(agentfield.Settings{URL: p.URL, NodeID: p.NodeID, BaseURL: p.BaseURL, Listen: p.Listen, Trust: factory.Tier(p.Trust)})
	if err != nil {
		return appwire.FactoryAgentFieldStatus{}, appwire.Errorf(appwire.CodeBadParams, "%s", err.Error())
	}
	node := host.agentFieldNode()
	if _, err := agentfield.Check(ctx, node.Conn.HTTP, settings.URL, p.APIKey); err != nil {
		return appwire.FactoryAgentFieldStatus{}, agentFieldWireError(err)
	}
	node.Stop()
	if err := node.Conn.Keep(settings, p.APIKey, p.InternalToken); err != nil {
		return appwire.FactoryAgentFieldStatus{}, agentFieldWireError(err)
	}
	if err := node.Start(host.ctx); err != nil {
		_ = node.Conn.Disconnect()
		return appwire.FactoryAgentFieldStatus{}, appwire.Errorf(appwire.CodeEngine, "%s", err.Error())
	}
	// THE ANSWER SAYS WHETHER THE CONTROL PLANE TOOK THE REGISTRATION: a
	// connect waits for the first one, and a refusal is the connect's.
	deadline := time.Now().Add(registerWait)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		h := node.Health()
		if !h.Registered.IsZero() {
			break
		}
		if h.LastError != "" {
			node.Stop()
			_ = node.Conn.Disconnect()
			return appwire.FactoryAgentFieldStatus{}, appwire.Errorf(appwire.CodeBadParams, "%s", h.LastError)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return agentFieldWireStatus(p.Factory, node), nil
}

func (b *appBackend) FactoryAgentFieldStatus(_ context.Context, id string) (appwire.FactoryAgentFieldStatus, error) {
	host, err := b.agentFieldHost(id)
	if err != nil {
		return appwire.FactoryAgentFieldStatus{}, err
	}
	return agentFieldWireStatus(id, host.agentFieldNode()), nil
}

func (b *appBackend) FactoryAgentFieldDisconnect(_ context.Context, id string) (appwire.FactoryAgentFieldStatus, error) {
	host, err := b.agentFieldHost(id)
	if err != nil {
		return appwire.FactoryAgentFieldStatus{}, err
	}
	node := host.agentFieldNode()
	node.Stop()
	node.Forget()
	if err := node.Conn.Disconnect(); err != nil {
		return appwire.FactoryAgentFieldStatus{}, agentFieldWireError(err)
	}
	return agentFieldWireStatus(id, node), nil
}

func (b *appBackend) FactoryAgentFieldTriggers(ctx context.Context, id string) (appwire.FactoryAgentFieldTriggers, error) {
	client, settings, err := b.agentFieldClient(id)
	if err != nil {
		return appwire.FactoryAgentFieldTriggers{}, err
	}
	triggers, err := client.Triggers(ctx, settings.NodeID)
	if err != nil {
		return appwire.FactoryAgentFieldTriggers{}, agentFieldWireError(err)
	}
	out := appwire.FactoryAgentFieldTriggers{Factory: id, NodeID: settings.NodeID, Triggers: []appwire.FactoryAgentFieldTrigger{}}
	for _, t := range triggers {
		// The control plane filters by node already; a trigger for another
		// node is never shown even if it did not.
		if t.TargetNodeID != settings.NodeID {
			continue
		}
		out.Triggers = append(out.Triggers, agentFieldWireTrigger(settings, t))
	}
	return out, nil
}

func (b *appBackend) FactoryAgentFieldSources(ctx context.Context, id string) (appwire.FactoryAgentFieldSources, error) {
	client, _, err := b.agentFieldClient(id)
	if err != nil {
		return appwire.FactoryAgentFieldSources{}, err
	}
	sources, err := client.Sources(ctx)
	if err != nil {
		return appwire.FactoryAgentFieldSources{}, agentFieldWireError(err)
	}
	out := appwire.FactoryAgentFieldSources{Factory: id, Sources: []appwire.FactoryAgentFieldSource{}}
	for _, s := range sources {
		out.Sources = append(out.Sources, appwire.FactoryAgentFieldSource{Name: s.Name, Kind: s.Kind, SecretRequired: s.SecretRequired, ConfigSchema: s.ConfigSchema})
	}
	return out, nil
}

func (b *appBackend) FactoryAgentFieldTriggerAdd(ctx context.Context, p appwire.FactoryAgentFieldTriggerAddParams) (appwire.FactoryAgentFieldTrigger, error) {
	client, settings, err := b.agentFieldClient(p.Factory)
	if err != nil {
		return appwire.FactoryAgentFieldTrigger{}, err
	}
	t, err := client.AddTrigger(ctx, agentfield.NewTrigger{SourceName: p.Source, Config: p.Config, SecretEnvVar: p.SecretEnvVar, TargetNodeID: settings.NodeID, TargetReasoner: agentfield.ReasonerIntake, EventTypes: p.EventTypes})
	if err != nil {
		return appwire.FactoryAgentFieldTrigger{}, agentFieldWireError(err)
	}
	return agentFieldWireTrigger(settings, t), nil
}

// agentFieldClient is a control plane client over the host's kept connection.
func (b *appBackend) agentFieldClient(id string) (*agentfield.Client, agentfield.Settings, error) {
	host, err := b.agentFieldHost(id)
	if err != nil {
		return nil, agentfield.Settings{}, err
	}
	client, settings, ok := host.agentFieldNode().Conn.Client()
	if !ok {
		return nil, agentfield.Settings{}, appwire.Errorf(appwire.CodeConflict, "this factory is not connected to AgentField")
	}
	return client, settings, nil
}

// agentFieldWireStatus is a node's status on the wire. It has no secret to
// carry: neither the settings nor the health hold one.
func agentFieldWireStatus(id string, node *agentfield.Node) appwire.FactoryAgentFieldStatus {
	st := node.Conn.Status()
	h := node.Health()
	out := appwire.FactoryAgentFieldStatus{Factory: id, Connected: st.Connected, URL: st.URL, NodeID: st.NodeID, BaseURL: st.BaseURL, Listen: st.Listen, Trust: string(st.Trust), KeyKept: st.KeyKept, TokenKept: st.TokenKept,
		Listening: h.Listening, Link: h.Link, LinkUps: h.LinkUps, LastEventVia: h.LastEventVia, LeaseSeconds: h.LeaseSeconds, LastEventSource: h.LastEventSource, LastEventWhat: h.LastEventWhat, LastError: h.LastError, Executions: len(node.Open())}
	out.LinkSince = wireTime(h.LinkSince)
	out.RegisteredAt = wireTime(h.Registered)
	out.LeaseUntil = wireTime(h.LeaseUntil)
	out.LastEvent = wireTime(h.LastEvent)
	out.LastErrorAt = wireTime(h.LastErrorAt)
	return out
}

func wireTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func agentFieldWireTrigger(s agentfield.Settings, t agentfield.Trigger) appwire.FactoryAgentFieldTrigger {
	return appwire.FactoryAgentFieldTrigger{ID: t.ID, Source: t.SourceName, Reasoner: t.TargetReasoner, EventTypes: t.EventTypes, Enabled: t.Enabled, ManagedBy: t.ManagedBy, SecretEnvVar: t.SecretEnvVar,
		IngestURL: strings.TrimRight(s.URL, "/") + "/sources/" + t.ID, Events24h: t.EventCount24h, Failed24h: t.Failed24h, LastEventAt: t.LastEventAt}
}

// agentFieldWireError says a refused call in the wire's codes: a control
// plane that never answered is unreachable, this machine failing to keep the
// answer is the engine's, and everything else (a wrong key, a refused
// trigger) is the request's.
func agentFieldWireError(err error) error {
	switch {
	case errors.Is(err, agentfield.ErrUnreachable):
		return appwire.Errorf(appwire.CodeUnreachable, "%s", err.Error())
	case errors.Is(err, agentfield.ErrNotKept):
		return appwire.Errorf(appwire.CodeEngine, "%s", err.Error())
	}
	return appwire.Errorf(appwire.CodeBadParams, "%s", err.Error())
}
