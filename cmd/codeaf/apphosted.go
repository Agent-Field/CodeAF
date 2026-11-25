package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

type appHostedRef struct {
	org, session string
}

type appHostedOpen struct {
	ref  appHostedRef
	door *orgDoor
}

type appHostedLink struct {
	door   *orgDoor
	client *remote.Client
}

type appHostedRow struct {
	orgSession
	Owner  string `json:"owner"`
	Role   string `json:"role"`
	Access string `json:"access"`
	// Verbs is what the server's policy lets this person do in the chat, as it stood when the
	// list was read. The server is the only party that decides it.
	Verbs []string `json:"verbs"`
}

func (o appHostedOpen) dial(ask appOpenAsk) (*appConn, error) {
	socket, err := o.door.open(context.Background(), o.ref.session)
	if err != nil {
		return nil, appHostedDoorError(o.door, err)
	}
	// The HTTP upgrade and the conversation hello each need an end to their wait.
	_ = socket.SetDeadline(time.Now().Add(orgCallWithin))
	hello := remote.Hello{Launch: appLaunchShape(ask), Window: true}
	client, err := remote.Dial(socket, o.door.host, hello)
	if err != nil {
		_ = socket.Close()
		if words, refused := remote.RefusalSentence(err); refused {
			return nil, appHostedWords(appwire.CodeConflict, o.door, words)
		}
		return nil, appHostedDoorError(o.door, o.door.unreachable())
	}
	_ = socket.SetDeadline(time.Time{})
	conn := appConnOf(client, func() { _ = client.Close() }, false)
	// Paths in the welcome name the server's disk. The app must never open them.
	conn.Workspace, conn.Transcript = "", ""
	conn.hosted = &appHostedLink{door: o.door, client: client}
	return conn, nil
}

func (b *appBackend) hostedDoor(ctx context.Context, org string) (*orgDoor, error) {
	hub := b.factoryHub()
	if hub == nil {
		return nil, appwire.Errorf(appwire.CodeClosed, "codeaf is shutting down")
	}
	row, held, err := hub.lookup(org)
	if err != nil {
		return nil, appwire.Errorf(appwire.CodeBadParams, "There is no such organization connection.")
	}
	host := orgHostName(row.URL)
	if held == "" {
		return nil, appHostedSignIn(host)
	}
	// A hosted call must never start a local factory to obtain credentials.
	ctx = context.WithValue(ctx, appFactoryPassiveRead{}, true)
	row, token, err := hub.credentialed(ctx, org)
	if err != nil || token == "" {
		return nil, appHostedSignIn(host)
	}
	base, err := factoryURL(row.URL)
	if err != nil || (strings.HasPrefix(base, "http://") && !factoryLoopback(base)) {
		return nil, appwire.Errorf(appwire.CodeBadParams, "Use https:// for %s.", host)
	}
	return orgDoorWith(base, token, nil), nil
}

func appHostedSignIn(host string) error {
	return appwire.Errorf(appwire.CodeSignedOut, "Sign in to %s first.", host)
}

func appHostedWords(code string, door *orgDoor, text string) error {
	// Preserve the server's sentence when it carries no sign-in.
	if strings.Contains(text, door.token) {
		text = door.clean(text)
	}
	if text == "" {
		text = door.host + " could not answer."
	}
	return appwire.Errorf(code, "%s", text)
}

func appHostedDoorError(door *orgDoor, err error) error {
	var call *orgDoorError
	if errors.As(err, &call) {
		switch call.status {
		case 0:
			return appHostedWords(appwire.CodeUnreachable, door, call.words)
		case http.StatusUnauthorized, http.StatusForbidden:
			return appHostedSignIn(door.host)
		case http.StatusBadRequest:
			return appHostedWords(appwire.CodeBadParams, door, call.words)
		}
	}
	return appHostedWords(appwire.CodeConflict, door, err.Error())
}

func (link *appHostedLink) wireError(err error) error {
	if err == nil {
		return nil
	}
	if link.client.Err() != nil {
		return appHostedDoorError(link.door, link.door.unreachable())
	}
	return appHostedWords(appwire.CodeConflict, link.door, err.Error())
}

