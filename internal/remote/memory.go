package remote

import (
	"encoding/json"
	"errors"

	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE THREE MEMORY COMMANDS, ACROSS THE HOST CONNECTION ───────────────────
//
// /remember, /forget and /memories (internal/tui3's memory.go) assert four
// doors on the agent under the surface. The ordinary launch attaches to this
// workspace's session host, so that agent is a *remote.Agent — and until this
// file it had none of the four, which made the assertion fail on every launch
// and every command answer "memory is off for this session" while the very
// session behind the socket was writing memory notes. It is the skill doors'
// bug (skills.go) in another room, and it gets the same repair: the doors
// cross the wire, and the engine says once, at the door, whether its
// conversation has a brain at all.

// memoryDoor is the slice of *session.Agent this file speaks to. It is asserted
// rather than required, on [skillDoor]'s terms.
type memoryDoor interface {
	Remembers() bool
	Remember(text string) (string, error)
	Forget(query string) (string, error)
	Memories(query string) ([]session.MemoryLine, error)
}

// memoryKnown is whether this engine's conversation has a brain to write into.
// It asks the agent's own predicate, so the welcome, the belt's `remember` and
// the surface's commands all read one fact.
func memoryKnown(agent any) bool {
	door, ok := agent.(memoryDoor)
	return ok && door.Remembers()
}

// errNoFarMemory is what a door answers against an engine whose conversation
// has no brain, or none that can cross.
var errNoFarMemory = errors.New("the engine this conversation is on is not remembering anything")

// Remembers answers for THE MACHINE AT THE OTHER END, off what it said at the
// door.
func (a *Agent) Remembers() bool { return a.c.Welcome().Memory }

// Remember keeps one thing on the far machine and answers with its title.
func (a *Agent) Remember(text string) (string, error) {
	return a.memoryTitle(MethodRemember, text)
}

// Forget drops the far machine's best match and answers with its title, or "".
func (a *Agent) Forget(query string) (string, error) {
	return a.memoryTitle(MethodForget, query)
}

// Memories lists what the far machine keeps, or the matches for a query.
func (a *Agent) Memories(query string) ([]session.MemoryLine, error) {
	if !a.Remembers() {
		return nil, errNoFarMemory
	}
	payload, err := a.c.call(nil, MethodMemories, query)
	if err != nil {
		return nil, err
	}
	var lines []session.MemoryLine
	return lines, json.Unmarshal(payload, &lines)
}

// memoryTitle is the one shape Remember and Forget share: a string in, the
// title it landed under or dropped out.
func (a *Agent) memoryTitle(method, text string) (string, error) {
	if !a.Remembers() {
		return "", errNoFarMemory
	}
	payload, err := a.c.call(nil, method, text)
	if err != nil {
		return "", err
	}
	var title string
	return title, json.Unmarshal(payload, &title)
}

// serveMemory answers the three doors against the agent this engine has open.
func serveMemory(agent any, call Frame) (json.RawMessage, error) {
	door, ok := agent.(memoryDoor)
	if !ok || !door.Remembers() {
		return nil, errNoFarMemory
	}
	text, err := arg[string](call)
	if err != nil {
		return nil, err
	}
	switch call.Method {
	case MethodRemember:
		return marshalTitle(door.Remember(text))
	case MethodForget:
		return marshalTitle(door.Forget(text))
	default:
		lines, err := door.Memories(text)
		if err != nil {
			return nil, err
		}
		return json.Marshal(lines)
	}
}

func marshalTitle(title string, err error) (json.RawMessage, error) {
	if err != nil {
		return nil, err
	}
	return json.Marshal(title)
}
