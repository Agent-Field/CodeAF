package remote

import "github.com/Agent-Field/codeaf/internal/session"

// Remembers reads the engine's pushed request-profile facts without a wire
// wait. Older engines omit that field and keep their welcome-only capability.
func (a *Agent) Remembers() bool {
	if memory := a.c.Facts().Memory; memory != nil {
		return *memory
	}
	return a.c.Welcome().Memory
}

// Remember writes through the conversation's own engine. The default local
// launch and an ssh attachment hold this same handle, so neither can mistake
// a missing local brain for the connected machine's memory setting.
func (a *Agent) Remember(text string) (string, error) { return a.c.Remember(text) }

// Forget removes the one line a query names on the engine machine — or, with
// an empty query, undoes that conversation's newest memory change — using the
// same command path as an in-process session rather than the place editor's
// row-id path.
func (a *Agent) Forget(query string) (string, error) { return a.c.ForgetQuery(query) }

// Memories lists the connected conversation's saved notes, including an empty
// enabled store. The server preserves an off setting as a refusal instead.
func (a *Agent) Memories(query string) ([]session.MemoryLine, error) {
	return a.c.Memories(query)
}

// RememberAlways is /always through the conversation's own engine, for the
// reason Remember is: that engine's project decides where the line holds.
func (a *Agent) RememberAlways(text string, everywhere bool) (string, error) {
	return a.c.RememberAlways(text, everywhere)
}

// AlwaysMemories lists what holds for the connected conversation.
func (a *Agent) AlwaysMemories(everywhere bool) ([]session.MemoryLine, error) {
	return a.c.AlwaysMemories(everywhere)
}

// MoveMemory moves one line to where the person says it holds, resolved by the
// conversation's own engine against its project.
func (a *Agent) MoveMemory(id, scope string) (string, error) { return a.c.MoveMemory(id, scope) }

// MemoryProject is the owner the connected conversation keeps project lines
// under.
func (a *Agent) MemoryProject() string { return a.c.MemoryProject() }
