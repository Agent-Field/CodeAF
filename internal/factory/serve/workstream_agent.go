package serve

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── A WORKSTREAM'S AGENT, RUN BY THE ORGANIZATION'S SERVER ─────────────────
//
// While the operator runs the organization's workstream agents on its server
// (factory.AgentRunnerServer, WAVE2.md §4.3), each workstream has ONE agent
// here, and each person who can edit the workstream has their OWN, PRIVATE
// conversation with it (api/workstream_agent.go has the wire):
//
//   - Nobody reads another person's conversation, its events reach only its
//     person, and the agent's memory is per person too. One message of a
//     person's is answered at a time; they may stop it.
//   - It works with the organization's key (org_keys.go), never a person's,
//     and what it spends is posted to the workstream under the person whose
//     message it answered. A workstream that has spent its budget takes no
//     more messages.
//   - It reads and writes the workstream through [AgentDoor], as the person
//     who asked: their rights are checked again on every write, so someone
//     whose edit was taken away mid-answer cannot write through the agent.
//
// THIS PACKAGE NEVER CALLS A MODEL. The turn itself is [Config.WorkstreamAgent],
// which `codeaf factory serve` builds from the engine's own conversation
// machinery (internal/session). A server without one does not offer
// "server" as a place agents run, and every route here answers as if the
// agent were absent.
//
// A PAGE BEING CO-EDITED IS NOT WRITTEN OVER. A page's words live in its live
// document (docs.go), which the server never parses. The agent writes a page
// nobody has opened yet straight into its words, and the first person to open
// it starts from them; a page whose live document exists gets the agent's text
// as a new version in its history, which anyone with edit restores from there.

// AgentRunner answers one message to a workstream's agent. It reports each
// step and its answer through turn.Step as they happen, and answers what the
// turn cost. A cancelled ctx is a stop: it returns promptly, with what was
// spent so far.
type AgentRunner func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error)

// AgentTurn is one message for a workstream's agent to answer.
type AgentTurn struct {
	// Workstream is the workstream's id, Name its name when the turn began.
	Workstream, Name string
	// Turn names this message; Asker is the handle of who sent it.
	Turn, Asker string
	Text        string
	// LeashWords are words in this turn a person wrote: a chat's message and
	// the person's earlier messages in it, or a task's title and request when
	// a person last wrote them and what a person typed for the run. Text is
	// composed and may hold words the model wrote; only an address in these
	// may be read by web_fetch (session's tools_web_leash.go).
	LeashWords []string
	// Now is when the message arrived, and TZ the asker's time zone (an
	// IANA name) when their app sent one: the agent is told today's date in
	// it, so it never takes a date it read for today (L3 F-17).
	Now time.Time
	TZ  string
	// Chat is the person's own chat this turn answers in, "" for the
	// workstream's Afi conversation. Space is true when the workstream is the
	// person's own space (myspace.go): the agent is told it is theirs alone and
	// holds no tasks.
	Chat  string
	Space bool
	// Agent is the named agent this chat talks to (org_agents.go), nil for
	// Afi: the turn is that agent, with its person's instructions.
	Agent *AgentPersona
	// Task is set when this turn works one task rather than answering a
	// person's message: Text is then what the task asks, and the turn is the
	// task's Working conversation, which everyone in the workstream reads.
	Task *AgentTask
	// Key is the organization's model key (OpenRouter's).
	Key string
	// Dir is a folder kept for this workstream's agent alone, across turns: its
	// own conversation lives there, so the next message continues it.
	Dir string
	// Door reads and writes the workstream as Asker.
	Door AgentDoor
	// About is the organization's name and its one line ("What it does, in
	// a line. Afi reads this."), empty when nobody wrote one.
	About string
	// Org is set for the organization's set-up conversation alone (org_setup.go):
	// Door is then nil, and the agent carries org_propose_setup and nothing
	// else.
	Org *AgentOrg
	// Library is true when Door also reads the organization's Library
	// (org_library.go), so the agent is told it has that tool.
	Library bool
	// Step reports one line of the conversation: api.AgentEntryStep with the
	// tool's name, or api.AgentEntryAgent with the answer.
	Step func(kind, text, tool string, failed bool)
	// Steers, on a task's run, carries words its owner or starter sent while
	// it works: the runner hands each to the turn at its next step, and
	// answers it as a new turn when the turn has already ended. Nil on a
	// person's own conversation, where one message is answered at a time.
	Steers <-chan string
	// Progress, on a task's run, is told after every step what the run has
	// spent so far and how many steps it has taken, so the server can pause
	// it at a limit (workstream_task_limits.go). Nil on a person's message.
	Progress func(usd float64, steps int)
}

// AgentOrg is what the set-up conversation knows of its organization, and
// the door Afi proposes the set-up through.
type AgentOrg struct {
	Name, Description string
	Setup             session.OrgSetupDoor
}

// AgentPersona is a named agent a turn speaks as.
type AgentPersona struct {
	ID, Name, Instructions string
}

// AgentTask is the task a turn works.
type AgentTask struct {
	ID, Title string
}

