package serve

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// ── A WORKSTREAM'S TASKS ───────────────────────────────────────────────────
//
// The routes of api/workstream_tasks.go. The rules are internal/factory's
// workstream_task.go; this file decides who the caller is to them and keeps
// what they change:
//
//   - VIEW READS, EDIT WRITES. Anyone who can see the workstream reads its
//     tasks and threads; anyone who can edit it adds a task, comments, hands a
//     task on, moves it and starts a group. An archived workstream takes no
//     change.
//   - THE ASK IS THE OWNER'S, and the tick of a person task. With owner
//     Everyone, anyone who edits; the first answer decides.
//   - A JOB OF THE WORKSTREAM IS A TASK, read from the job each time
//     ([factory.JobTask]); the jobs are those [Handler.jobWorkstreamOf]
//     places here (membership, else the go-ahead group), and only those the
//     caller can see. Answering its Ask goes through the job's own answer door.
//
// Every change publishes workstream.changed with part "tasks".

func (h *Handler) workstreamTaskRoutes() {
	h.route("/v1/workstreams/{id}/tasks", map[string]http.HandlerFunc{http.MethodGet: h.listWorkstreamTasks, http.MethodPost: h.createWorkstreamTask})
	h.route("/v1/workstreams/{id}/tasks/start_all", map[string]http.HandlerFunc{http.MethodPost: h.startAllWorkstreamTasks})
	h.route("/v1/workstreams/{id}/tasks/{tid}", map[string]http.HandlerFunc{http.MethodGet: h.getWorkstreamTask, http.MethodPatch: h.patchWorkstreamTask})
	h.route("/v1/workstreams/{id}/tasks/{tid}/events", map[string]http.HandlerFunc{http.MethodGet: h.workstreamTaskEvents})
	h.route("/v1/workstreams/{id}/tasks/{tid}/answer", map[string]http.HandlerFunc{http.MethodPost: h.answerWorkstreamTask})
	h.route("/v1/workstreams/{id}/tasks/{tid}/comment", map[string]http.HandlerFunc{http.MethodPost: h.commentWorkstreamTask})
}

var errTaskAnswered = errors.New("this question was already answered")

// ── who the caller is to the rules ──

func taskActor(v Viewer, ws factory.OrgWorkstream) factory.TaskActor {
	return factory.TaskActor{Handle: v.Handle, Edit: v.canWrite(ws) && ws.ArchivedAt == nil}
}

// taskBy is who a change is stamped with on the thread: the caller, or Afi
// when the workstream's agent made it for them (the caller's rights are
// still the ones checked).
func (h *Handler) taskBy(r *http.Request, v Viewer) factory.TaskEventBy {
	if call, ok := agentCallOf(r); ok && call.by.Kind != "" {
		return call.by
	}
	return factory.TaskEventBy{Kind: factory.TaskByPerson, ID: v.Handle, Name: v.Handle}
}

// taskDefaultOwner owns work nobody asked for directly: the workstream's
// "Owner of incoming work" (rules.default_owner), a named person or
// Everyone. A workstream that never set its rules gives it to its steward.
func taskDefaultOwner(ws factory.OrgWorkstream) factory.TaskOwner {
	o := ws.RulesOrDefault().DefaultOwner
	if o.Kind == factory.RulesOwnerPerson && o.ID != "" {
		name := o.Name
		if name == "" {
			name = o.ID
		}
		return factory.TaskOwner{Kind: factory.TaskOwnerPerson, ID: o.ID, Name: name}
	}
	return factory.TaskOwner{Kind: factory.TaskOwnerEveryone}
}

// taskFallback is the workstream's "If unanswered for a day" person
// (rules.fallback), "" for none: an Ask unanswered for a day goes to them
// too.
func taskFallback(ws factory.OrgWorkstream) string {
	if f := ws.RulesOrDefault().Fallback; f != nil {
		return f.ID
	}
	return ""
}

func (h *Handler) newTaskID(prefix string) func() string {
	return func() string {
		id, err := cell.NewID(h.clock())
		if err != nil {
			id = fmt.Sprint(h.clock().UnixNano())
		}
		return prefix + id
	}
}

