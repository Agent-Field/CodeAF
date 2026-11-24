package serve

import (
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// accessTranscript uses the same refusal for an absent and an inaccessible
// transcript so the caller cannot learn whether its conversation exists.
func (h *Handler) accessTranscript(w http.ResponseWriter, r *http.Request) {
	kind, id := r.PathValue("kind"), r.PathValue("id")
	v, err := h.itemViewer(r)
	if h.failed(w, err) {
		return
	}
	if h.config.Store == nil || !v.Member && !v.Operator || kind != chatKind && kind != store.SessionKind {
		h.transcriptNotFound(w)
		return
	}
	since, badSince := agentSince(r.URL.Query().Get("since"))
	out, found, err := h.readAccessTranscript(v, kind, id, since)
	if h.failed(w, err) {
		return
	}
	if !found {
		h.transcriptNotFound(w)
		return
	}
	if badSince != nil {
		h.fail(w, http.StatusBadRequest, badSince.Error())
		return
	}
	h.write(w, http.StatusOK, out)
}

func (h *Handler) transcriptNotFound(w http.ResponseWriter) {
	h.write(w, http.StatusNotFound, map[string]string{"error": "resource not found", "reason": reasonFor(http.StatusNotFound)})
}

func (h *Handler) readAccessTranscript(v Viewer, kind, id string, since int) (api.SessionTranscript, bool, error) {
	// A move must not change the folder between the access check and the read.
	h.agentDesk.mu.Lock()
	defer h.agentDesk.mu.Unlock()

	st := h.config.Store
	row, found, err := st.Resource(kind, id)
	if err != nil || !found || !safeWorkstreamFolder(row.Parent.ID) {
		return api.SessionTranscript{}, false, err
	}
	ws, found, err := st.Workstream(row.Parent.ID)
	if err != nil || !found || row.Parent != workstreamResource(ws).ref || !can(v, h.resourceItem(row, ws), acl.VerbView) {
		return api.SessionTranscript{}, false, err
	}

	out := api.SessionTranscript{ResourceRef: row.Resource, State: api.AgentIdle, Entries: []api.AgentEntry{}}
	var entries []api.AgentEntry
	switch {
	case kind == chatKind && chatLocationValid(row):
		seat := agentSeat{ws.ID, row.Owner.ID, row.ID}
		if !h.chatExists(seat) {
			return api.SessionTranscript{}, false, nil
		}
		out.State, out.Seq, entries = h.agentTranscript(seat)
	case kind == store.SessionKind && row.Owner.Kind == api.OwnerPerson &&
		personHandle.MatchString(row.Owner.ID) && id == store.AfiSessionID(ws.ID, row.Owner.ID):
		out.State, out.Seq, entries = h.agentTranscript(agentSeat{ws: ws.ID, person: row.Owner.ID})
	case kind == store.SessionKind && taskSession(Item{Kind: kind, ID: id}):
		key, ok, err := h.taskTranscriptKey(row, ws)
		if err != nil || !ok || !existsOnDisk(h.taskRunDir(key.ws, key.task)) {
			return api.SessionTranscript{}, false, err
		}
		if work := h.agentDesk.work[key]; work != nil && work.loaded {
			out.State, out.Seq, entries = work.state.State, work.seq, work.entries
		} else {
			entries, out.Seq = readAgentEntries(h.taskWorkFile(key))
		}
	default:
		return api.SessionTranscript{}, false, nil
	}

	out.Entries = append(out.Entries, agentEntriesSince(entries, since)...)
	return out, true, nil
}

// agentTranscript reads current memory or the saved lines without loading a
// seat, since loading an Afi seat can seed its files from an old conversation.
func (h *Handler) agentTranscript(seat agentSeat) (string, int, []api.AgentEntry) {
	if a := h.agentDesk.agents[seat]; a != nil && a.loaded {
		return a.state.State, a.seq, a.entries
	}
	entries, seq := readAgentEntries(filepath.Join(h.agentDir(seat), agentEntriesFile))
	return api.AgentIdle, seq, entries
}

func (h *Handler) taskTranscriptKey(row store.Resource, ws factory.OrgWorkstream) (taskRunKey, bool, error) {
	// Task ids come from the registered parent's tasks. A segment of a
	// client-supplied resource id is never used as a folder name.
	tasks, err := h.config.Store.WorkstreamTasks(ws.ID)
	if err != nil {
		return taskRunKey{}, false, err
	}
	for _, t := range tasks {
		if filepath.Base(t.ID) == t.ID && t.ID != "." && t.ID != ".." &&
			row.ID == store.TaskSessionID(ws.ID, t.ID) && row.Owner == (acl.Subject{Kind: t.Owner.Kind, ID: t.Owner.ID}) {
			return taskRunKey{ws.ID, t.ID}, true, nil
		}
	}
	return taskRunKey{}, false, nil
}

func agentSince(raw string) (int, error) {
	if raw = strings.TrimSpace(raw); raw == "" {
		return 0, nil
	}
	since, err := strconv.Atoi(raw)
	if err != nil || since < 0 {
		return 0, invalidf("since is the last seq you have")
	}
	return since, nil
}

func agentEntriesSince(entries []api.AgentEntry, since int) []api.AgentEntry {
	from := len(entries)
	for from > 0 && entries[from-1].Seq > since {
		from--
	}
	from = max(from, len(entries)-api.AgentEntriesMost)
	return entries[from:]
}
