package serve

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// ── AN ORGANIZATION'S AGENTS (factory/org_agent.go, org-space DESIGN §4.6) ──
//
//	GET    /v1/agents                         mine, the organization's list, and for the operator what waits on them
//	POST   /v1/agents                         a new agent in my space
//	PATCH  /v1/agents/{id}                    its person changes it
//	DELETE /v1/agents/{id}                    its person deletes it; every workstream drops its link
//	PUT    /v1/agents/{id}/roster {on}        its person asks for (or leaves) the organization's list; the operator approves or declines
//	GET    /v1/workstreams/{id}/agents        People and rules › Agents: the agents this workstream may give a task to
//	PUT    /v1/workstreams/{id}/agents/{a}    add one: my own (Share) or one on the organization's list
//	DELETE /v1/workstreams/{id}/agents/{a}    take one off: its person, or whoever edits the workstream
//
// THE OPERATOR IS NEVER TOLD OF AN AGENT ITS PERSON KEEPS TO THEMSELVES
// (OWNER-DECISIONS §8): not in this list, not in the record, which every member
// reads. They count among the space's things in the totals and nothing more.
// The record carries what happened, never a name.

var (
	errAgentGone     = errors.New("that agent is gone, or not one you can see")
	errAgentNotYours = errors.New("only the person who made this agent can change it")
)

func (h *Handler) orgAgentRoutes() {
	h.route("/v1/agents", map[string]http.HandlerFunc{http.MethodGet: h.listOrgAgents, http.MethodPost: h.createOrgAgent})
	h.route("/v1/agents/{agent}", map[string]http.HandlerFunc{http.MethodPatch: h.patchOrgAgent, http.MethodDelete: h.deleteOrgAgent})
	h.route("/v1/agents/{agent}/roster", map[string]http.HandlerFunc{http.MethodPut: h.putOrgAgentRoster})
	h.route("/v1/workstreams/{id}/agents", map[string]http.HandlerFunc{http.MethodGet: h.listWorkstreamAgents})
	h.route("/v1/workstreams/{id}/agents/{agent}", map[string]http.HandlerFunc{http.MethodPut: h.addWorkstreamAgent, http.MethodDelete: h.removeWorkstreamAgent})
}

// agentSeen is whether v may know agent a exists at all, and read its
// instructions: its person, everyone once it is on the organization's list,
// and the operator while it waits on them.
func agentSeen(v Viewer, a factory.OrgAgent) bool {
	return factory.NormalizeHandle(a.Owner) == v.Handle || a.Roster == factory.AgentRosterOrg || (v.Operator && a.Roster == factory.AgentRosterAsked)
}

// agentUsableBy is whether person may put agent a into a workstream: their
// own, or one on the organization's list.
func agentUsableBy(person string, a factory.OrgAgent) bool {
	return factory.NormalizeHandle(a.Owner) == person || a.Roster == factory.AgentRosterOrg
}

// linkStands is whether a workstream's link to a still means it: the agent is
// on the organization's list, or the person who shared it is its person. An
// agent its person took off the list stays in the workstreams they shared it
// into themselves, and leaves the ones others added it to.
func linkStands(l factory.WorkstreamAgentLink, a factory.OrgAgent) bool {
	return a.Roster == factory.AgentRosterOrg || factory.NormalizeHandle(l.By) == factory.NormalizeHandle(a.Owner)
}

func (h *Handler) orgAgentWire(v Viewer, a factory.OrgAgent, all factory.OrgWorkstreams, nameOf func(string) string) api.OrgAgent {
	owner := factory.NormalizeHandle(a.Owner)
	out := api.OrgAgent{ID: a.ID, Name: a.Name, Instructions: a.Instructions, Owner: owner, Roster: a.Roster, Team: a.Team,
		Mine: owner == v.Handle, CreatedAt: a.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: a.UpdatedAt.UTC().Format(time.RFC3339Nano)}
	if name := nameOf(owner); name != owner {
		out.OwnerName = name
	}
	if out.Mine {
		for _, ws := range all {
			if ws.ArchivedAt != nil {
				continue
			}
			for _, l := range ws.Agents {
				if l.Agent == a.ID && linkStands(l, a) && v.canSee(ws) {
					out.Workstreams = append(out.Workstreams, ws.ID)
				}
			}
		}
		slices.Sort(out.Workstreams)
	}
	return out
}

