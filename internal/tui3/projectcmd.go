package tui3

// /project — WHICH FOLDER THE NEXT CONVERSATION OPENS IN, AND NOTHING ELSE.
//
// Until 2026-09-22 this was the home half of `/folder`, and the owner's word
// for that was that `/folder` was doing two different jobs: in a conversation
// it hands the conversation a directory to be about, the way `/attach` hands
// it a file; on home the same word pinned the project the NEXT conversation
// would open in. Two acts behind one command, told apart by which screen you
// happened to be standing on.
//
// So the pin is its own command now. `/folder` means one thing everywhere —
// give this conversation a folder — and typing it on home opens a conversation
// first, like every other command about a conversation (homeslash.go's
// [fateNeedsChat]). `/project` is the pin, it lives on home, and a conversation
// answers it by saying so rather than by doing something else.
//
// ITS TWO FORMS ANSWER THE SAME QUESTION FROM THE TWO ENDS. With a path after
// it the answer is already known and the command takes it; bare, the person is
// asking to be shown the disk, and the browser opens the way it always did
// ([app.openProjectPick]).

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// The sentences /project says. Each is quoted in the manual exactly as it is
// spelled here.
const (
	// A TAKEN PATH SAYS NOTHING HERE, and `project · <path>` stood in this spot
	// until 2026-09-23. Home's sentence is the REFUSAL SLOT — it is drawn in
	// place of the keys row and stands until the next keystroke takes it
	// (homephone.go's [app.homeBar]) — so a success reported through it left
	// the owner looking at a home whose keys row was gone, saying a thing the
	// row it was covering already said: `project: <path>` at the right end,
	// from the very next frame. Two sentences about one pin, one of them
	// hiding the other. The browser road never said anything for the same
	// reason (folderact.go's [app.targetFolderConfirm]), so the typed road was
	// also the odd one out. A REFUSAL STILL SPEAKS: it is a fact about a door
	// somebody just tried, and nothing else on the screen carries it.
	//
	// projectNoFolderWord is a path that is not a directory on this machine: a
	// typo, a file, or somewhere that has been moved since. It names what was
	// typed rather than what it resolved to, because the resolved form is not
	// what the person can see to correct.
	projectNoFolderWord = "no folder there · "
	// projectIsHomesWord is /project typed in a conversation. It says which
	// screen the command lives on AND which command does the neighbouring job
	// here, because somebody who typed it in a conversation wanted one of the
	// two and both answers are one line.
	projectIsHomesWord = "/project is home's · it sets the folder the next conversation opens in · /folder gives this conversation one"
)

// runProjectCommand is /project on home. See this file's header.
//
// NOTHING HERE TOUCHES A DISK EXCEPT THE ONE STAT ON THE PATH THAT WAS TYPED,
// and that stat is the whole point of the command: a pin naming a folder that
// is not there would be a destination the next conversation cannot open, found
// out one `enter` later. One stat of one named directory is what [app.attach]
// already pays on the same road, and it is not a walk.
func (a *app) runProjectCommand(rest string) tea.Cmd {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return a.openProjectPick("")
	}
	// OVER A CONNECTION THERE IS NOTHING TRUE TO PIN. The folders this process
	// can stat are the laptop's and the next conversation is on the other
	// machine, which is [folderRemoteWord]'s argument said about the pin
	// rather than about the browser.
	if a.hosted() {
		a.home.say(folderRemoteWord, "")
		return nil
	}
	path := a.resolvePath(rest)
	info, err := os.Stat(path)
	if path == "" || err != nil || !info.IsDir() {
		a.home.say(projectNoFolderWord+rest, "")
		a.touch()
		return nil
	}
	a.noticeEvent(eventProjectSet)
	// A PIN IS A STRING ON THIS WINDOW AND NEVER A ROUND TRIP (folderact.go's
	// [app.targetFolderConfirm] says why that matters): the keys row under the
	// box says the new folder on the very next frame.
	a.target.where = path
	// AND THE ROW IS LEFT ALONE TO SAY IT (the constants above say why).
	a.touch()
	return nil
}

// openProjectPick is a bare /project: the ONE context browser, opened about the
// conversation home is about to start rather than about the one this window is
// holding behind the screen (folderplace.go's [app.openTargetContextPick] has
// the three things that makes different).
func (a *app) openProjectPick(query string) tea.Cmd {
	return a.openTargetContextPick(query, false)
}
