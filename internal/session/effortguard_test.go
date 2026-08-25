package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── THE LAW: ONE PLACE EFFORT COMES FROM ────────────────────────────────────
//
// The ladder is only worth having if it is the ONLY answer. A spawn site that
// decides for itself how hard its model should think is not a small local
// judgment — it is a knob that does nothing, silently, for whoever turned it,
// with no way to tell from the outside which of the two it is.
//
// The engine's model calls are spread over a dozen files here (a turn, a task
// worker, an errand, a firing, a sentinel, a design round), and the next one is
// always one file away. So this is a test rather than a paragraph: a lane that
// adds a stamp has to come here and say which it is, and the two lists below are
// the whole of what is allowed.

// ladderStampers are the files that stamp a rung from the ladder, and every one
// of them must ASK the ladder rather than answer for itself.
//
// The check is that the same file also resolves — through [Agent.effortFor],
// [Agent.effortLocked] or effort.Resolve — because a file that stamps without
// resolving has picked its own rung, which is exactly the defect.
var ladderStampers = map[string]string{
	"loop.go":         "the person's own turn: the rung latched with the model at the top of a turn",
	"auxiliary.go":    "every errand in the package, through the one role-call door",
	"standing_run.go": "a firing's sentinel, at the standing role's own floor",
}

// legacyEffortStampers are the calls that still speak the ADAPTER'S older
// vocabulary — provider.Effort, carried on a crew tier value or a harness phase
// — rather than a rung on the ladder.
//
// They are not a backlog and they are not wrong: a tier suffix is a narrower
// notation with three levels of its own (internal/roles), and a harness phase
// default is a decision about one call shape rather than about a person's
// depth. What this list buys is that the boundary is DELIBERATE. A new spawn
// site cannot join it by accident, because joining it means editing this map and
// writing down which of the two things the call is.
var legacyEffortStampers = map[string]string{
	"harness_build.go":  "a design round's role call, from the designer tier's own suffix",
	"harness_task.go":   "carries a design's phase effort onto its node",
	"task_shape.go":     "the shaper's role call, from the shaper tier's own suffix",
	"task_store.go":     "rebuilds a design's phase effort off the checkpoint",
	"orchestrate.go":    "an adaptive-run node's role call, from the node's own tier",
	"subharness_env.go": "a saved program's ai() call, from the options its author wrote",
	"agent.go":          "the per-model dial's doc comment, which names the shape it is NOT",
}

// EVERY FILE THAT STAMPS A RUNG ALSO RESOLVES ONE.
func TestEverySpawnSiteAsksTheLadderHowHardToThink(t *testing.T) {
	stamps := 0
	for name, source := range packageSources(t) {
		if !strings.Contains(source, "provider.WithConfiguredEffortRung(") &&
			!strings.Contains(source, "provider.WithEffortRung(") {
			continue
		}
		stamps++
		reason, allowed := ladderStampers[name]
		if !allowed {
			t.Fatalf("%s stamps a rung onto a provider call and is not in ladderStampers.\n"+
				"Either route it through the resolver and add it there with the reason, "+
				"or hand the rung to a child session's own ladder instead of stamping here "+
				"(effort.go states the law).", name)
		}
		if reason == "" {
			t.Fatalf("%s is in ladderStampers with no reason written down", name)
		}
		if !strings.Contains(source, "effort.Resolve(") &&
			!strings.Contains(source, "a.effortFor(") &&
			!strings.Contains(source, "a.effortLocked(") {
			t.Fatalf("%s stamps a rung it did not ask the ladder for. Every rung on a "+
				"provider call is effort.Resolve's answer — a file that decides its own "+
				"is a dial somebody turned that does nothing (effort.go).", name)
		}
	}
	// And the list cannot rot into a description of a package that has moved on.
	if stamps != len(ladderStampers) {
		t.Fatalf("%d files stamp a rung and ladderStampers names %d — a name in that map "+
			"that no longer stamps is a law nothing enforces", stamps, len(ladderStampers))
	}
}

// AND THE OLDER VOCABULARY CANNOT SPREAD BY ACCIDENT.
//
// provider.Effort still reaches the wire from a handful of places that are not
// the ladder, every one of them a crew tier's own suffix or a saved program's
// own option. This pins that set: a new file reaching for the adapter's words
// instead of a rung fails here and has to say which it meant.
func TestTheAdaptersOlderEffortVocabularyStaysWhereItIs(t *testing.T) {
	for name, source := range packageSources(t) {
		if !strings.Contains(source, "provider.Effort") &&
			!strings.Contains(source, "provider.ParseEffort(") &&
			!strings.Contains(source, "provider.WithReasoningEffort(") &&
			!strings.Contains(source, "provider.WithConfiguredReasoningEffort(") {
			continue
		}
		if _, allowed := legacyEffortStampers[name]; !allowed {
			t.Fatalf("%s reaches for the adapter's own effort vocabulary.\n"+
				"A person's depth is a rung on the ladder (internal/effort) and reaches the "+
				"wire through provider.WithConfiguredEffortRung. If this really is a crew "+
				"tier's suffix or one call shape's own economy, say so in "+
				"legacyEffortStampers.", name)
		}
	}
}

// packageSources reads every non-test Go file of this package, by base name.
// It is the same shape internal/config's derivation test uses to prove a
// settings row is wired to something: a law about where code may live can only
// be checked by reading where it lives.
func packageSources(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package: %v", err)
	}
	sources := make(map[string]string, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		sources[name] = string(raw)
	}
	if len(sources) == 0 {
		t.Fatal("no sources read; the law below would pass by reading nothing")
	}
	return sources
}
