package serve

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
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
	}
	return w
}

// taskWorkFile keeps every line of key's conversation, beside the run's own
// folder (taskRunDir), which holds the agent's memory of it.
func (h *Handler) taskWorkFile(key taskRunKey) string {
	return filepath.Join(h.taskRunDir(key.ws, key.task), "entries.jsonl")
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
// then says it ended.
func (h *Handler) openTaskWork(key taskRunKey, turn, starter string) <-chan string {
	now := h.clock().UTC()
	h.agentDesk.mu.Lock()
	w := h.taskWorkOfLocked(key)
	w.steers = make(chan string, taskWorkSteersQueued)
	w.state = api.AgentState{State: api.AgentWorking, Turn: turn, For: starter, Since: now.Format(time.RFC3339Nano)}
	state, steers := w.state, w.steers
	h.agentDesk.mu.Unlock()
	h.publishTaskWork(key, api.TaskWorkEvent{AgentState: &state})
	h.addTaskWorkEntry(key, api.AgentEntry{Kind: api.AgentEntryNote, By: starter, Text: "Started by " + starter, Turn: turn})
	return steers
}

// closeTaskWork says key's run ended. A steer that arrived too late for any
// step is said so, never silently dropped; why is the run's failure, if any.
func (h *Handler) closeTaskWork(key taskRunKey, turn string, why error) {
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
	out.AgentState, out.Seq = w.state, w.seq
	from := len(w.entries)
	for from > 0 && w.entries[from-1].Seq > since {
		from--
	}
	if len(w.entries)-from > api.AgentEntriesMost {
		from = len(w.entries) - api.AgentEntriesMost
	}
	out.Entries = append(out.Entries, w.entries[from:]...)
	going := w.steers != nil
	h.agentDesk.mu.Unlock()
	out.Steerer = taskSteerer(ws, v, t, out.For)
	out.CanSteer = out.Steerer && going && ws.ArchivedAt == nil
	return out
}

// getTaskWork is GET /v1/workstreams/{id}/tasks/{tid}/work?since=: the task's
// Working conversation, for anyone who can see the workstream.
func (h *Handler) getTaskWork(w http.ResponseWriter, r *http.Request) {
	ws, v, t, _, ok := h.oneTask(w, r)
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
	going, startedBy, turn := cur.steers != nil, cur.state.For, cur.state.Turn
	h.agentDesk.mu.Unlock()
	if !taskSteerer(ws, v, t, startedBy) {
		h.fail(w, http.StatusForbidden, errNotSteerer.Error())
		return
	}
	if !going {
		h.write(w, http.StatusConflict, map[string]string{"error": errNothingWorking.Error(), "reason": "idle"})
		return
	}
	me := factory.NormalizeHandle(v.Handle)
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