func (b *appBackend) RefuseHostedAction(org string) error {
	return appwire.Errorf(appwire.CodeBadParams, "That is not available for a chat on %s.", b.hostedHost(org))
}

func (b *appBackend) hostedHost(org string) string {
	if hub := b.factoryHub(); hub != nil {
		row, _, _ := hub.lookup(org)
		return orgHostName(row.URL)
	}
	return orgHostName("")
}

func (b *appBackend) hostedCredentialStillHeld(org string, door *orgDoor) error {
	row, token, err := b.factoryHub().lookup(org)
	switch {
	case err != nil || token == "":
		return appHostedSignIn(door.host)
	case token != door.token || !sameOrigin(door.base, row.URL):
		return appwire.Errorf(appwire.CodeConflict, "The sign-in to %s changed. Try again.", door.host)
	}
	return nil
}

// appHostedVerbs keeps the verbs this program knows, in their fixed order, from what the server
// said a person may do. A server that said nothing (an older build, or a chat the list did not
// carry) leaves view only, and that is safe because it only hides controls: the server's wire
// refuses every call the person has no right to whatever this window shows, so a missing verb
// can cost a button and never lends a right. Nothing here works a verb out from a role name,
// because the organization's roles and policy are its own to change.
func appHostedVerbs(said []string) []string {
	held := make([]string, 0, len(acl.Verbs))
	for _, verb := range acl.Verbs {
		if slices.Contains(said, verb) {
			held = append(held, verb)
		}
	}
	if len(held) == 0 {
		return []string{acl.VerbView}
	}
	return held
}

func appHostedFacts(ref appHostedRef, row appHostedRow, own bool, door *orgDoor) *appwire.HostedFacts {
	facts := &appwire.HostedFacts{Org: ref.org, Session: ref.session, Note: door.clean(row.Warning), Verbs: appHostedVerbs(row.Verbs)}
	if !own {
		facts.Owner, facts.Role = door.clean(row.Owner), door.clean(row.Role)
	}
	return facts
}

func appHostedRowView(org string, row appHostedRow, own bool, door *orgDoor) appwire.Agent {
	state := appwire.StateIdle
	if row.State == "stopped" {
		state = appwire.StateStopped
	}
	at := row.LastActive.Time
	if at.IsZero() {
		at = row.Created.Time
	}
	return appwire.Agent{
		ID: appwire.HostedAgentID(row.ID, org), State: state, UpdatedAt: at, LastActiveAt: row.LastActive.Time,
		Hosted: appHostedFacts(appHostedRef{org: org, session: row.ID}, row, own, door),
	}
}

func (b *appBackend) readHostedList(ctx context.Context, org string, door *orgDoor) (appwire.HostedListResult, error) {
	var list struct {
		Sessions []appHostedRow             `json:"sessions"`
		Shared   []appHostedRow             `json:"shared"`
		Hosting  map[string]json.RawMessage `json:"hosting"`
	}
	callErr := door.call(ctx, http.MethodGet, orgSessionsPath, &list)
	if callErr != nil {
		// With hosted chats off, the server has no list route.
		var refusal *orgDoorError
		if !errors.As(callErr, &refusal) || refusal.status != http.StatusNotFound || refusal.serverSaid {
			return appwire.HostedListResult{}, appHostedDoorError(door, callErr)
		}
	}
	if err := b.hostedCredentialStillHeld(org, door); err != nil {
		return appwire.HostedListResult{}, err
	}
	agents := make([]appwire.Agent, 0, len(list.Sessions)+len(list.Shared))
	for _, row := range list.Sessions {
		if appwire.ValidHostedSessionID(row.ID) && row.Access != "operator" {
			agents = append(agents, appHostedRowView(org, row, true, door))
		}
	}
	for _, row := range list.Shared {
		if appwire.ValidHostedSessionID(row.ID) && row.Access == "shared" {
			agents = append(agents, appHostedRowView(org, row, false, door))
		}
	}
	return appwire.HostedListResult{Agents: agents, Available: callErr == nil, Hosting: list.Hosting}, nil
}

