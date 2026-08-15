package registry

import (
	"strings"
	"testing"
)

// itemVerbIDs is the fixed contract three lanes build against: the notebook and
// the work board resolve these exact ids, and the backend wires them. A rename
// here is a rename in two packages that cannot see this one, so the list is
// written out rather than derived — that is the whole point of a contract.
var itemVerbIDs = []string{
	"charter.pause", "charter.cadence", "charter.probation", "charter.retire",
	"service.stop", "service.restart", "service.autorestart",
	"belief.forget", "belief.edit",
	"craft.run", "craft.revert", "craft.retire",
	"skill.retire",
}

func TestEveryItemVerbIsSeeded(t *testing.T) {
	for _, id := range itemVerbIDs {
		entry, found := ByID(id)
		if !found {
			t.Fatalf("%s is missing from the catalog; two UI lanes resolve that id", id)
		}
		if !entry.Scope.Has(ScopeItem) {
			t.Fatalf("%s is scoped %d, want the item scope", id, entry.Scope)
		}
		if entry.Verb == "" || entry.Description == "" {
			t.Fatalf("%s = %+v; every row draws a verb and a description", id, entry)
		}
		if entry.Key != "" || entry.ChordKey != "" || entry.Slash != "" {
			t.Fatalf("%s carries an accelerator (%q/%q/%q); the item IS the accelerator",
				id, entry.Key, entry.ChordKey, entry.Slash)
		}
		if entry.Journal.Empty() {
			t.Fatalf("%s journals nothing, so nothing it claims to do is durable", id)
		}
	}
}

// The two shapes, checked as data: a verb either fires (with a question first
// when it cannot be undone) or seeds the composer. Never both — that would be
// asking twice about one sentence — and never neither, which would leave a
// surface guessing which door it is drawing.
func TestAnItemVerbEitherAsksOrSteersButNeverBoth(t *testing.T) {
	steering := map[string]bool{"charter.cadence": true, "belief.edit": true, "craft.revert": true}
	destructive := map[string]bool{
		"charter.retire": true, "service.stop": true, "belief.forget": true,
		"craft.retire": true, "skill.retire": true,
	}
	for _, id := range itemVerbIDs {
		entry, _ := ByID(id)
		if entry.Confirm != "" && entry.Steer != "" {
			t.Fatalf("%s both asks and steers", id)
		}
		if steering[id] != (entry.Steer != "") {
			t.Fatalf("%s steers=%v, want %v", id, entry.Steer != "", steering[id])
		}
		if destructive[id] != entry.Destructive() {
			t.Fatalf("%s asks first=%v, want %v", id, entry.Destructive(), destructive[id])
		}
		if entry.Confirm != "" && !strings.Contains(entry.Confirm, "?") {
			t.Fatalf("%s confirms with %q, which is not a question", id, entry.Confirm)
		}
	}
}

// A steer is a sentence with the item's own name in it, and exactly one hole
// for that name — a seed with a %s left in it, or with two, is a sentence the
// person has to repair before they can write theirs.
func TestASteerSeedsOneNamedSentence(t *testing.T) {
	for _, entry := range entries {
		if entry.Steer == "" {
			if entry.SteerFor("the morning digest") != "" {
				t.Fatalf("%s has no steer but seeded one anyway", entry.ID)
			}
			continue
		}
		if count := strings.Count(entry.Steer, "%"); count != 1 || !strings.Contains(entry.Steer, "%s") {
			t.Fatalf("%s seeds %q, want exactly one %%s", entry.ID, entry.Steer)
		}
		seeded := entry.SteerFor("the morning digest")
		if !strings.Contains(seeded, "the morning digest") || strings.Contains(seeded, "%") {
			t.Fatalf("%s seeded %q", entry.ID, seeded)
		}
		if !strings.HasSuffix(seeded, " ") {
			t.Fatalf("%s seeded %q; a seed ends where the person starts typing", entry.ID, seeded)
		}
		// A seed with no item is no seed: a half-written sentence with a hole in
		// it is worse than an empty composer.
		if entry.SteerFor("  ") != "" {
			t.Fatalf("%s seeded a sentence with nothing to name", entry.ID)
		}
	}
}

// An item verb without its item is not a door. The unscoped palette — which is
// what a surface with no focus asks with — must never offer one.
func TestItemVerbsStayOutOfTheUnscopedPalette(t *testing.T) {
	for _, entry := range ForScope(ScopeAny) {
		if entry.Scope == ScopeItem {
			t.Fatalf("%s is offered with nothing selected", entry.ID)
		}
	}
	for _, match := range FuzzyMatch(ScopeAny, "retire") {
		if match.Entry.Scope == ScopeItem {
			t.Fatalf("the unscoped palette found %s by name", match.Entry.ID)
		}
	}
	found := false
	for _, entry := range ForScope(ScopeEvery) {
		found = found || entry.ID == "craft.retire"
	}
	if !found {
		t.Fatal("the whole catalog does not include the item verbs")
	}
	if _, ok := ByKey(ScopeItem, "r"); ok {
		t.Fatal("an item verb bound a bare letter")
	}
}

// The catalog's ASCII assumption covers the two new fields too: they are
// rendered by the same surfaces and matched by the same byte-indexed lowercase.
func TestConfirmAndSteerTextIsASCII(t *testing.T) {
	for _, entry := range entries {
		for _, field := range []string{entry.Confirm, entry.Steer} {
			for index := 0; index < len(field); index++ {
				if field[index] > 0x7f {
					t.Fatalf("%s has a non-ASCII byte in %q", entry.ID, field)
				}
			}
		}
	}
}