// AgentTurnEnd is what one turn cost.
type AgentTurnEnd struct {
	USD float64
	// Transient says the turn's error may pass by itself (a busy or failing
	// model provider, a dropped connection), so the run is tried once more.
	Transient bool
}

// AgentDoor is how a workstream's agent reads and writes its workstream.
// Every write is made as the person whose message it answers.
type AgentDoor interface {
	// Workstream is the workstream as it is now, its pages' words included
	// (the plain-text mirror of each live page).
	Workstream(ctx context.Context) (factory.OrgWorkstream, error)
	// WritePage writes body as page's words, or makes a new page titled title
	// when page is "". It answers one line saying what happened.
	WritePage(ctx context.Context, page, title, body string) (string, error)
	// Decide proposes a decision; a person keeps it.
	Decide(ctx context.Context, text string) (string, error)
}

// agentEntriesKept is how many lines a workstream's conversation keeps in
// memory; the file keeps every line.
const agentEntriesKept = 1000

// agentDirName is the folder under the store's root that holds every
// workstream's agent.
const agentDirName = "workstream-agents"

const workstreamArchivedWords = "This workstream is archived."

// errAgentBusy is the refusal a second message gets while one is answered.
var errAgentBusy = errors.New("busy")

type wsAgent struct {
	loaded  bool
	entries []api.AgentEntry
	seq     int
	state   api.AgentState
	cancel  context.CancelFunc
	done    <-chan struct{}
}

// runEnded checks the run's final signal because idle is set before the
// idle event is published.
func (a *wsAgent) runEnded() bool {
	if a.done == nil {
		return true
	}
	select {
	case <-a.done:
		return true
	default:
		return false
	}
}

// agentSeat is one person's conversation with one workstream's agent. AFI'S
// PANE IS PRIVATE TO EACH PERSON (DESIGN.md "Who you are talking to"): Marco
// never reads Priya's turns with it, and its own memory of a conversation is
// that one person's, so it cannot repeat one person's words to another. What
// it does to shared things (tasks, files, pages, decisions) stays visible as
// those things.
type agentSeat struct {
	ws, person string
	// chat is one of the person's own chats (chats.go), "" for Afi's one
	// conversation in the workstream.
	chat string
}

type agentDesk struct {
	mu     sync.Mutex
	agents map[agentSeat]*wsAgent
	// tasks are the task runs going now, each stopped by its cancel.
	tasks map[taskRunKey]context.CancelFunc
	// resume holds, for a task whose run is still ending, the run to start
	// once it has: an answer that arrived while Afi was finishing its turn.
	resume map[taskRunKey]taskResume
	// owner names this process on the task leases it holds, and since is
	// when it was first asked (workstream_task_lease.go).
	owner string
	since time.Time
	// queue holds the runs waiting under the limits, in the order they
	// came; served counts, per workstream, when it last had a run started,
	// for the round robin (workstream_task_queue.go).
	// gen counts each task's runs, so a run that ends releases only its
	// own place, never a newer run's on the same task.
	gen map[taskRunKey]int
	// watch is each going run's spending, steps and last step, for the
	// running row's second line (fillRuns).
	watch   map[taskRunKey]*runWatch
	queue   []pendingRun
	queueN  int
	served  map[string]int
	servedN int
	// sweepDone closes when the lease sweep has ended, after Close.
	sweepDone chan struct{}
	// work is each task's Working conversation (workstream_task_work.go).
	work map[taskRunKey]*taskWork
	// adding is the cards lines being added now (workstream_agent_cards.go).
	adding map[cardsKey]bool
	ctx    context.Context
	stop   context.CancelFunc
	wg     sync.WaitGroup
}

func (h *Handler) agentRoutes() {
	h.route("/v1/workstreams/{id}/agent", map[string]http.HandlerFunc{http.MethodGet: h.getWorkstreamAgent})
	h.route("/v1/workstreams/{id}/agent/messages", map[string]http.HandlerFunc{http.MethodPost: h.postAgentMessage})
	h.route("/v1/workstreams/{id}/agent/stop", map[string]http.HandlerFunc{http.MethodPost: h.stopWorkstreamAgent})
	h.route("/v1/workstreams/{id}/agent/cards/{seq}", map[string]http.HandlerFunc{http.MethodPost: h.postAgentCards})
}

// serverAgentServed is whether this server can run workstream agents at all:
// it has a runner and the operator has handed it a key.
func (h *Handler) serverAgentServed() bool {
	return h.config.WorkstreamAgent != nil && h.orgModelKey() != ""
}

// agentDir is s's folder: its lines, and the agent's own conversation.
func (h *Handler) agentDir(s agentSeat) string {
	if s.chat != "" {
		return h.chatDir(s.ws, s.person, s.chat)
	}
	return filepath.Join(h.config.Store.Root(), agentDirName, s.ws, "people", s.person)
}

// sharedAgentDir is where a workstream's ONE shared conversation lived before
// each person had their own. It is read once per person, for their own turns
// only, and never written again.
func (h *Handler) sharedAgentDir(ws string) string {
	return filepath.Join(h.config.Store.Root(), agentDirName, ws)
}