func (b *appBackend) HostedList(org string) (appwire.HostedListResult, error) {
	ctx := context.Background()
	door, err := b.hostedDoor(ctx, org)
	if err != nil {
		return appwire.HostedListResult{}, err
	}
	defer door.http.CloseIdleConnections()
	result, err := b.readHostedList(ctx, org, door)
	if err != nil {
		return appwire.HostedListResult{}, err
	}
	agents := result.Agents
	slices.SortStableFunc(agents, func(a, b appwire.Agent) int { return b.UpdatedAt.Compare(a.UpdatedAt) })
	b.locked(func() {
		for i := range agents {
			row := &agents[i]
			a := b.hostedAgents[row.ID]
			if a == nil {
				continue
			}
			// The server has confirmed that this caller can read the chat.
			row.Title = a.view.Title
			if a.live != nil && a.live.conn.hosted.door.token == door.token && sameOrigin(a.live.conn.hosted.door.base, door.base) {
				a.view.Hosted = row.Hosted
				row.State = a.view.State
			}
		}
	})
	return result, nil
}

func (b *appBackend) findHosted(id string, ref appHostedRef) (*appAgent, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closing {
		return nil, appwire.Errorf(appwire.CodeClosed, "codeaf is shutting down")
	}
	if b.hostedAgents == nil {
		b.hostedAgents = make(map[string]*appAgent)
	}
	a := b.hostedAgents[id]
	if a == nil {
		a = &appAgent{
			hosted: &ref,
			view: appwire.Agent{
				ID: id, State: appwire.StateIdle,
				Hosted: &appwire.HostedFacts{Org: ref.org, Session: ref.session, Verbs: []string{acl.VerbView}},
			},
		}
		b.hostedAgents[id] = a
	}
	return a, nil
}

func (b *appBackend) createHostedAgent(ask appwire.CreateAsk) (appwire.Agent, error) {
	ctx := context.Background()
	door, err := b.hostedDoor(ctx, ask.Org)
	if err != nil {
		return appwire.Agent{}, err
	}
	defer door.http.CloseIdleConnections()
	if len(ask.Attachments) > 0 {
		return appwire.Agent{}, appHostedAttachments(door.host)
	}
	if err := (session.Submission{ClientID: ask.ClientID}).Check(); err != nil {
		return appwire.Agent{}, appEngineError(err)
	}
	if b.isClosing() {
		return appwire.Agent{}, appwire.Errorf(appwire.CodeClosed, "codeaf is shutting down")
	}
	var row appHostedRow
	if err := door.call(ctx, http.MethodPost, orgSessionsPath, &row); err != nil {
		return appwire.Agent{}, appHostedDoorError(door, err)
	}
	if !appwire.ValidHostedSessionID(row.ID) {
		return appwire.Agent{}, appwire.Errorf(appwire.CodeConflict, "%s sent back something this program cannot use.", door.host)
	}
	if err := b.hostedCredentialStillHeld(ask.Org, door); err != nil {
		return appwire.Agent{}, err
	}
	view := appHostedRowView(ask.Org, row, true, door)
	a, err := b.find(view.ID)
	if err != nil {
		return appwire.Agent{}, err
	}
	a.gate.Lock()
	defer a.gate.Unlock()
	if _, err := b.attachHosted(a, door, &view); err != nil {
		return appwire.Agent{}, err
	}
	if words := strings.TrimSpace(ask.Text); words != "" {
		if _, err := b.startLocked(a, words, ask.ClientID, appHostedSaid(words, ask.Said, "")); err != nil {
			return appwire.Agent{}, err
		}
	}
	b.locked(func() { view = a.view })
	return view, nil
}

func (b *appBackend) openHostedAgent(id string, warm bool) (appwire.Agent, error) {
	a, err := b.find(id)
	if err != nil {
		return appwire.Agent{}, err
	}
	a.gate.Lock()
	defer a.gate.Unlock()
	var view appwire.Agent
	b.locked(func() { view = a.view })
	if warm && view.State == appwire.StateClosed {
		return appwire.Agent{}, b.RefuseHostedAction(a.hosted.org)
	}
	if _, err := b.ensureHosted(a); err != nil {
		return appwire.Agent{}, err
	}
	b.locked(func() { view = a.view })
	return view, nil
}

