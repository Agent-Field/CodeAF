package main

import "github.com/Agent-Field/codeaf/internal/appwire"

// ── WHO STARTED AN AGENT ────────────────────────────────────────────────────
//
// Every agent made here records its origin once, in the app's state file
// keyed by agent id: "person" for the person's own create, the creating
// agent's id for a member its manager started, or the word an app acting for
// the person passed to agents.create. A conversation folder is copied when a
// chat is branched, so the origin is kept beside the id rather than inside the
// folder. An agent made before this was kept has no entry and says nothing.

// keepCreatedBy records who started agent id and answers the word kept: the
// asked word, or the person when none was asked.
func (b *appBackend) keepCreatedBy(id, asked string) (string, error) {
	word := asked
	if word == "" {
		word = appwire.CreatedByPerson
	}
	err := b.state.write(func(f *appStateFile) error {
		if f.CreatedBy == nil {
			f.CreatedBy = map[string]string{}
		}
		f.CreatedBy[id] = word
		return nil
	})
	return word, err
}

// createdBy is who started agent id, "" when nothing was kept for it.
func (b *appBackend) createdBy(id string) string {
	word := ""
	_ = b.state.read(func(f *appStateFile) { word = f.CreatedBy[id] })
	return word
}
