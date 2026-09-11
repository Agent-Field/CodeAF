package tui3

import "strings"

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

// chatProjectWord is that rule: the folder's word, and NOTHING where the folder
// is not a project. `here` is the folder the window drawing the row is itself
// standing in, and "" where the caller has no such folder to compare against —
// which is the honest answer for a surface that draws several projects at once.
func chatProjectWord(project, workspace, folder, here, tilde string) string {
	project = strings.TrimSpace(project)
	switch {
	case project == "" || project == "~":
		return ""
	case homeScratchFolder(workspace, tilde):
		return ""
	case here != "" && folder == here:
		return ""
	}
	return project
}