// agentOfLocked is s's conversation, read from disk on first use. The desk's
// lock is held.
func (h *Handler) agentOfLocked(s agentSeat) *wsAgent {
	d := &h.agentDesk
	if d.agents == nil {
		d.agents = map[agentSeat]*wsAgent{}
	}
	a, ok := d.agents[s]
	if !ok {
		a = &wsAgent{state: api.AgentState{State: api.AgentIdle}}
		d.agents[s] = a
	}
	if !a.loaded {
		a.loaded = true
		path := filepath.Join(h.agentDir(s), agentEntriesFile)
		if _, err := os.Stat(path); s.chat == "" && errors.Is(err, os.ErrNotExist) {
			h.seedFromShared(s, path)
		}
		a.entries, a.seq = readAgentEntries(path)
	}
	return a
}

// seedFromShared starts s's own conversation with s's own turns from the
// workstream's old shared one: the lines of every turn s asked, renumbered.
// Nobody else's turn is copied, and the agent's memory of the shared
// conversation is not carried over, so it starts this person's afresh.
func (h *Handler) seedFromShared(s agentSeat, path string) {
	old, _ := readAgentEntries(filepath.Join(h.sharedAgentDir(s.ws), agentEntriesFile))
	mine := map[string]bool{}
	for _, e := range old {
		if e.Kind == api.AgentEntryPerson && e.Turn != "" && factory.NormalizeHandle(e.By) == s.person {
			mine[e.Turn] = true
		}
	}
	var out []byte
	seq := 0
	for _, e := range old {
		if !mine[e.Turn] {
			continue
		}
		seq++
		e.Seq = seq
		if raw, err := json.Marshal(e); err == nil {
			out = append(append(out, raw...), '\n')
		}
	}
	if os.MkdirAll(filepath.Dir(path), 0o700) == nil {
		_ = os.WriteFile(path, out, 0o600)
	}
}

// readAgentEntries reads a conversation file, keeping the newest lines. A
// line that does not parse (a write cut by a crash) is skipped.
func readAgentEntries(path string) ([]api.AgentEntry, int) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0
	}
	defer f.Close()
	var out []api.AgentEntry
	seq := 0
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 64<<10), 4<<20)
	for scan.Scan() {
		var e api.AgentEntry
		if json.Unmarshal(scan.Bytes(), &e) != nil || e.Seq <= seq {
			continue
		}
		seq = e.Seq
		out = append(out, e)
		if len(out) > 2*agentEntriesKept {
			out = slices.Clone(out[len(out)-agentEntriesKept:])
		}
	}
	if len(out) > agentEntriesKept {
		out = out[len(out)-agentEntriesKept:]
	}
	return out, seq
}

// addAgentEntry keeps one line of s's conversation, on disk and in memory,
// and publishes it to s's person alone.
func (h *Handler) addAgentEntry(s agentSeat, e api.AgentEntry) api.AgentEntry {
	h.agentDesk.mu.Lock()
	a := h.agentOfLocked(s)
	a.seq++
	e.Seq = a.seq
	e.At = h.clock().UTC().Format(time.RFC3339Nano)
	a.entries = append(a.entries, e)
	if len(a.entries) > agentEntriesKept {
		a.entries = slices.Clone(a.entries[len(a.entries)-agentEntriesKept:])
	}
	// Written under the lock so the file's lines stay in seq order.
	if raw, err := json.Marshal(e); err == nil {
		dir := h.agentDir(s)
		if os.MkdirAll(dir, 0o700) == nil {
			if f, err := os.OpenFile(filepath.Join(dir, agentEntriesFile), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
				_, _ = f.Write(append(raw, '\n'))
				_ = f.Close()
			}
		}
	}
	h.agentDesk.mu.Unlock()
	if s.chat == "" && e.Seq == 1 && safeWorkstreamFolder(s.ws) {
		if ws, found, err := h.config.Store.Workstream(s.ws); err == nil && found {
			h.keepAfiSession(ws, s.person)
		}
	}
	h.publishAgent(s, api.AgentEvent{Entry: &e})
	return e
}

func (h *Handler) setAgentState(s agentSeat, st api.AgentState) {
	h.agentDesk.mu.Lock()
	h.agentOfLocked(s).state = st
	h.agentDesk.mu.Unlock()
	h.publishAgent(s, api.AgentEvent{AgentState: &st})
}

// publishAgent writes one agent event for s's person. Who hears it is
// decided as it is sent (agentForViewer): that person, while they can still
// see the workstream.
func (h *Handler) publishAgent(s agentSeat, e api.AgentEvent) {
	e.ID, e.Person, e.Chat = s.ws, s.person, s.chat
	kind := api.EventAgentEntry
	if e.Entry == nil {
		kind = api.EventAgentState
	}
	_ = live.Append(h.eventRoot(), live.New(kind, 0, e))
}

// agentForViewer lets an agent event through to the one person whose
// conversation it is, while they can see its workstream. An event naming no
// person (one written before conversations were private) reaches nobody.
func (h *Handler) agentForViewer(r *http.Request, e live.Event) (live.Event, bool) {
	var body api.AgentEvent
	if json.Unmarshal(e.Data, &body) != nil || body.ID == "" || body.Person == "" || body.Person != h.orgActor(r) {
		return e, false
	}
	if body.ID == orgSetupSeat {
		return e, h.isOperator(r)
	}
	known, ok, err := h.workstreamCan(r, body.ID, factory.RoleView)
	return e, known && ok && err == nil
}

