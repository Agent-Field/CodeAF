package tui3

import "github.com/Agent-Field/codeaf/internal/factory"

// ── A STAGE THE RECIPE FIXES ────────────────────────────────────────────────
//
// FIXED STAGES ARE THE TEAM'S LAW AND SHOW IT (owner decision, 2026-10-08). A
// stage the recipe file fixes draws the lock ([tokens.GLocked]) where its
// `on` stands, its ask dim, and no key on the floor switches it off or edits
// it: the press says [factoryFixedWords] on the note line instead.

// stageFixedFor is the test hook on [stageFixed] until the field exists.
// REMOVED AT MERGE with factory/fixed, with the accessor's body.
var stageFixedFor func(factory.Stage) bool

// stageFixed says whether the recipe file fixes s. Every reader of the fact
// asks here, so the switch to the field is one line.
func stageFixed(s factory.Stage) bool {
	if stageFixedFor != nil {
		return stageFixedFor(s)
	}
	// until factory/fixed lands: then s.Fixed
	return false
}

// stageLocked says whether s is fixed AND on: the stage no key may switch
// off. A fixed stage that is off is drawn and turned like any other, because
// switching one on is never refused.
func stageLocked(s factory.Stage) bool { return s.On && stageFixed(s) }

// factoryFixedWords is the one sentence every refusal of a fixed stage says,
// the engine's own (FixedRefusal, on factory/fixed):
// `security is fixed by the recipe · change .codeaf/factory.md to change it`.
func factoryFixedWords(name string) string {
	return name + " " + wordFixedByRecipe + rowSep + wordChangeTheRecipeFile
}
