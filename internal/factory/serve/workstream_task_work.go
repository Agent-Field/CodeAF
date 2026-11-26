package serve

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// ── A TASK'S WORKING CONVERSATION ──────────────────────────────────────────
//
// A task Afi works on the server runs in a seat of its own (workstream_agent_task.go),
// and what it says and does there is the task's WORKING CONVERSATION. IT IS
// SHARED THE WAY THE TASK IS: everyone who can see the workstream reads it,
// live, and its lines are kept beside the run's own folder so a reader who
// opens the task later reads them too (OWNER-DECISIONS 2026-11-17 §7, §8).
//
// WHO STEERS IT. The task's owner, and whoever started the run that is going,
// may write to the agent while it works; the words reach the run at its next
// step and are a line of the conversation everyone reads. Nobody else has the
// verb: their words on a task are comments, which an agent never acts on. A
// steer needs a run going; with none, there is nobody to hear it, and the
// refusal says to start the task again or comment.
//
// A DONE TASK IS REVISED, NEVER REOPENED. With no run going, the owner's (or
// the last starter's) words to a done Afi task start a REVISION RUN on the
// same task: Afi changes its result as asked and the task stays done. A
// comment that asks for a change gets Afi's offer "Revise with this" under
// it; anyone who can edit may take it, which steers the run going or starts a
// revision with the comment's words (org-space DESIGN §11.5).
//
// AFI'S PANE AND A PERSON'S OWN CHATS STAY PRIVATE. Only a task's run is
// shared; the per-person conversations in workstream_agent.go are untouched.

// taskWorkSteersQueued is how many steers may wait for a run's next step.
const taskWorkSteersQueued = 8

// errNothingWorking is the refusal for a steer when no run is going.
var errNothingWorking = errors.New("nothing is working on this task now; start it again, or leave a comment")

// errNotSteerer is the refusal for a steer from someone who is neither the
// task's owner nor who started the run.
var errNotSteerer = errors.New("only the task's owner or whoever started this run can steer it; leave a comment instead")

// taskWork is one task's Working conversation as the server holds it.
type taskWork struct {
	loaded  bool
	entries []api.AgentEntry
	seq     int
	state   api.AgentState
	// steers is the run's inbox while it goes; nil when no run is going.
	steers chan string
	// usd is what every run of this task has spent, kept in its own file.
	usd float64
}

// taskWorkOfLocked is key's conversation, read from disk on first use. The
// desk's lock is held.
func (h *Handler) taskWorkOfLocked(key taskRunKey) *taskWork {
	d := &h.agentDesk
	if d.work == nil {
		d.work = map[taskRunKey]*taskWork{}
	}
	w, ok := d.work[key]
	if !ok {
		w = &taskWork{state: api.AgentState{State: api.AgentIdle}}
		d.work[key] = w
	}
	if !w.loaded {
		w.loaded = true
		w.entries, w.seq = readAgentEntries(h.taskWorkFile(key))
		if raw, err := os.ReadFile(h.taskWorkSpentFile(key)); err == nil {
			w.usd, _ = strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
		}
	}
	return w
}

// taskWorkFile keeps every line of key's conversation, beside the run's own
// folder (taskRunDir), which holds the agent's memory of it.
func (h *Handler) taskWorkFile(key taskRunKey) string {
	return filepath.Join(h.taskRunDir(key.ws, key.task), agentEntriesFile)
}

// taskWorkSpentFile keeps what key's runs have spent, in dollars.
func (h *Handler) taskWorkSpentFile(key taskRunKey) string {
	return filepath.Join(h.taskRunDir(key.ws, key.task), "spent")
}