// agentCannot is whether v may talk to ws's agent now, and the one sentence
// that says why not. A person who could never talk to it (they cannot edit
// the workstream, or this server runs no agents) is answered no sentence.
//
// A PERSON'S OWN CHATS AND THEIR SPACE ALWAYS RUN HERE, whatever the operator
// chose for Afi's pane: a chat kept on the server is answered by the server,
// so a laptop that sleeps loses nothing (org-space DESIGN §2.1).
func (h *Handler) agentCannot(ws factory.OrgWorkstream, v wsViewer, chat string) (bool, string) {
	if !v.canWrite(ws) || h.config.WorkstreamAgent == nil {
		return false, ""
	}
	serverAlways := chat != "" || ws.Space
	switch {
	case ws.ArchivedAt != nil:
		return false, workstreamArchivedWords
	case !serverAlways && h.agentRunner() != factory.AgentRunnerServer:
		return false, "This organization runs its workstreams' agents on the laptop of whoever drives them."
	case h.orgModelKey() == "":
		return false, "The organization has no model key yet. Its operator adds one in Settings › Organizations."
	}
	if ws.Space {
		if limit, spent := h.spaceMonth(ws.ID); spent >= limit {
			return false, spaceAtLimit
		}
	}
	if ws.BudgetUSD != nil {
		spent, _ := h.config.Store.WorkstreamSpend()
		if spent[ws.ID] >= *ws.BudgetUSD {
			return false, "This workstream has spent its budget."
		}
	}
	return true, ""
}

// agentWire is person's own conversation with ws's agent, as v reads it.
func (h *Handler) agentWire(ws factory.OrgWorkstream, v wsViewer, person, chat string, since int) api.WorkstreamAgent {
	out := api.WorkstreamAgent{ID: ws.ID, Chat: chat, AgentState: api.AgentState{State: api.AgentIdle}, Entries: []api.AgentEntry{}, Private: true}
	if person != "" {
		h.agentDesk.mu.Lock()
		seat := agentSeat{ws.ID, person, chat}
		if h.chatExists(seat) {
			a := h.agentOfLocked(seat)
			out.AgentState, out.Seq = a.state, a.seq
			out.Entries = append(out.Entries, agentEntriesSince(a.entries, since)...)
		}
		h.agentDesk.mu.Unlock()
		if chat == "" {
			h.keepAfiSession(ws, person)
		}
	}
	out.CanTalk, out.Cannot = h.agentCannot(ws, v, chat)
	if person == "" {
		out.CanTalk, out.Cannot = false, ""
	}
	return out
}

// getWorkstreamAgent is GET /v1/workstreams/{id}/agent?since=: the caller's
// own conversation with the workstream's agent.
func (h *Handler) getWorkstreamAgent(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.findWorkstream(w, r)
	if !ok {
		return
	}
	since, err := agentSince(r.URL.Query().Get("since"))
	if err != nil {
		h.fail(w, http.StatusBadRequest, err.Error())
		return
	}
	chat, ok := h.chatOf(w, ws, h.orgActor(r), r.URL.Query().Get("chat"))
	if !ok {
		return
	}
	h.write(w, http.StatusOK, h.agentWire(ws, v, h.orgActor(r), chat, since))
}

// postAgentMessage is POST /v1/workstreams/{id}/agent/messages: one person's
// message, answered in the background in their own conversation. The answer
// is 202 with that conversation after the caller's own line.
func (h *Handler) postAgentMessage(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.findWorkstream(w, r)
	if !ok {
		return
	}
	if !v.canWrite(ws) {
		h.fail(w, http.StatusForbidden, "only its owner and those it is shared with for edit can talk to this workstream's agent")
		return
	}
	if h.config.WorkstreamAgent == nil {
		h.fail(w, http.StatusNotFound, "this organization's server does not run agents")
		return
	}
	var body api.AgentMessageNew
	if !h.decode(w, r, &body, false) {
		return
	}
	chat, ok := h.chatOf(w, ws, h.orgActor(r), body.Chat)
	if !ok {
		return
	}
	if can, cannot := h.agentCannot(ws, v, chat); !can {
		h.fail(w, http.StatusConflict, cannot)
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" || len(text) > api.AgentMessageMost {
		h.fail(w, http.StatusUnprocessableEntity, "a message is 1 to 16384 bytes of words")
		return
	}
	asker := h.orgActor(r)
	if asker == "" {
		h.fail(w, http.StatusForbidden, "talking to this workstream's agent needs a signed-in person")
		return
	}
	err := h.startAgentTurn(ws, v, asker, chat, text, strings.TrimSpace(body.TZ), body.Cards)
	if errors.Is(err, errChatNotHere) {
		h.fail(w, http.StatusNotFound, errChatNotHere.Error())
		return
	}
	var gone chatAgentGone
	if errors.As(err, &gone) {
		h.fail(w, http.StatusConflict, gone.Error())
		return
	}
	if errors.Is(err, errAgentBusy) {
		h.write(w, http.StatusConflict, map[string]string{"error": "the agent is still answering your last message; wait for it, or stop it", "reason": "busy", "by": asker})
		return
	}
	if h.failed(w, err) {
		return
	}
	h.write(w, http.StatusAccepted, h.agentWire(ws, v, asker, chat, 0))
}

