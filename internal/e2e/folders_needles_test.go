package e2e

import "testing"

// folders_needles_test.go holds the NAMES of the folders-surface words the
// tmux suite will read off a real home once t-w1-live drives one. It is the
// same shape as harness_surface_e2e_test.go: the untagged word gate
// (tuiwords_test.go's TestEveryWordInTheTableIsWaitedForBySomething) needs to
// know they are alive, and inventing a tagged waitFor that this lane never ran
// would be a fake live pass.
func TestTheFoldersSurfaceWordsAreNamed(t *testing.T) {
	for _, name := range []string{
		"homePanelFolders",
		"homeFoldersWhisper",
		"homeFoldersUnwired",
		"homeFoldersAlsoIn",
		"homeFoldersNewChat",
		"foldersSlashWord",
		"folderChooserHelp",
	} {
		if say(t, name) == "" {
			t.Fatalf("the needle %q is in the table but spells nothing", name)
		}
	}
}