// addTaskWorkEntry keeps one line of key's conversation and publishes it to
// everyone who can see the workstream.
func (h *Handler) addTaskWorkEntry(key taskRunKey, e api.AgentEntry) api.AgentEntry {
	h.agentDesk.mu.Lock()
	w := h.taskWorkOfLocked(key)
	w.seq++
	e.Seq = w.seq
	e.At = h.clock().UTC().Format(time.RFC3339Nano)
	w.entries = append(w.entries, e)
	if len(w.entries) > agentEntriesKept {
		w.entries = slices.Clone(w.entries[len(w.entries)-agentEntriesKept:])
	}
	// Written under the lock so the file's lines stay in seq order.
	if raw, err := json.Marshal(e); err == nil {
		path := h.taskWorkFile(key)
		if os.MkdirAll(filepath.Dir(path), 0o700) == nil {
			if f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
				_, _ = f.Write(append(raw, '\n'))
				_ = f.Close()
			}
		}
	}
	h.agentDesk.mu.Unlock()
	if e.Seq == 1 && existsOnDisk(h.taskWorkFile(key)) {
		h.keepTaskSession(key)
	}
	h.publishTaskWork(key, api.TaskWorkEvent{Entry: &e})
	return e
}

// publishTaskWork writes one task.work event. Who hears it is decided as it
// is sent (taskWorkForViewer): whoever can see the workstream then.
func (h *Handler) publishTaskWork(key taskRunKey, e api.TaskWorkEvent) {
	e.ID, e.Task = key.ws, key.task
	_ = live.Append(h.eventRoot(), live.New(api.EventTaskWork, 0, e))
}

// taskWorkForViewer lets a task.work event through to everyone who can see
// its workstream now.
func (h *Handler) taskWorkForViewer(r *http.Request, e live.Event) (live.Event, bool) {
	var body api.TaskWorkEvent
	if json.Unmarshal(e.Data, &body) != nil || body.ID == "" || body.Task == "" {
		return e, false
	}
	known, ok, err := h.workstreamCan(r, body.ID, factory.RoleView)
	return e, known && ok && err == nil
}

// openTaskWork marks key's run going, started by starter, and answers the
// inbox its steers arrive in. The run reads it until it ends; closeTaskWork
// then says it ended. A run an answer started says so: the person answered
// and Afi carries on, never a second "Started by" (live test 2026-11-23 #44).
func (h *Handler) openTaskWork(key taskRunKey, turn, starter string, answered bool) <-chan string {
	now := h.clock().UTC()
	h.agentDesk.mu.Lock()
	w := h.taskWorkOfLocked(key)
	w.steers = make(chan string, taskWorkSteersQueued)
	w.state = api.AgentState{State: api.AgentWorking, Turn: turn, For: starter, Since: now.Format(time.RFC3339Nano)}
	state, steers := w.state, w.steers
	h.agentDesk.mu.Unlock()
	h.publishTaskWork(key, api.TaskWorkEvent{AgentState: &state})
	text := startedByPrefix + starter
	if answered {
		text = starter + answeredSuffix
	}
	h.addTaskWorkEntry(key, api.AgentEntry{Kind: api.AgentEntryNote, By: starter, Text: text, Turn: turn})
	return steers
}

// closeTaskWork says key's run ended, having spent usd. A steer that arrived
// too late for any step is said so, never silently dropped; why is the run's
// failure, if any.
func (h *Handler) closeTaskWork(key taskRunKey, turn string, why error, usd float64) {
	h.agentDesk.mu.Lock()
	w := h.taskWorkOfLocked(key)
	late := 0
	if w.steers != nil {
		for drained := false; !drained; {
			select {
			case <-w.steers:
				late++
			default:
				drained = true
			}
		}
	}
	w.steers = nil
	w.state = api.AgentState{State: api.AgentIdle}
	if usd > 0 {
		w.usd += usd
		if os.MkdirAll(filepath.Dir(h.taskWorkSpentFile(key)), 0o700) == nil {
			_ = os.WriteFile(h.taskWorkSpentFile(key), []byte(strconv.FormatFloat(w.usd, 'f', -1, 64)), 0o600)
		}
	}
	state := w.state
	h.agentDesk.mu.Unlock()
	if why != nil {
		h.addTaskWorkEntry(key, api.AgentEntry{Kind: api.AgentEntryNote, Text: "Afi could not finish: " + cutRunes(why.Error(), 300), Turn: turn})
	}
	if late > 0 {
		h.addTaskWorkEntry(key, api.AgentEntry{Kind: api.AgentEntryNote, Text: "The work ended before Afi read the last message; start it again to use it.", Turn: turn})
	}
	h.publishTaskWork(key, api.TaskWorkEvent{AgentState: &state})
}

