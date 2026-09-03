package tui3

import "testing"

// EVERY POSTURE THAT FOLDS HAS SOMETHING THAT FOLDS IT.
//
// lens.go dispatches a page's fold policy through a table ([folders]) rather
// than a switch, which is what lets this be a test at all — but a table has a
// failure a switch does not: a style added to the [foldStyle] block and not to
// the table falls through [app.deckFolds]'s `ok` check and folds NOTHING, on a
// page whose whole declaration says it folds. Silently, with no compiler
// anywhere in it.
//
// So the styles below [foldNone] are walked and each must have a folder.
// [foldNone] is the one deliberate omission — it is the absence of a fold, and
// deriving an empty answer twice would be the table saying the same thing in two
// ways — which is why it is last in the const block and why this stops there.
func TestEveryFoldStyleThatFoldsHasAFolder(t *testing.T) {
	for style := foldStyle(0); style < foldNone; style++ {
		if folders[style] == nil {
			t.Errorf("foldStyle %d has no folder in the folders table, so every page declaring it folds nothing.\n"+
				"Add its line to folders (lens.go), or place the style after foldNone if it genuinely folds none.", style)
		}
	}
	if folders[foldNone] != nil {
		t.Error("foldNone has a folder; it is the absence of a fold and derives nothing")
	}
}