// agentCaller is the signed-in member asking, or a refusal written.
func (h *Handler) agentCaller(w http.ResponseWriter, r *http.Request) (Viewer, bool) {
	if !h.requireVerb(w, h.spacesServed()) {
		return Viewer{}, false
	}
	v, err := h.wsViewer(r)
	if h.failed(w, err) {
		return v, false
	}
	if v.Handle == "" {
		h.fail(w, http.StatusForbidden, "an agent is someone's; sign in first")
		return v, false
	}
	return v, true
}

func (h *Handler) listOrgAgents(w http.ResponseWriter, r *http.Request) {
	v, ok := h.agentCaller(w, r)
	if !ok {
		return
	}
	agents, err := h.config.Store.OrgAgents()
	if h.failed(w, err) {
		return
	}
	all, err := h.config.Store.Workstreams()
	if h.failed(w, err) {
		return
	}
	nameOf := h.personNames()
	out := api.OrgAgentList{Agents: []api.OrgAgent{}}
	for _, a := range agents {
		if agentSeen(v, a) {
			out.Agents = append(out.Agents, h.orgAgentWire(v, a, all, nameOf))
		}
	}
	slices.SortFunc(out.Agents, func(a, b api.OrgAgent) int {
		if c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	h.write(w, http.StatusOK, out)
}

// agentFields checks a name, instructions and team as a person typed them.
func (h *Handler) agentFields(name, instructions, team string) (string, string, string, error) {
	name = strings.Join(strings.Fields(name), " ")
	instructions = strings.TrimSpace(instructions)
	team = strings.TrimSpace(team)
	switch {
	case name == "":
		return "", "", "", errors.New("an agent needs a name")
	case utf8.RuneCountInString(name) > api.OrgAgentNameMost:
		return "", "", "", errors.New("an agent's name is at most 60 characters")
	case strings.EqualFold(name, "Afi"):
		return "", "", "", errors.New("Afi is already here in every workstream; give this agent a name of its own")
	case instructions == "":
		return "", "", "", errors.New("an agent needs instructions: what it does and how")
	case len(instructions) > api.OrgAgentInstructionsMost:
		return "", "", "", errors.New("an agent's instructions are at most 8000 bytes")
	}
	if team != "" {
		teams, err := h.config.Store.Teams()
		if err != nil {
			return "", "", "", err
		}
		if !slices.ContainsFunc(teams, func(t store.Team) bool { return t.ID == team }) {
			return "", "", "", errors.New("that team is not one of this organization's")
		}
	}
	return name, instructions, team, nil
}

func (h *Handler) createOrgAgent(w http.ResponseWriter, r *http.Request) {
	v, ok := h.agentCaller(w, r)
	if !ok {
		return
	}
	var body api.OrgAgentNew
	if !h.decode(w, r, &body, false) {
		return
	}
	name, instructions, team, err := h.agentFields(body.Name, body.Instructions, body.Team)
	if err != nil {
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	now := h.clock().UTC()
	id, err := cell.NewID(now)
	if h.failed(w, err) {
		return
	}
	a := factory.OrgAgent{ID: "ag_" + id, Name: name, Instructions: instructions, Owner: v.Handle, Team: team, CreatedAt: now, UpdatedAt: now}
	if _, err := h.config.Store.EditOrgAgents(func(all factory.OrgAgents) error {
		n := 0
		for _, x := range all {
			if factory.NormalizeHandle(x.Owner) == v.Handle {
				n++
			}
		}
		if n >= api.OrgAgentsPerPersonMost {
			return errTooManyAgents
		}
		all[a.ID] = a
		return nil
	}); err != nil {
		h.agentFailed(w, err)
		return
	}
	h.write(w, http.StatusCreated, h.orgAgentWire(v, a, nil, h.personNames()))
}

var errTooManyAgents = errors.New("you keep 100 agents already; delete one first")

// agentFailed writes a store refusal in the person's words.
func (h *Handler) agentFailed(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errTooManyAgents):
		h.fail(w, http.StatusConflict, err.Error())
	case errors.Is(err, errAgentGone):
		h.fail(w, http.StatusNotFound, err.Error())
	case errors.Is(err, errAgentNotYours):
		h.fail(w, http.StatusForbidden, err.Error())
	default:
		h.failed(w, err)
	}
}

func (h *Handler) patchOrgAgent(w http.ResponseWriter, r *http.Request) {
	v, ok := h.agentCaller(w, r)
	if !ok {
		return
	}
	var body api.OrgAgentPatch
	if !h.decode(w, r, &body, false) {
		return
	}
	id := r.PathValue("agent")
	var out factory.OrgAgent
	_, err := h.config.Store.EditOrgAgents(func(all factory.OrgAgents) error {
		a, ok := all[id]
		if !ok || !agentSeen(v, a) {
			return errAgentGone
		}
		if factory.NormalizeHandle(a.Owner) != v.Handle {
			return errAgentNotYours
		}
		name, instructions, team := a.Name, a.Instructions, a.Team
		if body.Name != nil {
			name = *body.Name
		}
		if body.Instructions != nil {
			instructions = *body.Instructions
		}
		if body.Team != nil {
			team = *body.Team
		}
		var err error
		if a.Name, a.Instructions, a.Team, err = h.agentFields(name, instructions, team); err != nil {
			return agentInvalid{err}
		}
		a.UpdatedAt = h.clock().UTC()
		all[id] = a
		out = a
		return nil
	})
	var bad agentInvalid
	if errors.As(err, &bad) {
		h.fail(w, http.StatusUnprocessableEntity, bad.Error())
		return
	}
	if err != nil {
		h.agentFailed(w, err)
		return
	}
	all, _ := h.config.Store.Workstreams()
	h.write(w, http.StatusOK, h.orgAgentWire(v, out, all, h.personNames()))
}

// agentInvalid is a field refusal raised inside a store edit.
type agentInvalid struct{ error }

func (h *Handler) deleteOrgAgent(w http.ResponseWriter, r *http.Request) {
	v, ok := h.agentCaller(w, r)
	if !ok {
		return
	}
	id := r.PathValue("agent")
	if _, err := h.config.Store.EditOrgAgents(func(all factory.OrgAgents) error {
		a, ok := all[id]
		if !ok || !agentSeen(v, a) {
			return errAgentGone
		}
		if factory.NormalizeHandle(a.Owner) != v.Handle {
			return errAgentNotYours
		}
		delete(all, id)
		return nil
	}); err != nil {
		h.agentFailed(w, err)
		return
	}
	h.dropAgentLinks(r, id, func(factory.WorkstreamAgentLink) bool { return true })
	w.WriteHeader(http.StatusNoContent)
}

// dropAgentLinks takes agent off every workstream whose link drop says goes.
func (h *Handler) dropAgentLinks(r *http.Request, agent string, drop func(factory.WorkstreamAgentLink) bool) {
	var changed []string
	befores := map[string]factory.OrgWorkstream{}
	saved, err := h.config.Store.EditWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
		for id, ws := range all {
			kept := slices.DeleteFunc(slices.Clone(ws.Agents), func(l factory.WorkstreamAgentLink) bool { return l.Agent == agent && drop(l) })
			if len(kept) != len(ws.Agents) {
				befores[id] = ws
				ws.Agents = kept
				all[id] = ws
				changed = append(changed, id)
			}
		}
		return changed, nil
	})
	if err != nil {
		return
	}
	for _, id := range changed {
		before := befores[id]
		h.publishWorkstreams(api.EventWorkstreamChanged, h.orgActor(r), &before, saved[id])
	}
}

