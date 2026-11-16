package serve

import (
	"log"
	"net/http"
	"slices"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// ── A WORKSTREAM'S DECISIONS, AND THE VIEWS ON ITS ASKS ────────────────────
//
// The rules are internal/factory's workstream_decisions.go. This file keeps
// what they settle:
//
//   - THE LIST RECORDS ITSELF. Answering an Ask, a request note that names
//     what it settles, and a change of rules each add a decision here, made by
//     the person who did it; no client writes one by hand.
//   - ONLY ITS MAKER withdraws or replaces a decision; the old one stays,
//     greyed.
//   - A VIEW is one line per person on someone else's Ask; it changes nothing
//     but the Ask, and reaches the decider.

func (h *Handler) workstreamDecisionRoutes() {
	h.route("/v1/workstreams/{id}/decisions/{did}/withdraw", map[string]http.HandlerFunc{http.MethodPost: h.withdrawWorkstreamDecision})
	h.route("/v1/workstreams/{id}/decisions/{did}/replace", map[string]http.HandlerFunc{http.MethodPost: h.replaceWorkstreamDecision})
	h.route("/v1/workstreams/{id}/tasks/{tid}/view", map[string]http.HandlerFunc{http.MethodPost: h.viewWorkstreamTask})
}

// keepDecisions adds ds to workstream id's Decisions in one save, and says
// so. A save that fails leaves the task change made: the work is the record
// people act on, and a decision lost is told in the server's log, never by
// undoing what the person did.
func (h *Handler) keepDecisions(r *http.Request, id string, v Viewer, ds []factory.WorkstreamDecision) {
	if len(ds) == 0 {
		return
	}
	now := h.clock().UTC()
	_, saved, err := h.editPart(id, v, func(cur *factory.OrgWorkstream, _ factory.OrgWorkstreams) (bool, error) {
		cur.Decisions = append(slices.Clone(cur.Decisions), ds...)
		cur.UpdatedAt = now
		return true, nil
	})
	if err != nil {
		log.Printf("factory: workstream %s: a decision was not kept: %v", id, err)
		return
	}
	for _, d := range ds {
		h.recordWorkstream(r, id, "kept decision", d.ID)
	}
	h.publishWorkstreamPart(saved, api.PartWorkstream, "", h.orgActor(r), saved.Rev, false)
}

// nameDecisions gives every decision its maker's name, and the name of whoever
// withdrew or replaced it, for the list.
func nameDecisions(ds []factory.WorkstreamDecision, nameOf func(string) string) []factory.WorkstreamDecision {
	out := slices.Clone(ds)
	for i := range out {
		if out[i].By != "" && (out[i].ByName == "" || out[i].ByName == out[i].By) {
			out[i].ByName = nameOf(out[i].By)
		}
		if c := out[i].ChangedBy; c != nil && (c.Name == "" || c.Name == c.ID) {
			out[i].ChangedBy = &factory.PersonRef{ID: c.ID, Name: nameOf(c.ID)}
		}
	}
	return out
}

func (h *Handler) changeDecision(w http.ResponseWriter, r *http.Request, change func(list []factory.WorkstreamDecision, who factory.PersonRef) ([]factory.WorkstreamDecision, error)) {
	ws, v, ok := h.writablePart(w, r)
	if !ok {
		return
	}
	who := factory.PersonRef{ID: v.Handle, Name: h.personNames()(v.Handle)}
	_, saved, err := h.editPart(ws.ID, v, func(cur *factory.OrgWorkstream, _ factory.OrgWorkstreams) (bool, error) {
		next, err := change(cur.Decisions, who)
		if err != nil {
			return false, err
		}
		cur.Decisions, cur.UpdatedAt = next, h.clock().UTC()
		return true, nil
	})
	if h.taskRefused(w, err) {
		return
	}
	h.recordWorkstream(r, ws.ID, "changed decision", r.PathValue("did"))
	h.publishWorkstreamPart(saved, api.PartWorkstream, "", h.orgActor(r), saved.Rev, false)
	h.writeWorkstream(w, http.StatusOK, saved, v)
}

func (h *Handler) withdrawWorkstreamDecision(w http.ResponseWriter, r *http.Request) {
	h.changeDecision(w, r, func(list []factory.WorkstreamDecision, who factory.PersonRef) ([]factory.WorkstreamDecision, error) {
		return factory.WithdrawDecision(list, r.PathValue("did"), who, h.clock().UTC())
	})
}

func (h *Handler) replaceWorkstreamDecision(w http.ResponseWriter, r *http.Request) {
	var body api.DecisionReplace
	if !h.decode(w, r, &body, false) {
		return
	}
	h.changeDecision(w, r, func(list []factory.WorkstreamDecision, who factory.PersonRef) ([]factory.WorkstreamDecision, error) {
		next, _, err := factory.ReplaceDecision(list, r.PathValue("did"), body.Text, "dc_"+h.newTaskID("")(), who, h.clock().UTC())
		return next, err
	})
}

func (h *Handler) viewWorkstreamTask(w http.ResponseWriter, r *http.Request) {
	ws, v, t, _, ok := h.oneTask(w, r)
	if !ok {
		return
	}
	var body api.TaskViewSet
	if !h.decode(w, r, &body, false) {
		return
	}
	now := h.clock().UTC()
	who := factory.PersonRef{ID: v.Handle, Name: h.personNames()(v.Handle)}
	changed, err := factory.AddView(t, body.Ask, body.Text, taskActor(v, ws), who, now)
	if h.taskRefused(w, err) {
		return
	}
	err = h.config.Store.EditWorkstreamTasks(func(tw store.TaskWrite) error {
		kept, err := tw.Tasks(ws.ID)
		if err != nil {
			return err
		}
		cur, found := findTask(kept, t.ID)
		if _, job := factory.IsJobTask(t.ID); job {
			o := factory.JobOverlay(changed)
			if found {
				o.Rev = cur.Rev + 1
				return tw.Put(o, cur.Rev)
			}
			o.Rev = 1
			return tw.Put(o, 0)
		}
		if !found || cur.Rev != t.Rev {
			return store.ErrTaskStale
		}
		return tw.Put(changed, cur.Rev)
	})
	if h.taskRefused(w, err) {
		return
	}
	h.recordWorkstream(r, ws.ID, "added a view", t.ID)
	h.publishTasks(ws, h.orgActor(r), changed)
	h.write(w, http.StatusOK, nameTask(h.personNames(), changed))
}

// ruleDecisions are the decisions a change of ws's rules records, made by v.
func (h *Handler) ruleDecisions(before, after factory.WorkstreamRules, v Viewer) []factory.WorkstreamDecision {
	// A standing order lives on its holder's computer, so the server does not
	// know its name; switching one is recorded by the desk that holds it.
	names := map[string]string{}
	who := factory.PersonRef{ID: v.Handle, Name: h.personNames()(v.Handle)}
	return factory.RuleDecisions(before, after, who, func(id string) string { return names[id] }, func() string { return "dc_" + h.newTaskID("")() }, h.clock().UTC())
}

// foreignDecisionChange refuses a whole-workstream save that withdraws,
// replaces or rewords a decision someone else made: ONLY ITS MAKER changes
// one. A decision left out is kept as superseded by supersedeDropped, so
// leaving one out counts as changing it.
func foreignDecisionChange(next, before []factory.WorkstreamDecision, who string) error {
	for _, was := range before {
		if was.By == "" || factory.NormalizeHandle(was.By) == factory.NormalizeHandle(who) || was.State != factory.DecisionKept {
			continue
		}
		i := slices.IndexFunc(next, func(d factory.WorkstreamDecision) bool { return d.ID == was.ID })
		if i < 0 || next[i].Text != was.Text || next[i].State != was.State {
			return factory.ErrDecisionNotMaker
		}
	}
	return nil
}