// ── reading ──

// workstreamJobs are the jobs of ws the caller may see, by number.
func (h *Handler) workstreamJobs(r *http.Request, ws factory.OrgWorkstream) (map[int]factory.Item, error) {
	items, err := h.config.Store.List()
	if err != nil {
		return nil, err
	}
	member, err := h.config.Store.JobWorkstreams()
	if err != nil {
		return nil, err
	}
	sight := h.jobSight(r)
	out := map[int]factory.Item{}
	for _, it := range items {
		if h.jobWorkstreamOf(it, member) != ws.ID || !h.repoVisible(r, itemRepo(it)) || !sight(it) {
			continue
		}
		out[it.ID] = it
	}
	return out, nil
}

// readTasks is every task of ws as the caller sees it: the kept tasks, then
// each job as a task (its overlay applied), oldest first. A job's overlay whose
// job left the workstream is not shown.
func (h *Handler) readTasks(r *http.Request, ws factory.OrgWorkstream) ([]factory.WorkstreamTask, error) {
	kept, err := h.config.Store.WorkstreamTasks(ws.ID)
	if err != nil {
		return nil, err
	}
	jobs, err := h.workstreamJobs(r, ws)
	if err != nil {
		return nil, err
	}
	overlays := map[int]*factory.WorkstreamTask{}
	out := make([]factory.WorkstreamTask, 0, len(kept)+len(jobs))
	for i := range kept {
		if n, ok := factory.IsJobTask(kept[i].ID); ok {
			overlays[n] = &kept[i]
			continue
		}
		out = append(out, kept[i])
	}
	numbers := make([]int, 0, len(jobs))
	for n := range jobs {
		numbers = append(numbers, n)
	}
	slices.Sort(numbers)
	owner := taskDefaultOwner(ws)
	for _, n := range numbers {
		out = append(out, factory.JobTask(jobs[n], ws.ID, overlays[n], owner))
	}
	slices.SortStableFunc(out, func(a, b factory.WorkstreamTask) int { return strings.Compare(a.CreatedAt, b.CreatedAt) })
	nameTasks(h.personNames(), out)
	return out, nil
}

func findTask(all []factory.WorkstreamTask, id string) (factory.WorkstreamTask, bool) {
	i := slices.IndexFunc(all, func(t factory.WorkstreamTask) bool { return t.ID == id })
	if i < 0 {
		return factory.WorkstreamTask{}, false
	}
	return all[i], true
}

func (h *Handler) listWorkstreamTasks(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := h.findWorkstream(w, r)
	if !ok {
		return
	}
	all, err := h.readTasks(r, ws)
	if h.failed(w, err) {
		return
	}
	h.write(w, http.StatusOK, api.WorkstreamTasks{Tasks: all})
}

// oneTask is the workstream and the task the path names, with every task of
// the workstream, when the caller can see them; otherwise the refusal is
// written.
func (h *Handler) oneTask(w http.ResponseWriter, r *http.Request) (factory.OrgWorkstream, Viewer, factory.WorkstreamTask, []factory.WorkstreamTask, bool) {
	ws, v, ok := h.findWorkstream(w, r)
	if !ok {
		return ws, v, factory.WorkstreamTask{}, nil, false
	}
	all, err := h.readTasks(r, ws)
	if h.failed(w, err) {
		return ws, v, factory.WorkstreamTask{}, nil, false
	}
	t, found := findTask(all, r.PathValue("tid"))
	if !found {
		h.fail(w, http.StatusNotFound, store.ErrTaskGone.Error())
		return ws, v, t, nil, false
	}
	return ws, v, t, all, true
}

func (h *Handler) getWorkstreamTask(w http.ResponseWriter, r *http.Request) {
	if _, _, t, _, ok := h.oneTask(w, r); ok {
		h.write(w, http.StatusOK, t)
	}
}

func (h *Handler) workstreamTaskEvents(w http.ResponseWriter, r *http.Request) {
	ws, _, t, _, ok := h.oneTask(w, r)
	if !ok {
		return
	}
	events, err := h.config.Store.WorkstreamTaskEvents(ws.ID, t.ID)
	if h.failed(w, err) {
		return
	}
	h.write(w, http.StatusOK, api.WorkstreamTaskEvents{Events: events})
}

