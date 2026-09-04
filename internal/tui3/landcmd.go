package tui3

// PUTTING THE WORK INTO THE FOLDER — /land, and the line that says there is
// something to land.
//
// Where the conversation stands it writes; where it only REFERS to a folder,
// its edits are kept in a copy of that folder and go in when somebody says so
// (internal/session/standingtree.go). This file is that "somebody says so".
//
// ── SHOWN FIRST, THEN DONE ──
//
// `/land` never moves a file. It says which folder, how many files, and names
// the first few; `/land now` is what actually puts them in. That split is the
// design's own law and not caution for its own sake: referring to a folder is
// allowed to be casual — inference, a dropped directory, a path in a sentence —
// precisely BECAUSE certainty is placed at the landing rather than at the
// referring. The one moment a person has to be sure is the one moment they are
// shown exactly what changes. It is the shape /cache clean already asks in
// (cachecmd.go), for the same reason and in the same words.
//
// ── AND THE LINE ABOVE THE BOX ──
//
// [app.landRow] is drawn only while something is waiting, and says nothing at
// all otherwise: a folder the conversation merely referred to is not news, and
// a permanent row naming it would be inventory. What is news is that there are
// changes somewhere that are not in the person's folder yet, which is exactly
// what the row says and the only thing it says.

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// folderLander is the session's landing seam, asserted rather than required of
// every Agent for [placeReferrer]'s reason: a scripted test agent and a hosted
// connection honestly do not have it, and a capability that cannot work is
// absent rather than broken.
type folderLander interface {
	UnlandedChanges() []session.StandingChange
	Land(folder string) (session.FolderLanding, error)
}

// landNamesShown is how many files a line names before it starts counting. A
// person reading this row is recognising their own work, not auditing it.
const landNamesShown = 4

// landRemoteWord is /land over a connection, and it is the same wall /folder
// meets: the conversation is on the other machine and the folders this process
// could put anything into are the laptop's.
const landRemoteWord = "putting changes into a folder is not available over --host yet — the conversation is on the other machine."

// landNothingWord is /land with nothing waiting. It says where changes DO go,
// because somebody typing this expected something and the useful answer is
// which folder their edits have been going into all along.
const landNothingWord = "nothing is waiting · what this conversation writes in the folder it is standing in is already there"

// landNoteMsg is one finished landing's answer, written as the line to show.
// The landing runs git — a commit, a merge, sometimes a copy of a whole folder
// — so it goes off the loop and the answer arrives here, exactly as a cache
// errand's does.
type landNoteMsg struct{ line string }

// waitingChanges is what has been written and not put in yet, or nothing. It is
// the ONE reading — the row above the box, the command's own answer and its
// refusals all ask this and never the session twice in two ways.
func (a *app) waitingChanges() []session.StandingChange {
	door, ok := a.agent.(folderLander)
	if !ok || a.hosted() {
		return nil
	}
	return door.UnlandedChanges()
}

// landHeight is the one row the waiting line takes, or none.
func (a *app) landHeight() int {
	if len(a.waitingChanges()) == 0 {
		return 0
	}
	return 1
}

// landRow is the line above the box: which folder is holding changes, how many
// files, and the word that puts them in.
//
// IT NAMES THE FOLDER BY ITS BASENAME, because that is what a person calls it
// and the full path is already the scope chip's job. Several folders at once
// collapse into a count rather than a list: the row exists to say there is
// something to land, and `/land` itself says the rest.
func (a *app) landRow(width int) string {
	waiting := a.waitingChanges()
	if len(waiting) == 0 {
		return ""
	}
	files := 0
	for _, change := range waiting {
		files += change.Files
	}
	where := waiting[0].Name
	if len(waiting) > 1 {
		where += " and " + itoa(len(waiting)-1) + " more"
	}
	return a.pal.dim(fit("  changes for "+where+" · "+itoa(files)+" "+plural("file", files)+" · /land", width))
}

// runLandCommand consumes /land and its two forms.
//
// The argument is read as "a folder, then maybe the word now", so all four
// shapes a person reaches for work: `/land`, `/land now`, `/land agentfield`,
// `/land agentfield now`. A folder is named only when more than one is waiting,
// which is the case this surface would otherwise have to guess at.
func (a *app) runLandCommand(rest string) tea.Cmd {
	if a.hosted() {
		a.note(landRemoteWord)
		return nil
	}
	door, ok := a.agent.(folderLander)
	if !ok {
		a.note(landNothingWord)
		return nil
	}
	folder, now := strings.TrimSpace(rest), false
	if trimmed := strings.TrimSuffix(folder, "now"); trimmed != folder &&
		(trimmed == "" || strings.HasSuffix(trimmed, " ")) {
		folder, now = strings.TrimSpace(trimmed), true
	}
	waiting := door.UnlandedChanges()
	if len(waiting) == 0 {
		a.note(landNothingWord)
		return nil
	}
	if folder == "" && len(waiting) > 1 {
		// The one thing this surface may not guess. Naming them is the answer
		// rather than picking the first, because the wrong folder is somebody's
		// real project.
		var names []string
		for _, change := range waiting {
			names = append(names, change.Name)
		}
		a.note("changes are waiting for " + strings.Join(names, " and ") + " · say which one · /land " + names[0])
		return nil
	}
	if !now {
		a.note(a.landPreview(waiting, folder))
		return nil
	}
	return func() tea.Msg { return landNoteMsg{line: landed(door.Land(folder))} }
}

// landPreview is what /land says before anything moves: the folder, the count,
// and the first few files by name.
func (a *app) landPreview(waiting []session.StandingChange, folder string) string {
	chosen := waiting[0]
	for _, change := range waiting {
		if folder != "" && (change.Name == folder || change.Folder == folder) {
			chosen = change
		}
	}
	files := ""
	if door, ok := a.agent.(interface {
		LandingFor(string) (session.FolderLanding, bool)
	}); ok {
		if landing, held := door.LandingFor(chosen.Folder); held {
			files = " · " + namedFiles(landing.Files)
		}
	}
	say := "now"
	if len(waiting) > 1 {
		say = chosen.Name + " now"
	}
	return "changes for " + chosen.Name + " · " + itoa(chosen.Files) + " " + plural("file", chosen.Files) +
		files + " · type /land " + say + " to put them in"
}

// namedFiles lists paths the way a sentence does: the first few by name and the
// rest as a count.
func namedFiles(files []string) string {
	if len(files) <= landNamesShown {
		return strings.Join(files, ", ")
	}
	return strings.Join(files[:landNamesShown], ", ") + " and " + itoa(len(files)-landNamesShown) + " more"
}

// landed is the one line a finished landing says, and it says WHERE THE WORK IS
// NOW in every branch — which is the half a person actually needs when
// something did not go cleanly.
func landed(landing session.FolderLanding, err error) string {
	if err != nil {
		return err.Error()
	}
	count := itoa(len(landing.Files)) + " " + plural("file", len(landing.Files))
	// A POLICY KEEP IS NOT A FAILURE AND MUST NOT BORROW ONE'S WORDS. The
	// landing did everything it was asked to; aforge declined to write a
	// protected checkout, which is the whole point of the road. The note is
	// already the complete sentence — it names the branch, the reason, and the
	// one command that takes the work — so it is said on its own.
	if landing.KeptByPolicy() && landing.Note != "" {
		return landing.Note
	}
	if landing.Kept() {
		return "could not put it all into " + landing.Name + " · " + landing.Note
	}
	line := landing.Name + " now has the changes · " + count
	if landing.Note != "" {
		line += " · " + landing.Note
	}
	return line
}