// The words a run's opening line says: "Started by mia", or, for a run an
// answer started, "mia answered; Afi carries on". The desk names the person.
const (
	startedByPrefix = "Started by "
	answeredSuffix  = " answered; Afi carries on"
)

// lastStarterLocked is who started key's latest run, read from its opening
// line so it outlives a restart. The desk's lock is held.
func lastStarterLocked(w *taskWork) string {
	for i := len(w.entries) - 1; i >= 0; i-- {
		if e := w.entries[i]; e.Kind == api.AgentEntryNote && e.By != "" && (strings.HasPrefix(e.Text, startedByPrefix) || strings.HasSuffix(e.Text, answeredSuffix)) {
			return e.By
		}
	}
	return ""
}

// revisable is whether t takes a revision run: Afi does it and it is done.
func revisable(t factory.WorkstreamTask) bool {
	return t.DoneBy.Kind == factory.TaskByAfi && t.State == factory.TaskDone
}

// revisionWords is what Afi is told on a revision run: the task, and what to
// change, with the rule that the task stays done.
func revisionWords(t factory.WorkstreamTask, who, words string) string {
	return taskWords(t) + "\n\nThis task is already done. " + firstNonEmpty(who, "Someone") + " asks for a revision: " + strings.TrimSpace(words) +
		"\n\nRevise its result as asked. Rewrite the document you made with ws_file_create (set file), or suggest the change with ws_file_suggest if a person has edited it. " +
		"Keep the task done: never set it up next. Post one line on the task with ws_task_comment saying what you changed."
}

// startRevision starts a revision run on the done task t, as starter, after
// the line that asked for it.
// words are a person's (their revise note, or the comment by a person whose
// offer they took), so the run may follow an address in them.
func (h *Handler) startRevision(ws factory.OrgWorkstream, t factory.WorkstreamTask, starter, words string) error {
	return h.startTaskRunTyped(ws, t, starter, revisionWords(t, starter, words), words, 0, false)
}

// requestLike is a comment that asks the doer for a change: it opens with a
// verb of change or a polite ask, or says "can you" or "please" anywhere.
var requestLike = regexp.MustCompile(`(?i)^\s*(please|can you|could you|would you|can we|could we|add|change|make|use|remove|drop|rewrite|reword|shorten|lengthen|cut|fix|include|swap|replace|update|try|give|tighten|split|merge|turn|move|rename)\b|\b(can|could|would) you\b|\bplease\b`)

// reviseOfferWords is Afi's offer under a comment that asks for a change.
const reviseOfferWords = "Revise with this"

// offerRevision hangs Afi's "Revise with this" under comment, when the
// comment asks for a change and Afi's task can still take it (running, or
// done). Anything else is left a comment.
func (h *Handler) offerRevision(ws factory.OrgWorkstream, t factory.WorkstreamTask, comment factory.WorkstreamTaskEvent) {
	if t.DoneBy.Kind != factory.TaskByAfi || (t.State != factory.TaskDone && t.State != factory.TaskRunning) || !requestLike.MatchString(comment.Text) {
		return
	}
	afi := factory.TaskEventBy{Kind: factory.TaskByAfi, Name: "Afi"}
	actor := factory.TaskActor{Edit: true}
	_ = h.config.Store.EditWorkstreamTasks(func(tw store.TaskWrite) error {
		thread, err := tw.Events(ws.ID, t.ID)
		if err != nil {
			return err
		}
		out, err := factory.Note(t, factory.TaskNote{Kind: factory.TaskEventOffer, Text: reviseOfferWords, OfferFor: comment.ID}, actor, afi, thread, h.newTaskID("te_"), h.clock().UTC())
		if err != nil {
			return err
		}
		return tw.Append(ws.ID, out.Events...)
	})
	h.publishTasks(ws, "", t)
}

