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
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/guard"
)

// ── A WORKSTREAM'S AGENT, RUN BY THE ORGANIZATION'S SERVER ─────────────────
//
// While the operator runs the organization's workstream agents on its server
// (factory.AgentRunnerServer, WAVE2.md §4.3), each workstream has ONE agent
// here, and one conversation with it that everyone who can see the workstream
// reads (api/workstream_agent.go has the wire):
//
//   - Those who can edit the workstream talk to it. One message is answered at
//     a time; a second one while it works is refused naming whose message it
//     is answering, and anyone who can edit may stop it.
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
	// Key is the organization's model key (OpenRouter's).
	Key string
	// Dir is a folder kept for this workstream's agent alone, across turns: its
	// own conversation lives there, so the next message continues it.
	Dir string
	// Door reads and writes the workstream as Asker.
	Door AgentDoor
	// Step reports one line of the conversation: api.AgentEntryStep with the
	// tool's name, or api.AgentEntryAgent with the answer.
	Step func(kind, text, tool string, failed bool)
}

// AgentTurnEnd is what one turn cost.
type AgentTurnEnd struct {
	USD float64
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

// errAgentBusy is the refusal a second message gets while one is answered.
var errAgentBusy = errors.New("busy")

type wsAgent struct {
	loaded  bool
	entries []api.AgentEntry
	seq     int
	state   api.AgentState
	cancel  context.CancelFunc
}

type agentDesk struct {
	mu     sync.Mutex
	agents map[string]*wsAgent
	ctx    context.Context
	stop   context.CancelFunc
	wg     sync.WaitGroup
}

func (h *Handler) agentRoutes() {
	h.route("/v1/workstreams/{id}/agent", map[string]http.HandlerFunc{http.MethodGet: h.getWorkstreamAgent})
	h.route("/v1/workstreams/{id}/agent/messages", map[string]http.HandlerFunc{http.MethodPost: h.postAgentMessage})
	h.route("/v1/workstreams/{id}/agent/stop", map[string]http.HandlerFunc{http.MethodPost: h.stopWorkstreamAgent})
}

// serverAgentServed is whether this server can run workstream agents at all:
// it has a runner and the operator has handed it a key.
func (h *Handler) serverAgentServed() bool {
	return h.config.WorkstreamAgent != nil && h.orgModelKey() != ""
}

func (h *Handler) agentDir(id string) string {
	return filepath.Join(h.config.Store.Root(), agentDirName, id)
}

// agentOf is id's agent, its conversation read from disk on first use. The
// desk's lock is held.
func (h *Handler) agentOfLocked(id string) *wsAgent {
	d := &h.agentDesk
	if d.agents == nil {
		d.agents = map[string]*wsAgent{}
	}
	a, ok := d.agents[id]
	if !ok {
		a = &wsAgent{state: api.AgentState{State: api.AgentIdle}}
		d.agents[id] = a
	}
	if !a.loaded {
		a.loaded = true
		a.entries, a.seq = readAgentEntries(filepath.Join(h.agentDir(id), "entries.jsonl"))
	}
	return a
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

// addEntry keeps one line of id's conversation, on disk and in memory, and
// publishes it.
func (h *Handler) addAgentEntry(id string, e api.AgentEntry) api.AgentEntry {
	h.agentDesk.mu.Lock()
	a := h.agentOfLocked(id)
	a.seq++
	e.Seq = a.seq
	e.At = h.clock().UTC().Format(time.RFC3339Nano)
	a.entries = append(a.entries, e)
	if len(a.entries) > agentEntriesKept {
		a.entries = slices.Clone(a.entries[len(a.entries)-agentEntriesKept:])
	}
	// Written under the lock so the file's lines stay in seq order.
	if raw, err := json.Marshal(e); err == nil {
		dir := h.agentDir(id)
		if os.MkdirAll(dir, 0o700) == nil {
			if f, err := os.OpenFile(filepath.Join(dir, "entries.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
				_, _ = f.Write(append(raw, '\n'))
				_ = f.Close()
			}
		}
	}
	h.agentDesk.mu.Unlock()
	h.publishAgent(api.AgentEvent{ID: id, Entry: &e})
	return e
}

func (h *Handler) setAgentState(id string, s api.AgentState) {
	h.agentDesk.mu.Lock()
	h.agentOfLocked(id).state = s
	h.agentDesk.mu.Unlock()
	h.publishAgent(api.AgentEvent{ID: id, AgentState: &s})
}

// publishAgent writes one agent event. Who hears it is decided as it is
// sent (agentForViewer): everyone who can see the workstream then.
func (h *Handler) publishAgent(e api.AgentEvent) {
	kind := api.EventAgentEntry
	if e.Entry == nil {
		kind = api.EventAgentState
	}
	_ = live.Append(h.eventRoot(), live.New(kind, 0, e))
}

// agentForViewer lets an agent event through to a caller who can see its
// workstream now.
func (h *Handler) agentForViewer(r *http.Request, e live.Event) (live.Event, bool) {
	var body api.AgentEvent
	if json.Unmarshal(e.Data, &body) != nil || body.ID == "" {
		return e, false
	}
	known, ok, err := h.workstreamCan(r, body.ID, factory.RoleView)
	return e, known && ok && err == nil
}

// agentCannot is whether v may talk to ws's agent now, and the one sentence
// that says why not. A person who could never talk to it (they cannot edit
// the workstream, or this server runs no agents) is answered no sentence.
func (h *Handler) agentCannot(ws factory.OrgWorkstream, v wsViewer) (bool, string) {
	if !v.canWrite(ws) || h.config.WorkstreamAgent == nil {
		return false, ""
	}
	switch {
	case ws.ArchivedAt != nil:
		return false, "This workstream is archived."
	case h.agentRunner() != factory.AgentRunnerServer:
		return false, "This organization runs its workstreams' agents on the laptop of whoever drives them."
	case h.orgModelKey() == "":
		return false, "The organization has no model key yet. Its operator adds one in Settings › Factory."
	}
	if ws.BudgetUSD != nil {
		spent, _ := h.config.Store.WorkstreamSpend()
		if spent[ws.ID] >= *ws.BudgetUSD {
			return false, "This workstream has spent its budget."
		}
	}
	return true, ""
}

func (h *Handler) agentWire(ws factory.OrgWorkstream, v wsViewer, since int) api.WorkstreamAgent {
	h.agentDesk.mu.Lock()
	a := h.agentOfLocked(ws.ID)
	out := api.WorkstreamAgent{ID: ws.ID, AgentState: a.state, Seq: a.seq, Entries: []api.AgentEntry{}}
	from := len(a.entries)
	for from > 0 && a.entries[from-1].Seq > since {
		from--
	}
	if len(a.entries)-from > api.AgentEntriesMost {
		from = len(a.entries) - api.AgentEntriesMost
	}
	out.Entries = append(out.Entries, a.entries[from:]...)
	h.agentDesk.mu.Unlock()
	out.CanTalk, out.Cannot = h.agentCannot(ws, v)
	return out
}

// getWorkstreamAgent is GET /v1/workstreams/{id}/agent?since=.
func (h *Handler) getWorkstreamAgent(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.findWorkstream(w, r)
	if !ok {
		return
	}
	since := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			h.fail(w, http.StatusBadRequest, "since is the last seq you have")
			return
		}
		since = n
	}
	h.write(w, http.StatusOK, h.agentWire(ws, v, since))
}

// postAgentMessage is POST /v1/workstreams/{id}/agent/messages: one person's
// message, answered in the background. The answer is 202 with the
// conversation after the caller's own line.
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
	if can, cannot := h.agentCannot(ws, v); !can {
		h.fail(w, http.StatusConflict, cannot)
		return
	}
	var body api.AgentMessageNew
	if !h.decode(w, r, &body, false) {
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" || len(text) > api.AgentMessageMost {
		h.fail(w, http.StatusUnprocessableEntity, "a message is 1 to 16384 bytes of words")
		return
	}
	asker := h.orgActor(r)
	err := h.startAgentTurn(ws, v, asker, text)
	if errors.Is(err, errAgentBusy) {
		h.writeAgentBusy(w, ws.ID)
		return
	}
	if h.failed(w, err) {
		return
	}
	h.write(w, http.StatusAccepted, h.agentWire(ws, v, 0))
}