// startAgentTurn claims asker's conversation with ws's agent for one message
// and runs it in the background. It is errAgentBusy while their previous
// message is answered; other people's conversations never make it busy.
func (h *Handler) startAgentTurn(ws factory.OrgWorkstream, v wsViewer, asker, chat, text, tz string, cards bool) error {
	now := h.clock().UTC()
	tid, err := cell.NewID(now)
	if err != nil {
		return err
	}
	turn := "turn_" + tid
	seat := agentSeat{ws.ID, asker, chat}
	d := &h.agentDesk
	d.mu.Lock()
	if !h.chatExists(seat) {
		d.mu.Unlock()
		return errChatNotHere
	}
	persona, err := h.chatPersona(ws, seat)
	if err != nil {
		d.mu.Unlock()
		return err
	}
	if d.ctx == nil {
		d.ctx, d.stop = context.WithCancel(context.Background())
	}
	if d.ctx.Err() != nil {
		d.mu.Unlock()
		return errors.New("this factory is closing")
	}
	a := h.agentOfLocked(seat)
	if a.state.State != api.AgentIdle || chat != "" && !a.runEnded() {
		d.mu.Unlock()
		return errAgentBusy
	}
	// What the person wrote in this conversation, this message last: the
	// only words its web_fetch may follow an address in.
	leash := []string{}
	for _, e := range a.entries {
		if e.Kind == api.AgentEntryPerson {
			leash = append(leash, e.Text)
		}
	}
	leash = append(leash, text)
	ctx, cancel := context.WithCancel(d.ctx)
	done := make(chan struct{})
	a.cancel, a.done = cancel, done
	a.state = api.AgentState{State: api.AgentWorking, Turn: turn, For: asker, Since: now.Format(time.RFC3339Nano)}
	state := a.state
	d.wg.Add(1)
	d.mu.Unlock()

	h.addAgentEntry(seat, api.AgentEntry{Kind: api.AgentEntryPerson, By: asker, Text: text, Turn: turn})
	if chat != "" {
		h.chatTouched(seat, text)
	}
	h.publishAgent(seat, api.AgentEvent{AgentState: &state})
	door := &agentDoor{h: h, id: ws.ID, asker: asker, operator: v.Operator, cards: cards && chat == "" && !ws.Space, space: ws.Space, allWork: ws.Space && chat == ""}
	if persona != nil {
		door.agent = persona.ID
	}
	run := AgentTurn{Workstream: ws.ID, Name: ws.Name, Turn: turn, Asker: asker, Text: text, LeashWords: leash, Now: now, TZ: tz, About: h.orgAbout(), Chat: chat, Space: ws.Space, Agent: persona,
		Key: h.orgModelKey(), Dir: filepath.Join(h.agentDir(seat), "agent"), Door: h.withLibrary(door), Library: h.spacesServed(),
		Step: func(kind, text, tool string, failed bool) {
			switch kind {
			case api.AgentEntryStep, api.AgentEntryAgent, api.AgentEntryNote:
			default:
				return
			}
			h.addAgentEntry(seat, api.AgentEntry{Kind: kind, By: asker, Text: text, Tool: tool, Failed: failed, Turn: turn})
		}}
	guard.Go("factory workstream agent", func() {
		defer d.wg.Done()
		defer close(done)
		defer cancel()
		h.runAgentTurn(ctx, run)
	})
	return nil
}

// runAgentTurn answers one message, posts what it spent, and leaves the
// person's conversation idle again.
func (h *Handler) runAgentTurn(ctx context.Context, run AgentTurn) {
	seat := agentSeat{run.Workstream, run.Asker, run.Chat}
	if err := os.MkdirAll(run.Dir, 0o700); err != nil {
		h.addAgentEntry(seat, api.AgentEntry{Kind: api.AgentEntryNote, Text: "The agent could not start: " + err.Error(), Turn: run.Turn})
		h.setAgentState(seat, api.AgentState{State: api.AgentIdle})
		return
	}
	end, err := h.config.WorkstreamAgent(ctx, run)
	stopped := ""
	h.agentDesk.mu.Lock()
	if a := h.agentOfLocked(seat); a.state.State == api.AgentStopping {
		stopped = a.state.For
	}
	h.agentDesk.mu.Unlock()
	switch {
	case stopped != "":
		h.addAgentEntry(seat, api.AgentEntry{Kind: api.AgentEntryNote, Text: "Stopped.", By: stopped, Turn: run.Turn})
	case err != nil && ctx.Err() == nil:
		h.addAgentEntry(seat, api.AgentEntry{Kind: api.AgentEntryNote, Text: "The agent could not finish: " + err.Error(), Turn: run.Turn})
	}
	// THE SET-UP CONVERSATION HAS NO WORKSTREAM TO POST TO; what it spent is
	// in the organization's record instead (org_setup.go).
	if end.USD > 0 && run.Org == nil {
		h.postAgentSpend(run, end.USD)
	} else if end.USD > 0 {
		h.recordAs(context.Background(), run.Asker, record.SettingsChanged, "Afi set-up spend", orgObjectFactory, strconv.FormatFloat(end.USD, 'f', -1, 64)+" USD")
	}
	h.setAgentState(seat, api.AgentState{State: api.AgentIdle})
}

