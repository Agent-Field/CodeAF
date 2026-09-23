package remote

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/Agent-Field/codeaf/internal/store"
)

// ── THE SKILLS A PERSON PUTS IN FRONT OF A HOSTED CONVERSATION ──────────────
//
// internal/session holds the attachment (its skillattach.go): the names a
// person chose with /skill, carried ahead of anything retrieval found on every
// message the conversation sends. The picker (internal/tui3's skillpick.go)
// asserts those four doors, and the shelf reading beside them, on the agent it
// holds — and until this file *remote.Agent had none of them, so on the
// ordinary launch, which attaches to this workspace's session host, /skill
// listed every skill a person had and answered every choice with "this
// conversation cannot carry attached skills".
//
// THE ATTACHMENT COMES DOWN UNASKED; EVERYTHING ELSE IS A CALL. The tray chip
// reads the attachment on every frame it draws, so it rides the facts
// photograph ([session.Facts.Skills]) and [Agent.AttachedSkills] is a read of
// the replica — the engine states the set again whenever a door moves it, so
// another window's change reaches this chip without being asked for. The
// shelf and the three doors that move the attachment are calls, asked off the
// surface's update loop (internal/tui3's offloop.go), and each one that moves
// the set writes the answer into the replica so the next frame draws it.
//
// AND THE CAPABILITY IS THE WELCOME'S TO ANSWER ([Welcome.Skills]): every
// connection has these methods, so the type assertion cannot tell a far engine
// with the doors from one without them. Against an engine that has none, each
// door answers exactly what a session with nothing attached answers, and the
// shelf answers an error, which is the picker's own word for "no shelf here".

// skillDoor is the slice of *session.Agent this file speaks to. It is asserted
// rather than required, on [effortDoor]'s terms.
type skillDoor interface {
	AttachSkills(names ...string) []string
	DetachSkill(name string) bool
	AttachedSkills() []string
	ClearAttachedSkills() int
	SkillFacts(status string, limit int) ([]store.Fact, error)
}

// skillsKnown is whether this engine's conversation has every skill door.
func skillsKnown(agent any) bool { _, ok := agent.(skillDoor); return ok }

// errNoFarSkills is what the shelf answers against an engine that has no
// skill doors at all.
var errNoFarSkills = errors.New("the engine this conversation is on cannot list skills; update it and reconnect")

// SkillsSupported answers for THE MACHINE AT THE OTHER END, off what it said
// at the door.
func (a *Agent) SkillsSupported() bool { return a.c.Welcome().Skills }

// AttachSkills puts names in front of the far conversation and answers the
// set as it now stands there.
func (a *Agent) AttachSkills(names ...string) []string {
	if !a.SkillsSupported() {
		return nil
	}
	payload, err := a.c.call(nil, MethodAttachSkills, names)
	if err != nil {
		return a.AttachedSkills()
	}
	var held []string
	_ = json.Unmarshal(payload, &held)
	a.c.facts.setSkills(held)
	return held
}

// DetachSkill takes one name back off and says whether it was there.
func (a *Agent) DetachSkill(name string) bool {
	if !a.SkillsSupported() {
		return false
	}
	payload, err := a.c.call(nil, MethodDetachSkill, name)
	if err != nil {
		return false
	}
	var was bool
	_ = json.Unmarshal(payload, &was)
	if was {
		kept := make([]string, 0, len(a.AttachedSkills()))
		for _, held := range a.AttachedSkills() {
			if !strings.EqualFold(held, name) {
				kept = append(kept, held)
			}
		}
		a.c.facts.setSkills(kept)
	}
	return was
}

// AttachedSkills is the set as the far conversation last stated it, in
// attachment order, read off the replica and never asked for.
func (a *Agent) AttachedSkills() []string {
	if !a.SkillsSupported() {
		return nil
	}
	return append([]string(nil), a.c.facts.read().Skills...)
}

// ClearAttachedSkills takes every name back off and says how many were on.
func (a *Agent) ClearAttachedSkills() int {
	if !a.SkillsSupported() {
		return 0
	}
	payload, err := a.c.call(nil, MethodClearSkills, nil)
	if err != nil {
		return 0
	}
	var count int
	_ = json.Unmarshal(payload, &count)
	a.c.facts.setSkills(nil)
	return count
}

// SkillFacts is the far conversation's skill shelf, as that session reads it.
func (a *Agent) SkillFacts(status string, limit int) ([]store.Fact, error) {
	if !a.SkillsSupported() {
		return nil, errNoFarSkills
	}
	payload, err := a.c.call(nil, MethodSkillShelf, SkillShelfArgs{Status: status, Limit: limit})
	if err != nil {
		return nil, err
	}
	var facts []store.Fact
	if err := json.Unmarshal(payload, &facts); err != nil {
		return nil, err
	}
	return facts, nil
}

// serveSkills answers the five skill doors against the agent this engine has
// open. The shelf crosses as the four fields a list draws — the kind, the
// status, the folder the name is read from and the one-line doc — because the
// rest of a fact is the store's bookkeeping and a picker has no use for it.
func serveSkills(agent any, call Frame) (json.RawMessage, error) {
	door, ok := agent.(skillDoor)
	if !ok {
		return nil, errNoFarSkills
	}
	switch call.Method {
	case MethodAttachSkills:
		names, err := arg[[]string](call)
		if err != nil {
			return nil, err
		}
		return json.Marshal(door.AttachSkills(names...))
	case MethodDetachSkill:
		name, err := arg[string](call)
		if err != nil {
			return nil, err
		}
		return json.Marshal(door.DetachSkill(name))
	case MethodAttachedSkills:
		return json.Marshal(door.AttachedSkills())
	case MethodClearSkills:
		return json.Marshal(door.ClearAttachedSkills())
	default:
		args, err := arg[SkillShelfArgs](call)
		if err != nil {
			return nil, err
		}
		facts, err := door.SkillFacts(args.Status, args.Limit)
		if err != nil {
			return nil, err
		}
		shelf := make([]store.Fact, 0, len(facts))
		for _, fact := range facts {
			shelf = append(shelf, store.Fact{Kind: fact.Kind, Status: fact.Status, Artifact: fact.Artifact, Body: fact.Body})
		}
		return json.Marshal(shelf)
	}
}