// writeAgentBusy is the 409 a message gets while another is answered.
func (h *Handler) writeAgentBusy(w http.ResponseWriter, id string) {
	h.agentDesk.mu.Lock()
	who := h.agentOfLocked(id).state.For
	h.agentDesk.mu.Unlock()
	msg := "the agent is still answering"
	if who != "" {
		msg += " " + who
	}
	h.write(w, http.StatusConflict, map[string]string{"error": msg + "; wait for it, or stop it", "reason": "busy", "by": who})
}

// startAgentTurn claims id's agent for one message and runs it in the
// background. It is errAgentBusy while another message is answered.
func (h *Handler) startAgentTurn(ws factory.OrgWorkstream, v wsViewer, asker, text string) error {
	now := h.clock().UTC()
	tid, err := cell.NewID(now)
	if err != nil {
		return err
	}
	turn := "turn_" + tid
	d := &h.agentDesk
	d.mu.Lock()
	if d.ctx == nil {
		d.ctx, d.stop = context.WithCancel(context.Background())
	}
	if d.ctx.Err() != nil {
		d.mu.Unlock()
		return errors.New("this factory is closing")
	}
	a := h.agentOfLocked(ws.ID)
	if a.state.State != api.AgentIdle {
		d.mu.Unlock()
		return errAgentBusy
	}
	ctx, cancel := context.WithCancel(d.ctx)
	a.cancel = cancel
	a.state = api.AgentState{State: api.AgentWorking, Turn: turn, For: asker, Since: now.Format(time.RFC3339Nano)}
	state := a.state
	d.wg.Add(1)
	d.mu.Unlock()

	h.addAgentEntry(ws.ID, api.AgentEntry{Kind: api.AgentEntryPerson, By: asker, Text: text, Turn: turn})
	h.publishAgent(api.AgentEvent{ID: ws.ID, AgentState: &state})
	door := &agentDoor{h: h, id: ws.ID, asker: asker, operator: v.Operator}
	run := AgentTurn{Workstream: ws.ID, Name: ws.Name, Turn: turn, Asker: asker, Text: text,
		Key: h.orgModelKey(), Dir: filepath.Join(h.agentDir(ws.ID), "agent"), Door: door,
		Step: func(kind, text, tool string, failed bool) {
			switch kind {
			case api.AgentEntryStep, api.AgentEntryAgent, api.AgentEntryNote:
			default:
				return
			}
			h.addAgentEntry(ws.ID, api.AgentEntry{Kind: kind, By: asker, Text: text, Tool: tool, Failed: failed, Turn: turn})
		}}
	guard.Go("factory workstream agent", func() {
		defer d.wg.Done()
		defer cancel()
		h.runAgentTurn(ctx, run)
	})
	return nil
}