func (h *Handler) putOrgAgentRoster(w http.ResponseWriter, r *http.Request) {
	v, ok := h.agentCaller(w, r)
	if !ok {
		return
	}
	var body api.OrgAgentRosterPut
	if !h.decode(w, r, &body, false) {
		return
	}
	id := r.PathValue("agent")
	var out factory.OrgAgent
	var verb string
	_, err := h.config.Store.EditOrgAgents(func(all factory.OrgAgents) error {
		a, ok := all[id]
		if !ok || !agentSeen(v, a) {
			return errAgentGone
		}
		mine := factory.NormalizeHandle(a.Owner) == v.Handle
		switch {
		case mine && body.On && a.Roster == factory.AgentRosterMine:
			// The operator's own agent needs nobody's approval.
			if v.Operator {
				a.Roster, verb = factory.AgentRosterOrg, "added org agent"
			} else {
				a.Roster, verb = factory.AgentRosterAsked, "asked for org agent"
			}
		case v.Operator && a.Roster == factory.AgentRosterAsked:
			if body.On {
				a.Roster, verb = factory.AgentRosterOrg, "approved org agent"
			} else {
				a.Roster, verb = factory.AgentRosterMine, "declined org agent"
			}
		case mine && !body.On:
			a.Roster, verb = factory.AgentRosterMine, "removed org agent"
		case v.Operator && !body.On && a.Roster == factory.AgentRosterOrg:
			a.Roster, verb = factory.AgentRosterMine, "removed org agent"
		case mine:
			// Asking again for what is already asked or approved changes nothing.
		default:
			return errAgentNotYours
		}
		a.UpdatedAt = h.clock().UTC()
		all[id] = a
		out = a
		return nil
	})
	if err != nil {
		if errors.Is(err, errAgentNotYours) {
			h.fail(w, http.StatusForbidden, "only the agent's person asks for the organization's list, and only the operator approves it")
			return
		}
		h.agentFailed(w, err)
		return
	}
	if verb != "" {
		h.recordOrg(r, record.AgentChanged, verb, "agent:"+out.ID, "")
	}
	if out.Roster != factory.AgentRosterOrg {
		// Off the list: the workstreams others added it to lose it; the ones
		// its person shared it into keep it.
		h.dropAgentLinks(r, out.ID, func(l factory.WorkstreamAgentLink) bool { return !linkStands(l, out) })
	}
	all, _ := h.config.Store.Workstreams()
	h.write(w, http.StatusOK, h.orgAgentWire(v, out, all, h.personNames()))
}

