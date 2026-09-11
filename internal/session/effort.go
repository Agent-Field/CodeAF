package session

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/effort"
)

// ── the effort ladder, as this session reads it ─────────────────────────────
//
// THIS FILE IS WHERE EVERY MODEL CALL IN THIS PROCESS LEARNS HOW HARD TO THINK,
// and [Agent.effortFor] is the one function that answers. A spawn site that
// picks its own rung is the defect the ladder was written to end: a person who
// dials a rung expects it to BE the rung, and a call that quietly kept its own
// answer is a knob that does nothing with no way to tell from outside.
//
// The scopes and their order are internal/effort's; nothing about the
// precedence is decided here. What is decided here is which of this session's
// fields fills which scope, and it is this:
//
//	turn         the level dialled onto the model now in use (agent.go's map)
//	conversation the rung this session was set to, kept in meta.json
//	task         the rung set on the work this session IS (Config.Effort)
//	role         what this session is for (Config.EffortRole)
//	default      the install's `effort` row (Config.DefaultEffort)
//
// A child agent — a task worker, a standing firing, an adaptive-run node — is a
// whole session of its own, so the work's rung reaches it as ITS conversation
// rather than as a fifth argument threaded down every call: the child is the
// work, and its own turns are the work being done.

// effortFor is the rung one call on this model should ask for.
//
// It takes the model because the turn scope is held per model id: a step that
// has moved to a fallback asks that model for the level somebody set on IT,
// never for the level dialled onto the model that stopped answering.
func (a *Agent) effortFor(model string) effort.Rung {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.effortLocked(model)
}

func (a *Agent) effortLocked(model string) effort.Rung {
	return effort.Resolve(effort.Scope{
		Turn:         a.reasoningLocked(model),
		Conversation: a.effort,
		Task:         a.config.Effort,
		Role:         a.config.EffortRole,
		Default:      a.config.DefaultEffort,
	})
}

// ── the setters a surface calls ─────────────────────────────────────────────

// ConversationEffort is the rung this conversation was set to, "" when nobody
// has set one. It is the STORED rung and not the resolved one: a surface drawing
// the dial has to show what a person chose, and the emptiness law says a scope
// that chose nothing draws nothing.
func (a *Agent) ConversationEffort() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.effort.String()
}

// ResolvedEffort is the rung this session's next turn will actually ask for,
// whichever scope decided it. It is what a surface shows when it wants to say
// what is HAPPENING rather than what was chosen.
func (a *Agent) ResolvedEffort() string {
	return a.effortFor(a.Model()).String()
}

// SetConversationEffort sets this conversation's rung and reports whether the
// word was one. It is sticky: the rung lands in the session folder's meta.json
// with everything else about this conversation (placemeta.go), so a person who
// dials it, closes the terminal and comes back finds it where they left it.
//
// A CALL IN FLIGHT IS UNAFFECTED. The rung is latched at the top of a turn with
// the model (loop.go), so a change made while the agent is working lands on the
// next call and never half-way through the one being answered.
func (a *Agent) SetConversationEffort(rung string) bool {
	parsed, ok := effort.Parse(rung)
	if !ok {
		return false
	}
	a.mu.Lock()
	a.effort = parsed
	a.mu.Unlock()
	// Written through the same door every other fact about this conversation is
	// written through, so the file keeps one shape and one writer.
	a.stampEffort()
	return true
}

// DefaultEffort is the install's own rung, as this session was launched with.
// It is read-only here: the row lives in the profile and a surface writes it
// with config.WriteDefaultEffort, because a setting that outlives the process
// cannot be owned by a session.
func (a *Agent) DefaultEffort() string { return a.config.DefaultEffort.String() }

// TaskEffort is the rung set on one task, "" when nobody has set one and for a
// task this session does not have.
func (a *Agent) TaskEffort(id uint64) string {
	node := a.taskNode(id)
	if node == nil {
		return ""
	}
	node.graph.mu.Lock()
	defer node.graph.mu.Unlock()
	if node.nextEffort != nil {
		return *node.nextEffort
	}
	return node.spec.effort.String()
}

// SetTaskEffort sets the rung one task's workers run at.
//
// It is checkpointed with the rest of the task (task_store.go writes
// spec.effort), so a task that outlives the process comes back at the rung it
// was set to.
//
// Settled ordinary tasks save a separate continuation rung; queued and running
// tasks update their worker setup. Saving a rung never restarts work.
//
// A WORKER ALREADY RUNNING KEEPS THE RUNG IT STARTED ON, and that is the same
// contract the model switch has rather than a second mechanism. The child was
// launched with the rung as its own conversation, so a change here reaches the
// next worker this task starts — a retry, a repair, a part handed out after the
// change — and never reshapes a request already on the wire.
func (a *Agent) SetTaskEffort(id uint64, rung string) error {
	parsed, ok := effort.Parse(rung)
	if !ok {
		return fmt.Errorf("%q is not a thinking level. Use one of: %s, or auto",
			rung, strings.Join(rungWords(), ", "))
	}
	node := a.taskNode(id)
	if node == nil {
		return fmt.Errorf("no task %d in this session", id)
	}
	node.graph.mu.Lock()
	switch node.state {
	case TaskRunning, TaskQueued:
		node.spec.effort = parsed
	case TaskDone, TaskFailed, TaskUnverified:
		if node.kind == TaskKindHarness || node.kind == TaskKindSubharness {
			node.graph.mu.Unlock()
			return fmt.Errorf("task %d cannot be continued", id)
		}
		word := parsed.String()
		node.nextEffort = &word
	default:
		state := node.state
		node.graph.mu.Unlock()
		return fmt.Errorf("task %d is %s, not available for thinking changes", id, state)
	}
	node.graph.mu.Unlock()
	node.graph.checkpoint()
	a.emitTaskUpdate(node.notice())
	return nil
}

// rungWords is the ladder as a sentence lists it. It reads the ladder rather
// than repeating it, so a rung added or dropped there moves every refusal that
// names them (CLAUDE.md's one-source-of-truth law).
func rungWords() []string {
	words := make([]string, 0, len(effort.Rungs))
	for _, rung := range effort.Rungs {
		words = append(words, rung.String())
	}
	return words
}
