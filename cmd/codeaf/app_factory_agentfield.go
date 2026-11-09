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
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/agentfield"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/inbox"
	"github.com/Agent-Field/codeaf/internal/guard"
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
			Filed: func(job int) {
				if h.jobFiled != nil {
					h.jobFiled(job)
				}
			},
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

// agentFieldKeyMoved restarts this machine's node with an AgentField secret
// the app handed over after the factory started. THE DESK WAITS ONLY A FEW
// SECONDS FOR ITS KEYCHAIN AT LAUNCH, so a factory that starts connected can
// start with no key, and its registration is refused until one arrives; the
// node read its secrets when it started, so without this it would go on
// registering keyless until the next launch. Refresh is a no-op for a node
// already running on the secret handed over, so the launch's own hand-back
// (which arrives before the factory starts, or with what it holds) starts
// nothing twice. It runs off the wire's thread: stopping the old node tells
// the control plane it is offline, which may take a few seconds. A factory
// not started yet, or not connected, has nothing to restart; its own start
// reads the key.
func (b *appBackend) agentFieldKeyMoved(provider string) {
	switch provider {
	case config.AppAgentFieldProvider, config.AppAgentFieldTokenProvider, config.AppAgentFieldNodeProvider:
	default:
		return
	}
	if host := b.liveLocalHost(); host != nil {
		guard.Go("app/factory agentfield key", host.refreshAgentField)
	}
}

// liveLocalHost is this machine's factory host while it is started, or nil.
func (b *appBackend) liveLocalHost() *appFactoryHost {
	l := &b.local
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.running || l.closed {
		return nil
	}
	return l.host
}