// ── writing ──

// taskRefused writes the refusal a task change ended with.
func (h *Handler) taskRefused(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, store.ErrTaskStale):
		h.fail(w, http.StatusPreconditionFailed, err.Error())
	case errors.Is(err, store.ErrTaskGone):
		h.fail(w, http.StatusNotFound, err.Error())
	case errors.Is(err, errTaskAnswered), errors.Is(err, factory.ErrTaskNoAsk), errors.Is(err, factory.ErrTaskOfferGone),
		errors.Is(err, factory.ErrTaskUndoOver), errors.Is(err, factory.ErrTaskFactoryState):
		h.fail(w, http.StatusConflict, err.Error())
	case errors.As(err, new(factory.TaskInvalid)):
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
	case errors.As(err, new(factory.TaskRefused)):
		h.fail(w, http.StatusForbidden, err.Error())
	default:
		return h.workstreamRefused(w, err)
	}
	return true
}

// keepTask keeps a changed task: a kept task under its rev, a job's task as
// its overlay (under the overlay's rev, 0 for its first).
func keepTask(tw store.TaskWrite, t factory.WorkstreamTask, readRev int) error {
	if _, job := factory.IsJobTask(t.ID); job {
		return tw.Put(factory.JobOverlay(t), readRev)
	}
	return tw.Put(t, readRev)
}

func (h *Handler) publishTasks(ws factory.OrgWorkstream, by string, tasks ...factory.WorkstreamTask) {
	for _, t := range tasks {
		h.publishWorkstreamPart(ws, api.PartTasks, t.ID, by, t.Rev, false)
	}
}

func (h *Handler) createWorkstreamTask(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.writablePart(w, r)
	if !ok {
		return
	}
	var body api.TaskCreate
	if !h.decode(w, r, &body, false) {
		return
	}
	if body.DoneBy.Kind == factory.TaskByFactory {
		h.fail(w, http.StatusUnprocessableEntity, "a Factory task is its job: add the job to the workstream instead")
		return
	}
	now := h.clock().UTC()
	var made factory.WorkstreamTask
	jobs, err := h.workstreamJobs(r, ws)
	if h.failed(w, err) {
		return
	}
	err = h.config.Store.EditWorkstreamTasks(func(tw store.TaskWrite) error {
		all, err := readTasksIn(tw, ws, jobs)
		if err != nil {
			return err
		}
		if store.CountWorkstreamTasks(all) >= factory.TasksMost {
			return factory.TaskInvalid(fmt.Sprintf("a workstream keeps at most %d tasks", factory.TasksMost))
		}
		made, err = factory.NewTask(ws.ID, h.newTaskID("tk_")(), body, factory.PersonRef{ID: v.Handle, Name: v.Handle}, taskDefaultOwner(ws), all, now)
		if err != nil {
			return err
		}
		return tw.Put(made, 0)
	})
	if h.taskRefused(w, err) {
		return
	}
	h.recordWorkstream(r, ws.ID, "added task", made.ID)
	h.publishTasks(ws, h.orgActor(r), made)
	h.write(w, http.StatusCreated, nameTask(h.personNames(), made))
}

// readTasksIn is readTasks with the kept tasks read inside a write. jobs are
// read BEFORE the write begins ([Handler.workstreamJobs]): the database has
// one connection, and a read of job membership inside the write's transaction
// would wait on that transaction for ever.
func readTasksIn(tw store.TaskWrite, ws factory.OrgWorkstream, jobs map[int]factory.Item) ([]factory.WorkstreamTask, error) {
	kept, err := tw.Tasks(ws.ID)
	if err != nil {
		return nil, err
	}
	out := []factory.WorkstreamTask{}
	overlays := map[int]*factory.WorkstreamTask{}
	for i := range kept {
		if n, ok := factory.IsJobTask(kept[i].ID); ok {
			overlays[n] = &kept[i]
			continue
		}
		out = append(out, kept[i])
	}
	for n, it := range jobs {
		out = append(out, factory.JobTask(it, ws.ID, overlays[n], taskDefaultOwner(ws)))
	}
	return out, nil
}