// workstreamAgentRows is the agents ws lists that still stand, in the order
// they were added.
func (h *Handler) workstreamAgentRows(ws factory.OrgWorkstream, agents factory.OrgAgents) []api.WorkstreamAgentRow {
	nameOf := h.personNames()
	out := []api.WorkstreamAgentRow{}
	for _, l := range ws.Agents {
		a, ok := agents[l.Agent]
		if !ok || !linkStands(l, a) {
			continue
		}
		owner := factory.NormalizeHandle(a.Owner)
		row := api.WorkstreamAgentRow{ID: a.ID, Name: a.Name, Kind: "agent", From: "space", Owner: owner, Team: a.Team,
			By: factory.NormalizeHandle(l.By), At: l.At.UTC().Format(time.RFC3339Nano)}
		if factory.NormalizeHandle(l.By) != owner {
			row.From = "org"
		}
		if name := nameOf(owner); name != owner {
			row.OwnerName = name
		}
		out = append(out, row)
	}
	return out
}

func (h *Handler) listWorkstreamAgents(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.spacesServed()) {
		return
	}
	ws, _, ok := h.findWorkstream(w, r)
	if !ok {
		return
	}
	agents, err := h.config.Store.OrgAgents()
	if h.failed(w, err) {
		return
	}
	h.write(w, http.StatusOK, api.WorkstreamAgentList{Agents: h.workstreamAgentRows(ws, agents)})
}