// refreshAgentField starts the node again when its kept secrets changed.
func (h *appFactoryHost) refreshAgentField() {
	node := h.agentFieldNode()
	if _, _, ok := node.Conn.Kept(); !ok || h.ctx.Err() != nil {
		return
	}
	_ = node.Refresh(h.ctx)
	// A host that closed while the node restarted already stopped it once;
	// the node outlives no host.
	if h.ctx.Err() != nil {
		node.Stop()
	}
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

// FactoryAgentFieldTriggers is factory.agentfield_triggers: the triggers
// aimed at this factory's node, and the source catalog a new one is picked
// from, read together so every trigger can say what its source is.
func (b *appBackend) FactoryAgentFieldTriggers(ctx context.Context, id string) (appwire.FactoryAgentFieldTriggers, error) {
	client, settings, err := b.agentFieldClient(id)
	if err != nil {
		return appwire.FactoryAgentFieldTriggers{}, err
	}
	sources, err := client.Sources(ctx)
	if err != nil {
		return appwire.FactoryAgentFieldTriggers{}, agentFieldWireError(err)
	}
	triggers, err := client.Triggers(ctx, settings.NodeID)
	if err != nil {
		return appwire.FactoryAgentFieldTriggers{}, agentFieldWireError(err)
	}
	out := appwire.FactoryAgentFieldTriggers{Factory: id, NodeID: settings.NodeID, Sources: agentFieldWireSources(sources), Triggers: []appwire.FactoryAgentFieldTrigger{}}
	for _, t := range triggers {
		// The control plane filters by node already; a trigger for another
		// node is never shown even if it did not.
		if t.TargetNodeID != settings.NodeID {
			continue
		}
		out.Triggers = append(out.Triggers, agentFieldWireTrigger(settings, t, sources))
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
	return appwire.FactoryAgentFieldSources{Factory: id, Sources: agentFieldWireSources(sources)}, nil
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
	return agentFieldWireTrigger(settings, t, nil), nil
}

var _ appwire.FactoryAgentFieldTriggerAdder = (*appBackend)(nil)

// FactoryAgentFieldAddTrigger is factory.agentfield_add_trigger: one trigger
// on the control plane aimed at this factory's node and its intake reasoner,
// so the source's events become jobs. The source is looked up in the
// catalog first, so a name the control plane does not have, or a source that
// needs a signing secret and was given no variable for it, is refused in
// words before anything is made.
//
// THE CONFIG IS NEVER WRITTEN DOWN HERE: it goes to the control plane and
// nowhere else, and a refusal never quotes it.
func (b *appBackend) FactoryAgentFieldAddTrigger(ctx context.Context, p appwire.FactoryAgentFieldAddTriggerParams) (appwire.FactoryAgentFieldAddedTrigger, error) {
	none := appwire.FactoryAgentFieldAddedTrigger{}
	client, settings, err := b.agentFieldClient(p.Factory)
	if err != nil {
		return none, err
	}
	sources, err := client.Sources(ctx)
	if err != nil {
		return none, agentFieldWireError(err)
	}
	src, ok := agentFieldSourceNamed(sources, p.Source)
	switch {
	case !ok:
		return none, appwire.Errorf(appwire.CodeBadParams, "AgentField has no source named %q; its sources are %s", p.Source, agentFieldSourceNames(sources))
	case src.SecretRequired && p.SecretEnvVar == "":
		return none, appwire.Errorf(appwire.CodeBadParams, "%s needs a signing secret: name the control plane's environment variable that holds it (secret_env_var)", agentfield.SourceLabel(src.Name))
	}
	t, err := client.AddTrigger(ctx, agentfield.NewTrigger{SourceName: src.Name, Config: p.Config, SecretEnvVar: p.SecretEnvVar, TargetNodeID: settings.NodeID, TargetReasoner: agentfield.ReasonerIntake, EventTypes: p.EventTypes})
	if err != nil {
		return none, agentFieldWireError(err)
	}
	wire := agentFieldWireTrigger(settings, t, sources)
	out := appwire.FactoryAgentFieldAddedTrigger{Factory: p.Factory, Trigger: wire}
	if wire.WebhookURL != "" || t.SecretEnvVar != "" {
		out.Setup = &appwire.FactoryAgentFieldTriggerSetup{WebhookURL: wire.WebhookURL, SecretEnvVar: t.SecretEnvVar}
	}
	return out, nil
}

func agentFieldSourceNamed(sources []agentfield.Source, name string) (agentfield.Source, bool) {
	for _, s := range sources {
		if strings.EqualFold(s.Name, name) {
			return s, true
		}
	}
	return agentfield.Source{}, false
}

func agentFieldSourceNames(sources []agentfield.Source) string {
	if len(sources) == 0 {
		return "none"
	}
	names := make([]string, 0, len(sources))
	for _, s := range sources {
		names = append(names, s.Name)
	}
	return strings.Join(names, ", ")
}

// agentFieldWireSources is the catalog on the wire, each source with its
// label and the fields of its config.
func agentFieldWireSources(sources []agentfield.Source) []appwire.FactoryAgentFieldSource {
	out := make([]appwire.FactoryAgentFieldSource, 0, len(sources))
	for _, s := range sources {
		w := appwire.FactoryAgentFieldSource{Name: s.Name, Kind: s.Kind, SecretRequired: s.SecretRequired, ConfigSchema: s.ConfigSchema, Label: agentfield.SourceLabel(s.Name)}
		for _, f := range agentfield.ConfigFields(s.ConfigSchema) {
			w.ConfigFields = append(w.ConfigFields, appwire.FactoryAgentFieldConfigField{Name: f.Name, Label: f.Label, Secret: f.Secret})
		}
		out = append(out, w)
	}
	return out
}

// agentFieldClient is a control plane client over the host's kept connection.
func (b *appBackend) agentFieldClient(id string) (*agentfield.Client, agentfield.Settings, error) {
	host, err := b.agentFieldHost(id)
	if err != nil {
		return nil, agentfield.Settings{}, err
	}
	client, settings, ok := host.agentFieldNode().Conn.Client()
	if !ok {
		return nil, agentfield.Settings{}, appwire.Errorf(appwire.CodeConflict, "this organization is not connected to AgentField; connect it first")
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

// agentFieldWireTrigger is one trigger on the wire. sources is the catalog
// when it was read, which says whether anything posts to the trigger: a loop
// source (cron) is run by the control plane and has no webhook address. With
// no catalog the address is given, as ingest_url always was.
func agentFieldWireTrigger(s agentfield.Settings, t agentfield.Trigger, sources []agentfield.Source) appwire.FactoryAgentFieldTrigger {
	ingest := strings.TrimRight(s.URL, "/") + "/sources/" + t.ID
	out := appwire.FactoryAgentFieldTrigger{ID: t.ID, Source: t.SourceName, Reasoner: t.TargetReasoner, EventTypes: t.EventTypes, Enabled: t.Enabled, ManagedBy: t.ManagedBy, SecretEnvVar: t.SecretEnvVar,
		IngestURL: ingest, Events24h: t.EventCount24h, Failed24h: t.Failed24h, LastEventAt: t.LastEventAt,
		EntrySource: agentfield.SourceName(t.SourceName), Label: agentfield.SourceLabel(t.SourceName), WebhookURL: ingest, CreatedAt: t.CreatedAt}
	if src, ok := agentFieldSourceNamed(sources, t.SourceName); ok && src.Kind == "loop" {
		out.WebhookURL = ""
	}
	if out.CreatedAt != nil && out.CreatedAt.IsZero() {
		out.CreatedAt = nil
	}
	return out
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