// postAgentSpend posts what one turn cost to its workstream, under the person
// whose message it answered, once per turn.
func (h *Handler) postAgentSpend(run AgentTurn, usd float64) {
	usd = min(usd, spendOneMostUSD)
	s := store.Spend{At: h.clock().UTC().Format(time.RFC3339Nano), By: run.Asker, USD: usd, Chat: "agent", Key: "agent:" + run.Turn}
	if _, err := h.config.Store.AddSpend(run.Workstream, s); err != nil {
		return
	}
	if ws, ok, err := h.config.Store.Workstream(run.Workstream); err == nil && ok {
		h.recordAs(context.Background(), run.Asker, record.WorkstreamChanged, "posted spend", orgObjectWorkstream+run.Workstream,
			strconv.FormatFloat(usd, 'f', -1, 64)+" USD by the agent")
		h.publishWorkstreamPart(ws, api.PartSpend, "", run.Asker, 0, false)
	}
}

// stopWorkstreamAgent is POST /v1/workstreams/{id}/agent/stop: a person stops
// the answer to their own message. Nobody can stop, or see, another
// person's conversation.
func (h *Handler) stopWorkstreamAgent(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.findWorkstream(w, r)
	if !ok {
		return
	}
	if !v.canWrite(ws) {
		h.fail(w, http.StatusForbidden, "only its owner and those it is shared with for edit can stop this workstream's agent")
		return
	}
	who := h.orgActor(r)
	chat, ok := h.chatOf(w, ws, who, r.URL.Query().Get("chat"))
	if !ok {
		return
	}
	if who == "" {
		h.write(w, http.StatusOK, h.agentWire(ws, v, who, chat, 0))
		return
	}
	seat := agentSeat{ws.ID, who, chat}
	h.agentDesk.mu.Lock()
	if !h.chatExists(seat) {
		h.agentDesk.mu.Unlock()
		h.fail(w, http.StatusNotFound, errChatNotHere.Error())
		return
	}
	a := h.agentOfLocked(seat)
	var state *api.AgentState
	if a.state.State == api.AgentWorking && a.cancel != nil {
		a.state.State, a.state.For = api.AgentStopping, who
		a.cancel()
		s := a.state
		state = &s
	}
	h.agentDesk.mu.Unlock()
	if state != nil {
		h.publishAgent(seat, api.AgentEvent{AgentState: state})
	}
	h.write(w, http.StatusOK, h.agentWire(ws, v, who, chat, 0))
}

// closeAgents stops every answer in flight and waits for them; Close calls it.
func (h *Handler) closeAgents() {
	d := &h.agentDesk
	d.mu.Lock()
	if d.ctx == nil {
		d.ctx, d.stop = context.WithCancel(context.Background())
	}
	d.stop()
	swept := d.sweepDone
	d.mu.Unlock()
	d.wg.Wait()
	if swept != nil {
		<-swept
	}
}

// recordAs appends one organization change made by who, outside a request:
// the agent's writes and spend, made for the person who asked.
func (h *Handler) recordAs(ctx context.Context, who, kind, verb, object, detail string) {
	if h.config.Record == nil || who == "" {
		return
	}
	_, _ = h.config.Record.Append(ctx, record.Event{Kind: kind, Verb: verb, Object: object,
		Handle: who, Detail: strings.TrimSpace(detail), Actor: record.Actor{Kind: "person", Name: "@" + who}})
}

// ── the door the agent reads and writes through ──

type agentDoor struct {
	h        *Handler
	id       string
	asker    string
	agent    string
	operator bool
	// wrote is set for a task's run alone: the files the run made or rewrote,
	// so a run that ends having written its work is not called a failure
	// (finishTaskRun). A person's conversation leaves it nil.
	wrote *taskWrites
	// runTask is the task this door's run does, when it is a task's run: what
	// the run makes is stamped from it and what it reads is used by it, so the
	// task page lists both without the agent having to say so.
	runTask string
	// goesOn is set for a task's run that carries on an earlier run's work,
	// after an answer or a restart: what the task already made counts as
	// written when the run ends (finishTaskRun).
	goesOn bool
	// cards is whether the app this person's message came from draws task
	// cards (workstream_agent_cards.go); without it the agent never proposes.
	cards bool
	// canvasJoined are the canvases this door has made the agent a member
	// of (workstream_agent_canvas.go), so it asks once per canvas.
	canvasJoined sync.Map
	// space is whether the workstream is a person's own space (myspace.go),
	// where the agent draws no canvas: it is told of none there.
	space bool
	// allWork is the read-only scope of Afi in My space, never a named chat.
	allWork bool
}