// runAgentTurn answers one message, posts what it spent, and leaves the agent
// idle again.
func (h *Handler) runAgentTurn(ctx context.Context, run AgentTurn) {
	if err := os.MkdirAll(run.Dir, 0o700); err != nil {
		h.addAgentEntry(run.Workstream, api.AgentEntry{Kind: api.AgentEntryNote, Text: "The agent could not start: " + err.Error(), Turn: run.Turn})
		h.setAgentState(run.Workstream, api.AgentState{State: api.AgentIdle})
		return
	}
	end, err := h.config.WorkstreamAgent(ctx, run)
	stopped := ""
	h.agentDesk.mu.Lock()
	if a := h.agentOfLocked(run.Workstream); a.state.State == api.AgentStopping {
		stopped = a.state.For
	}
	h.agentDesk.mu.Unlock()
	switch {
	case stopped != "":
		h.addAgentEntry(run.Workstream, api.AgentEntry{Kind: api.AgentEntryNote, Text: "Stopped by " + stopped + ".", By: stopped, Turn: run.Turn})
	case err != nil && ctx.Err() == nil:
		h.addAgentEntry(run.Workstream, api.AgentEntry{Kind: api.AgentEntryNote, Text: "The agent could not finish: " + err.Error(), Turn: run.Turn})
	}
	if end.USD > 0 {
		h.postAgentSpend(run, end.USD)
	}
	h.setAgentState(run.Workstream, api.AgentState{State: api.AgentIdle})
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

// stopWorkstreamAgent is POST /v1/workstreams/{id}/agent/stop: anyone who
// can talk to the agent may stop the answer it is working on.
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
	h.agentDesk.mu.Lock()
	a := h.agentOfLocked(ws.ID)
	var state *api.AgentState
	if a.state.State == api.AgentWorking && a.cancel != nil {
		a.state.State, a.state.For = api.AgentStopping, who
		a.cancel()
		s := a.state
		state = &s
	}
	h.agentDesk.mu.Unlock()
	if state != nil {
		h.publishAgent(api.AgentEvent{ID: ws.ID, AgentState: state})
	}
	h.write(w, http.StatusOK, h.agentWire(ws, v, 0))
}