// decodeTaskPatch reads a PATCH body, telling a null (clear) from an absent
// field.
func (h *Handler) decodeTaskPatch(w http.ResponseWriter, r *http.Request) (factory.TaskPatch, bool) {
	var raw map[string]json.RawMessage
	if !h.decode(w, r, &raw, false) {
		return factory.TaskPatch{}, false
	}
	var p factory.TaskPatch
	data, _ := json.Marshal(raw)
	if err := json.Unmarshal(data, &p); err != nil {
		h.fail(w, http.StatusUnprocessableEntity, "this task change cannot be read: "+err.Error())
		return p, false
	}
	isNull := func(k string) bool { v, ok := raw[k]; return ok && string(v) == "null" }
	p.ClearGroup, p.ClearAsk = isNull("group"), isNull("ask")
	return p, true
}

func (h *Handler) patchWorkstreamTask(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.writablePart(w, r)
	if !ok {
		return
	}
	rev, ok := h.ifMatch(w, r)
	if !ok {
		return
	}
	p, ok := h.decodeTaskPatch(w, r)
	if !ok {
		return
	}
	var target factory.OrgWorkstream
	if p.MoveTo != "" && p.MoveTo != ws.ID {
		other, found, err := h.config.Store.Workstream(p.MoveTo)
		if h.failed(w, err) {
			return
		}
		if !found || !v.canSee(other) {
			h.fail(w, http.StatusNotFound, errWorkstreamGone.Error())
			return
		}
		if !v.canWrite(other) || other.ArchivedAt != nil {
			h.fail(w, http.StatusForbidden, "moving a task needs edit on the workstream it moves to")
			return
		}
		target = other
	}
	tid := r.PathValue("tid")
	if n, job := factory.IsJobTask(tid); job && target.ID != "" {
		// A Factory task moves with its job: the job joins the other
		// workstream by the one door that does that, and its overlay follows.
		if _, err := h.joinWorkstreamJob(r, target, v, n); h.partRefused(w, err) {
			return
		}
		_ = h.config.Store.EditWorkstreamTasks(func(tw store.TaskWrite) error { return tw.Move(ws.ID, target.ID, tid) })
		h.publishTasks(ws, h.orgActor(r), factory.WorkstreamTask{ID: tid})
		r2 := r.Clone(r.Context())
		r2.SetPathValue("id", target.ID)
		_, _, t, _, ok := h.oneTask(w, r2)
		if ok {
			h.write(w, http.StatusOK, t)
		}
		return
	}
	now := h.clock().UTC()
	actor, by := taskActor(v, ws), h.taskBy(r, v)
	var out factory.TaskChange
	jobs, err := h.workstreamJobs(r, ws)
	if h.failed(w, err) {
		return
	}
	err = h.config.Store.EditWorkstreamTasks(func(tw store.TaskWrite) error {
		all, err := readTasksIn(tw, ws, jobs)
		if err != nil {
			return err
		}
		t, found := findTask(all, tid)
		if !found {
			return store.ErrTaskGone
		}
		if t.Rev != rev {
			return store.ErrTaskStale
		}
		out, err = factory.ApplyPatch(t, p, actor, by, all, h.newTaskID("te_"), now)
		if err != nil {
			return err
		}
		if err := keepTask(tw, out.Task, rev); err != nil {
			return err
		}
		// A released task stays up next (factory.Released): its thread says it
		// can start, and whoever wakes its doer marks it running.
		for _, rel := range out.Released {
			out.Events = append(out.Events, factory.WorkstreamTaskEvent{
				ID: h.newTaskID("te_")(), Task: rel.ID, At: out.Task.UpdatedAt, By: by, Kind: factory.TaskEventState,
				Text: "Ready to start: " + out.Task.Title + " is done",
			})
		}
		if target.ID != "" {
			// A move takes the task and its thread; what waited on it here
			// stops waiting, and what it waited on stays behind.
			for _, other := range all {
				if other.ID == t.ID || !slices.Contains(other.WaitingOn, t.ID) {
					continue
				}
				other.WaitingOn = slices.DeleteFunc(slices.Clone(other.WaitingOn), func(id string) bool { return id == t.ID })
				prev := other.Rev
				other.Rev++
				if err := keepTask(tw, other, prev); err != nil {
					return err
				}
			}
			if err := tw.Move(ws.ID, target.ID, t.ID); err != nil {
				return err
			}
			moved := out.Task
			moved.WS, moved.WaitingOn = target.ID, []string{}
			if err := tw.Put(moved, 0); err != nil {
				return err
			}
			out.Task = moved
			out.Events = append(out.Events, factory.WorkstreamTaskEvent{
				ID: h.newTaskID("te_")(), Task: t.ID, At: moved.UpdatedAt, By: by, Kind: factory.TaskEventMoved, Text: "Moved from " + ws.Name,
			})
			return tw.Append(target.ID, out.Events...)
		}
		return tw.Append(ws.ID, out.Events...)
	})
	if h.taskRefused(w, err) {
		return
	}
	h.recordWorkstream(r, ws.ID, "changed task", tid)
	h.publishTasks(ws, h.orgActor(r), append([]factory.WorkstreamTask{out.Task}, out.Released...)...)
	if target.ID != "" {
		h.publishTasks(target, h.orgActor(r), out.Task)
	}
	h.write(w, http.StatusOK, nameTask(h.personNames(), out.Task))
}