// madeByTask is the files Afi made for task, which an earlier run of it
// wrote. A file that cannot be listed counts as none.
func (d *agentDoor) madeByTask(ctx context.Context, task string) []string {
	files, err := d.files(ctx, "")
	if err != nil {
		return nil
	}
	var out []string
	for _, f := range files.Files {
		if f.FromTask == task && f.MadeBy.Kind == factory.MakerAfi && f.Kind != factory.FileFolder {
			out = append(out, f.ID)
		}
	}
	return out
}

// taskWrites is the files one task run made or rewrote, in the order it wrote
// them, each once. The agent may call its tools side by side, so it locks.
type taskWrites struct {
	mu    sync.Mutex
	files []string
}

// note records a file the run wrote; a nil one (a conversation) keeps nothing.
func (w *taskWrites) note(id string) {
	id = strings.TrimSpace(id)
	if w == nil || id == "" {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !slices.Contains(w.files, id) {
		w.files = append(w.files, id)
	}
}

// list is the files written so far.
func (w *taskWrites) list() []string {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.files)
}

// viewer is the asker as the access core sees them now: their teams are read
// again on every call, so a membership taken away mid-answer counts.
func (d *agentDoor) viewer() wsViewer {
	v := d.h.viewerOf(d.asker)
	v.Solo = d.h.mode() == api.FactorySolo
	v.Operator = v.Solo || d.isCurrentOperator()
	v.Agent = &acl.Agent{ID: d.agentID(), Sponsor: v.Handle}
	return v
}

func (d *agentDoor) isCurrentOperator() bool {
	if d.h.mode() == api.FactorySolo {
		return d.operator
	}
	handle := factory.NormalizeHandle(d.asker)
	return handle != "" && handle == factory.NormalizeHandle(d.h.operatorLogin(nil))
}

func (d *agentDoor) agentID() string {
	if d.agent != "" {
		return d.agent
	}
	return "afi"
}

func (d *agentDoor) Workstream(context.Context) (factory.OrgWorkstream, error) {
	ws, ok, err := d.h.config.Store.Workstream(d.id)
	if err != nil {
		return ws, err
	}
	if !ok || !d.viewer().canSee(ws) {
		return factory.OrgWorkstream{}, errWorkstreamGone
	}
	return ws, nil
}

// errAgentNoWrite is what the agent is told when the person it answers may
// not write the workstream (any more).
var errAgentNoWrite = errors.New("the person you are answering cannot change this workstream")

func (d *agentDoor) WritePage(ctx context.Context, page, title, body string) (string, error) {
	h := d.h
	v := d.viewer()
	if len(body) > workstreamBodyMost {
		return "", errors.New("a page holds at most 256 KiB of words")
	}
	if page == "" {
		// A new page is a new document made by Afi for the person who asked,
		// through the route a person's client makes one with, so Files and
		// the page agree on who made it.
		title = strings.TrimSpace(title)
		if title == "" {
			return "", errors.New("a new page needs a title")
		}
		raw, err := d.call(ctx, h.createWorkstreamFile, http.MethodPost, "/v1/workstreams/"+d.id+"/files", nil,
			api.FileNew{Kind: factory.FileDocument, Name: title, BodyMD: body, MadeBy: &factory.FileMaker{Kind: factory.MakerAfi, Name: "Afi"}}, -1)
		if err != nil {
			return "", agentWriteRefused(err)
		}
		var f factory.WorkstreamFile
		if err := json.Unmarshal(raw, &f); err != nil {
			return "", err
		}
		d.wrote.note(f.ID)
		return "made the page " + strconv.Quote(title) + " (" + f.PageID + ")", nil
	}
	ws, err := d.Workstream(ctx)
	if err != nil {
		return "", err
	}
	if !v.canWrite(ws) {
		return "", errAgentNoWrite
	}
	if ws.ArchivedAt != nil {
		return "", errWorkstreamArchived
	}
	at := slices.IndexFunc(ws.Pages, func(p factory.WorkstreamPage) bool { return p.ID == page })
	if at < 0 {
		return "", errors.New("this workstream has no page " + page)
	}
	cur := ws.Pages[at]
	// AN AGENT REWRITES ONLY A DOCUMENT IT MADE THAT NO PERSON HAS EDITED.
	// Any other page gets the agent's text as a suggestion: a version in its
	// History that applies nothing until someone restores it.
	if !h.agentMayRewrite(ws, page) {
		n, err := d.keepSuggestion(ctx, ws, cur, body, "rewrite")
		if err != nil {
			return "", err
		}
		return "the page " + strconv.Quote(cur.Title) + " was made or edited by a person, so your text was not applied: " + keptWords(n), nil
	}
	live, err := h.pageDocLive(d.id, page)
	if err != nil {
		return "", err
	}
	if !live {
		// Nobody has opened the page's live document yet, so its words are
		// these: the first person to open it starts from them. The words
		// before are kept in its History first, and the new ones after, so
		// nothing the page said is lost and History shows what it holds.
		stamp := h.clock().UTC().Format(time.RFC3339Nano)
		if strings.TrimSpace(cur.BodyMD) != "" && cur.BodyMD != body {
			_, _, _ = h.config.Store.AddPageVersion(d.id, page, store.PageVersion{At: stamp, By: firstNonEmpty(cur.UpdatedBy, v.Handle), Title: cur.Title, BodyMD: cur.BodyMD, Reason: "manual"})
		}
		after, err := h.config.Store.MirrorWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
			w, ok := all[d.id]
			if !ok {
				return nil, errWorkstreamGone
			}
			i := slices.IndexFunc(w.Pages, func(p factory.WorkstreamPage) bool { return p.ID == page })
			if i < 0 {
				return nil, errPageGone
			}
			w.Pages = slices.Clone(w.Pages)
			w.Pages[i].BodyMD, w.Pages[i].UpdatedAt, w.Pages[i].UpdatedBy = body, stamp, v.Handle
			all[d.id] = w
			return []string{d.id}, nil
		})
		if err != nil {
			return "", agentWriteRefused(err)
		}
		// A person who opened it in the meantime seeded the live document
		// from the words before; their copy wins, and the agent's goes to
		// the history below instead.
		if live, err := h.pageDocLive(d.id, page); err == nil && !live {
			_, _, _ = h.config.Store.AddPageVersion(d.id, page, store.PageVersion{At: stamp, By: v.Handle, Title: cur.Title, BodyMD: body, Reason: "manual"})
			h.recordAs(ctx, v.Handle, record.WorkstreamChanged, "changed page", orgObjectWorkstream+d.id, page+" by the agent")
			if w, ok := after[d.id]; ok {
				h.publishWorkstreamPart(w, api.PartPage, page, v.Handle, cur.Rev, false)
			}
			h.agentEditedFile(ws, page, v.Handle)
			return "wrote the page " + strconv.Quote(cur.Title), nil
		}
	}
	n, err := d.keepSuggestion(ctx, ws, cur, body, "edited live")
	if err != nil {
		return "", err
	}
	return "the page " + strconv.Quote(cur.Title) + " is edited live, so your text was not applied: " + keptWords(n), nil
}