// closeAgents stops every answer in flight and waits for them; Close calls it.
func (h *Handler) closeAgents() {
	d := &h.agentDesk
	d.mu.Lock()
	if d.ctx == nil {
		d.ctx, d.stop = context.WithCancel(context.Background())
	}
	d.stop()
	d.mu.Unlock()
	d.wg.Wait()
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
	operator bool
}

// viewer is the asker as the access core sees them now: their teams are read
// again on every call, so a membership taken away mid-answer counts.
func (d *agentDoor) viewer() wsViewer {
	v := Viewer{Handle: factory.NormalizeHandle(d.asker), Operator: d.operator, Teams: map[string]bool{}}
	if v.Handle != "" {
		for _, team := range d.h.Membership().TeamsOf(v.Handle) {
			v.Teams[team] = true
		}
	}
	return v
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
	now := h.clock().UTC()
	stamp := now.Format(time.RFC3339Nano)
	if page == "" {
		title = strings.TrimSpace(title)
		if title == "" {
			return "", errors.New("a new page needs a title")
		}
		id, err := cell.NewID(now)
		if err != nil {
			return "", err
		}
		p := factory.WorkstreamPage{ID: "pg_" + id, Title: title, Kind: "page", BodyMD: body, UpdatedAt: stamp, UpdatedBy: v.Handle, Rev: 1}
		_, saved, err := h.editPart(d.id, v, func(cur *factory.OrgWorkstream, _ factory.OrgWorkstreams) (bool, error) {
			cur.Pages = append(cur.Pages, p)
			cur.UpdatedAt = now
			return true, cleanWorkstream(cur, v.Handle, now)
		})
		if err != nil {
			return "", agentWriteRefused(err)
		}
		h.recordAs(ctx, v.Handle, record.WorkstreamChanged, "added page", orgObjectWorkstream+d.id, p.ID+" by the agent")
		h.publishWorkstreamPart(saved, api.PartPage, p.ID, v.Handle, 1, false)
		return "made the page " + strconv.Quote(title) + " (" + p.ID + ")", nil
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
	live, err := h.pageDocLive(d.id, page)
	if err != nil {
		return "", err
	}
	if !live {
		// Nobody has opened the page's live document yet, so its words are
		// these: the first person to open it starts from them.
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
			h.recordAs(ctx, v.Handle, record.WorkstreamChanged, "changed page", orgObjectWorkstream+d.id, page+" by the agent")
			if w, ok := after[d.id]; ok {
				h.publishWorkstreamPart(w, api.PartPage, page, v.Handle, cur.Rev, false)
			}
			return "wrote the page " + strconv.Quote(cur.Title), nil
		}
	}
	saved, added, err := h.config.Store.AddPageVersion(d.id, page, store.PageVersion{At: stamp, By: v.Handle, Title: cur.Title, BodyMD: body, Reason: "agent"})
	if err != nil {
		return "", err
	}
	if added {
		h.recordAs(ctx, v.Handle, record.WorkstreamChanged, "changed page", orgObjectWorkstream+d.id, page+" version "+strconv.Itoa(saved.Version)+" by the agent")
		h.publishWorkstreamPart(ws, api.PartVersions, page, v.Handle, saved.Version, false)
	}
	return "the page " + strconv.Quote(cur.Title) + " is open for editing, so your text was kept as version " +
		strconv.Itoa(saved.Version) + " in its history; anyone who can edit it restores that version to apply it", nil
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
	return "proposed the decision (" + id + "); a person keeps it", nil
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