// ensureHosted acquires a wire under the agent's gate and checks the held
// caller on every action so a changed sign-in cannot reuse a previous view.
func (b *appBackend) ensureHosted(a *appAgent) (*appLive, error) {
	var id string
	b.locked(func() { id = a.view.ID })
	door, err := b.hostedDoor(context.Background(), a.hosted.org)
	live := b.liveOf(a)
	if live != nil && (err != nil || live.conn.hosted.client.Err() != nil || live.conn.hosted.door.token != door.token || !sameOrigin(live.conn.hosted.door.base, door.base)) {
		b.release(a)
		live = nil
	}
	if err != nil {
		return nil, err
	}
	defer door.http.CloseIdleConnections()
	if live != nil {
		return live, nil
	}
	// An id can come from an earlier app process. The server supplies its current
	// sharing facts, and the wire itself still decides whether the chat opens.
	list, err := b.readHostedList(context.Background(), a.hosted.org, door)
	if err != nil {
		return nil, err
	}
	if !list.Available {
		return nil, b.RefuseHostedAction(a.hosted.org)
	}
	for _, row := range list.Agents {
		if row.ID == id {
			return b.attachHosted(a, door, &row)
		}
	}
	unknown := appwire.Agent{ID: id, Hosted: appHostedFacts(*a.hosted, appHostedRow{}, false, door)}
	return b.attachHosted(a, door, &unknown)
}

// attachHosted publishes a remote view without introducing any local ownership.
func (b *appBackend) attachHosted(a *appAgent, door *orgDoor, view *appwire.Agent) (*appLive, error) {
	if live := b.liveOf(a); live != nil {
		return live, nil
	}
	if b.isClosing() {
		return nil, appwire.Errorf(appwire.CodeClosed, "codeaf is shutting down")
	}
	ask := appOpenAsk{Hosted: &appHostedOpen{ref: *a.hosted, door: door}, ClockS: appProposedClockS}
	_ = b.state.read(func(f *appStateFile) { ask.ClockS = f.clock() })
	conn, err := b.open(ask)
	if err != nil {
		return nil, err
	}
	if err := b.hostedCredentialStillHeld(a.hosted.org, door); err != nil {
		conn.Release()
		return nil, err
	}

	live := b.newLive(conn, b.hostedEmit)
	live.lanesDone = nil
	startTitles := b.prepareTitles(a, live)
	title, model := conn.Conv.Title(), conn.Conv.Model()
	if welcome := conn.hosted.client.Welcome(); welcome.Facts == nil {
		// Older welcomes carry the chat's name and model directly.
		title, model = welcome.Title, welcome.Model
	}
	view.Title, view.Model = door.clean(title), door.clean(model)
	view.State, view.Context = appwire.StateIdle, appContextOf(conn.Conv)
	accepted := false
	b.locked(func() {
		if b.closing {
			return
		}
		a.view, a.live, a.users, a.marks = *view, live, conn.UserMessages, nil
		accepted = true
	})
	if !accepted {
		if live.stopTitles != nil {
			live.stopTitles()
		}
		conn.Release()
		live.box.Close()
		return nil, appwire.Errorf(appwire.CodeClosed, "codeaf is shutting down")
	}
	b.hostedEmit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: *view})
	startTitles()
	questions, stop := conn.Conv.WatchQuestions()
	live.stopAsks = stop
	live.stopWakes = b.watchWakes(a, live)
	live.stopPasses = b.watchPasses(a, live)
	guard.Go("app/hosted asks", func() {
		defer close(live.asksDone)
		for event := range questions {
			b.questionEvent(a, view.ID, live, event)
		}
		b.died(a, live)
	})
	return live, nil
}

func appHostedAttachments(host string) error {
	return appwire.Errorf(appwire.CodeBadParams, "Attachments cannot go to a chat on %s yet.", host)
}

func appHostedSaid(text string, said *string, from string) *appSending {
	if said == nil && from == "" {
		return nil
	}
	if said != nil {
		text = *said
	}
	return &appSending{text: text, from: from}
}

