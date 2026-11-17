package serve

import (
	"context"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// THE DOER WORD (org-space DESIGN §11.1, shared with a task's brief): an
// agent naming who does a task says it in ONE string, so the person a review is
// meant for is the person who gets it. Live use on 2026-11-17 made "Marco
// reviews the store text" with Priya as its doer, because the only person word
// was "person", which meant the owner.
//
//	afi            Afi (the server's agent); "this_chat" is its older spelling.
//	person:<id>    that person, who must be someone in the workstream.
//	agent:<id>     that agent or crew, which must be on the workstream's list.
//	factory        refused: a Factory task is a job started in the Factory.
//	person         the owner (the older spelling, kept for old prompts).
//
// L7's ws_task_create schema interpolates session.WorkstreamDoneBySchema and
// resolves its done_by through doerOfWord; neither restates the grammar.
type doerRef struct {
	Kind string // afi, person, owner, agent, factory
	ID   string
}

func parseDoerRef(word string) (doerRef, error) {
	w := strings.TrimSpace(word)
	kind, id, named := strings.Cut(w, ":")
	kind = strings.ToLower(strings.TrimSpace(kind))
	id = strings.TrimPrefix(strings.TrimSpace(id), "@")
	if named {
		if id == "" || (kind != "person" && kind != "agent") {
			return doerRef{}, fmt.Errorf("invalid arguments: done_by is afi, person:<id>, agent:<id> or factory, not %q", word)
		}
		return doerRef{Kind: kind, ID: id}, nil
	}
	switch kind {
	case "", "afi", "this_chat":
		return doerRef{Kind: "afi"}, nil
	case "person":
		return doerRef{Kind: "owner"}, nil
	case "factory":
		return doerRef{Kind: "factory"}, nil
	}
	return doerRef{}, fmt.Errorf("invalid arguments: done_by is afi, person:<id>, agent:<id> or factory, not %q", word)
}

// doerOfWord resolves the model's done_by word in this door's workstream. owner
// is the task's owner as already resolved (nil: the person the agent answers).
func (d *agentDoor) doerOfWord(ctx context.Context, word string, owner *factory.TaskOwner) (factory.TaskDoneBy, error) {
	ref, err := parseDoerRef(word)
	if err != nil {
		return factory.TaskDoneBy{}, err
	}
	switch ref.Kind {
	case "afi":
		return factory.TaskDoneBy{Kind: factory.TaskByAfi, Name: "Afi"}, nil
	case "owner":
		// The owner ticks a person task, so the owner does it.
		who := factory.NormalizeHandle(d.asker)
		if owner != nil && owner.Kind == factory.TaskOwnerPerson {
			who = owner.ID
		}
		return factory.TaskDoneBy{Kind: factory.TaskByPerson, ID: who, Name: d.h.personNames()(who)}, nil
	case "factory":
		return factory.TaskDoneBy{}, fmt.Errorf("a Factory task is a Factory job in this workstream: ask the person to start the job in the Factory, and it appears in Tasks by itself")
	}
	ws, err := d.Workstream(ctx)
	if err != nil {
		return factory.TaskDoneBy{}, err
	}
	if ref.Kind == "agent" {
		a, ok := d.h.workstreamAgent(ws, ref.ID)
		if !ok {
			return factory.TaskDoneBy{}, fmt.Errorf("%q is not one of this workstream's agents: done_by agent:<id> names an agent its people added to it; otherwise use afi or person:<id>", ref.ID)
		}
		return factory.TaskDoneBy{Kind: factory.TaskByAgent, AgentID: a.ID, Name: a.Name, Crew: a.Crew}, nil
	}
	handle := factory.NormalizeHandle(ref.ID)
	v := Viewer{Handle: handle, Teams: map[string]bool{}}
	for _, team := range d.h.Membership().TeamsOf(handle) {
		v.Teams[team] = true
	}
	if handle == "" || !v.canWrite(ws) {
		return factory.TaskDoneBy{}, fmt.Errorf("%q is not someone in this workstream: done_by person:<id> names a person in it (an owner.id from ws_tasks)", ref.ID)
	}
	return factory.TaskDoneBy{Kind: factory.TaskByPerson, ID: handle, Name: d.h.personNames()(handle)}, nil
}
