package tui3

import (
	"path/filepath"
	"strings"
)

// projecttag.go holds THE ONE RULE about whether a row naming a conversation
// also names the folder that conversation works in.
//
// The mission-control ruling retired two of the three answers this surface used
// to give (docs/design/home-mission-control/DESIGN.md §1, "What is retired"):
// a project tag on a row in THIS WINDOW'S OWN FOLDER, where the tag is the same
// word on every row and says nothing that tells one row from another; and `~`
// as a project name, because the home directory is where a person stands rather
// than something they are working on. A scratch folder at the top of the
// temporary directory goes with them — its word is a name somebody made up for
// a minute. The projects panel still lists all three, as the paths they are.
//
// IT IS HERE AND NOT ON ONE OF THE TWO PAGES because both of them draw the same
// object. Home's list of conversations asks it ([chatProjectTag]) and the tasks
// place asks it of every conversation root ([tasksChatRow]); the tasks place
// used to ask nobody at all, so a chat in the home directory arrived on it
// wearing a lone `~` at the right of its name.

// chatFolder is the three facts the rule needs about one conversation: what its
// folder is CALLED, the tools root recorded for the conversation, and the
// project directory it belongs to.
//
// IT IS A STRUCT AND NOT THREE STRINGS IN A ROW. Every one of them is a path or
// a path's name, so a positional signature is one a caller can transpose and
// still compile — and the failure that buys is silent, a tag drawn for the wrong
// reason on a row nobody looks at twice.
type chatFolder struct {
	project   string
	workspace string
	dir       string
}

// chatProjectWord is that rule: the folder's word, and NOTHING where the folder
// is not a project. `here` is the folder the window drawing the row is itself
// standing in, and "" where the caller has no such folder to compare against —
// which is the honest answer for a surface that draws several projects at once,
// and the answer a window whose own conversation the world scan has not met yet
// falls back to ([readTasks] says so where it reads the folder).
func chatProjectWord(chat chatFolder, here, tilde string) string {
	project := strings.TrimSpace(chat.project)
	switch {
	case project == "" || project == "~":
		return ""
	case homeScratchFolder(chat.workspace, tilde):
		return ""
	// BOTH SIDES ARE CLEANED, and not because either is known to be ragged: the
	// check above this one cleans its paths, [session.Project]'s own path is a
	// recorded workspace or a launch directory with no cleaning applied to
	// either, and a rule that is byte-exact on one line and forgiving on the next
	// is a rule nobody can predict.
	case here != "" && sameFolder(chat.dir, here):
		return ""
	}
	return project
}

// sameFolder is that comparison, spelled once.
func sameFolder(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	return filepath.Clean(a) == filepath.Clean(b)
}