func (h *Handler) addWorkstreamAgent(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.spacesServed()) {
		return
	}
	ws, v, ok := h.findWorkstream(w, r)
	if !ok {
		return
	}
	if ws.Space {
		h.fail(w, http.StatusUnprocessableEntity, "your own agents are already in My space; share one into a workstream instead")
		return
	}
	if !v.canWrite(ws) {
		h.fail(w, http.StatusForbidden, "only its owner and those it is shared with for edit add agents to this workstream")
		return
	}
	if ws.ArchivedAt != nil {
		h.fail(w, http.StatusConflict, "This workstream is archived.")
		return
	}
	agents, err := h.config.Store.OrgAgents()
	if h.failed(w, err) {
		return
	}
	a, found := agents[r.PathValue("agent")]
	if !found || !agentSeen(v, a) {
		h.fail(w, http.StatusNotFound, errAgentGone.Error())
		return
	}
	if !agentUsableBy(v.Handle, a) {
		h.fail(w, http.StatusForbidden, "only its person shares this agent; ask them, or ask the operator to put it on the organization's list")
		return
	}
	link := factory.WorkstreamAgentLink{Agent: a.ID, By: v.Handle, At: h.clock().UTC()}
	before := ws
	saved, err := h.config.Store.EditWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
		cur, ok := all[ws.ID]
		if !ok {
			return nil, errWorkstreamGone
		}
		before = cur
		for i, l := range cur.Agents {
			if l.Agent == a.ID {
				if linkStands(l, a) {
					return nil, nil
				}
				// A link that no longer stood is made again by whoever adds it now.
				cur.Agents[i] = link
				all[ws.ID] = cur
				return []string{ws.ID}, nil
			}
		}
		if len(cur.Agents) >= api.WorkstreamAgentsMost {
			return nil, errWorkstreamAgentsFull
		}
		cur.Agents = append(cur.Agents, link)
		all[ws.ID] = cur
		return []string{ws.ID}, nil
	})
	if errors.Is(err, errWorkstreamAgentsFull) {
		h.fail(w, http.StatusConflict, err.Error())
		return
	}
	if h.failed(w, err) {
		return
	}
	if saved[ws.ID].Rev != before.Rev {
		h.recordWorkstream(r, ws.ID, "added agent", "")
		h.publishWorkstreams(api.EventWorkstreamChanged, v.Handle, &before, saved[ws.ID])
	}
	h.write(w, http.StatusOK, api.WorkstreamAgentList{Agents: h.workstreamAgentRows(saved[ws.ID], agents)})
}

var errWorkstreamAgentsFull = errors.New("a workstream lists 50 agents at most; take one off first")

func (h *Handler) removeWorkstreamAgent(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.spacesServed()) {
		return
	}
	ws, v, ok := h.findWorkstream(w, r)
	if !ok {
		return
	}
	agents, err := h.config.Store.OrgAgents()
	if h.failed(w, err) {
		return
	}
	id := r.PathValue("agent")
	a, known := agents[id]
	theirs := known && factory.NormalizeHandle(a.Owner) == v.Handle
	if !v.canWrite(ws) && !theirs {
		h.fail(w, http.StatusForbidden, "only its person, or whoever edits this workstream, takes an agent off it")
		return
	}
	before := ws
	saved, err := h.config.Store.EditWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
		cur, ok := all[ws.ID]
		if !ok {
			return nil, errWorkstreamGone
		}
		before = cur
		kept := slices.DeleteFunc(slices.Clone(cur.Agents), func(l factory.WorkstreamAgentLink) bool { return l.Agent == id })
		if len(kept) == len(cur.Agents) {
			return nil, nil
		}
		cur.Agents = kept
		all[ws.ID] = cur
		return []string{ws.ID}, nil
	})
	if h.failed(w, err) {
		return
	}
	if saved[ws.ID].Rev != before.Rev {
		h.recordWorkstream(r, ws.ID, "removed agent", "")
		h.publishWorkstreams(api.EventWorkstreamChanged, v.Handle, &before, saved[ws.ID])
	}
	h.write(w, http.StatusOK, api.WorkstreamAgentList{Agents: h.workstreamAgentRows(saved[ws.ID], agents)})
}

// chatAgent is the agent a new chat in ws may talk to: the caller's own, or
// one ws lists; in a person's space, their own or one on the organization's
// list.
func (h *Handler) chatAgent(ws factory.OrgWorkstream, person, id string) (factory.OrgAgent, bool) {
	agents, err := h.config.Store.OrgAgents()
	if err != nil {
		return factory.OrgAgent{}, false
	}
	a, ok := agents[id]
	if !ok {
		return factory.OrgAgent{}, false
	}
	if ws.Space {
		return a, agentUsableBy(person, a)
	}
	for _, l := range ws.Agents {
		if l.Agent == id && linkStands(l, a) {
			return a, true
		}
	}
	return a, false
}
