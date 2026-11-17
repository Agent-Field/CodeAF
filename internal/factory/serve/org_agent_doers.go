package serve

import "github.com/Agent-Field/codeaf/internal/factory"

// THE ORGANIZATION'S AGENT LIST IS WHO A WORKSTREAM'S TASKS MAY BE DONE BY
// (org-space DESIGN §4.6). Lane L1 made [Config.WorkstreamAgents] the seam a
// task's named doer is checked against; lane L3 keeps the agents. When the
// door that builds this server leaves the seam empty and the server keeps a
// store, the seam reads that store: the agents a workstream has had shared
// into it, or added from the organization's list, and that still stand there.
// An agent nobody shared into the workstream is not one of its doers, and an
// agent its person unshared or deleted stops being one at once.

// sharedWorkstreamAgents is ws's agents, read from the organization's store,
// in the order they were added. Each is an agent (never a crew: the store
// keeps none), named as its person named it, with its instructions as the
// brief a task it does is started with.
func (h *Handler) sharedWorkstreamAgents(ws factory.OrgWorkstream) []WorkstreamAgent {
	if h.config.Store == nil || len(ws.Agents) == 0 {
		return nil
	}
	agents, err := h.config.Store.OrgAgents()
	if err != nil {
		return nil
	}
	var out []WorkstreamAgent
	for _, l := range ws.Agents {
		a, ok := agents[l.Agent]
		if !ok || !linkStands(l, a) {
			continue
		}
		out = append(out, WorkstreamAgent{ID: a.ID, Name: a.Name, Brief: a.Instructions})
	}
	return out
}