func (h *Handler) answerWorkstreamTask(w http.ResponseWriter, r *http.Request) {
	ws, v, t, _, ok := h.oneTask(w, r)
	if !ok {
		return
	}
	var body api.TaskAnswer
	if !h.decode(w, r, &body, false) {
		return
	}
	now := h.clock().UTC()
	actor, by := taskActor(v, ws), h.taskBy(r, v)
	// The rules first, on the task as read: who answers, and what.
	change, err := factory.AnswerTask(t, body.Option, body.Text, actor, by, taskFallback(ws), h.newTaskID("te_"), now)
	if h.taskRefused(w, err) {
		return
	}
	if n, job := factory.IsJobTask(t.ID); job && !h.answerJob(w, r, n, t, body) {
		return
	}
	err = h.config.Store.EditWorkstreamTasks(func(tw store.TaskWrite) error {
		kept, err := tw.Tasks(ws.ID)
		if err != nil {
			return err
		}
		cur, found := findTask(kept, t.ID)
		if _, job := factory.IsJobTask(t.ID); job {
			// The job took the answer; the overlay records who gave it.
			o := factory.JobOverlay(t)
			o.AnsweredBy, o.UpdatedAt = change.Task.AnsweredBy, change.Task.UpdatedAt
			if found {
				o.Rev = cur.Rev + 1
				err = tw.Put(o, cur.Rev)
			} else {
				o.Rev = 1
				err = tw.Put(o, 0)
			}
			if err != nil {
				return err
			}
		} else {
			// THE FIRST ANSWER DECIDES: an Ask somebody answered meanwhile
			// is not answered twice.
			if !found || cur.Ask == nil || cur.Ask.ID != t.Ask.ID {
				return errTaskAnswered
			}
			if err := tw.Put(change.Task, cur.Rev); err != nil {
				return err
			}
		}
		return tw.Append(ws.ID, change.Events...)
	})
	if h.taskRefused(w, err) {
		return
	}
	h.recordWorkstream(r, ws.ID, "answered task", t.ID)
	_, _, fresh, _, ok := h.oneTask(httptest.NewRecorder(), r)
	if !ok {
		fresh = change.Task
	}
	h.publishTasks(ws, h.orgActor(r), fresh)
	h.write(w, http.StatusOK, fresh)
}