// reviseWith hands words, from the comment by commenter that taker took Afi's
// offer under, to t's run: into the run going, or as a revision run on a done
// task. Its line in the Working conversation names both.
func (h *Handler) reviseWith(ws factory.OrgWorkstream, t factory.WorkstreamTask, taker string, commenter factory.TaskEventBy, words string) {
	key := taskRunKey{ws.ID, t.ID}
	h.agentDesk.mu.Lock()
	going := h.taskWorkOfLocked(key).steers != nil
	turn := h.taskWorkOfLocked(key).state.Turn
	h.agentDesk.mu.Unlock()
	line := words
	if factory.NormalizeHandle(commenter.ID) != taker {
		line = "From " + firstNonEmpty(commenter.Name, commenter.ID, "a") + "'s comment: " + words
	}
	h.addTaskWorkEntry(key, api.AgentEntry{Kind: api.AgentEntryPerson, By: taker, Text: line, Turn: turn})
	if going {
		h.agentDesk.mu.Lock()
		sent := false
		if cur := h.taskWorkOfLocked(key); cur.steers != nil {
			select {
			case cur.steers <- line:
				sent = true
			default:
			}
		}
		h.agentDesk.mu.Unlock()
		if sent {
			return
		}
	}
	if revisable(t) {
		if err := h.startRevision(ws, t, taker, line); err == nil {
			return
		}
	}
	h.addTaskWorkEntry(key, api.AgentEntry{Kind: api.AgentEntryNote, Text: "Afi could not take that now. Start the task again to use it.", Turn: turn})
}

// taskWorkStep is the Step of key's run: each line becomes a line of the
// task's Working conversation.
func (h *Handler) taskWorkStep(key taskRunKey, turn string) func(kind, text, tool string, failed bool) {
	return func(kind, text, tool string, failed bool) {
		switch kind {
		case api.AgentEntryStep, api.AgentEntryAgent, api.AgentEntryNote:
		default:
			return
		}
		h.addTaskWorkEntry(key, api.AgentEntry{Kind: kind, Text: text, Tool: tool, Failed: failed, Turn: turn})
	}
}

// taskSteerer is whether v may steer t's run while one is going: they own
// the task, they started the run (startedBy), or the task is everyone's and
// they can edit the workstream.
func taskSteerer(ws factory.OrgWorkstream, v wsViewer, t factory.WorkstreamTask, startedBy string) bool {
	me := factory.NormalizeHandle(v.Handle)
	if me == "" || !v.canSee(ws) {
		return false
	}
	switch {
	case t.Owner.Kind == factory.TaskOwnerPerson && factory.NormalizeHandle(t.Owner.ID) == me:
		return true
	case startedBy != "" && factory.NormalizeHandle(startedBy) == me:
		return true
	}
	return t.Owner.Kind == factory.TaskOwnerEveryone && v.canWrite(ws)
}

// taskWorkWire is t's Working conversation as v reads it.
func (h *Handler) taskWorkWire(ws factory.OrgWorkstream, v wsViewer, t factory.WorkstreamTask, since int) api.TaskWork {
	key := taskRunKey{ws.ID, t.ID}
	out := api.TaskWork{ID: ws.ID, Task: t.ID, Entries: []api.AgentEntry{}}
	h.agentDesk.mu.Lock()
	w := h.taskWorkOfLocked(key)
	out.AgentState, out.Seq, out.USD = w.state, w.seq, w.usd
	starter := firstNonEmpty(w.state.For, lastStarterLocked(w))
	out.Entries = append(out.Entries, agentEntriesSince(w.entries, since)...)
	going := w.steers != nil
	h.agentDesk.mu.Unlock()
	out.Steerer = taskSteerer(ws, v, t, starter)
	open := ws.ArchivedAt == nil && v.canWrite(ws)
	out.CanSteer = out.Steerer && going && open
	out.CanRevise = out.Steerer && !going && open && revisable(t) && h.serverAgentServed()
	return out
}

