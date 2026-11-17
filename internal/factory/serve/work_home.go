package serve

import (
	"net/http"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// inWorkstream excludes the operator's organization-wide read privilege from a person's Work feed.
func inWorkstream(v Viewer, ws factory.OrgWorkstream) bool {
	v.Operator = false
	return roleOf(v, workstreamItem(ws)) != ""
}

func (h *Handler) workHome(w http.ResponseWriter, r *http.Request) {
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
	out := api.WorkHome{Workstreams: []api.WorkstreamSummary{}, Tasks: []factory.WorkstreamTask{}, People: []api.WorkstreamMember{}}
	seen := map[string]bool{}
	for _, ws := range all {
		if ws.ArchivedAt != nil || !inWorkstream(v, ws) {
			continue
		}
		tasks, err := h.readTasks(r, ws)
		if h.failed(w, err) {
			return
		}
		out.Tasks = append(out.Tasks, tasks...)
		out.Workstreams = append(out.Workstreams, workstreamSummary(ws, workstreamCount{}, v))
		for _, p := range h.workstreamPeople(ws, h.personNames()) {
			key := p.Kind + ":" + p.ID
			if !seen[key] {
				seen[key] = true
				out.People = append(out.People, p)
			}
		}
	}
	slices.SortFunc(out.Workstreams, func(a, b api.WorkstreamSummary) int { return strings.Compare(a.Name, b.Name) })
	out.PersonStatus = h.workPeopleStatus(out.People)
	h.write(w, http.StatusOK, out)
}

func (h *Handler) workOverview(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.findWorkstream(w, r)
	if !ok {
		return
	}
	tasks, err := h.readTasks(r, ws)
	if h.failed(w, err) {
		return
	}
	out := api.WorkOverview{Tasks: tasks, Steps: map[string][]string{}, Previews: map[string]string{}, Children: []api.WorkstreamSummary{}, People: h.workstreamPeople(ws, h.personNames()), Decisions: nameDecisions(ws.Decisions, h.personNames()), Files: []factory.WorkstreamFile{}, AskedOf: []api.WorkRequest{}}
	files, err := h.config.Store.WorkstreamFiles(ws.ID)
	if h.failed(w, err) {
		return
	}
	for _, f := range files {
		if f.DeletedAt != nil {
			continue
		}
		out.Files = append(out.Files, f)
		if f.Kind == factory.FileDocument {
			for _, p := range ws.Pages {
				if p.ID == f.PageID {
					out.Previews[f.ID] = previewLines(p.BodyMD)
				}
			}
		}
	}
	for _, t := range tasks {
		if t.State == factory.TaskUpNext && t.Group != nil && t.Group.Kind == "batch" {
			out.PlanCount++
		}
		if t.State != factory.TaskRunning {
			continue
		}
		events, err := h.config.Store.WorkstreamTaskEvents(ws.ID, t.ID)
		if h.failed(w, err) {
			return
		}
		lines := []string{}
		for _, e := range events {
			if e.Kind == factory.TaskEventWorked {
				lines = append(lines, e.Text)
			}
		}
		out.Steps[t.ID] = lines[max(0, len(lines)-3):]
	}
	all, err := h.config.Store.Workstreams()
	if h.failed(w, err) {
		return
	}
	for _, other := range all {
		if other.ArchivedAt != nil {
			continue
		}
		if other.Parent == ws.ID && v.canSee(other) {
			out.Children = append(out.Children, workstreamSummary(other, workstreamCount{}, v))
		}
		// A request is shared with its source even when the destination is not. Only the request's public facts leave it.
		if other.ID == ws.ID || other.Private {
			continue
		}
		requested, err := h.config.Store.WorkstreamTasks(other.ID)
		if h.failed(w, err) {
			return
		}
		for _, t := range requested {
			if t.CameFrom.WS == ws.ID {
				out.AskedOf = append(out.AskedOf, api.WorkRequest{ID: t.ID, WS: other.ID, WSName: other.Name, Title: t.Title, Owner: t.Owner, State: t.State})
			}
		}
	}
	slices.SortFunc(out.Children, func(a, b api.WorkstreamSummary) int { return strings.Compare(a.Name, b.Name) })
	out.PersonStatus = h.workPeopleStatus(out.People)
	h.write(w, http.StatusOK, out)
}

// A preview is bounded text from the real result document, never from the task's request.
func previewLines(body string) string {
	lines := strings.Split(strings.TrimSpace(body), "\n")
	body = strings.Join(lines[:min(len(lines), 4)], "\n")
	runes := []rune(body)
	return string(runes[:min(len(runes), 600)])
}

// Only people already visible on the page gain a join state; this is not an organization directory.
func (h *Handler) workPeopleStatus(people []api.WorkstreamMember) map[string]string {
	visible := map[string]bool{}
	for _, p := range people {
		if p.Kind == api.MemberPerson {
			visible[p.ID] = true
		}
		for _, id := range p.Members {
			visible[id] = true
		}
	}
	out := map[string]string{}
	for _, m := range h.memberList() {
		if visible[m.Handle] && !m.LastSeen.IsZero() {
			out[m.Handle] = "joined"
		}
	}
	for _, tok := range h.tokenRows() {
		if tok.Revoked || tok.InvitedLogin == "" || tok.Profile != nil || (!tok.Expires.IsZero() && !h.now().Before(tok.Expires)) {
			continue
		}
		if visible[tok.InvitedLogin] && out[tok.InvitedLogin] == "" {
			out[tok.InvitedLogin] = "invited"
		}
	}
	return out
}