func (b *appBackend) sendHosted(id string, message appwire.AgentMessage) (uint64, error) {
	a, err := b.find(id)
	if err != nil {
		return 0, err
	}
	if len(message.Attachments) != 0 || message.Submission != nil {
		return 0, appHostedAttachments(b.hostedHost(a.hosted.org))
	}
	if len(message.Reads) != 0 {
		return 0, b.RefuseHostedAction(a.hosted.org)
	}
	if err := (session.Submission{ClientID: message.ClientID}).Check(); err != nil {
		return 0, appEngineError(err)
	}
	a.gate.Lock()
	defer a.gate.Unlock()
	if _, err := b.ensureHosted(a); err != nil {
		return 0, err
	}
	return b.startLocked(a, message.Text, message.ClientID, appHostedSaid(message.Text, message.Said, message.From))
}

func (b *appBackend) hostedEntries(a *appAgent, before string, limit int) (appwire.EntriesResult, error) {
	a.gate.Lock()
	defer a.gate.Unlock()
	live, err := b.ensureHosted(a)
	if err != nil {
		return appwire.EntriesResult{}, err
	}
	link := live.conn.hosted
	record, err := link.client.Agent().ReadHistory()
	if err != nil {
		return appwire.EntriesResult{}, link.wireError(err)
	}
	var id string
	var at time.Time
	marks := map[int]appTurnMark{}
	b.locked(func() {
		id, at = a.view.ID, a.view.UpdatedAt
		for n, mark := range a.marks {
			marks[n] = mark
		}
	})
	entries, more, err := appEntryPage(id, record, at, marks, before, limit, nil)
	return appwire.EntriesResult{Entries: entries, More: more}, err
}

func (b *appBackend) stopHosted(id string) error {
	a, err := b.find(id)
	if err != nil {
		return err
	}
	a.gate.Lock()
	defer a.gate.Unlock()
	live, err := b.ensureHosted(a)
	if err != nil {
		return err
	}
	link := live.conn.hosted
	b.locked(func() { live.stopping = live.running })
	if err := link.client.Agent().InterruptWithinErr(session.StopGrace); err != nil {
		b.locked(func() { live.stopping = false })
		return link.wireError(err)
	}
	return nil
}

func (b *appBackend) listHostedAsks(id string) ([]appwire.Ask, error) {
	a, err := b.find(id)
	if err != nil {
		return nil, err
	}
	a.gate.Lock()
	defer a.gate.Unlock()
	live, err := b.ensureHosted(a)
	if err != nil {
		return nil, err
	}
	questions := live.conn.hosted.client.Agent().OpenQuestions()
	asks := make([]appwire.Ask, 0, len(questions))
	for _, q := range questions {
		ask := appAskOf(id, q)
		appAskFacts(id, &ask, q)
		asks = append(asks, ask)
	}
	return asks, nil
}

func (b *appBackend) answerHostedAsk(id, askID, option, text, scope string, secretDoor bool) error {
	a, err := b.find(id)
	if err != nil {
		return err
	}
	a.gate.Lock()
	defer a.gate.Unlock()
	live, err := b.ensureHosted(a)
	if err != nil {
		return err
	}
	link := live.conn.hosted
	for _, q := range link.client.Agent().OpenQuestions() {
		if appAskID(id, q) != askID {
			continue
		}
		if secretDoor && !q.SecretAnswer() {
			return appwire.Errorf(appwire.CodeBadParams, "That question does not take a secret.")
		}
		if !secretDoor && q.SecretAnswer() && strings.TrimSpace(text) != "" {
			return appwire.Errorf(appwire.CodeBadParams, "Use the secret field to answer this question.")
		}
		if option == appwire.OptionAlways || scope != "" {
			return b.RefuseHostedAction(a.hosted.org)
		}
		key, err := appValidateQuestionAnswer(askID, q, option, text, scope)
		if err != nil {
			return err
		}
		answer := session.Answer{Kind: q.Kind, ID: q.ID, Ref: q.Ref, Ask: q.Ask, Key: key, Change: strings.TrimSpace(text)}
		if key != "" {
			answer.Picked = []string{key}
		}
		b.locked(func() { live.answered[askID] = appAnsweredMark + option })
		if err := link.client.Agent().ResolveQuestion(answer); err != nil {
			b.locked(func() { delete(live.answered, askID) })
			return link.wireError(err)
		}
		return nil
	}
	return appwire.Errorf(appwire.CodeNotFound, "That question is no longer open.")
}