// getTaskWork is GET /v1/workstreams/{id}/tasks/{tid}/work?since=: the task's
// Working conversation, for anyone who can see the workstream.
func (h *Handler) getTaskWork(w http.ResponseWriter, r *http.Request) {
	ws, v, t, _, ok := h.oneTask(w, r)
	if !ok {
		return
	}
	since, err := agentSince(r.URL.Query().Get("since"))
	if err != nil {
		h.fail(w, http.StatusBadRequest, err.Error())
		return
	}
	h.write(w, http.StatusOK, h.taskWorkWire(ws, v, t, since))
}

// postTaskSteer is POST /v1/workstreams/{id}/tasks/{tid}/work/messages: the
// task's owner, or whoever started its run, steers the run that is going.
// The words are a line of the conversation everyone reads, and reach the
// agent at its next step. The answer is 202 with the conversation.
func (h *Handler) postTaskSteer(w http.ResponseWriter, r *http.Request) {
	ws, v, t, _, ok := h.oneTask(w, r)
	if !ok {
		return
	}
	var body api.TaskSteerNew
	if !h.decode(w, r, &body, false) {
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" || len(text) > api.AgentMessageMost {
		h.fail(w, http.StatusUnprocessableEntity, "a message is 1 to 16384 bytes of words")
		return
	}
	if ws.ArchivedAt != nil {
		h.fail(w, http.StatusConflict, errWorkstreamArchived.Error())
		return
	}
	key := taskRunKey{ws.ID, t.ID}
	h.agentDesk.mu.Lock()
	cur := h.taskWorkOfLocked(key)
	going, startedBy, turn := cur.steers != nil, firstNonEmpty(cur.state.For, lastStarterLocked(cur)), cur.state.Turn
	h.agentDesk.mu.Unlock()
	if !taskSteerer(ws, v, t, startedBy) {
		h.fail(w, http.StatusForbidden, errNotSteerer.Error())
		return
	}
	me := factory.NormalizeHandle(v.Handle)
	if !going && revisable(t) && v.canWrite(ws) {
		// A done task is revised, never reopened: the words start a revision run.
		h.addTaskWorkEntry(key, api.AgentEntry{Kind: api.AgentEntryPerson, By: me, Text: text})
		if err := h.startRevision(ws, t, me, text); err != nil {
			h.addTaskWorkEntry(key, api.AgentEntry{Kind: api.AgentEntryNote, Text: "Afi could not start the revision: " + err.Error()})
			h.fail(w, http.StatusConflict, err.Error())
			return
		}
		h.write(w, http.StatusAccepted, h.taskWorkWire(ws, v, t, 0))
		return
	}
	if !going {
		h.write(w, http.StatusConflict, map[string]string{"error": errNothingWorking.Error(), "reason": "idle"})
		return
	}
	// The line first, so everyone reads the words before the answer to them.
	h.addTaskWorkEntry(key, api.AgentEntry{Kind: api.AgentEntryPerson, By: me, Text: text, Turn: turn})
	h.agentDesk.mu.Lock()
	sent := false
	if cur := h.taskWorkOfLocked(key); cur.steers != nil {
		select {
		case cur.steers <- text:
			sent = true
		default:
		}
	}
	h.agentDesk.mu.Unlock()
	if !sent {
		h.addTaskWorkEntry(key, api.AgentEntry{Kind: api.AgentEntryNote, Text: "Afi did not read that message: the work ended, or too many were waiting. Start the task again to use it.", Turn: turn})
	}
	h.write(w, http.StatusAccepted, h.taskWorkWire(ws, v, t, 0))
}
