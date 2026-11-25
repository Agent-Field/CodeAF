package serve

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// WHO MAY DO A WORKSTREAM'S TASK (org-space DESIGN §4). A task's doer is a
// person in the workstream, Afi, a chat's agent, a named agent or crew from
// the workstream's agent list, or a Factory job. This file checks a doer a
// person names (Add task, Change who does it) and words what an agent doer is
// asked when the server starts it.

// WorkstreamAgent is one named agent or crew a workstream may hand tasks to:
// the id a task's done_by names it by, the name people read, and the brief it
// works by (what it is for, in its maker's words).
type WorkstreamAgent struct {
	ID    string
	Name  string
	Crew  bool
	Brief string
}

// errAgentNotHere is the refusal for a doer the workstream's agent list does
// not name: an agent becomes a doer only once it is added to the workstream.
var errAgentNotHere = errors.New("that agent is not one of this workstream's agents: add it to the workstream first")

// workstreamAgents is ws's agent list: none until the organization's list
// (Config.WorkstreamAgents) says otherwise.
func (h *Handler) workstreamAgents(ws factory.OrgWorkstream) []WorkstreamAgent {
	if h.config.WorkstreamAgents == nil {
		return nil
	}
	return h.config.WorkstreamAgents(ws)
}

// workstreamAgent finds id in ws's agent list.
func (h *Handler) workstreamAgent(ws factory.OrgWorkstream, id string) (WorkstreamAgent, bool) {
	for _, a := range h.workstreamAgents(ws) {
		if a.ID == id {
			return a, true
		}
	}
	return WorkstreamAgent{}, false
}

// checkDoer refuses a doer ws cannot have: a person who is not in it, or an
// agent its list does not name. It answers the doer as kept: an agent's name
// and kind are the list's, never the caller's.
func (h *Handler) checkDoer(ws factory.OrgWorkstream, d factory.TaskDoneBy) (factory.TaskDoneBy, error) {
	switch d.Kind {
	case factory.TaskByPerson:
		handle := factory.NormalizeHandle(d.ID)
		v := h.viewerOf(handle)
		if handle == "" || !v.canWrite(ws) {
			return d, factory.TaskInvalid(fmt.Sprintf("%s is not someone in this workstream", firstNonEmpty(d.Name, d.ID)))
		}
	case factory.TaskByAgent:
		a, ok := h.workstreamAgent(ws, d.AgentID)
		if !ok {
			return d, factory.TaskInvalid(errAgentNotHere.Error())
		}
		d.Name, d.Crew = a.Name, a.Crew
	}
	return d, nil
}

// agentTaskWords is what a named agent or a chat's agent is asked when the
// server starts its task: who it is working as, its brief when it has one,
// then the task. IT BEGINS FROM THE TASK'S BRIEF, never a private chat
// (org-space DESIGN §3.1): a chat that planned the work stays private, and the
// task's own run is the task's.
func agentTaskWords(name, brief string, t factory.WorkstreamTask) string {
	lines := []string{fmt.Sprintf("You are doing this task as %s.", name)}
	if b := strings.TrimSpace(brief); b != "" {
		lines = append(lines, "What "+name+" is for: "+b)
	}
	lines = append(lines, "", taskWords(t))
	return strings.Join(lines, "\n")
}