// answerJob answers job n through its own door (POST /v1/jobs/{n}/answer),
// under the job's own rules; a refusal there is written and ok is false.
func (h *Handler) answerJob(w http.ResponseWriter, r *http.Request, n int, t factory.WorkstreamTask, body api.TaskAnswer) bool {
	words := strings.TrimSpace(body.Text)
	yes := true
	switch body.Option {
	case "yes", "":
	case "no":
		yes = false
	default:
		words = strings.TrimSpace(strings.Join([]string{body.Option, words}, " "))
	}
	data, _ := json.Marshal(map[string]any{"yes": yes, "words": words})
	r2 := r.Clone(r.Context())
	r2.Method, r2.URL.Path, r2.RequestURI = http.MethodPost, fmt.Sprintf("/v1/jobs/%d/answer", n), ""
	r2.Body, r2.ContentLength = io.NopCloser(bytes.NewReader(data)), int64(len(data))
	r2.Header = r.Header.Clone()
	r2.Header.Del("If-Match")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r2)
	if rec.Code >= 300 {
		for k, vs := range rec.Header() {
			w.Header()[k] = vs
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
		return false
	}
	return true
}

func (h *Handler) commentWorkstreamTask(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.writablePart(w, r)
	if !ok {
		return
	}
	var body api.TaskComment
	if !h.decode(w, r, &body, false) {
		return
	}
	now := h.clock().UTC()
	actor, by := taskActor(v, ws), h.taskBy(r, v)
	if body.Kind == factory.TaskEventOffer || body.Kind == factory.TaskEventRequestNote {
		// An offer is Afi's, and a private request's note is made by the
		// private chat the person is in: the caller's own client writes both,
		// as Afi, for the person it works for.
		if body.Kind == factory.TaskEventOffer {
			by = factory.TaskEventBy{Kind: factory.TaskByAfi, Name: "Afi"}
		}
	}
	tid := r.PathValue("tid")
	var out factory.TaskChange
	var ev factory.WorkstreamTaskEvent
	jobs, err := h.workstreamJobs(r, ws)
	if h.failed(w, err) {
		return
	}
	err = h.config.Store.EditWorkstreamTasks(func(tw store.TaskWrite) error {
		all, err := readTasksIn(tw, ws, jobs)
		if err != nil {
			return err
		}
		t, found := findTask(all, tid)
		if !found {
			return store.ErrTaskGone
		}
		thread, err := tw.Events(ws.ID, tid)
		if err != nil {
			return err
		}
		if body.Offer != nil {
			i := slices.IndexFunc(thread, func(e factory.WorkstreamTaskEvent) bool { return e.ID == body.Offer.Event })
			if i < 0 {
				return store.ErrTaskGone
			}
			ev, err = factory.SetOffer(t, thread[i], body.Offer.State, actor, h.taskBy(r, v), now)
			if err != nil {
				return err
			}
			out.Task = t
			return tw.Replace(ws.ID, ev)
		}
		out, err = factory.Note(t, body.TaskNote, actor, by, thread, h.newTaskID("te_"), now)
		if err != nil {
			return err
		}
		ev = out.Events[0]
		if out.Task.Rev != t.Rev {
			if err := keepTask(tw, out.Task, t.Rev); err != nil {
				return err
			}
		}
		return tw.Append(ws.ID, out.Events...)
	})
	if h.taskRefused(w, err) {
		return
	}
	h.publishTasks(ws, h.orgActor(r), out.Task)
	status := http.StatusCreated
	if body.Offer != nil {
		status = http.StatusOK
	}
	h.write(w, status, ev)
}

func (h *Handler) startAllWorkstreamTasks(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := h.writablePart(w, r)
	if !ok {
		return
	}
	var body api.TasksStartAll
	if !h.decode(w, r, &body, false) {
		return
	}
	if strings.TrimSpace(body.Group) == "" {
		h.fail(w, http.StatusUnprocessableEntity, "start all names its group")
		return
	}
	// Start all names the group's ready Afi and chat tasks and starts none of
	// them: running means a doer is at work, and only whoever wakes the doer
	// can say so (factory.StartAll). It writes nothing and publishes nothing.
	all, err := h.config.Store.WorkstreamTasks(ws.ID)
	if h.taskRefused(w, err) {
		return
	}
	started := factory.StartAll(body.Group, all, h.clock().UTC())
	if started == nil {
		started = []factory.WorkstreamTask{}
	}
	nameTasks(h.personNames(), started)
	h.write(w, http.StatusOK, api.WorkstreamTasks{Tasks: started})
}
