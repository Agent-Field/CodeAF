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

// Forget removes the best match on the engine machine, using the same command
// path as an in-process session rather than the place editor's row-id path.
func (a *Agent) Forget(query string) (string, error) { return a.c.ForgetQuery(query) }

// Memories lists the connected conversation's saved notes, including an empty
// enabled store. The server preserves an off setting as a refusal instead.
func (a *Agent) Memories(query string) ([]session.MemoryLine, error) {
	return a.c.Memories(query)
}
