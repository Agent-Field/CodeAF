package e2e

import "testing"

// folders_entry_needles_test.go holds the NAMES of the Folders-entry words the
// tmux suite waits for on J36–J43. It is the same shape as folders_needles_test.go:
// the untagged word gate (tuiwords_test.go's TestEveryWordInTheTableIsWaitedForBySomething)
// needs to know they are alive. Live tmux is t-fe-validate; inventing a tagged
// waitFor that this lane never defined would be a fake live pass.
func TestTheFoldersEntryJourneyWordsAreNamed(t *testing.T) {
	for _, name := range []string{
		"barHomeWord",
		"barTasksWord",
		"homePanelSpend",
		"barSettingsWord",
		"barFoldersWord",
		"homePanelFolders",
		"homeFoldersWhisper",
		"homeFoldersUnwired",
		"homeFoldersNewChat",
		"foldersSlashWord",
		"folderChooserHelp",
		"foldersPlaceNewFolder",
		"foldersPlaceNewChat",
		"foldersOrganizeExistingWord",
		"foldersOrganizeSlashWord",
		"foldersJobQueuedWord",
		"foldersJobRunningWord",
		"foldersJobDelayedWord",
		"foldersJobDoneWord",
		"foldersJobCancelWord",
		"homeFoldersAlsoIn",
		"folderInstructWord",
		"folderDiscoveryDelayedWord",
	} {
		if say(t, name) == "" {
			t.Fatalf("the needle %q is in the table but spells nothing", name)
		}
	}
	if say(t, "foldersPlaceNewFolder") != foldersEntryNewFolderWord {
		t.Fatalf("New folder needle drifted from the Folders-entry contract")
	}
	if say(t, "foldersOrganizeExistingWord") != foldersEntryOrganizeWord {
		t.Fatalf("Organize existing chats needle drifted from the Folders-entry contract")
	}
}
