package e2e

import "testing"

// folders_columns_needles_test.go holds the NAMES of the J44–J49 / F09/F10
// words the tmux suite waits for. It is the same shape as
// folders_entry_needles_test.go: the untagged word gate
// (tuiwords_test.go's TestEveryWordInTheTableIsWaitedForBySomething) needs to
// know they are alive. Live tmux is t-rx-validate; inventing a tagged waitFor
// that this lane never defined would be a fake live pass.
func TestTheFoldersColumnsJourneyWordsAreNamed(t *testing.T) {
	for _, name := range []string{
		"foldersPlaceNewFolder",
		"foldersPlaceNewChat",
		"foldersOrganizeExistingWord",
		"foldersOrganizeThisChatWord",
		"foldersManageThisFolderWord",
		"foldersAddExistingWord",
		"foldersCoordinateSelectedWord",
		"foldersOpenChatWord",
		"foldersAddedToPrefix",
		"foldersWhyUndoTail",
		"foldersShiftRightHint",
		"homeFoldersAlsoIn",
		"homePanelFolders",
		"homeFoldersWhisper",
		"folderChooserHelp",
	} {
		if say(t, name) == "" {
			t.Fatalf("the needle %q is in the table but spells nothing", name)
		}
	}
	if say(t, "foldersOrganizeThisChatWord") != foldersOrganizeThisChatWord {
		t.Fatalf("Organize this chat needle drifted from the columns+reactive contract")
	}
	if say(t, "foldersManageThisFolderWord") != foldersManageThisFolderWord {
		t.Fatalf("Manage this folder needle drifted from the columns+reactive contract")
	}
	if say(t, "foldersCoordinateSelectedWord") != foldersCoordinateSelectedWord {
		t.Fatalf("Coordinate selected needle drifted from the columns+reactive contract")
	}
	if say(t, "foldersShiftRightHint") != foldersShiftRightHint {
		t.Fatalf("shift+→ actions needle drifted from the columns+reactive contract")
	}
	if say(t, "homeFoldersAlsoIn") != foldersAlsoInWord {
		t.Fatalf("also in  needle drifted from the shared-identity contract")
	}
}
