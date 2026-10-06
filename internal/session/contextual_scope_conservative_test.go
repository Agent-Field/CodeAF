package session

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/reflex"
	"github.com/Agent-Field/codeaf/internal/store"
)

// SCOPE WIDENS ONLY ON THE PERSON'S OWN UNAMBIGUOUS WORDS. Bare "everywhere" is
// ambiguous between the person at large and a span the clause never named, so it
// stays local; a clause with a project qualifier, an exception or a negation
// stays local; and a genuine cross-project grant still widens.
func TestExplicitGlobalScopeIsConservative(t *testing.T) {
	local := []string{
		"Use tabs everywhere",
		"Use tabs everywhere in this app",
		"Use tabs everywhere in this service",
		"Use tabs everywhere unless vendor",
		"Use tabs everywhere, project-local only",
		"Use tabs everywhere in our repo",
		"Use tabs everywhere in this monorepo",
		"Use tabs everywhere in this workspace I mean",
		"Use tabs everywhere except generated files",
		"Use tabs everywhere restricted to the frontend",
		"Use tabs, not everywhere",
		"Use tabs never everywhere",
	}
	for _, quote := range local {
		if explicitContextualGlobal(quote) {
			t.Errorf("%q widened to the person at large", quote)
		}
	}
	global := []string{
		"Across all projects I prefer concise answers",
		"Use tabs everywhere in all projects",
		"Use tabs everywhere across projects",
		"I have a personal preference for tabs",
	}
	for _, quote := range global {
		if !explicitContextualGlobal(quote) {
			t.Errorf("%q did not widen on an explicit grant", quote)
		}
	}
}

// AND THE GATE RUNS THROUGH THE REAL GROUND PIPELINE, not only the helper: a bare
// "everywhere" claim lands in the project, and a cross-project grant lands on the
// person.
func TestConservativeScopeThroughGround(t *testing.T) {
	demoted := memoryTurnEvidence{User: "Use tabs everywhere in this app.", At: time.Now()}.ground(
		reflex.ExtractResult{Mem: 1, Type: store.MemoryPreference, Scope: store.MemoryScopeUser,
			Source: "user", SourceQuote: "Use tabs everywhere in this app", Authority: "observation", Text: "use tabs"})
	if demoted.Scope == store.MemoryScopeUser {
		t.Fatalf("a bare everywhere widened through ground: %+v", demoted)
	}
	kept := memoryTurnEvidence{User: "Across all projects I prefer concise answers.", At: time.Now()}.ground(
		reflex.ExtractResult{Mem: 1, Type: store.MemoryPreference, Scope: store.MemoryScopeUser,
			Source: "user", SourceQuote: "Across all projects I prefer concise answers", Authority: "observation", Text: "prefer concise answers"})
	if kept.Scope != store.MemoryScopeUser {
		t.Fatalf("an explicit cross-project grant was demoted through ground: %+v", kept)
	}
}
