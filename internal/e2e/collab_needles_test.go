package e2e

import "testing"

// collab_needles_test.go holds the NAMES of the Wave 3 coordination words the
// tmux suite will read off a real management chat once t-w3-live drives one.
// It is the same shape as folders_needles_test.go: the untagged word gate
// (tuiwords_test.go's TestEveryWordInTheTableIsWaitedForBySomething) needs to
// know they are alive, and inventing a tagged waitFor that this lane never ran
// would be a fake live pass.
func TestTheCollabSurfaceWordsAreNamed(t *testing.T) {
	for _, name := range []string{
		"collabMarkWord",
		"collabUnmarkWord",
		"collabCoordinateWord",
		"collabNeedChatWord",
		"collabMarkedWord",
		"collabRequestWord",
		"collabReplyWord",
		"collabSentWord",
		"collabSourceWord",
		"collabCouldNotMark",
		"collabCouldNotUnmark",
		"collabCouldNotCoord",
		"collabNoStandWord",
	} {
		if say(t, name) == "" {
			t.Fatalf("the needle %q is in the table but spells nothing", name)
		}
	}
}
