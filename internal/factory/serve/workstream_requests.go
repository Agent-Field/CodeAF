package serve

import (
	"net/http"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// ── A REQUEST INTO ANOTHER WORKSTREAM (Workstreams final spec, "Working with
// other people": "Work for another team is a request into their workstream.
// It becomes a task there, owned by that workstream's owner of incoming
// work.") ──
//
// Anyone in the organization may send one, into any workstream that is not
// private, whether or not they are in it. They see only what they sent: the
// task's id, title and owner, never the workstream's other work. The work it
// came from, when they can edit it, gains one line saying where it went.

func (h *Handler) workstreamRequestRoutes() {
	h.route("/v1/workstreams/requestable", map[string]http.HandlerFunc{http.MethodGet: h.requestableWorkstreams})
	h.route("/v1/workstreams/{id}/requests", map[string]http.HandlerFunc{http.MethodPost: h.requestIntoWorkstream})
}

// requestableBy is whether v may send a request into ws: it is open, and v
// is in it or it is not private.
func requestableBy(v Viewer, ws factory.OrgWorkstream) bool {
	return ws.ArchivedAt == nil && !ws.Space && v.Handle != "" && (v.canSee(ws) || !ws.Private)
}

// requestableWorkstreams lists every workstream the caller may send a
// request into, by name, with who receives it (its owner of incoming work).
func (h *Handler) requestableWorkstreams(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) {
		return
	}
	v, err := h.wsViewer(r)
	if h.failed(w, err) {
		return
	}
	all, err := h.config.Store.Workstreams()
	if h.failed(w, err) {
		return
	}
	nameOf := h.personNames()
	out := api.RequestableWorkstreams{Workstreams: []api.RequestableWorkstream{}}
	for _, ws := range all {
		if !requestableBy(v, ws) {
			continue
		}
		one := api.RequestableWorkstream{ID: ws.ID, Name: ws.Name, Parent: ws.Parent, In: v.canSee(ws)}
		if o := taskDefaultOwner(ws); o.Kind == factory.TaskOwnerPerson {
			one.Receiver = &factory.PersonRef{ID: o.ID, Name: nameOf(o.ID)}
		}
		out.Workstreams = append(out.Workstreams, one)
	}
	slices.SortFunc(out.Workstreams, func(a, b api.RequestableWorkstream) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	h.write(w, http.StatusOK, out)
}

func (h *Handler) requestIntoWorkstream(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) {
		return
	}
	v, err := h.wsViewer(r)
	if h.failed(w, err) {
		return
	}
	ws, ok, err := h.config.Store.Workstream(r.PathValue("id"))
	if h.failed(w, err) {
		return
	}
	if !ok || !requestableBy(v, ws) {
		h.fail(w, http.StatusNotFound, errWorkstreamGone.Error())
		return
	}
	var body factory.TaskRequest
	if !h.decode(w, r, &body, false) {
		return
	}
	nameOf := h.personNames()
	me := factory.PersonRef{ID: v.Handle, Name: nameOf(v.Handle)}
	from := factory.TaskCameFrom{Kind: factory.TaskByPerson, ID: v.Handle, Name: me.Name}
	// The work it came from: named on the new task, and told where it went,
	// only when the caller may edit it.
	var source factory.OrgWorkstream
	if body.FromWS != "" && body.FromWS != ws.ID {
		if src, found, err := h.config.Store.Workstream(body.FromWS); err == nil && found && v.canWrite(src) && src.ArchivedAt == nil {
			source, from.WS, from.WSName = src, src.ID, src.Name
		}
	}
	incoming := taskDefaultOwner(ws)
	if incoming.Kind == factory.TaskOwnerPerson {
		incoming.Name = nameOf(incoming.ID)
	}
	now := h.clock().UTC()
	var made factory.WorkstreamTask
	jobs, err := h.workstreamJobs(r, ws)
	if h.failed(w, err) {
		return
	}
	by := factory.TaskEventBy{Kind: factory.TaskByPerson, ID: v.Handle, Name: me.Name}
	err = h.config.Store.EditWorkstreamTasks(func(tw store.TaskWrite) error {
		all, err := readTasksIn(tw, ws, jobs)
		if err != nil {
			return err
		}
		if store.CountWorkstreamTasks(all) >= factory.TasksMost {
			return factory.TaskInvalid("this workstream is full")
		}
		made, err = factory.RequestTask(ws.ID, h.newTaskID("tk_")(), body, me, from, incoming, all, now)
		if err != nil {
			return err
		}
		// A person's request carries their words; an agent's or a bot's does
		// not, nor one whose client says Afi wrote it.
		factory.StampWords(&made, factory.WordsAuthor(wordsKind(r), body.WordsBy))
		if err := tw.Put(made, 0); err != nil {
			return err
		}
		said := "Requested by " + me.Name
		if from.WSName != "" {
			said += " from " + from.WSName
		}
		stamp := now.Format("2006-01-02T15:04:05.999999999Z07:00")
		if err := tw.Append(ws.ID, factory.WorkstreamTaskEvent{ID: h.newTaskID("te_")(), Task: made.ID, At: stamp, By: by, Kind: factory.TaskEventRequestNote, Text: said}); err != nil {
			return err
		}
		// The task it came from says where the request went: one line, never
		// the receiving workstream's work.
		if source.ID != "" && body.FromTask != "" {
			kept, err := tw.Tasks(source.ID)
			if err != nil {
				return err
			}
			if slices.ContainsFunc(kept, func(t factory.WorkstreamTask) bool { return t.ID == body.FromTask }) {
				return tw.Append(source.ID, factory.WorkstreamTaskEvent{ID: h.newTaskID("te_")(), Task: body.FromTask, At: stamp, By: by,
					Kind: factory.TaskEventRequestNote, Text: "Sent a request to " + ws.Name + ": " + made.Title})
			}
		}
		return nil
	})
	if h.taskRefused(w, err) {
		return
	}
	h.recordWorkstream(r, ws.ID, "added task", made.ID)
	h.publishTasks(ws, h.orgActor(r), made)
	if source.ID != "" && body.FromTask != "" {
		h.publishWorkstreamPart(source, api.PartTasks, body.FromTask, h.orgActor(r), 0, false)
	}
	out := api.RequestSent{ID: made.ID, WS: ws.ID, WSName: ws.Name, Title: made.Title, Owner: made.Owner}
	if v.canSee(ws) {
		t := nameTask(nameOf, made)
		out.Task = &t
	}
	h.write(w, http.StatusCreated, out)
}