// agentMayRewrite is whether the agent may write page's words directly: its
// file was made by an agent and no person has edited it. A page Files has not
// folded in yet was a person's.
func (h *Handler) agentMayRewrite(ws factory.OrgWorkstream, page string) bool {
	files, err := h.foldFiles(ws)
	if err != nil {
		return false
	}
	for _, f := range files {
		if f.PageID == page && f.Kind == factory.FileDocument && f.DeletedAt == nil {
			return f.AgentMayEdit()
		}
	}
	return false
}

// agentEditedFile stamps the document of page as changed by the person the
// agent answered, the way a person's client stamps its own edits, and flags
// the running tasks that use it.
func (h *Handler) agentEditedFile(ws factory.OrgWorkstream, page, by string) {
	now := h.clock().UTC()
	files, err := h.config.Store.EditWorkstreamFiles(ws.ID, now.Format(time.RFC3339Nano), func(files map[string]factory.WorkstreamFile) ([]string, bool, error) {
		for id, f := range files {
			if f.PageID == page && f.Kind == factory.FileDocument {
				f.ChangedAt, f.ChangedBy = now, by
				files[id] = f
				return []string{id}, false, nil
			}
		}
		return nil, false, nil
	})
	if err != nil {
		return
	}
	for _, f := range files {
		if f.PageID == page && f.Kind == factory.FileDocument {
			h.publishFileChange(ws, f, by, true)
			return
		}
	}
}

func (d *agentDoor) Decide(ctx context.Context, text string) (string, error) {
	h := d.h
	v := d.viewer()
	text = strings.TrimSpace(text)
	if text == "" {
		return "", errors.New("a decision needs its words")
	}
	now := h.clock().UTC()
	var id string
	_, saved, err := h.editPart(d.id, v, func(cur *factory.OrgWorkstream, _ factory.OrgWorkstreams) (bool, error) {
		n, err := cell.NewID(now)
		if err != nil {
			return false, err
		}
		id = "dc_" + n
		cur.Decisions = append(slices.Clone(cur.Decisions), factory.WorkstreamDecision{ID: id, Text: text, State: "proposed",
			At: now.Format(time.RFC3339Nano), By: v.Handle})
		cur.UpdatedAt = now
		return true, cleanWorkstream(cur, v.Handle, now)
	})
	if err != nil {
		return "", agentWriteRefused(err)
	}
	h.recordAs(ctx, v.Handle, record.WorkstreamChanged, "changed workstream", orgObjectWorkstream+d.id, "proposed decision "+id+" by the agent")
	h.publishWorkstreamPart(saved, api.PartWorkstream, "", v.Handle, saved.Rev, false)
	return "suggested the decision; it waits in Decisions until a person keeps it", nil
}

// agentWriteRefused is a write refusal in words the agent can act on.
func agentWriteRefused(err error) error {
	if errors.Is(err, errWorkstreamNoWrite) {
		return errAgentNoWrite
	}
	return err
}

// pageDocLive is whether page's live document has anything in it: once it
// does, the document holds the page's words, not the plain-text mirror.
func (h *Handler) pageDocLive(id, page string) (bool, error) {
	ds, err := h.docStore()
	if err != nil {
		return false, err
	}
	head, err := ds.Head("ws/" + id + "/page/" + page)
	return head > 0, err
}
